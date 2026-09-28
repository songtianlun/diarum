package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	adminS3Timeout   = 30 * time.Second
	adminPullTimeout = 10 * time.Minute
	adminUsersMax    = 200
)

// RequireAdmin lets only admins through. It must run after the auth
// middleware.
func RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		user := auth.CurrentUser(c)
		if user == nil {
			return unauthorized("The request requires valid authorization token.")
		}
		if user.Role != store.RoleAdmin {
			return forbidden("Administrator access required")
		}
		return next(c)
	}
}

type adminRoutes struct {
	store   *store.Store
	audit   *audit.Logger
	version string
	started time.Time
}

// RegisterAdminRoutes serves the admin console: system statistics, the user
// list and the system audit log.
func RegisterAdminRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, l *audit.Logger, version string) {
	r := &adminRoutes{store: s, audit: l, version: version, started: time.Now()}
	group := e.Group("/api/v1/admin", authMiddleware, RequireAdmin)
	group.GET("/overview", r.overview)
	group.GET("/users", r.users)
	group.PUT("/users/:id/role", r.setRole)
	group.GET("/audit/settings", r.auditSettings)
	group.PUT("/audit/settings", r.putAuditSettings)
	group.POST("/audit/settings/test", r.testArchive)
	group.GET("/audit/logs", r.auditLogs)
	group.GET("/audit/files", r.auditFiles)
	group.GET("/audit/files/:date", r.downloadFile)
	group.GET("/audit/archives", r.archives)
	group.POST("/audit/archives/run", r.archiveNow)
	group.POST("/audit/cleanup", r.cleanupNow)
	group.POST("/audit/archives/:date/pull", r.pull)
	group.DELETE("/audit/pulled/:date", r.removePulled)
}

func (r *adminRoutes) overview(c echo.Context) error {
	stats, err := r.store.SystemStats(time.Now())
	if err != nil {
		return serverError("Failed to load statistics", err)
	}
	files, _ := r.audit.Files(true)
	var auditBytes int64
	for _, file := range files {
		auditBytes += file.Size
	}
	return c.JSON(http.StatusOK, map[string]any{
		"stats": stats,
		"system": map[string]any{
			"version":         r.version,
			"go_version":      runtime.Version(),
			"started":         r.started.UTC().Format(time.RFC3339),
			"uptime_seconds":  int64(time.Since(r.started).Seconds()),
			"server_timezone": time.Local.String(),
			"goroutines":      runtime.NumGoroutine(),
		},
		"audit": map[string]any{
			"enabled": r.audit != nil,
			"files":   len(files),
			"bytes":   auditBytes,
			"writer":  r.audit.WriterStats(),
		},
	})
}

func (r *adminRoutes) users(c echo.Context) error {
	opts := store.UserListOptions{
		Query: c.QueryParam("q"),
		Role:  c.QueryParam("role"),
		Sort:  c.QueryParam("sort"),
		Limit: 50,
	}
	if opts.Role != "" && !store.ValidRole(opts.Role) {
		return badRequest("invalid role", nil)
	}
	if raw := c.QueryParam("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return badRequest("invalid limit", nil)
		}
		opts.Limit = min(limit, adminUsersMax)
	}
	if raw := c.QueryParam("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return badRequest("invalid offset", nil)
		}
		opts.Offset = offset
	}
	users, total, err := r.store.ListUsers(opts)
	if err != nil {
		return serverError("Failed to list users", err)
	}
	admins, err := r.store.CountAdmins()
	if err != nil {
		return serverError("Failed to list users", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"users": users, "total": total, "admins": admins})
}

func (r *adminRoutes) setRole(c echo.Context) error {
	var body struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	if !store.ValidRole(body.Role) {
		return badRequest(store.ErrInvalidRole.Error(), nil)
	}
	me := auth.CurrentUser(c)
	target, err := r.store.GetUserByID(c.PathParam("id"))
	if err != nil {
		return notFound("User not found")
	}
	if target.Role == body.Role {
		return c.JSON(http.StatusOK, target)
	}
	if target.ID == me.ID && body.Role != store.RoleAdmin {
		return badRequest("You cannot remove your own admin role; ask another admin or use the command line", nil)
	}
	if err := r.store.SetUserRole(target.ID, body.Role); err != nil {
		return serverError("Failed to change role", err)
	}
	recordAudit(c, audit.ActionAdminRole, target.Username, map[string]any{"user_id": target.ID, "from": target.Role, "to": body.Role})
	updated, err := r.store.GetUserByID(target.ID)
	if err != nil {
		return serverError("Failed to load user", err)
	}
	return c.JSON(http.StatusOK, updated)
}

// ----- Audit settings -----

type auditSettingsResponse struct {
	audit.Settings
	SecretSet      bool        `json:"secret_set"`
	State          audit.State `json:"state"`
	ServerTimezone string      `json:"server_timezone"`
	Enabled        bool        `json:"enabled"`
	Limits         any         `json:"limits"`
}

func (r *adminRoutes) settingsResponse() auditSettingsResponse {
	settings := r.audit.Settings()
	resp := auditSettingsResponse{
		Settings:       settings,
		SecretSet:      settings.Archive.S3.Secret != "",
		State:          r.audit.ArchiveState(),
		ServerTimezone: time.Local.String(),
		Enabled:        r.audit != nil,
		Limits: map[string]int{
			"retention_min":         audit.MinRetentionDays,
			"retention_max":         audit.MaxRetentionDays,
			"retention_default":     audit.DefaultRetentionDays,
			"archive_retention_min": audit.MinArchiveRetentionDays,
			"archive_retention_max": audit.MaxArchiveRetentionDays,
			"archive_retention_def": audit.DefaultArchiveRetentionDays,
		},
	}
	// The secret never leaves the server.
	resp.Archive.S3.Secret = ""
	return resp
}

func (r *adminRoutes) auditSettings(c echo.Context) error {
	return c.JSON(http.StatusOK, r.settingsResponse())
}

func (r *adminRoutes) putAuditSettings(c echo.Context) error {
	if r.audit == nil {
		return serverError("Audit logging is disabled on this server", nil)
	}
	var body audit.Settings
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	previous := r.audit.Settings()
	next, err := r.audit.UpdateSettings(body)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	recordAudit(c, audit.ActionAdminAudit, "", map[string]any{
		"retention_days":         [2]int{previous.RetentionDays, next.RetentionDays},
		"archive_enabled":        [2]bool{previous.Archive.Enabled, next.Archive.Enabled},
		"archive_retention_days": [2]int{previous.Archive.RetentionDays, next.Archive.RetentionDays},
		"bucket":                 next.Archive.S3.Bucket,
		"secret_changed":         body.Archive.S3.Secret != "",
	})
	return c.JSON(http.StatusOK, r.settingsResponse())
}

func (r *adminRoutes) testArchive(c echo.Context) error {
	var body backup.S3Config
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
	defer cancel()
	if err := r.audit.TestArchive(ctx, body); err != nil {
		return badRequest("Connection test failed: "+err.Error(), nil)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// ----- Audit logs -----

func (r *adminRoutes) auditLogs(c echo.Context) error {
	q := audit.Query{
		UserID:        strings.TrimSpace(c.QueryParam("user")),
		Status:        c.QueryParam("status"),
		Method:        strings.ToUpper(strings.TrimSpace(c.QueryParam("method"))),
		Source:        strings.TrimSpace(c.QueryParam("source")),
		IP:            strings.TrimSpace(c.QueryParam("ip")),
		Text:          c.QueryParam("q"),
		IncludePulled: c.QueryParam("pulled") == "1" || c.QueryParam("pulled") == "true",
		Stats:         c.QueryParam("stats") == "1" || c.QueryParam("stats") == "true",
	}
	if raw := strings.TrimSpace(c.QueryParam("action")); raw != "" {
		q.Actions = strings.Split(raw, ",")
	}
	switch q.Status {
	case "", "2xx", "3xx", "4xx", "5xx", "error":
	default:
		return badRequest("invalid status", nil)
	}
	var err error
	for _, p := range []struct {
		name string
		dest *int
	}{{"limit", &q.Limit}, {"offset", &q.Offset}} {
		raw := c.QueryParam(p.name)
		if raw == "" {
			continue
		}
		value, convErr := strconv.Atoi(raw)
		if convErr != nil || value < 0 {
			return badRequest("invalid "+p.name, nil)
		}
		*p.dest = value
		if p.name == "limit" && value == 0 {
			// Statistics only.
			*p.dest = -1
		}
	}
	if q.Start, err = parseAuditTime(c.QueryParam("start")); err != nil {
		return badRequest("invalid start", err)
	}
	if q.End, err = parseAuditTime(c.QueryParam("end")); err != nil {
		return badRequest("invalid end", err)
	}
	if !q.Start.IsZero() && !q.End.IsZero() && q.Start.After(q.End) {
		q.Start, q.End = q.End, q.Start
	}
	if tz := c.QueryParam("tz"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			q.Location = loc
		}
	}
	result, err := r.audit.Search(q)
	if err != nil {
		return serverError("Failed to read audit logs", err)
	}
	r.fillUsernames(result)
	return c.JSON(http.StatusOK, result)
}

// fillUsernames names users that were only known by ID when recorded.
func (r *adminRoutes) fillUsernames(result *audit.Result) {
	missing := false
	for _, entry := range result.Entries {
		if entry.UserID != "" && entry.User == "" {
			missing = true
			break
		}
	}
	if result.Stats != nil {
		for _, user := range result.Stats.TopUsers {
			if user.Label == "" {
				missing = true
			}
		}
	}
	if !missing {
		return
	}
	names, err := r.store.UserNames()
	if err != nil {
		return
	}
	for i := range result.Entries {
		if entry := &result.Entries[i]; entry.UserID != "" && entry.User == "" {
			entry.User = names[entry.UserID]
		}
	}
	if result.Stats != nil {
		for i := range result.Stats.TopUsers {
			if user := &result.Stats.TopUsers[i]; user.Label == "" {
				user.Label = names[user.Key]
			}
		}
	}
}

func parseAuditTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func (r *adminRoutes) auditFiles(c echo.Context) error {
	files, err := r.audit.Files(true)
	if err != nil {
		return serverError("Failed to list audit logs", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"files": files, "state": r.audit.ArchiveState()})
}

func (r *adminRoutes) downloadFile(c echo.Context) error {
	date := c.PathParam("date")
	pulled := c.QueryParam("pulled") == "1" || c.QueryParam("pulled") == "true"
	f, err := r.audit.OpenFile(date, pulled)
	if errors.Is(err, audit.ErrInvalidDate) {
		return badRequest("invalid date", nil)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return notFound("Audit log not found")
		}
		return serverError("Failed to open audit log", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return serverError("Failed to read audit log", err)
	}
	name := "diarum-system-audit-" + date + ".log"
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+name+`"`)
	c.Response().Header().Set(echo.HeaderContentType, "application/x-ndjson; charset=utf-8")
	http.ServeContent(c.Response(), c.Request(), name, stat.ModTime(), f)
	return nil
}

func (r *adminRoutes) archives(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
	defer cancel()
	archives, err := r.audit.ListArchives(ctx)
	if errors.Is(err, audit.ErrArchiveDisabled) {
		return c.JSON(http.StatusOK, map[string]any{"enabled": false, "archives": []audit.ArchiveInfo{}})
	}
	if err != nil {
		return serverError("Failed to list archives", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": true, "archives": archives})
}

func (r *adminRoutes) archiveNow(c echo.Context) error {
	report, err := r.audit.ArchiveNow("manual")
	if errors.Is(err, audit.ErrArchiveDisabled) {
		return badRequest(err.Error(), nil)
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"message": err.Error(), "report": report})
	}
	return c.JSON(http.StatusOK, map[string]any{"report": report})
}

func (r *adminRoutes) cleanupNow(c echo.Context) error {
	if r.audit == nil {
		return serverError("Audit logging is disabled on this server", nil)
	}
	report := r.audit.Cleanup("manual")
	status := http.StatusOK
	if report.Error != "" {
		status = http.StatusBadGateway
	}
	return c.JSON(status, map[string]any{"report": report, "message": report.Error})
}

func (r *adminRoutes) pull(c echo.Context) error {
	date := c.PathParam("date")
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminPullTimeout)
	defer cancel()
	file, err := r.audit.Pull(ctx, date)
	switch {
	case errors.Is(err, audit.ErrInvalidDate):
		return badRequest("invalid date", nil)
	case errors.Is(err, audit.ErrArchiveDisabled), errors.Is(err, audit.ErrAlreadyLocal):
		return badRequest(err.Error(), nil)
	case errors.Is(err, audit.ErrArchiveNotFound):
		return notFound(err.Error())
	case err != nil:
		return serverError("Failed to pull archive", err)
	}
	recordAudit(c, audit.ActionAdminPull, date, map[string]any{"size": file.Size})
	return c.JSON(http.StatusOK, map[string]any{"file": file, "state": r.audit.ArchiveState()})
}

func (r *adminRoutes) removePulled(c echo.Context) error {
	err := r.audit.RemovePulled(c.PathParam("date"))
	switch {
	case errors.Is(err, audit.ErrInvalidDate):
		return badRequest("invalid date", nil)
	case errors.Is(err, os.ErrNotExist):
		return notFound("Pulled log not found")
	case err != nil:
		return serverError("Failed to remove pulled log", err)
	}
	return c.NoContent(http.StatusNoContent)
}
