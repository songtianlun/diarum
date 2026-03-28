package api

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
)

// RegisterDiaryRoutes registers custom API endpoints for diary operations
func RegisterDiaryRoutes(app *pocketbase.PocketBase, e *core.ServeEvent) {
	// Get diary by date
	e.Router.GET("/api/diaries/by-date/:date", func(c echo.Context) error {
		dateStr := c.PathParam("date")

		// Get authenticated user
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Parse date and create time range for the entire day
		// Input format: "2026-01-28"
		// Create range: "2026-01-28 00:00:00" to "2026-01-28 23:59:59"
		startTime := dateStr + " 00:00:00.000Z"
		endTime := dateStr + " 23:59:59.999Z"

		// Query diary by date range and owner
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
			// Return empty diary if not found
			return c.JSON(http.StatusOK, map[string]any{
				"date":    dateStr,
				"content": "",
				"exists":  false,
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"id":      record.GetId(),
			"date":    dateStr, // Return original date format
			"content": record.GetString("content"),
			"mood":    record.GetString("mood"),
			"weather": record.GetString("weather"),
			"exists":  true,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Check which dates have diaries
	e.Router.GET("/api/diaries/exists", func(c echo.Context) error {
		start := c.QueryParam("start")
		end := c.QueryParam("end")

		// Get authenticated user
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Parse date range
		if start == "" || end == "" {
			// Default to current month
			now := time.Now()
			start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			end = time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		}

		// Convert to full timestamp range
		startTime := start + " 00:00:00.000Z"
		endTime := end + " 23:59:59.999Z"

		// Query all diaries in date range
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

		type calendarEntry struct {
			Date    string `json:"date"`
			Mood    string `json:"mood"`
			Weather string `json:"weather"`
		}

		// Extract dates (convert from timestamp to YYYY-MM-DD format)
		dates := make([]string, 0, len(records))
		entries := make([]calendarEntry, 0, len(records))
		for _, record := range records {
			dateTime := record.GetString("date")
			// Extract just the date part (YYYY-MM-DD) from "YYYY-MM-DD HH:MM:SS.SSSZ"
			if len(dateTime) >= 10 {
				date := dateTime[:10]
				dates = append(dates, date)
				entries = append(entries, calendarEntry{
					Date:    date,
					Mood:    record.GetString("mood"),
					Weather: record.GetString("weather"),
				})
			}
		}

		return c.JSON(http.StatusOK, map[string]any{
			"dates":   dates,
			"entries": entries,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Get diary stats (streak and total)
	e.Router.GET("/api/diaries/stats", func(c echo.Context) error {
		// Get authenticated user
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Get timezone from query param, default to UTC
		tz := c.QueryParam("tz")
		loc := time.UTC
		if tz != "" {
			if parsedLoc, err := time.LoadLocation(tz); err == nil {
				loc = parsedLoc
			}
		}

		// Get total count using COUNT query for better performance
		var total int
		err := app.Dao().DB().
			NewQuery("SELECT COUNT(*) FROM diaries WHERE owner = {:owner}").
			Bind(map[string]any{"owner": userId}).
			Row(&total)
		if err != nil {
			total = 0
		}

		// Calculate streak - only fetch recent records (last 365 days max)
		streak := 0
		now := time.Now().In(loc)
		today := now.Format("2006-01-02")
		oneYearAgo := now.AddDate(-1, 0, 0).Format("2006-01-02")

		records, err := app.Dao().FindRecordsByFilter(
			"diaries",
			"owner = {:owner} && date >= {:start}",
			"-date",
			365,
			0,
			map[string]any{
				"owner": userId,
				"start": oneYearAgo + " 00:00:00.000Z",
			},
		)

		if err == nil && len(records) > 0 {
			// Create a set of dates for quick lookup
			dateSet := make(map[string]bool)
			for _, record := range records {
				dateTime := record.GetString("date")
				if len(dateTime) >= 10 {
					dateSet[dateTime[:10]] = true
				}
			}

			// Start counting from today or yesterday
			yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
			var checkDate time.Time
			if dateSet[today] {
				checkDate = now
			} else if dateSet[yesterday] {
				checkDate = now.AddDate(0, 0, -1)
			}

			if !checkDate.IsZero() {
				for {
					dateStr := checkDate.Format("2006-01-02")
					if dateSet[dateStr] {
						streak++
						checkDate = checkDate.AddDate(0, 0, -1)
					} else {
						break
					}
				}
			}
		}

		return c.JSON(http.StatusOK, map[string]any{
			"total":  total,
			"streak": streak,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Search diaries
	e.Router.GET("/api/diaries/search", func(c echo.Context) error {
		query := c.QueryParam("q")

		// Get authenticated user
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		if query == "" {
			return apis.NewBadRequestError("Query parameter 'q' is required", nil)
		}

		userId := authRecord.Id

		// Search in content
		records, err := app.Dao().FindRecordsByFilter(
			"diaries",
			"content ~ {:query} && owner = {:owner}",
			"-date",
			50, // Limit to 50 results
			0,
			map[string]any{
				"query": query,
				"owner": userId,
			},
		)

		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Search failed",
			})
		}

		// Format results
		results := make([]map[string]any, 0, len(records))
		for _, record := range records {
			content := record.GetString("content")
			// Get snippet (first 200 chars)
			snippet := content
			if len(content) > 200 {
				snippet = content[:200] + "..."
			}

			// Extract date part from timestamp
			dateTime := record.GetString("date")
			dateStr := dateTime
			if len(dateTime) >= 10 {
				dateStr = dateTime[:10]
			}

			results = append(results, map[string]any{
				"id":      record.GetId(),
				"date":    dateStr,
				"snippet": snippet,
				"mood":    record.GetString("mood"),
				"weather": record.GetString("weather"),
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"results": results,
			"total":   len(results),
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}
