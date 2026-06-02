package api

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/songtianlun/diarum/internal/config"
)

// RegisterPublicRoutes registers public API endpoints that use API token authentication
func RegisterPublicRoutes(app *pocketbase.PocketBase, e *core.ServeEvent) {
	configService := config.NewConfigService(app)

	// Get diaries by date or date range using API token
	e.Router.GET("/api/v1/diaries", func(c echo.Context) error {
		token := c.QueryParam("token")
		if token == "" {
			return apis.NewUnauthorizedError("API token is required", nil)
		}

		// Validate token and get owner using ConfigService
		userId, err := configService.ValidateTokenAndGetUser(token)
		if err == config.ErrAPIDisabled {
			return apis.NewUnauthorizedError("API is disabled for this user", nil)
		}
		if err != nil || userId == "" {
			return apis.NewUnauthorizedError("Invalid API token", nil)
		}

		// Check query parameters
		date := c.QueryParam("date")
		start := c.QueryParam("start")
		end := c.QueryParam("end")

		// Single date query
		if date != "" {
			startTime := date + " 00:00:00.000Z"
			endTime := date + " 23:59:59.999Z"

			record, err := app.Dao().FindFirstRecordByFilter(
				"diaries",
				"date >= {:start} && date <= {:end} && owner = {:owner}",
				map[string]any{
					"start": startTime,
					"end":   endTime,
					"owner": userId,
				},
			)

			if err != nil {
				return c.JSON(http.StatusOK, map[string]any{
					"date":    date,
					"content": "",
					"exists":  false,
				})
			}

			return c.JSON(http.StatusOK, map[string]any{
				"id":      record.GetId(),
				"date":    date,
				"content": record.GetString("content"),
				"mood":    record.GetString("mood"),
				"weather": record.GetString("weather"),
				"exists":  true,
			})
		}

		// Date range query
		if start != "" && end != "" {
			startTime := start + " 00:00:00.000Z"
			endTime := end + " 23:59:59.999Z"

			records, err := app.Dao().FindRecordsByFilter(
				"diaries",
				"date >= {:start} && date <= {:end} && owner = {:owner}",
				"-date",
				-1,
				0,
				map[string]any{
					"start": startTime,
					"end":   endTime,
					"owner": userId,
				},
			)

			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Failed to query diaries",
				})
			}

			// Format results
			results := make([]map[string]any, 0, len(records))
			for _, record := range records {
				dateTime := record.GetString("date")
				dateStr := dateTime
				if len(dateTime) >= 10 {
					dateStr = dateTime[:10]
				}

				results = append(results, map[string]any{
					"id":      record.GetId(),
					"date":    dateStr,
					"content": record.GetString("content"),
					"mood":    record.GetString("mood"),
					"weather": record.GetString("weather"),
				})
			}

			return c.JSON(http.StatusOK, map[string]any{
				"diaries": results,
				"total":   len(results),
			})
		}

		return apis.NewBadRequestError("Either 'date' or both 'start' and 'end' query parameters are required", nil)
	}, apis.ActivityLogger(app))
}
