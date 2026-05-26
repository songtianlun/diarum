package api

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// RegisterVersionRoutes registers the version API endpoint
func RegisterVersionRoutes(e *echo.Echo, version, name string) {
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"version": version,
			"name":    name,
		})
	}

	// v1 canonical endpoint
	e.GET("/api/v1/version", handler)
	// backward compatibility for existing clients
	e.GET("/api/version", handler)
}
