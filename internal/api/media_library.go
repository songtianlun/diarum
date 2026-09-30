package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

// maxMediaBatch caps how many images one trash request may act on.
const maxMediaBatch = 1000

type mediaIDsBody struct {
	IDs []string `json:"ids"`
}

// mediaBatchResult reports a batch action image by image.
type mediaBatchResult struct {
	Done   []string          `json:"done"`
	Failed map[string]string `json:"failed"`
}

func newMediaBatchResult() *mediaBatchResult {
	return &mediaBatchResult{Done: []string{}, Failed: map[string]string{}}
}

func bindMediaIDs(c echo.Context) ([]string, error) {
	var body mediaIDsBody
	if err := c.Bind(&body); err != nil {
		return nil, badRequest("Invalid request body", err)
	}
	ids := make([]string, 0, len(body.IDs))
	seen := make(map[string]bool, len(body.IDs))
	for _, id := range body.IDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, badRequest("No images selected", nil)
	}
	if len(ids) > maxMediaBatch {
		return nil, badRequest("Too many images in one request", nil)
	}
	return ids, nil
}

// RegisterMediaLibraryRoutes serves the media library trash, statistics,
// the unused image scan and the housekeeping settings.
func RegisterMediaLibraryRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, svc *medialib.Service) {
	configService := config.NewConfigService(s)
	group := e.Group("/api/v1/media", authMiddleware)

	group.GET("/stats", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		stats, err := s.MediaStats(user.ID)
		if err != nil {
			return serverError("Failed to load media statistics", err)
		}
		provider, _ := configService.GetString(user.ID, "image_upload.provider")
		if provider = normalizeImageUploadProvider(provider); provider == "" {
			provider = "local"
		}
		return c.JSON(http.StatusOK, map[string]any{
			"provider": provider,
			"current":  stats.ByStorage[provider],
			"stats":    stats,
		})
	})

	group.GET("/settings", func(c echo.Context) error {
		return c.JSON(http.StatusOK, svc.LoadSettings(auth.CurrentUser(c).ID))
	})

	group.PUT("/settings", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		var body medialib.Settings
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.TrashRetentionDays < 0 || body.TrashRetentionDays > medialib.MaxTrashRetentionDays {
			return badRequest("Retention must be between 0 and 3650 days", nil)
		}
		payload := map[string]any{
			medialib.SettingTrashRetentionDays: body.TrashRetentionDays,
			medialib.SettingAutoCleanUnlinked:  body.AutoCleanUnlinked,
		}
		if err := configService.SetBatch(user.ID, payload); err != nil {
			return badRequest("Failed to save media library settings", err)
		}
		recordAudit(c, audit.ActionSettingsUpdate, "media_library", map[string]any{
			"keys":                 settingKeys(payload),
			"trash_retention_days": body.TrashRetentionDays,
			"auto_clean_unlinked":  body.AutoCleanUnlinked,
		})
		return c.JSON(http.StatusOK, map[string]any{"success": true, "settings": svc.LoadSettings(user.ID)})
	})

	group.GET("/trash", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		page := parsePositiveInt(c.QueryParam("page"), 1)
		perPage := parsePositiveInt(c.QueryParam("perPage"), 50)
		items, total, err := s.ListTrash(user.ID, page, perPage)
		if err != nil {
			return serverError("Failed to fetch trash", err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"page":          page,
			"perPage":       perPage,
			"totalItems":    total,
			"totalPages":    store.TotalPages(total, perPage),
			"items":         items,
			"retentionDays": svc.LoadSettings(user.ID).TrashRetentionDays,
		})
	})

	group.GET("/trash/ids", func(c echo.Context) error {
		ids, err := s.TrashedMediaIDs(auth.CurrentUser(c).ID)
		if err != nil {
			return serverError("Failed to fetch trash", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"ids": ids, "maxBatch": maxMediaBatch})
	})

	group.POST("/trash/restore", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		ids, err := bindMediaIDs(c)
		if err != nil {
			return err
		}
		result := newMediaBatchResult()
		for _, id := range ids {
			media, err := s.RestoreMedia(id, user.ID)
			if err != nil {
				result.Failed[id] = failureMessage(err)
				continue
			}
			result.Done = append(result.Done, id)
			recordAudit(c, audit.ActionMediaRestore, media.Name, medialib.Detail(media, store.MediaTriggerManual, ""))
		}
		return c.JSON(http.StatusOK, result)
	})

	purge := func(c echo.Context, ids []string, reason string) error {
		user := auth.CurrentUser(c)
		result := newMediaBatchResult()
		for _, id := range ids {
			media, err := s.PurgeMedia(id, user.ID)
			if err != nil {
				result.Failed[id] = failureMessage(err)
				if media != nil {
					detail := medialib.Detail(media, store.MediaTriggerManual, reason)
					detail["error"] = err.Error()
					recordAudit(c, audit.ActionMediaPurge, media.Name, detail)
				}
				continue
			}
			result.Done = append(result.Done, id)
			recordAudit(c, audit.ActionMediaPurge, media.Name, medialib.Detail(media, store.MediaTriggerManual, reason))
		}
		return c.JSON(http.StatusOK, result)
	}

	group.POST("/trash/purge", func(c echo.Context) error {
		ids, err := bindMediaIDs(c)
		if err != nil {
			return err
		}
		return purge(c, ids, store.MediaReasonUser)
	})

	group.POST("/trash/empty", func(c echo.Context) error {
		trashed, err := s.TrashedMedia(auth.CurrentUser(c).ID, time.Time{})
		if err != nil {
			return serverError("Failed to read trash", err)
		}
		ids := make([]string, 0, len(trashed))
		for _, media := range trashed {
			ids = append(ids, media.ID)
		}
		return purge(c, ids, store.MediaReasonEmptyTrash)
	})

	// Images no entry references any more. Images uploaded in the last few
	// minutes are left out: their entry may not have been saved yet.
	group.GET("/unlinked", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		items, err := s.UnlinkedMedia(user.ID, time.Now().Add(-medialib.ManualScanGrace))
		if err != nil {
			return serverError("Failed to scan media", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"items": items, "total": len(items)})
	})

	// Moves the given unused images to the trash. Each one is checked again,
	// so an image an entry started using since the scan is kept.
	group.POST("/unlinked/clean", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		ids, err := bindMediaIDs(c)
		if err != nil {
			return err
		}
		unlinked, err := s.UnlinkedMedia(user.ID, time.Now().Add(-medialib.ManualScanGrace))
		if err != nil {
			return serverError("Failed to scan media", err)
		}
		stillUnlinked := make(map[string]bool, len(unlinked))
		for _, media := range unlinked {
			stillUnlinked[media.ID] = true
		}
		result := newMediaBatchResult()
		for _, id := range ids {
			if !stillUnlinked[id] {
				result.Failed[id] = "in use or not found"
				continue
			}
			media, err := s.TrashMedia(id, user.ID, store.TrashInfo{By: user.Username, Reason: store.MediaReasonUnlinked, Trigger: store.MediaTriggerManual})
			if err != nil {
				result.Failed[id] = failureMessage(err)
				continue
			}
			result.Done = append(result.Done, id)
			recordAudit(c, audit.ActionMediaTrash, media.Name, medialib.Detail(media, store.MediaTriggerManual, store.MediaReasonUnlinked))
		}
		return c.JSON(http.StatusOK, result)
	})
}

func failureMessage(err error) string {
	switch {
	case store.IsNoRows(err):
		return "not found"
	case errors.Is(err, store.ErrMediaBusy):
		return "changed meanwhile"
	default:
		return err.Error()
	}
}
