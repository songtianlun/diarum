package api

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/core"
)

// RegisterVersionRoutes registers the version API endpoint
func RegisterVersionRoutes(e *core.ServeEvent, version, name string) {
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"version": version,
			"name":    name,
		})
	}

	// v1 canonical endpoint
	e.Router.GET("/api/v1/version", handler)
	// backward compatibility for existing clients
	e.Router.GET("/api/version", handler)
}
