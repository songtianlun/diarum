package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"

	"github.com/songtianlun/diarum/internal/config"
)

// RegisterCheveretoRoutes registers Chevereto image hosting API endpoints
func RegisterCheveretoRoutes(app *pocketbase.PocketBase, e *core.ServeEvent) {
	configService := config.NewConfigService(app)

	// Get Chevereto settings
	e.Router.GET("/api/chevereto/settings", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		enabled, _ := configService.GetBool(userId, "chevereto.enabled")
		domain, _ := configService.GetString(userId, "chevereto.domain")
		apiKey, _ := configService.GetString(userId, "chevereto.api_key")
		albumId, _ := configService.GetString(userId, "chevereto.album_id")

		return c.JSON(http.StatusOK, map[string]any{
			"enabled":  enabled,
			"domain":   domain,
			"api_key":  apiKey,
			"album_id": albumId,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Save Chevereto settings
	e.Router.PUT("/api/chevereto/settings", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		var body struct {
			Enabled bool   `json:"enabled"`
			Domain  string `json:"domain"`
			APIKey  string `json:"api_key"`
			AlbumID string `json:"album_id"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		// Validate: if enabling, domain and api_key must be non-empty
		if body.Enabled {
			if strings.TrimSpace(body.Domain) == "" || strings.TrimSpace(body.APIKey) == "" {
				return apis.NewBadRequestError("Domain and API Key are required to enable Chevereto", nil)
			}
		}

		// Normalize domain: remove trailing slash
		body.Domain = strings.TrimRight(strings.TrimSpace(body.Domain), "/")

		settings := map[string]any{
			"chevereto.enabled":  body.Enabled,
			"chevereto.domain":   body.Domain,
			"chevereto.api_key":  body.APIKey,
			"chevereto.album_id": body.AlbumID,
		}

		if err := configService.SetBatch(userId, settings); err != nil {
			return apis.NewBadRequestError("Failed to save Chevereto settings", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Test Chevereto connection
	e.Router.POST("/api/chevereto/test", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		var body struct {
			Domain string `json:"domain"`
			APIKey string `json:"api_key"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		if strings.TrimSpace(body.Domain) == "" || strings.TrimSpace(body.APIKey) == "" {
			return apis.NewBadRequestError("Domain and API Key are required", nil)
		}

		domain := strings.TrimRight(strings.TrimSpace(body.Domain), "/")

		// Test by making a GET request to the API endpoint
		// Chevereto v4 API: GET /api/1/upload with API key should return method not allowed (405)
		// which confirms the endpoint exists and the server is reachable
		client := &http.Client{Timeout: 10 * time.Second}
		testURL := fmt.Sprintf("%s/api/1/upload", domain)

		req, err := http.NewRequest("GET", testURL, nil)
		if err != nil {
			return c.JSON(http.StatusOK, map[string]any{
				"success": false,
				"message": fmt.Sprintf("Invalid domain URL: %v", err),
			})
		}
		req.Header.Set("X-API-Key", body.APIKey)

		resp, err := client.Do(req)
		if err != nil {
			return c.JSON(http.StatusOK, map[string]any{
				"success": false,
				"message": fmt.Sprintf("Connection failed: %v", err),
			})
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)

		// 200, 400, 401, 403, 405 all indicate the server is reachable
		// Only network errors or 404 indicate a problem
		if resp.StatusCode == http.StatusNotFound {
			return c.JSON(http.StatusOK, map[string]any{
				"success": false,
				"message": "Chevereto API endpoint not found. Please check the domain.",
			})
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return c.JSON(http.StatusOK, map[string]any{
				"success": false,
				"message": "Authentication failed. Please check your API key.",
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
			"message": "Connection successful",
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}
