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

	"github.com/songtianlun/diaria/internal/chat"
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

	// Initialize chat service
	chatService := chat.NewChatService(app, embeddingService)

	// Get all conversations for user
	e.Router.GET("/api/ai/conversations", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		conversations, err := app.Dao().FindRecordsByFilter(
			"ai_conversations",
			"owner = {:owner}",
			"-updated",
			100,
			0,
			map[string]any{"owner": authRecord.Id},
		)
		if err != nil {
			return apis.NewBadRequestError("Failed to fetch conversations", err)
		}

		result := make([]map[string]any, 0, len(conversations))
		for _, conv := range conversations {
			result = append(result, map[string]any{
				"id":      conv.Id,
				"title":   conv.GetString("title"),
				"created": conv.Created.String(),
				"updated": conv.Updated.String(),
			})
		}

		return c.JSON(http.StatusOK, result)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Create new conversation
	e.Router.POST("/api/ai/conversations", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		var body struct {
			Title string `json:"title"`
		}
		c.Bind(&body)

		collection, err := app.Dao().FindCollectionByNameOrId("ai_conversations")
		if err != nil {
			return apis.NewBadRequestError("Failed to find conversations collection", err)
		}

		record := models.NewRecord(collection)
		record.Set("title", body.Title)
		record.Set("owner", authRecord.Id)

		if err := app.Dao().SaveRecord(record); err != nil {
			return apis.NewBadRequestError("Failed to create conversation", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"id":      record.Id,
			"title":   record.GetString("title"),
			"created": record.Created.String(),
			"updated": record.Updated.String(),
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Get conversation with messages
	e.Router.GET("/api/ai/conversations/:id", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		convID := c.PathParam("id")
		conv, err := app.Dao().FindRecordById("ai_conversations", convID)
		if err != nil {
			return apis.NewNotFoundError("Conversation not found", err)
		}

		if conv.GetString("owner") != authRecord.Id {
			return apis.NewForbiddenError("Access denied", nil)
		}

		messages, err := app.Dao().FindRecordsByFilter(
			"ai_messages",
			"conversation = {:conv}",
			"created",
			100,
			0,
			map[string]any{"conv": convID},
		)
		if err != nil {
			return apis.NewBadRequestError("Failed to fetch messages", err)
		}

		msgList := make([]map[string]any, 0, len(messages))
		for _, msg := range messages {
			msgList = append(msgList, map[string]any{
				"id":                  msg.Id,
				"role":                msg.GetString("role"),
				"content":             msg.GetString("content"),
				"referenced_diaries":  msg.Get("referenced_diaries"),
				"created":             msg.Created.String(),
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"conversation": map[string]any{
				"id":      conv.Id,
				"title":   conv.GetString("title"),
				"created": conv.Created.String(),
				"updated": conv.Updated.String(),
			},
			"messages": msgList,
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Delete conversation
	e.Router.DELETE("/api/ai/conversations/:id", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		convID := c.PathParam("id")
		conv, err := app.Dao().FindRecordById("ai_conversations", convID)
		if err != nil {
			return apis.NewNotFoundError("Conversation not found", err)
		}

		if conv.GetString("owner") != authRecord.Id {
			return apis.NewForbiddenError("Access denied", nil)
		}

		if err := app.Dao().DeleteRecord(conv); err != nil {
			return apis.NewBadRequestError("Failed to delete conversation", err)
		}

		return c.JSON(http.StatusOK, map[string]any{"success": true})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Update conversation title
	e.Router.PUT("/api/ai/conversations/:id", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		convID := c.PathParam("id")
		conv, err := app.Dao().FindRecordById("ai_conversations", convID)
		if err != nil {
			return apis.NewNotFoundError("Conversation not found", err)
		}

		if conv.GetString("owner") != authRecord.Id {
			return apis.NewForbiddenError("Access denied", nil)
		}

		var body struct {
			Title string `json:"title"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		conv.Set("title", body.Title)
		if err := app.Dao().SaveRecord(conv); err != nil {
			return apis.NewBadRequestError("Failed to update conversation", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"id":      conv.Id,
			"title":   conv.GetString("title"),
			"updated": conv.Updated.String(),
		})
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	// Streaming chat endpoint
	e.Router.POST("/api/ai/chat", func(c echo.Context) error {
		authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if authRecord == nil {
			return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
		}

		var body struct {
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		if body.ConversationID == "" || body.Content == "" {
			return apis.NewBadRequestError("conversation_id and content are required", nil)
		}

		// Verify conversation ownership
		conv, err := app.Dao().FindRecordById("ai_conversations", body.ConversationID)
		if err != nil {
			return apis.NewNotFoundError("Conversation not found", err)
		}
		if conv.GetString("owner") != authRecord.Id {
			return apis.NewForbiddenError("Access denied", nil)
		}

		// Save user message first
		_, err = chatService.SaveMessage(authRecord.Id, body.ConversationID, "user", body.Content, nil)
		if err != nil {
			logger.Error("[POST /api/ai/chat] failed to save user message: %v", err)
		}

		// Set SSE headers
		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().WriteHeader(http.StatusOK)

		// Create stream writer
		writer := &sseWriter{w: c.Response()}

		// Stream chat response
		ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Minute)
		defer cancel()

		fullResponse, referencedDiaries, err := chatService.StreamChat(ctx, authRecord.Id, body.ConversationID, body.Content, writer)
		if err != nil {
			logger.Error("[POST /api/ai/chat] stream chat error: %v", err)
			errData, _ := json.Marshal(map[string]string{"error": err.Error()})
			writer.Write([]byte("data: " + string(errData) + "\n\n"))
			writer.Flush()
			return nil
		}

		// Save assistant message
		_, err = chatService.SaveMessage(authRecord.Id, body.ConversationID, "assistant", fullResponse, referencedDiaries)
		if err != nil {
			logger.Error("[POST /api/ai/chat] failed to save assistant message: %v", err)
		}

		// Send done event
		doneData, _ := json.Marshal(map[string]any{
			"done":                true,
			"referenced_diaries":  referencedDiaries,
		})
		writer.Write([]byte("data: " + string(doneData) + "\n\n"))
		writer.Flush()

		return nil
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

// sseWriter wraps http.ResponseWriter for SSE streaming
type sseWriter struct {
	w http.ResponseWriter
}

func (s *sseWriter) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

func (s *sseWriter) Flush() {
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}
