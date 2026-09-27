package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/store"
)

const auditContextKey = "diarum_audit"

// requestAudit collects what handlers say about the request being served;
// the middleware turns it into the request's audit entry once it finishes.
type requestAudit struct {
	userID string
	user   string
	source string
	// entries are the actions handlers recorded, in order. The first one is
	// merged into the request's own entry; any further ones (an MCP batch,
	// for example) are written as entries of their own.
	entries []audit.Entry
}

// AuditMiddleware records every API call: who made it, from where, what it
// hit and how it ended, plus whatever meaning the handler attached. Only the
// entry is built on the request goroutine; encoding and writing happen on the
// logger's own goroutine. A nil logger records nothing.
//
// Register it before middleware.Recover so panics are recorded as 500s.
func AuditMiddleware(l *audit.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if l == nil || !strings.HasPrefix(req.URL.Path, "/api/") {
				return next(c)
			}
			ra := &requestAudit{}
			c.Set(auditContextKey, ra)
			started := time.Now()
			err := next(c)
			finished := time.Now()

			status := c.Response().Status
			message := ""
			if err != nil {
				var httpErr *echo.HTTPError
				if errors.As(err, &httpErr) {
					status = httpErr.Code
					message = fmt.Sprint(httpErr.Message)
				} else {
					status = 500
					message = err.Error()
				}
			}
			if status == 0 {
				status = 200
			}

			userID, username, source := ra.userID, ra.user, ra.source
			if user := auth.CurrentUser(c); user != nil && userID == "" {
				userID, username = user.ID, user.Username
			}
			if source == "" {
				source = audit.SourceWeb
			}
			base := audit.Entry{
				Time:     finished,
				UserID:   userID,
				User:     username,
				Source:   source,
				Method:   req.Method,
				Route:    c.Path(),
				Path:     redactPath(c, req.URL.Path),
				Status:   status,
				Duration: finished.Sub(started).Milliseconds(),
				IP:       c.RealIP(),
				UA:       req.UserAgent(),
				Error:    message,
			}
			if len(ra.entries) == 0 {
				switch status {
				case 401:
					base.Action = audit.ActionAuthDenied
				case 403:
					base.Action = audit.ActionAuthForbidden
				}
				l.Record(base)
				return err
			}
			for i, annotation := range ra.entries {
				entry := base
				entry.Action, entry.Target, entry.Detail = annotation.Action, annotation.Target, annotation.Detail
				if annotation.UserID != "" {
					entry.UserID, entry.User = annotation.UserID, annotation.User
				}
				if annotation.Source != "" {
					entry.Source = annotation.Source
				}
				if i > 0 {
					// Follow-up actions share the request's facts but not its
					// duration, so statistics count the call only once.
					entry.Duration = 0
					entry.Route, entry.Method = "", ""
				}
				l.Record(entry)
			}
			return err
		}
	}
}

// redactPath hides secrets carried in the URL path, such as webhook tokens.
func redactPath(c echo.Context, path string) string {
	for _, param := range c.PathParams() {
		name := strings.ToLower(param.Name)
		if param.Value != "" && (strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "key")) {
			path = strings.ReplaceAll(path, param.Value, "***")
		}
	}
	return path
}

func requestAuditOf(c echo.Context) *requestAudit {
	ra, _ := c.Get(auditContextKey).(*requestAudit)
	return ra
}

// auditEnabled reports whether this request is being audited, so handlers
// can skip lookups that only feed the audit trail.
func auditEnabled(c echo.Context) bool {
	return requestAuditOf(c) != nil
}

// auditIdentify names the account behind a request authenticated by other
// means than a session (API tokens, webhooks, MCP).
func auditIdentify(c echo.Context, userID, username, source string) {
	if ra := requestAuditOf(c); ra != nil {
		ra.userID, ra.user, ra.source = userID, username, source
	}
}

// recordAudit attaches an action taken by the signed-in user through the web
// API to the request's audit entry.
func recordAudit(c echo.Context, action, target string, detail map[string]any) {
	user := auth.CurrentUser(c)
	if user == nil {
		recordAuditFor(c, "", "", "", action, target, detail)
		return
	}
	recordAuditFor(c, user.ID, user.Username, audit.SourceWeb, action, target, detail)
}

func recordAuditFor(c echo.Context, userID, actor, source, action, target string, detail map[string]any) {
	ra := requestAuditOf(c)
	if ra == nil {
		return
	}
	ra.entries = append(ra.entries, audit.Entry{UserID: userID, User: actor, Source: source, Action: action, Target: target, Detail: detail})
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
// state it replaced (nil when the entry was new).
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
	detail["words_before"] = CountWords(before.Content)
	if before.Content == after.Content && before.Mood == after.Mood && before.Weather == after.Weather {
		detail["unchanged"] = true
	}
	if before.Mood != after.Mood {
		detail["mood_before"] = before.Mood
	}
	if before.Weather != after.Weather {
		detail["weather_before"] = before.Weather
	}
	recordAuditFor(c, userID, actor, source, audit.ActionDiaryUpdate, date, detail)
}
