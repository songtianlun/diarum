package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/archive"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/store"
)

const maxImportSize = 200 << 20

type ExportRequest struct {
	DateRange            string `json:"date_range"`
	StartDate            string `json:"start_date,omitempty"`
	EndDate              string `json:"end_date,omitempty"`
	IncludeDiaries       bool   `json:"include_diaries"`
	IncludeMedia         bool   `json:"include_media"`
	IncludeConversations bool   `json:"include_conversations"`
}

type (
	exportData         = archive.Data
	exportDiary        = archive.Diary
	exportMedia        = archive.Media
	exportConversation = archive.Conversation
	exportMessage      = archive.Message
	exportStats        = archive.ExportStats
)

func RegisterExportImportRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, embeddingService *embedding.EmbeddingService) {
	group := e.Group("/api/v1", authMiddleware)
	group.POST("/export", func(c echo.Context) error { return handleExport(c, s) })
	group.POST("/import", func(c echo.Context) error { return handleImport(c, s, embeddingService) })
}

func handleExport(c echo.Context, s *store.Store) error {
	userID := auth.CurrentUser(c).ID
	var req ExportRequest
	if err := c.Bind(&req); err != nil {
		req = ExportRequest{DateRange: "3m", IncludeDiaries: true, IncludeMedia: true, IncludeConversations: true}
	}
	if req.DateRange == "" {
		req.DateRange = "3m"
	}
	startDate, endDate, err := calculateDateRange(req)
	if err != nil {
		return badRequest(err.Error(), nil)
	}

	// Build the archive on disk rather than in memory; the stats header has
	// to be sent before the body, so the archive must be complete first.
	tmp, err := os.CreateTemp("", "diarum-export-*.zip")
	if err != nil {
		return serverError("Failed to create export file", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	stats, err := archive.Export(c.Request().Context(), s, userID, archive.Options{
		Start:                startDate,
		End:                  endDate,
		IncludeDiaries:       req.IncludeDiaries,
		IncludeMedia:         req.IncludeMedia,
		IncludeConversations: req.IncludeConversations,
	}, tmp)
	if err != nil {
		return serverError("Failed to create ZIP", err)
	}
	stats.DateRangeType = req.DateRange
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return serverError("Failed to read export file", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return serverError("Failed to read export file", err)
	}

	statsJSON, _ := json.Marshal(stats)
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=diarum_export.zip")
	c.Response().Header().Set("Content-Length", strconv.FormatInt(size, 10))
	c.Response().Header().Set("X-Export-Stats", string(statsJSON))
	c.Response().Header().Set("Access-Control-Expose-Headers", "X-Export-Stats")
	c.Response().WriteHeader(http.StatusOK)
	_, _ = io.Copy(c.Response(), tmp)
	return nil
}

func handleImport(c echo.Context, s *store.Store, embeddingService *embedding.EmbeddingService) error {
	userID := auth.CurrentUser(c).ID
	fh, err := c.FormFile("file")
	if err != nil {
		return badRequest("Missing upload file", err)
	}
	if fh.Size > maxImportSize {
		return badRequest("File too large (max 200MB)", nil)
	}
	f, err := fh.Open()
	if err != nil {
		return badRequest("Failed to open upload", err)
	}
	defer f.Close()
	zipReader, err := zip.NewReader(f, fh.Size)
	if err != nil {
		return badRequest("Failed to read ZIP file", err)
	}
	stats, err := archive.Import(c.Request().Context(), s, userID, zipReader)
	if err != nil {
		if errors.Is(err, archive.ErrMissingManifest) || errors.Is(err, archive.ErrInvalidManifest) {
			return badRequest(err.Error(), nil)
		}
		return serverError("Import failed", err)
	}
	rebuildVectorsAfterImport(s, embeddingService, userID)
	return c.JSON(http.StatusOK, stats)
}

// rebuildVectorsAfterImport refreshes the AI index in the background when the
// user has AI enabled.
func rebuildVectorsAfterImport(s *store.Store, embeddingService *embedding.EmbeddingService, userID string) {
	if embeddingService == nil {
		return
	}
	if enabled, _ := config.NewConfigService(s).GetBool(userID, "ai.enabled"); !enabled {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _ = embeddingService.BuildIncrementalVectors(ctx, userID)
	}()
}

func isValidZipPath(name string) bool {
	return archive.ValidPath(name)
}

func generateMarkdown(d exportDiary) string {
	return archive.Markdown(d)
}

func calculateDateRange(req ExportRequest) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	endDate := now
	switch req.DateRange {
	case "1m":
		return now.AddDate(0, -1, 0), endDate, nil
	case "3m":
		return now.AddDate(0, -3, 0), endDate, nil
	case "6m":
		return now.AddDate(0, -6, 0), endDate, nil
	case "1y":
		return now.AddDate(-1, 0, 0), endDate, nil
	case "all":
		return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), endDate, nil
	case "custom":
		if req.StartDate == "" || req.EndDate == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date and end_date are required for custom date range")
		}
		start, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date format, expected YYYY-MM-DD")
		}
		end, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date format, expected YYYY-MM-DD")
		}
		if start.After(end) {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date cannot be after end_date")
		}
		return start, end.Add(24*time.Hour - time.Second), nil
	default:
		return now.AddDate(0, -3, 0), endDate, nil
	}
}
