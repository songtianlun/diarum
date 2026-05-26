package api

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

// RegisterPublicRoutes registers public API endpoints that use API token authentication.
func RegisterPublicRoutes(e *echo.Echo, s *store.Store) {
	configService := config.NewConfigService(s)

	e.GET("/api/v1/diaries", func(c echo.Context) error {
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

		date := c.QueryParam("date")
		start := c.QueryParam("start")
		end := c.QueryParam("end")

		if date != "" {
			diary, err := s.GetDiaryByDate(userId, date+" 00:00:00.000Z", date+" 23:59:59.999Z")
			if err != nil {
				return c.JSON(http.StatusOK, map[string]any{"date": date, "content": "", "exists": false})
			}
			return c.JSON(http.StatusOK, diaryResponse(diary, date, true))
		}

		if start != "" && end != "" {
			diaries, err := s.ListDiaries(userId, start+" 00:00:00.000Z", end+" 23:59:59.999Z", "-date", 0)
			if err != nil {
				return serverError("Failed to query diaries", err)
			}
			results := make([]map[string]any, 0, len(diaries))
			for _, diary := range diaries {
				results = append(results, map[string]any{"id": diary.ID, "date": store.DateOnly(diary.Date), "content": diary.Content, "mood": diary.Mood, "weather": diary.Weather})
			}
			return c.JSON(http.StatusOK, map[string]any{"diaries": results, "total": len(results)})
		}

		return badRequest("Either 'date' or both 'start' and 'end' query parameters are required", nil)
	})
}
