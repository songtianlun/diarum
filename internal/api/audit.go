package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	auditContextKey   = "diarum_audit"
	auditDefaultLimit = 100
	auditMaxLimit     = 5000
)

// AuditMiddleware makes the audit logger available to every handler. Routes
// served without it (as in most tests) simply record nothing.
func AuditMiddleware(l *audit.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(auditContextKey, l)
			return next(c)
		}
	}
}

func auditLogger(c echo.Context) *audit.Logger {
	l, _ := c.Get(auditContextKey).(*audit.Logger)
	return l
}

// auditEnabled reports whether recording is on, so handlers can skip the
// extra lookups that only feed the audit trail.
func auditEnabled(c echo.Context) bool {
	return auditLogger(c) != nil
}

// recordAudit logs an action taken by the signed-in user through the web API.
func recordAudit(c echo.Context, action, target string, detail map[string]any) {
	user := auth.CurrentUser(c)
	if user == nil {
		return
	}
	recordAuditFor(c, user.ID, user.Username, audit.SourceWeb, action, target, detail)
}

func recordAuditFor(c echo.Context, userID, actor, source, action, target string, detail map[string]any) {
	l := auditLogger(c)
	if l == nil {
		return
	}
	req := c.Request()
	l.Record(audit.Entry{
		User:   userID,
		Actor:  actor,
		Action: action,
		Target: target,
		Source: source,
		IP:     c.RealIP(),
		UA:     req.UserAgent(),
		Detail: detail,
	})
}

// diaryAuditDetail summarises an entry for the trail without its content.
func diaryAuditDetail(diary *store.Diary) map[string]any {
	detail := map[string]any{}
	if diary == nil {
		return detail
	}
	detail["id"] = diary.ID
	detail["words"] = CountWords(diary.Content)
	if diary.Mood != "" {
		detail["mood"] = diary.Mood
	}
	if diary.Weather != "" {
		detail["weather"] = diary.Weather
	}
	return detail
}

// recordDiarySave logs a save that went through UpsertDiary. before is the
// state it replaced (nil when the entry was new); saves that changed nothing
// are not logged.
func recordDiarySave(c echo.Context, userID, actor, source string, before, after *store.Diary) {
	if after == nil {
		return
	}
	date := store.DateOnly(after.Date)
	detail := diaryAuditDetail(after)
	if before == nil {
		recordAuditFor(c, userID, actor, source, audit.ActionDiaryCreate, date, detail)
		return
	}
	if before.Content == after.Content && before.Mood == after.Mood && before.Weather == after.Weather {
		return
	}
	detail["words_before"] = CountWords(before.Content)
	if before.Mood != after.Mood {
		detail["mood_before"] = before.Mood
	}
	if before.Weather != after.Weather {
		detail["weather_before"] = before.Weather
	}
	recordAuditFor(c, userID, actor, source, audit.ActionDiaryUpdate, date, detail)
}

// RegisterAuditRoutes serves a user's own audit trail and its settings.
func RegisterAuditRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, l *audit.Logger) {
	configService := config.NewConfigService(s)
	group := e.Group("/api/v1/audit", authMiddleware)

	settingsResponse := func(userID string) map[string]any {
		return map[string]any{
			"retention_days": l.RetentionDays(userID),
			"default":        audit.DefaultRetentionDays,
			"min":            audit.MinRetentionDays,
			"max":            audit.MaxRetentionDays,
			"enabled":        l != nil,
		}
	}

	group.GET("/settings", func(c echo.Context) error {
		return c.JSON(http.StatusOK, settingsResponse(auth.CurrentUser(c).ID))
	})

	group.PUT("/settings", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		var body struct {
			RetentionDays *int `json:"retention_days"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.RetentionDays == nil {
			return badRequest("retention_days is required", nil)
		}
		days := *body.RetentionDays
		if days < audit.MinRetentionDays || days > audit.MaxRetentionDays {
			return badRequest("retention_days must be between "+strconv.Itoa(audit.MinRetentionDays)+" and "+strconv.Itoa(audit.MaxRetentionDays), nil)
		}
		previous := l.RetentionDays(user.ID)
		if err := configService.Set(user.ID, audit.SettingRetentionDays, days); err != nil {
			return badRequest("Failed to save audit settings", err)
		}
		if previous != days {
			recordAudit(c, audit.ActionSettingsUpdate, audit.SettingRetentionDays, map[string]any{"from": previous, "to": days})
		}
		// Apply a shorter window right away rather than at the next sweep.
		l.Cleanup(user.ID)
		return c.JSON(http.StatusOK, settingsResponse(user.ID))
	})

	group.GET("/files", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		files, err := l.Files(user.ID)
		if err != nil {
			return serverError("Failed to list audit logs", err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"files":          files,
			"retention_days": l.RetentionDays(user.ID),
		})
	})

	group.GET("/files/:date", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		date := c.PathParam("date")
		f, err := l.OpenFile(user.ID, date)
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
		name := "diarum-audit-" + date + ".log"
		c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+name+`"`)
		c.Response().Header().Set(echo.HeaderContentType, "application/x-ndjson; charset=utf-8")
		http.ServeContent(c.Response(), c.Request(), name, stat.ModTime(), f)
		return nil
	})

	// Entries, newest first. start/end are RFC 3339 instants so the client can
	// ask for its own local day regardless of the server's timezone.
	group.GET("/entries", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		query := audit.Query{
			Text:   c.QueryParam("q"),
			Action: c.QueryParam("action"),
			Limit:  auditDefaultLimit,
		}
		if raw := c.QueryParam("limit"); raw != "" {
			limit, err := strconv.Atoi(raw)
			if err != nil || limit <= 0 {
				return badRequest("invalid limit", nil)
			}
			query.Limit = min(limit, auditMaxLimit)
		}
		var err error
		if query.Start, err = parseAuditTime(c.QueryParam("start")); err != nil {
			return badRequest("invalid start", err)
		}
		if query.End, err = parseAuditTime(c.QueryParam("end")); err != nil {
			return badRequest("invalid end", err)
		}
		if !query.Start.IsZero() && !query.End.IsZero() && query.Start.After(query.End) {
			query.Start, query.End = query.End, query.Start
		}
		result, err := l.Search(user.ID, query)
		if err != nil {
			return serverError("Failed to read audit logs", err)
		}
		return c.JSON(http.StatusOK, result)
	})
}

func parseAuditTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}
