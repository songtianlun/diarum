package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
)

// generateToken generates a random 32-character hex token
func generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// RegisterSettingsRoutes registers settings-related API endpoints
func RegisterSettingsRoutes(app *pocketbase.PocketBase, e *core.ServeEvent) {
	// Get API token status and value
	e.Router.GET("/api/settings/api-token", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Find existing token for this user
		record, err := app.Dao().FindFirstRecordByFilter(
			"api_tokens",
			"owner = {:owner}",
			map[string]any{
				"owner": userId,
			},
		)

		if err != nil {
			// No token exists yet
			return c.JSON(http.StatusOK, map[string]any{
				"exists":  false,
				"enabled": false,
				"token":   "",
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"exists":  true,
			"enabled": record.GetBool("enabled"),
			"token":   record.GetString("token"),
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Toggle API token enabled/disabled
	e.Router.POST("/api/settings/api-token/toggle", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Find existing token for this user
		record, err := app.Dao().FindFirstRecordByFilter(
			"api_tokens",
			"owner = {:owner}",
			map[string]any{
				"owner": userId,
			},
		)

		if err != nil {
			// No token exists, create one and enable it
			token, err := generateToken()
			if err != nil {
				return apis.NewBadRequestError("Failed to generate token", err)
			}

			collection, err := app.Dao().FindCollectionByNameOrId("api_tokens")
			if err != nil {
				return apis.NewBadRequestError("Failed to find api_tokens collection", err)
			}

			record = models.NewRecord(collection)
			record.Set("token", token)
			record.Set("enabled", true)
			record.Set("owner", userId)

			if err := app.Dao().SaveRecord(record); err != nil {
				return apis.NewBadRequestError("Failed to save token", err)
			}

			return c.JSON(http.StatusOK, map[string]any{
				"enabled": true,
				"token":   token,
			})
		}

		// Toggle existing token
		newEnabled := !record.GetBool("enabled")
		record.Set("enabled", newEnabled)

		if err := app.Dao().SaveRecord(record); err != nil {
			return apis.NewBadRequestError("Failed to update token", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"enabled": newEnabled,
			"token":   record.GetString("token"),
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Reset API token (generate new one)
	e.Router.POST("/api/settings/api-token/reset", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		// Generate new token
		token, err := generateToken()
		if err != nil {
			return apis.NewBadRequestError("Failed to generate token", err)
		}

		// Find existing token for this user
		record, err := app.Dao().FindFirstRecordByFilter(
			"api_tokens",
			"owner = {:owner}",
			map[string]any{
				"owner": userId,
			},
		)

		if err != nil {
			// No token exists, create one
			collection, err := app.Dao().FindCollectionByNameOrId("api_tokens")
			if err != nil {
				return apis.NewBadRequestError("Failed to find api_tokens collection", err)
			}

			record = models.NewRecord(collection)
			record.Set("token", token)
			record.Set("enabled", true)
			record.Set("owner", userId)
		} else {
			// Update existing token
			record.Set("token", token)
		}

		if err := app.Dao().SaveRecord(record); err != nil {
			return apis.NewBadRequestError("Failed to save token", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"enabled": record.GetBool("enabled"),
			"token":   token,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}
