package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

	// Proxy upload to Chevereto (avoids CORS issues)
	e.Router.POST("/api/chevereto/upload", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Read settings from config
		enabled, _ := configService.GetBool(userId, "chevereto.enabled")
		if !enabled {
			return apis.NewBadRequestError("Chevereto is not enabled", nil)
		}

		domain, _ := configService.GetString(userId, "chevereto.domain")
		apiKey, _ := configService.GetString(userId, "chevereto.api_key")
		albumId, _ := configService.GetString(userId, "chevereto.album_id")

		if domain == "" || apiKey == "" {
			return apis.NewBadRequestError("Chevereto domain and API key are not configured", nil)
		}

		// Get uploaded file from request
		file, header, err := c.Request().FormFile("source")
		if err != nil {
			return apis.NewBadRequestError("No file provided", err)
		}
		defer file.Close()

		// Build multipart request to Chevereto
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		part, err := writer.CreateFormFile("source", header.Filename)
		if err != nil {
			return apis.NewBadRequestError("Failed to create form file", err)
		}
		if _, err := io.Copy(part, file); err != nil {
			return apis.NewBadRequestError("Failed to read file", err)
		}

		if albumId != "" {
			writer.WriteField("album_id", albumId)
		}
		writer.WriteField("title", header.Filename)
		writer.Close()

		// Send to Chevereto
		uploadURL := fmt.Sprintf("%s/api/1/upload", domain)
		req, err := http.NewRequest("POST", uploadURL, &buf)
		if err != nil {
			return apis.NewBadRequestError("Failed to create request", err)
		}
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return apis.NewBadRequestError(fmt.Sprintf("Upload to Chevereto failed: %v", err), nil)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return apis.NewBadRequestError("Failed to read Chevereto response", err)
		}

		if resp.StatusCode != http.StatusOK {
			var errResp map[string]any
			if json.Unmarshal(respBody, &errResp) == nil {
				if errObj, ok := errResp["error"].(map[string]any); ok {
					if msg, ok := errObj["message"].(string); ok {
						return apis.NewBadRequestError(fmt.Sprintf("Chevereto error: %s", msg), nil)
					}
				}
			}
			return apis.NewBadRequestError(fmt.Sprintf("Chevereto returned status %d", resp.StatusCode), nil)
		}

		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			return apis.NewBadRequestError("Failed to parse Chevereto response", err)
		}

		// Extract image URL
		imageObj, ok := result["image"].(map[string]any)
		if !ok {
			return apis.NewBadRequestError("No image data in Chevereto response", nil)
		}
		imageURL, ok := imageObj["url"].(string)
		if !ok || imageURL == "" {
			return apis.NewBadRequestError("No image URL in Chevereto response", nil)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"url": imageURL,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}
