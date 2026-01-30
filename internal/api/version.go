package api

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/core"
)

// RegisterVersionRoutes registers the version API endpoint
func RegisterVersionRoutes(e *core.ServeEvent, version, name string) {
	e.Router.GET("/api/version", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"version": version,
			"name":    name,
		})
	})
}
