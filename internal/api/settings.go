package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

// generateToken generates a random 32-character hex token
func generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func getSettingHandler(configService *config.ConfigService) echo.HandlerFunc {
	return func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		key := c.PathParam("key")

		if _, ok := config.GetConfigMeta(key); !ok {
			return badRequest("Unknown setting key: "+key, nil)
		}

		value, err := configService.Get(userId, key)
		if err != nil {
			return badRequest("Failed to get setting", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"key":   key,
			"value": value,
		})
	}
}

func putSettingHandler(configService *config.ConfigService) echo.HandlerFunc {
	return func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		key := c.PathParam("key")

		if _, ok := config.GetConfigMeta(key); !ok {
			return badRequest("Unknown setting key: "+key, nil)
		}

		var body struct {
			Value any `json:"value"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		if err := configService.Set(userId, key, body.Value); err != nil {
			return badRequest("Failed to save setting", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
		})
	}
}

func deleteSettingHandler(configService *config.ConfigService) echo.HandlerFunc {
	return func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		key := c.PathParam("key")

		if _, ok := config.GetConfigMeta(key); !ok {
			return badRequest("Unknown setting key: "+key, nil)
		}

		if err := configService.Delete(userId, key); err != nil {
			return badRequest("Failed to delete setting", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
		})
	}
}

// RegisterSettingsRoutes registers settings-related API endpoints
func RegisterSettingsRoutes(e *echo.Echo, store *store.Store, authMiddleware echo.MiddlewareFunc) {
	configService := config.NewConfigService(store)
	group := e.Group("/api/v1/settings", authMiddleware)

	// Get API token status and value
	group.GET("/api-token", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		token, err := configService.GetString(userId, "api.token")
		if err != nil {
			logger.Debug("[GET /api/v1/settings/api-token] error getting token: %v", err)
		}
		enabled, err := configService.GetBool(userId, "api.enabled")
		if err != nil {
			logger.Debug("[GET /api/v1/settings/api-token] error getting enabled: %v", err)
		}

		mcpEnabled, err := configService.GetBool(userId, "api.mcp_enabled")
		if err != nil {
			logger.Debug("[GET /api/v1/settings/api-token] error getting mcp enabled: %v", err)
		}

		if token == "" {
			return c.JSON(http.StatusOK, map[string]any{
				"exists":      false,
				"enabled":     false,
				"token":       "",
				"mcp_enabled": false,
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"exists":      true,
			"enabled":     enabled,
			"token":       token,
			"mcp_enabled": mcpEnabled,
		})
	})

	// Toggle MCP server enabled/disabled. MCP reuses the API token, so it can
	// only be switched on while API access itself is enabled.
	group.POST("/mcp/toggle", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		current, err := configService.GetBool(userId, "api.mcp_enabled")
		if err != nil {
			logger.Debug("[POST /api/v1/settings/mcp/toggle] error getting mcp enabled: %v", err)
		}

		newEnabled := !current
		if body.Enabled != nil {
			newEnabled = *body.Enabled
		}

		if newEnabled {
			apiEnabled, err := configService.GetBool(userId, "api.enabled")
			if err != nil {
				logger.Debug("[POST /api/v1/settings/mcp/toggle] error getting api enabled: %v", err)
			}
			token, err := configService.GetString(userId, "api.token")
			if err != nil {
				logger.Debug("[POST /api/v1/settings/mcp/toggle] error getting token: %v", err)
			}
			if !apiEnabled || token == "" {
				return badRequest("Enable API access before enabling MCP", nil)
			}
		}

		if err := configService.Set(userId, "api.mcp_enabled", newEnabled); err != nil {
			return badRequest("Failed to update MCP status", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"mcp_enabled": newEnabled,
		})
	})

	// Toggle API token enabled/disabled
	group.POST("/api-token/toggle", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		token, err := configService.GetString(userId, "api.token")
		if err != nil {
			logger.Debug("[POST /api/v1/settings/api-token/toggle] error getting token: %v", err)
		}
		enabled, err := configService.GetBool(userId, "api.enabled")
		if err != nil {
			logger.Debug("[POST /api/v1/settings/api-token/toggle] error getting enabled: %v", err)
		}

		if token == "" {
			// No token exists, create one and enable it
			newToken, err := generateToken()
			if err != nil {
				return badRequest("Failed to generate token", err)
			}

			if err := configService.Set(userId, "api.token", newToken); err != nil {
				return badRequest("Failed to save token", err)
			}
			if err := configService.Set(userId, "api.enabled", true); err != nil {
				return badRequest("Failed to save enabled status", err)
			}

			return c.JSON(http.StatusOK, map[string]any{
				"enabled":     true,
				"token":       newToken,
				"mcp_enabled": false,
			})
		}

		// Toggle existing token
		newEnabled := !enabled
		if err := configService.Set(userId, "api.enabled", newEnabled); err != nil {
			return badRequest("Failed to update token", err)
		}

		// MCP rides on the same token, so turning API access off turns it off too.
		mcpEnabled := false
		if newEnabled {
			if mcpEnabled, err = configService.GetBool(userId, "api.mcp_enabled"); err != nil {
				logger.Debug("[POST /api/v1/settings/api-token/toggle] error getting mcp enabled: %v", err)
			}
		} else if err := configService.Set(userId, "api.mcp_enabled", false); err != nil {
			logger.Debug("[POST /api/v1/settings/api-token/toggle] error disabling mcp: %v", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"enabled":     newEnabled,
			"token":       token,
			"mcp_enabled": mcpEnabled,
		})
	})

	// Reset API token (generate new one)
	group.POST("/api-token/reset", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		// Generate new token
		newToken, err := generateToken()
		if err != nil {
			return badRequest("Failed to generate token", err)
		}

		// Save new token
		if err := configService.Set(userId, "api.token", newToken); err != nil {
			return badRequest("Failed to save token", err)
		}

		// Ensure enabled is set (default to true for reset)
		enabled, err := configService.GetBool(userId, "api.enabled")
		if err != nil {
			logger.Debug("[POST /api/v1/settings/api-token/reset] error getting enabled: %v", err)
		}
		if !enabled {
			if err := configService.Set(userId, "api.enabled", true); err != nil {
				logger.Debug("[POST /api/v1/settings/api-token/reset] error setting enabled: %v", err)
			}
			enabled = true
		}

		mcpEnabled, err := configService.GetBool(userId, "api.mcp_enabled")
		if err != nil {
			logger.Debug("[POST /api/v1/settings/api-token/reset] error getting mcp enabled: %v", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"enabled":     enabled,
			"token":       newToken,
			"mcp_enabled": mcpEnabled,
		})
	})

	// Get all settings (new v1 API)
	group.GET("", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		settings, err := configService.GetBatch(userId)
		if err != nil {
			return badRequest("Failed to get settings", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"settings": settings,
		})
	})

	// Batch update settings (new v1 API)
	group.PUT("/batch", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID

		var body struct {
			Settings map[string]any `json:"settings"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		// Validate keys against registry
		for key := range body.Settings {
			if _, ok := config.GetConfigMeta(key); !ok {
				return badRequest("Unknown setting key: "+key, nil)
			}
		}

		if err := configService.SetBatch(userId, body.Settings); err != nil {
			return badRequest("Failed to save settings", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
		})
	})

	// Get single setting by key
	group.GET("/:key", getSettingHandler(configService))

	// Update single setting by key
	group.PUT("/:key", putSettingHandler(configService))

	// Delete single setting by key
	group.DELETE("/:key", deleteSettingHandler(configService))
}
