package api

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/systems3"
	"github.com/songtianlun/diarum/internal/visits"
)

type adminS3Routes struct {
	s3      *systems3.Service
	audit   *audit.Logger
	tracker *visits.Tracker
}

// RegisterAdminS3Routes serves the system S3 storage that the audit archive,
// the visit archive and user backups can share.
func RegisterAdminS3Routes(e *echo.Echo, authMiddleware echo.MiddlewareFunc, s3 *systems3.Service, l *audit.Logger, t *visits.Tracker) {
	r := &adminS3Routes{s3: s3, audit: l, tracker: t}
	group := e.Group("/api/v1/admin/s3", authMiddleware, RequireAdmin)
	group.GET("", r.get)
	group.PUT("", r.put)
	group.POST("/test", r.test)
}

type adminS3Response struct {
	systems3.Settings
	SecretSet  bool              `json:"secret_set"`
	Configured bool              `json:"configured"`
	Paths      map[string]string `json:"paths"`
	// UsedBy lists the system features that currently write to it.
	UsedBy []string `json:"used_by"`
}

func (r *adminS3Routes) response() adminS3Response {
	settings := r.s3.Settings()
	resp := adminS3Response{
		Settings:   settings,
		SecretSet:  settings.S3.Secret != "",
		Configured: settings.Configured(),
		Paths: map[string]string{
			"audit":        r.s3.Path(systems3.FolderAudit),
			"visits":       r.s3.Path(systems3.FolderVisits),
			"user_backups": r.s3.Path(systems3.FolderUserBackups),
		},
		UsedBy: r.usedBy(),
	}
	// The secret never leaves the server.
	resp.S3.Secret = ""
	return resp
}

func (r *adminS3Routes) usedBy() []string {
	used := []string{}
	if r.audit.UsesSystemS3() {
		used = append(used, "audit")
	}
	if r.tracker.UsesSystemS3() {
		used = append(used, "visits")
	}
	return used
}

func (r *adminS3Routes) get(c echo.Context) error {
	return c.JSON(http.StatusOK, r.response())
}

func (r *adminS3Routes) put(c echo.Context) error {
	var body systems3.Settings
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	previous := r.s3.Settings()
	removing := body.S3.Normalized().Bucket == ""
	// Archives that write here would stop archiving, and their local days
	// would then be cleaned up without a copy: switch them away first.
	if removing && previous.Configured() {
		if used := r.usedBy(); len(used) > 0 {
			return badRequest("The system S3 is still used by the "+joinUses(used)+" archive; switch it to a custom S3 or turn it off first", nil)
		}
	}
	// Prove a new or changed connection works before anything writes to it.
	if !removing {
		probe := body
		if probe.S3.Secret == "" {
			probe.S3.Secret = previous.S3.Secret
		}
		candidate := probe.S3.Normalized()
		if candidate.Prefix == "" {
			candidate.Prefix = systems3.DefaultPrefix
		}
		if !previous.Configured() || candidate != previous.S3 {
			ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
			defer cancel()
			if err := r.s3.Test(ctx, body); err != nil {
				return badRequest("S3 check failed: "+err.Error(), nil)
			}
		}
	}
	next, err := r.s3.Update(body)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	recordAudit(c, audit.ActionAdminS3, "", map[string]any{
		"configured":         [2]bool{previous.Configured(), next.Configured()},
		"bucket":             [2]string{previous.S3.Bucket, next.S3.Bucket},
		"prefix":             [2]string{previous.S3.Prefix, next.S3.Prefix},
		"allow_user_backups": [2]bool{previous.AllowUserBackups, next.AllowUserBackups},
		"secret_changed":     body.S3.Secret != "",
	})
	return c.JSON(http.StatusOK, r.response())
}

func (r *adminS3Routes) test(c echo.Context) error {
	var body systems3.Settings
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
	defer cancel()
	if err := r.s3.Test(ctx, body); err != nil {
		return badRequest("Connection test failed: "+err.Error(), nil)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func joinUses(used []string) string {
	names := map[string]string{"audit": "audit log", "visits": "visitor statistics"}
	out := ""
	for i, u := range used {
		if i > 0 {
			out += " and "
		}
		out += names[u]
	}
	return out
}
