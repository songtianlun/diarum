package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	backupTestTimeout = 30 * time.Second
	backupListTimeout = 2 * time.Minute
	previewRunCount   = 5
)

type backupSettingsResponse struct {
	backup.Settings
	NextRun        string `json:"next_run,omitempty"`
	ServerTimezone string `json:"server_timezone"`
}

type backupRoutes struct {
	config    *config.ConfigService
	service   *backup.Service
	scheduler *backup.Scheduler
}

// RegisterBackupRoutes registers the automatic backup endpoints.
func RegisterBackupRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, service *backup.Service, scheduler *backup.Scheduler) {
	r := &backupRoutes{config: config.NewConfigService(s), service: service, scheduler: scheduler}
	group := e.Group("/api/v1/backup", authMiddleware)
	group.GET("/settings", r.getSettings)
	group.PUT("/settings", r.putSettings)
	group.POST("/test", r.test)
	group.POST("/schedule/preview", r.previewSchedule)
	group.GET("/status", r.status)
	group.GET("/backups", r.list)
	group.POST("/backups", r.start)
	group.GET("/backups/:id/log", r.log)
	group.GET("/backups/:id/download", r.download)
	group.DELETE("/backups/:id", r.delete)
	group.POST("/backups/:id/restore", r.restore)
}

func (r *backupRoutes) settingsResponse(userID string, settings backup.Settings) backupSettingsResponse {
	resp := backupSettingsResponse{Settings: settings, ServerTimezone: time.Local.String()}
	if next, ok := r.scheduler.NextRun(userID); ok {
		resp.NextRun = next.Format(time.RFC3339)
	}
	return resp
}

func (r *backupRoutes) getSettings(c echo.Context) error {
	userID := auth.CurrentUser(c).ID
	settings, err := backup.LoadSettings(r.config, userID)
	if err != nil {
		return serverError("Failed to load backup settings", err)
	}
	return c.JSON(http.StatusOK, r.settingsResponse(userID, settings))
}

func (r *backupRoutes) putSettings(c echo.Context) error {
	userID := auth.CurrentUser(c).ID
	var body backup.Settings
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	body.S3 = body.S3.Normalized()
	body.Schedule = strings.Join(strings.Fields(body.Schedule), " ")
	body.Timezone = strings.TrimSpace(body.Timezone)
	if body.Schedule == "" {
		body.Schedule = backup.DefaultSchedule
	}
	if body.Keep < backup.MinKeep || body.Keep > backup.MaxKeep {
		return badRequest("Backups to keep must be between 1 and 100", nil)
	}
	if _, err := backup.ParseSchedule(body.Schedule, body.Timezone); err != nil {
		return badRequest("Invalid schedule", err)
	}

	if body.Enabled {
		if err := requireS3Fields(body.S3.Bucket, body.S3.Region, body.S3.AccessKey, body.S3.Secret); err != nil {
			return err
		}
		// Prove the destination works whenever it is switched on or changed,
		// rather than finding out at the first scheduled run.
		current, err := backup.LoadSettings(r.config, userID)
		if err != nil {
			return serverError("Failed to load backup settings", err)
		}
		if !current.Enabled || current.S3 != body.S3 {
			ctx, cancel := context.WithTimeout(c.Request().Context(), backupTestTimeout)
			defer cancel()
			if err := r.service.Test(ctx, userID, body.S3); err != nil {
				return badRequest("S3 check failed", err)
			}
		}
	}

	if err := r.config.SetBatch(userID, body.Values()); err != nil {
		return badRequest("Failed to save backup settings", err)
	}
	r.scheduler.Reload(userID)
	saved, err := backup.LoadSettings(r.config, userID)
	if err != nil {
		return serverError("Failed to reload backup settings", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "settings": r.settingsResponse(userID, saved)})
}

func (r *backupRoutes) test(c echo.Context) error {
	userID := auth.CurrentUser(c).ID
	var body backup.S3Config
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), backupTestTimeout)
	defer cancel()
	if err := r.service.Test(ctx, userID, body); err != nil {
		return c.JSON(http.StatusOK, map[string]any{"success": false, "message": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Connection works: write, read, list and delete succeeded"})
}

func (r *backupRoutes) previewSchedule(c echo.Context) error {
	var body struct {
		Schedule string `json:"schedule"`
		Timezone string `json:"timezone"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	schedule, err := backup.ParseSchedule(body.Schedule, body.Timezone)
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{"valid": false, "error": err.Error(), "next_runs": []string{}})
	}
	runs := make([]string, 0, previewRunCount)
	for _, run := range schedule.NextRuns(time.Now(), previewRunCount) {
		runs = append(runs, run.Format(time.RFC3339))
	}
	return c.JSON(http.StatusOK, map[string]any{"valid": true, "timezone": schedule.Loc.String(), "next_runs": runs})
}

func (r *backupRoutes) status(c echo.Context) error {
	userID := auth.CurrentUser(c).ID
	resp := map[string]any{"job": r.service.Status(userID)}
	if next, ok := r.scheduler.NextRun(userID); ok {
		resp["next_run"] = next.Format(time.RFC3339)
	}
	return c.JSON(http.StatusOK, resp)
}

func (r *backupRoutes) list(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), backupListTimeout)
	defer cancel()
	entries, err := r.service.List(ctx, auth.CurrentUser(c).ID)
	if err != nil {
		return backupError("Failed to list backups", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"backups": entries})
}

func (r *backupRoutes) start(c echo.Context) error {
	job, err := r.service.StartBackup(auth.CurrentUser(c).ID, backup.TriggerManual)
	if err != nil {
		return backupError("Failed to start backup", err)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"job": job})
}

func (r *backupRoutes) log(c echo.Context) error {
	log, err := r.service.Log(c.Request().Context(), auth.CurrentUser(c).ID, c.PathParam("id"))
	if err != nil {
		return backupError("Failed to read backup log", err)
	}
	return c.JSON(http.StatusOK, log)
}

func (r *backupRoutes) download(c echo.Context) error {
	id := c.PathParam("id")
	rc, err := r.service.Open(c.Request().Context(), auth.CurrentUser(c).ID, id)
	if err != nil {
		return backupError("Failed to download backup", err)
	}
	defer rc.Close()
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=diarum-backup-"+id+".zip")
	c.Response().WriteHeader(http.StatusOK)
	_, _ = io.Copy(c.Response(), rc)
	return nil
}

func (r *backupRoutes) delete(c echo.Context) error {
	if err := r.service.Delete(c.Request().Context(), auth.CurrentUser(c).ID, c.PathParam("id")); err != nil {
		return backupError("Failed to delete backup", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true})
}

func (r *backupRoutes) restore(c echo.Context) error {
	job, err := r.service.StartRestore(auth.CurrentUser(c).ID, c.PathParam("id"))
	if err != nil {
		return backupError("Failed to start restore", err)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"job": job})
}

// backupError maps service errors to HTTP statuses. Anything unexpected is
// most likely the storage provider, hence Bad Gateway.
func backupError(message string, err error) error {
	switch {
	case errors.Is(err, backup.ErrBusy):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, backup.ErrNotFound):
		return notFound(err.Error())
	case errors.Is(err, backup.ErrDisabled), errors.Is(err, backup.ErrIncomplete), errors.Is(err, backup.ErrInvalidID):
		return badRequest(err.Error(), nil)
	default:
		return echo.NewHTTPError(http.StatusBadGateway, message+": "+err.Error())
	}
}
