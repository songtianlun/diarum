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

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

func RegisterCheveretoRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc) {
	configService := config.NewConfigService(s)
	group := e.Group("/api/v1/chevereto", authMiddleware)

	group.GET("/settings", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		enabled, _ := configService.GetBool(userId, "chevereto.enabled")
		domain, _ := configService.GetString(userId, "chevereto.domain")
		apiKey, _ := configService.GetString(userId, "chevereto.api_key")
		albumId, _ := configService.GetString(userId, "chevereto.album_id")
		return c.JSON(http.StatusOK, map[string]any{"enabled": enabled, "domain": domain, "api_key": apiKey, "album_id": albumId})
	})

	group.PUT("/settings", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		var body struct {
			Enabled bool   `json:"enabled"`
			Domain  string `json:"domain"`
			APIKey  string `json:"api_key"`
			AlbumID string `json:"album_id"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.Enabled && (strings.TrimSpace(body.Domain) == "" || strings.TrimSpace(body.APIKey) == "") {
			return badRequest("Domain and API Key are required to enable Chevereto", nil)
		}
		body.Domain = strings.TrimRight(strings.TrimSpace(body.Domain), "/")
		settings := map[string]any{"chevereto.enabled": body.Enabled, "chevereto.domain": body.Domain, "chevereto.api_key": body.APIKey, "chevereto.album_id": body.AlbumID}
		if err := configService.SetBatch(userId, settings); err != nil {
			return badRequest("Failed to save Chevereto settings", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	group.POST("/test", func(c echo.Context) error {
		var body struct {
			Domain string `json:"domain"`
			APIKey string `json:"api_key"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if strings.TrimSpace(body.Domain) == "" || strings.TrimSpace(body.APIKey) == "" {
			return badRequest("Domain and API Key are required", nil)
		}
		domain := strings.TrimRight(strings.TrimSpace(body.Domain), "/")
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/1/upload", domain), nil)
		if err != nil {
			return c.JSON(http.StatusOK, map[string]any{"success": false, "message": fmt.Sprintf("Invalid domain URL: %v", err)})
		}
		req.Header.Set("X-API-Key", body.APIKey)
		resp, err := client.Do(req)
		if err != nil {
			return c.JSON(http.StatusOK, map[string]any{"success": false, "message": fmt.Sprintf("Connection failed: %v", err)})
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return c.JSON(http.StatusOK, map[string]any{"success": false, "message": "Chevereto API endpoint not found. Please check the domain."})
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return c.JSON(http.StatusOK, map[string]any{"success": false, "message": "Authentication failed. Please check your API key."})
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Connection successful"})
	})

	group.POST("/upload", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		enabled, _ := configService.GetBool(userId, "chevereto.enabled")
		if !enabled {
			return badRequest("Chevereto is not enabled", nil)
		}
		domain, _ := configService.GetString(userId, "chevereto.domain")
		apiKey, _ := configService.GetString(userId, "chevereto.api_key")
		albumId, _ := configService.GetString(userId, "chevereto.album_id")
		if domain == "" || apiKey == "" {
			return badRequest("Chevereto domain and API key are not configured", nil)
		}
		file, header, err := c.Request().FormFile("source")
		if err != nil {
			return badRequest("No file provided", err)
		}
		defer file.Close()

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("source", header.Filename)
		if err != nil {
			return badRequest("Failed to create form file", err)
		}
		if _, err := io.Copy(part, file); err != nil {
			return badRequest("Failed to read file", err)
		}
		if albumId != "" {
			writer.WriteField("album_id", albumId)
		}
		writer.WriteField("title", header.Filename)
		writer.Close()

		req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/1/upload", domain), &buf)
		if err != nil {
			return badRequest("Failed to create request", err)
		}
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
		if err != nil {
			return badRequest(fmt.Sprintf("Upload to Chevereto failed: %v", err), nil)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return badRequest(fmt.Sprintf("Chevereto returned status %d", resp.StatusCode), nil)
		}
		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			return badRequest("Failed to parse Chevereto response", err)
		}
		imageObj, ok := result["image"].(map[string]any)
		if !ok {
			return badRequest("No image data in Chevereto response", nil)
		}
		imageURL, ok := imageObj["url"].(string)
		if !ok || imageURL == "" {
			return badRequest("No image URL in Chevereto response", nil)
		}
		return c.JSON(http.StatusOK, map[string]any{"url": imageURL})
	})
}
