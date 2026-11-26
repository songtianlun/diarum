package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/holidays"
	"github.com/songtianlun/diarum/internal/store"
)

const maxBatchHolidayYears = 20

// RegisterHolidayRoutes registers user-facing public-holiday lookup endpoints.
// All endpoints respect the global admin toggle (holidays.cn.enabled).
func RegisterHolidayRoutes(e *echo.Echo, svc *holidays.Service, authMiddleware echo.MiddlewareFunc) {
	if svc == nil {
		return
	}
	group := e.Group("/api/v1/holidays", authMiddleware)

	// GET /api/v1/holidays/cn/status — reports whether the admin has enabled the feature.
	group.GET("/cn/status", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"region":  holidays.RegionCN,
			"enabled": svc.Enabled(),
		})
	})

	// GET /api/v1/holidays/cn?year=2026 or ?years=2025,2026
	group.GET("/cn", func(c echo.Context) error {
		yearsParam := strings.TrimSpace(c.QueryParam("years"))
		if !svc.Enabled() {
			if yearsParam != "" {
				return c.JSON(http.StatusOK, map[string]any{
					"region":  holidays.RegionCN,
					"enabled": false,
					"years":   map[string]any{},
				})
			}
			return c.JSON(http.StatusOK, map[string]any{
				"region":    holidays.RegionCN,
				"enabled":   false,
				"published": false,
				"days":      []holidays.Day{},
				"papers":    []string{},
			})
		}

		if yearsParam != "" {
			parts := strings.Split(yearsParam, ",")
			if len(parts) > maxBatchHolidayYears {
				return badRequest("Too many years requested (max 20)", nil)
			}
			seen := make(map[int]struct{}, len(parts))
			results := make(map[string]*holidays.YearResponse, len(parts))
			for _, raw := range parts {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				year, err := strconv.Atoi(raw)
				if err != nil || year < holidays.MinYear || year > holidays.MaxYear {
					return badRequest("Invalid year: "+raw, nil)
				}
				if _, ok := seen[year]; ok {
					continue
				}
				seen[year] = struct{}{}
				res, err := svc.GetYear(c.Request().Context(), year)
				if err != nil {
					return serverError("Failed to load holiday data", err)
				}
				results[strconv.Itoa(year)] = res
			}
			return c.JSON(http.StatusOK, map[string]any{
				"region":  holidays.RegionCN,
				"enabled": true,
				"years":   results,
			})
		}

		year := time.Now().Year()
		if raw := strings.TrimSpace(c.QueryParam("year")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < holidays.MinYear || parsed > holidays.MaxYear {
				return badRequest("Invalid year: "+raw, nil)
			}
			year = parsed
		}

		res, err := svc.GetYear(c.Request().Context(), year)
		if err != nil {
			return serverError("Failed to load holiday data", err)
		}
		return c.JSON(http.StatusOK, res)
	})
}

// RegisterAdminHolidayRoutes registers admin-only controls for China mainland
// holidays: global enable toggle, official policy sync, and manual date overrides.
func RegisterAdminHolidayRoutes(e *echo.Echo, svc *holidays.Service, authMiddleware echo.MiddlewareFunc) {
	if svc == nil {
		return
	}
	group := e.Group("/api/v1/admin/holidays", authMiddleware, RequireAdmin)

	// GET /api/v1/admin/holidays/cn — global switch, sync status, and manual overrides.
	group.GET("/cn", func(c echo.Context) error {
		status, err := svc.Status()
		if err != nil {
			return serverError("Failed to read holiday status", err)
		}
		overrides, err := svc.ListOverrides(0)
		if err != nil {
			return serverError("Failed to list holiday overrides", err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"region":          status.Region,
			"enabled":         status.Enabled,
			"years":           status.Years,
			"last_fetched_at": status.LastFetchedAt,
			"overrides":       overrides,
		})
	})

	// GET /api/v1/admin/holidays/cn/year?year=2026 — merged effective schedule for admin preview.
	group.GET("/cn/year", func(c echo.Context) error {
		year := time.Now().Year()
		if raw := strings.TrimSpace(c.QueryParam("year")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < holidays.MinYear || parsed > holidays.MaxYear {
				return badRequest("Invalid year: "+raw, nil)
			}
			year = parsed
		}
		res, err := svc.GetYear(c.Request().Context(), year)
		if err != nil {
			return serverError("Failed to load holiday year", err)
		}
		return c.JSON(http.StatusOK, res)
	})

	// PUT /api/v1/admin/holidays/cn/settings — toggle global feature availability.
	group.PUT("/cn/settings", func(c echo.Context) error {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if err := svc.SetEnabled(body.Enabled); err != nil {
			return serverError("Failed to save holiday settings", err)
		}
		recordAudit(c, audit.ActionAdminHolidays, "cn", map[string]any{
			"op":      "toggle",
			"enabled": body.Enabled,
		})
		return c.JSON(http.StatusOK, map[string]any{
			"region":  holidays.RegionCN,
			"enabled": body.Enabled,
		})
	})

	// POST /api/v1/admin/holidays/cn/refresh — admin-only policy sync from upstream mirrors.
	group.POST("/cn/refresh", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 25*time.Second)
		defer cancel()
		status, err := svc.RefreshPolicyYears(ctx)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadGateway, "Failed to refresh holiday data: "+err.Error())
		}
		overrides, err := svc.ListOverrides(0)
		if err != nil {
			return serverError("Failed to list holiday overrides", err)
		}
		recordAudit(c, audit.ActionAdminHolidays, "cn", map[string]any{"op": "refresh"})
		return c.JSON(http.StatusOK, map[string]any{
			"region":          status.Region,
			"enabled":         status.Enabled,
			"years":           status.Years,
			"last_fetched_at": status.LastFetchedAt,
			"overrides":       overrides,
		})
	})

	// PUT /api/v1/admin/holidays/cn/overrides — create or update a manual holiday/workday override.
	group.PUT("/cn/overrides", func(c echo.Context) error {
		var body struct {
			Date     string `json:"date"`
			Name     string `json:"name"`
			IsOffDay bool   `json:"isOffDay"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		day, err := svc.SetOverride(body.Date, body.Name, body.IsOffDay)
		if err != nil {
			if errors.Is(err, holidays.ErrInvalidDate) || errors.Is(err, holidays.ErrInvalidYear) || errors.Is(err, holidays.ErrInvalidName) {
				return badRequest(err.Error(), nil)
			}
			return serverError("Failed to save holiday override", err)
		}
		recordAudit(c, audit.ActionAdminHolidays, day.Date, map[string]any{
			"op":       "set_override",
			"name":     day.Name,
			"isOffDay": day.IsOffDay,
		})
		return c.JSON(http.StatusOK, day)
	})

	// DELETE /api/v1/admin/holidays/cn/overrides/:date — remove a manual override.
	group.DELETE("/cn/overrides/:date", func(c echo.Context) error {
		date := c.PathParam("date")
		if err := svc.DeleteOverride(date); err != nil {
			if errors.Is(err, holidays.ErrInvalidDate) {
				return badRequest(err.Error(), nil)
			}
			if errors.Is(err, store.ErrNotFound) {
				return notFound("Holiday override not found")
			}
			return serverError("Failed to delete holiday override", err)
		}
		recordAudit(c, audit.ActionAdminHolidays, date, map[string]any{"op": "delete_override"})
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})
}
