package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/holidays"
)

const maxBatchHolidayYears = 20

// RegisterHolidayRoutes registers public-holiday lookup and sync endpoints.
func RegisterHolidayRoutes(e *echo.Echo, svc *holidays.Service, authMiddleware echo.MiddlewareFunc) {
	if svc == nil {
		return
	}
	group := e.Group("/api/v1/holidays", authMiddleware)

	// GET /api/v1/holidays/cn?year=2026 or ?years=2025,2026
	group.GET("/cn", func(c echo.Context) error {
		yearsParam := strings.TrimSpace(c.QueryParam("years"))
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
				"region": holidays.RegionCN,
				"years":  results,
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

	// GET /api/v1/holidays/cn/status
	group.GET("/cn/status", func(c echo.Context) error {
		status, err := svc.Status()
		if err != nil {
			return serverError("Failed to read holiday status", err)
		}
		return c.JSON(http.StatusOK, status)
	})

	// POST /api/v1/holidays/cn/refresh
	group.POST("/cn/refresh", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 25*time.Second)
		defer cancel()
		status, err := svc.RefreshPolicyYears(ctx)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadGateway, "Failed to refresh holiday data: "+err.Error())
		}
		return c.JSON(http.StatusOK, status)
	})
}
