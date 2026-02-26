package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/songtianlun/diarum/internal/api"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
	_ "github.com/songtianlun/diarum/internal/migrations"
	"github.com/songtianlun/diarum/internal/static"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/spf13/cobra"
)

// getDataDir returns the data directory path
// Priority: command line flag > environment variable > default value
func getDataDir() string {
	// Check new environment variable first
	if dataDir := os.Getenv("DIARUM_DATA_PATH"); dataDir != "" {
		return dataDir
	}
	// Fallback to legacy environment variable for backwards compatibility
	if dataDir := os.Getenv("DIARIA_DATA_PATH"); dataDir != "" {
		return dataDir
	}
	// Default value
	return "./diarum_data"
}

// serveSPA serves the SPA with fallback to index.html for client-side routing
func serveSPA(c echo.Context, fsys fs.FS) error {
	path := c.Request().URL.Path

	// Skip API routes and PocketBase admin routes
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/_/") {
		return echo.ErrNotFound
	}

	// Clean the path
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}

	// Remove leading slash for fs.FS
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	// Try to serve the requested file
	file, err := fsys.Open(path)
	if err == nil {
		defer file.Close()

		// Get file info to check if it's a directory
		stat, err := file.Stat()
		if err != nil {
			return err
		}

		// If it's a directory, try index.html
		if stat.IsDir() {
			indexPath := filepath.Join(path, "index.html")
			file.Close()
			file, err = fsys.Open(indexPath)
			if err == nil {
				defer file.Close()
				stat, err = file.Stat()
				if err != nil {
					return err
				}
			}
		}

		// Serve the file
		if err == nil {
			http.ServeContent(c.Response(), c.Request(), stat.Name(), stat.ModTime(), file.(io.ReadSeeker))
			return nil
		}
	}

	// If file not found, serve index.html for SPA routing
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
	// Get data directory from environment or default
	defaultDataDir := getDataDir()

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: defaultDataDir,
	})

	// Add data-dir flag to serve command
	var dataDirFlag string
	app.RootCmd.PersistentFlags().StringVar(
		&dataDirFlag,
		"data-dir",
		defaultDataDir,
		"the directory to store application data",
	)

	// Register migrations
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: true, // Auto-run migrations on startup
	})

	// Add version command
	app.RootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s version %s\n", Name, Version)
		},
	})

	// Register custom routes and serve embedded frontend
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		// Print data directory information
		absDataDir, err := filepath.Abs(app.DataDir())
		if err != nil {
			log.Printf("Data directory: %s", app.DataDir())
		} else {
			log.Printf("Data directory: %s", absDataDir)
		}

		// Initialize vector database and embedding service
		vectorDB, err := embedding.NewVectorDB(app.DataDir())
		if err != nil {
			log.Printf("Warning: Failed to initialize vector database: %v", err)
		}

		var embeddingService *embedding.EmbeddingService
		if vectorDB != nil {
			embeddingService = embedding.NewEmbeddingService(app, vectorDB)
		}

		// Initialize config service for checking AI settings
		configService := config.NewConfigService(app)

		// Add diary create hook for auto vector build
		app.OnRecordAfterCreateRequest("diaries").Add(func(e *core.RecordCreateEvent) error {
			userID := e.Record.GetString("owner")
			if userID == "" {
				return nil
			}

			// Check if AI is enabled for this user
			enabled, _ := configService.GetBool(userID, "ai.enabled")
			if !enabled || embeddingService == nil {
				return nil
			}

			// Run incremental vector build in background
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()

				logger.Info("[AutoVectorBuild] triggered by diary create for user: %s", userID)
				result, err := embeddingService.BuildIncrementalVectors(ctx, userID)
				if err != nil {
					logger.Error("[AutoVectorBuild] failed for user %s: %v", userID, err)
					return
				}
				logger.Info("[AutoVectorBuild] completed for user %s: %d built, %d failed", userID, result.Success, result.Failed)
			}()

			return nil
		})

		// Add diary update hook for auto vector build
		app.OnRecordAfterUpdateRequest("diaries").Add(func(e *core.RecordUpdateEvent) error {
			userID := e.Record.GetString("owner")
			if userID == "" {
				return nil
			}

			// Check if AI is enabled for this user
			enabled, _ := configService.GetBool(userID, "ai.enabled")
			if !enabled || embeddingService == nil {
				return nil
			}

			// Run incremental vector build in background
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()

				logger.Info("[AutoVectorBuild] triggered by diary update for user: %s", userID)
				result, err := embeddingService.BuildIncrementalVectors(ctx, userID)
				if err != nil {
					logger.Error("[AutoVectorBuild] failed for user %s: %v", userID, err)
					return
				}
				logger.Info("[AutoVectorBuild] completed for user %s: %d built, %d failed", userID, result.Success, result.Failed)
			}()

			return nil
		})

		// Register API routes
		api.RegisterDiaryRoutes(app, e)
		api.RegisterSettingsRoutes(app, e)
		api.RegisterAIRoutes(app, e, embeddingService)
		api.RegisterExportImportRoutes(app, e, embeddingService)
		api.RegisterCheveretoRoutes(app, e)
		api.RegisterPublicRoutes(app, e)
		api.RegisterVersionRoutes(e, Version, Name)

		// Serve embedded frontend static files with SPA fallback
		staticFS, err := static.GetFS()
		if err != nil {
			log.Printf("Warning: Failed to get embedded static files: %v", err)
		} else {
			// Add SPA handler for all routes (with lowest priority)
			e.Router.GET("/*", func(c echo.Context) error {
				return serveSPA(c, staticFS)
			})
		}

		return nil
	})

	// Start the application
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
