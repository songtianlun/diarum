package api

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

// RegisterPublicRoutes registers public API endpoints that use API token authentication.
func RegisterPublicRoutes(e *echo.Echo, s *store.Store) {
	configService := config.NewConfigService(s)

	e.GET("/api/v1/diaries", func(c echo.Context) error {
		// Tag the request before authenticating, so rejected tokens show up
		// as token API attempts rather than anonymous web calls.
		auditIdentify(c, "", "", audit.SourceAPI)
		token := c.QueryParam("token")
		if token == "" {
			return unauthorized("API token is required")
		}

		userId, err := configService.ValidateTokenAndGetUser(token)
		if err == config.ErrAPIDisabled {
			return unauthorized("API is disabled for this user")
		}
		if err != nil || userId == "" {
			return unauthorized("Invalid API token")
		}
		auditIdentify(c, userId, "", audit.SourceAPI)

		date := c.QueryParam("date")
		start := c.QueryParam("start")
		end := c.QueryParam("end")
		// recordCall audits a call that read no diary. The query is kept
		// because the path alone does not say what was asked; the token
		// parameter is never part of it.
		recordCall := func(target string, detail map[string]any) {
			if args := auditArgs(map[string]any{"date": date, "start": start, "end": end}); len(args) > 0 {
				detail["arguments"] = args
			}
			recordAuditFor(c, userId, "", audit.SourceAPI, audit.ActionAPICall, target, detail)
		}

		if date != "" {
			diary, err := s.GetDiaryByDate(userId, date+" 00:00:00.000Z", date+" 23:59:59.999Z")
			if err != nil {
				recordCall(date, map[string]any{"exists": false})
				return c.JSON(http.StatusOK, map[string]any{"date": date, "content": "", "exists": false})
			}
			recordAuditFor(c, userId, "", audit.SourceAPI, audit.ActionDiaryView, date, diaryAuditDetail(diary))
			return c.JSON(http.StatusOK, diaryResponse(diary, date, true))
		}

		if start != "" && end != "" {
			diaries, err := s.ListDiaries(userId, start+" 00:00:00.000Z", end+" 23:59:59.999Z", "-date", 0)
			if err != nil {
				recordCall(start+".."+end, map[string]any{})
				return serverError("Failed to query diaries", err)
			}
			results := make([]map[string]any, 0, len(diaries))
			for _, diary := range diaries {
				results = append(results, map[string]any{"id": diary.ID, "date": store.DateOnly(diary.Date), "content": diary.Content, "mood": diary.Mood, "weather": diary.Weather})
			}
			if len(diaries) == 0 {
				recordCall(start+".."+end, map[string]any{"results": 0})
			} else {
				recordAuditFor(c, userId, "", audit.SourceAPI, audit.ActionDiaryView, start+".."+end, map[string]any{"dates": diaryDates(diaries)})
			}
			return c.JSON(http.StatusOK, map[string]any{"diaries": results, "total": len(results)})
		}

		recordCall("", map[string]any{})
		return badRequest("Either 'date' or both 'start' and 'end' query parameters are required", nil)
	})
}
