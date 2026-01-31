package api

import (
	"context"
	"encoding/json"
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

	"github.com/songtianlun/diaria/internal/config"
	"github.com/songtianlun/diaria/internal/embedding"
	"github.com/songtianlun/diaria/internal/logger"
)

// ModelInfo represents a model from the API
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ModelsResponse represents the response from /v1/models endpoint
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// RegisterAIRoutes registers AI-related API endpoints
func RegisterAIRoutes(app *pocketbase.PocketBase, e *core.ServeEvent, embeddingService *embedding.EmbeddingService) {
	configService := config.NewConfigService(app)

	// Get AI settings
	e.Router.GET("/api/ai/settings", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		apiKey, _ := configService.GetString(userId, "ai.api_key")
		baseUrl, _ := configService.GetString(userId, "ai.base_url")
		chatModel, _ := configService.GetString(userId, "ai.chat_model")
		embeddingModel, _ := configService.GetString(userId, "ai.embedding_model")
		enabled, _ := configService.GetBool(userId, "ai.enabled")

		return c.JSON(http.StatusOK, map[string]any{
			"api_key":         apiKey,
			"base_url":        baseUrl,
			"chat_model":      chatModel,
			"embedding_model": embeddingModel,
			"enabled":         enabled,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Save AI settings
	e.Router.PUT("/api/ai/settings", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		userId := authRecord.Id

		var body struct {
			APIKey         string `json:"api_key"`
			BaseURL        string `json:"base_url"`
			ChatModel      string `json:"chat_model"`
			EmbeddingModel string `json:"embedding_model"`
			Enabled        bool   `json:"enabled"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		// Validate: if enabled is true, all fields must be filled
		if body.Enabled {
			if body.APIKey == "" || body.BaseURL == "" || body.ChatModel == "" || body.EmbeddingModel == "" {
				return apis.NewBadRequestError("All AI settings must be configured before enabling AI features", nil)
			}
		}

		settings := map[string]any{
			"ai.api_key":         body.APIKey,
			"ai.base_url":        body.BaseURL,
			"ai.chat_model":      body.ChatModel,
			"ai.embedding_model": body.EmbeddingModel,
			"ai.enabled":         body.Enabled,
		}

		if err := configService.SetBatch(userId, settings); err != nil {
			return apis.NewBadRequestError("Failed to save AI settings", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Fetch models from OpenAI-compatible API
	e.Router.POST("/api/ai/models", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		var body struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		if body.APIKey == "" || body.BaseURL == "" {
			return apis.NewBadRequestError("API key and base URL are required", nil)
		}

		models, err := fetchModels(body.BaseURL, body.APIKey)
		if err != nil {
			logger.Error("[POST /api/ai/models] error fetching models: %v", err)
			return apis.NewBadRequestError("Failed to fetch models: "+err.Error(), nil)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"models": models,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Build all vectors for user's diaries
	e.Router.POST("/api/ai/vectors/build", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		if embeddingService == nil {
			return apis.NewBadRequestError("Embedding service not initialized", nil)
		}

		userId := authRecord.Id

		// Use a longer timeout for vector building
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Minute)
		defer cancel()

		result, err := embeddingService.BuildAllVectors(ctx, userId)
		if err != nil {
			logger.Error("[POST /api/ai/vectors/build] error building vectors: %v", err)
			return apis.NewBadRequestError("Failed to build vectors: "+err.Error(), nil)
		}

		return c.JSON(http.StatusOK, result)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Incremental build vectors (only new and outdated)
	e.Router.POST("/api/ai/vectors/build-incremental", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		if embeddingService == nil {
			return apis.NewBadRequestError("Embedding service not initialized", nil)
		}

		userId := authRecord.Id

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Minute)
		defer cancel()

		result, err := embeddingService.BuildIncrementalVectors(ctx, userId)
		if err != nil {
			logger.Error("[POST /api/ai/vectors/build-incremental] error: %v", err)
			return apis.NewBadRequestError("Failed to build vectors: "+err.Error(), nil)
		}

		return c.JSON(http.StatusOK, result)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Get vector stats for user's diaries
	e.Router.GET("/api/ai/vectors/stats", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		if embeddingService == nil {
			return apis.NewBadRequestError("Embedding service not initialized", nil)
		}

		userId := authRecord.Id

		stats, err := embeddingService.GetVectorStats(c.Request().Context(), userId)
		if err != nil {
			logger.Error("[GET /api/ai/vectors/stats] error getting stats: %v", err)
			return apis.NewBadRequestError("Failed to get vector stats: "+err.Error(), nil)
		}

		return c.JSON(http.StatusOK, stats)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}

// fetchModels fetches available models from an OpenAI-compatible API
func fetchModels(baseURL, apiKey string) ([]ModelInfo, error) {
	// Normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")

	url := baseURL + "/v1/models"
	logger.Debug("[fetchModels] fetching models from: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return modelsResp.Data, nil
}
