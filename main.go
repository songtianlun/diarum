package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/songtianlun/diarum/internal/api"
	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/static"
	"github.com/songtianlun/diarum/internal/store"
)

var startServer = func(e *echo.Echo, addr string) error {
	return e.Start(addr)
}

func getDataDir() string {
	if dataDir := os.Getenv("DIARUM_DATA_PATH"); dataDir != "" {
		return dataDir
	}
	if dataDir := os.Getenv("DIARIA_DATA_PATH"); dataDir != "" {
		return dataDir
	}
	return "./diarum_data"
}

func serveSPA(c echo.Context, fsys fs.FS) error {
	path := c.Request().URL.Path
	if strings.HasPrefix(path, "/api/") {
		return echo.ErrNotFound
	}
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}
	path = strings.TrimPrefix(path, "/")
	file, err := fsys.Open(path)
	if err == nil {
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		if stat.IsDir() {
			file.Close()
			file, err = fsys.Open(filepath.Join(path, "index.html"))
			if err == nil {
				defer file.Close()
				stat, err = file.Stat()
				if err != nil {
					return err
				}
			}
		}
		if err == nil {
			http.ServeContent(c.Response(), c.Request(), stat.Name(), stat.ModTime(), file.(io.ReadSeeker))
			return nil
		}
	}
	indexFile, err := fsys.Open("index.html")
	if err != nil {
		return echo.ErrNotFound
	}
	defer indexFile.Close()
	stat, err := indexFile.Stat()
	if err != nil {
		return err
	}
	http.ServeContent(c.Response(), c.Request(), "index.html", stat.ModTime(), indexFile.(io.ReadSeeker))
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	if command == "version" {
		_, err := fmt.Fprintf(stdout, "%s version %s\n", Name, Version)
		return err
	}
	if command != "serve" {
		return fmt.Errorf("unknown command: %s", command)
	}

	serveFlags := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir := serveFlags.String("data-dir", getDataDir(), "the directory to store application data")
	httpAddr := serveFlags.String("http", ":8090", "HTTP listen address")
	if err := serveFlags.Parse(args); err != nil {
		return err
	}

	appStore, err := store.Open(*dataDir)
	if err != nil {
		return err
	}
	defer appStore.Close()

	absDataDir, err := filepath.Abs(appStore.DataDir)
	if err != nil {
		log.Printf("Data directory: %s", appStore.DataDir)
	} else {
		log.Printf("Data directory: %s", absDataDir)
	}

	vectorDB, err := embedding.NewVectorDB(appStore.DataDir)
	if err != nil {
		log.Printf("Warning: Failed to initialize vector database: %v", err)
	}
	var embeddingService *embedding.EmbeddingService
	if vectorDB != nil {
		embeddingService = embedding.NewEmbeddingService(appStore, vectorDB)
	}

	configService := config.NewConfigService(appStore)
	authService := auth.NewService(appStore)

	// The audit trail is best effort: if its directory cannot be created the
	// app still runs, just without recording.
	auditLog, err := audit.New(appStore.DataDir, audit.Options{Retention: func(userID string) int {
		value, err := appStore.GetSetting(userID, audit.SettingRetentionDays)
		if number, ok := value.(float64); err == nil && ok {
			return int(min(number, audit.MaxRetentionDays))
		}
		return audit.DefaultRetentionDays
	}})
	if err != nil {
		logger.Error("[AUDIT] !!! audit logging disabled: %v", err)
	} else {
		defer auditLog.Close()
		stopSignals := flushAuditOnSignal(auditLog)
		defer stopSignals()
		log.Printf("Audit logs: %s", audit.Dir(appStore.DataDir))
	}

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	if auditLog != nil {
		e.Use(api.AuditMiddleware(auditLog))
	}

	authMiddleware := authService.Middleware
	onDiaryChanged := func(userID string) {
		enabled, _ := configService.GetBool(userID, "ai.enabled")
		if !enabled || embeddingService == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			logger.Info("[AutoVectorBuild] triggered for user: %s", userID)
			result, err := embeddingService.BuildIncrementalVectors(ctx, userID)
			if err != nil {
				logger.Error("[AutoVectorBuild] failed for user %s: %v", userID, err)
				return
			}
			logger.Info("[AutoVectorBuild] completed for user %s: %d built, %d failed", userID, result.Success, result.Failed)
		}()
	}

	api.RegisterAuthRoutes(e, appStore, authService)
	api.RegisterDiaryRoutes(e, appStore, authMiddleware, onDiaryChanged)
	api.RegisterMediaRoutes(e, appStore, authMiddleware)
	api.RegisterImageUploadRoutes(e, appStore, authMiddleware)
	api.RegisterSettingsRoutes(e, appStore, authMiddleware)
	api.RegisterMemosRoutes(e, appStore, authMiddleware, onDiaryChanged)
	api.RegisterAIRoutes(e, appStore, authMiddleware, embeddingService)
	api.RegisterExportImportRoutes(e, appStore, authMiddleware, embeddingService)
	backupService := backup.NewService(appStore, Version)
	backupService.OnRestored = onDiaryChanged
	backupScheduler := backup.NewScheduler(backupService)
	backupScheduler.Start(context.Background())
	api.RegisterBackupRoutes(e, appStore, authMiddleware, backupService, backupScheduler)
	api.RegisterCheveretoRoutes(e, appStore, authMiddleware)
	api.RegisterPublicRoutes(e, appStore)
	api.RegisterMCPRoutes(e, appStore, Version)
	api.RegisterAuditRoutes(e, appStore, authMiddleware, auditLog)
	api.RegisterVersionRoutes(e, Version, Name)
	if logger.GetLevel() <= logger.LevelDebug {
		api.RegisterOpenAPIRoutes(e, Version, Name)
		logger.Info("[Docs] OpenAPI docs enabled in debug mode at /api/docs and /api/v1/docs")
	}

	staticFS, err := static.GetFS()
	if err != nil {
		log.Printf("Warning: Failed to get embedded static files: %v", err)
	} else {
		e.GET("/*", func(c echo.Context) error { return serveSPA(c, staticFS) })
	}

	if err := startServer(e, *httpAddr); err != nil && err != http.ErrServerClosed {
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	return nil
}

// flushAuditOnSignal writes out merged audit entries when the process is
// asked to stop, then lets the signal take its usual effect.
func flushAuditOnSignal(auditLog *audit.Logger) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		if sig := awaitStopSignal(signals, done, auditLog); sig != nil {
			signal.Stop(signals)
			resendSignal(sig)
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// awaitStopSignal blocks until a signal arrives (flushing the audit log and
// returning it) or done is closed (returning nil).
func awaitStopSignal(signals <-chan os.Signal, done <-chan struct{}, auditLog *audit.Logger) os.Signal {
	select {
	case sig := <-signals:
		auditLog.Close()
		return sig
	case <-done:
		return nil
	}
}

var resendSignal = func(sig os.Signal) {
	if process, err := os.FindProcess(os.Getpid()); err == nil && process.Signal(sig) == nil {
		return
	}
	os.Exit(1)
}
