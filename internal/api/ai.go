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

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/chat"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

func RegisterAIRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, embeddingService *embedding.EmbeddingService) {
	configService := config.NewConfigService(s)
	chatService := chat.NewChatService(s, embeddingService)
	group := e.Group("/api/v1/ai", authMiddleware)

	group.GET("/settings", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		apiKey, _ := configService.GetString(userId, "ai.api_key")
		baseUrl, _ := configService.GetString(userId, "ai.base_url")
		chatModel, _ := configService.GetString(userId, "ai.chat_model")
		embeddingModel, _ := configService.GetString(userId, "ai.embedding_model")
		enabled, _ := configService.GetBool(userId, "ai.enabled")
		return c.JSON(http.StatusOK, map[string]any{"api_key": apiKey, "base_url": baseUrl, "chat_model": chatModel, "embedding_model": embeddingModel, "enabled": enabled})
	})

	group.PUT("/settings", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		var body struct {
			APIKey         string `json:"api_key"`
			BaseURL        string `json:"base_url"`
			ChatModel      string `json:"chat_model"`
			EmbeddingModel string `json:"embedding_model"`
			Enabled        bool   `json:"enabled"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.Enabled && (body.APIKey == "" || body.BaseURL == "" || body.ChatModel == "" || body.EmbeddingModel == "") {
			return badRequest("All AI settings must be configured before enabling AI features", nil)
		}
		settings := map[string]any{"ai.api_key": body.APIKey, "ai.base_url": body.BaseURL, "ai.chat_model": body.ChatModel, "ai.embedding_model": body.EmbeddingModel, "ai.enabled": body.Enabled}
		if err := configService.SetBatch(userId, settings); err != nil {
			return badRequest("Failed to save AI settings", err)
		}
		recordAudit(c, audit.ActionSettingsUpdate, "ai", map[string]any{"keys": settingKeys(settings), "enabled": body.Enabled})
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	group.POST("/models", func(c echo.Context) error {
		var body struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.APIKey == "" || body.BaseURL == "" {
			return badRequest("API key and base URL are required", nil)
		}
		models, err := fetchModels(body.BaseURL, body.APIKey)
		if err != nil {
			logger.Error("[POST /api/v1/ai/models] error fetching models: %v", err)
			return badRequest("Failed to fetch models: "+err.Error(), nil)
		}
		return c.JSON(http.StatusOK, map[string]any{"models": models})
	})

	group.POST("/vectors/build", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		if embeddingService == nil {
			return badRequest("Embedding service not initialized", nil)
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Minute)
		defer cancel()
		result, err := embeddingService.BuildAllVectors(ctx, userId)
		if err != nil {
			return badRequest("Failed to build vectors: "+err.Error(), nil)
		}
		return c.JSON(http.StatusOK, result)
	})

	group.POST("/vectors/build-incremental", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		if embeddingService == nil {
			return badRequest("Embedding service not initialized", nil)
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Minute)
		defer cancel()
		result, err := embeddingService.BuildIncrementalVectors(ctx, userId)
		if err != nil {
			return badRequest("Failed to build vectors: "+err.Error(), nil)
		}
		return c.JSON(http.StatusOK, result)
	})

	group.GET("/vectors/stats", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		if embeddingService == nil {
			return badRequest("Embedding service not initialized", nil)
		}
		stats, err := embeddingService.GetVectorStats(c.Request().Context(), userId)
		if err != nil {
			return badRequest("Failed to get vector stats: "+err.Error(), nil)
		}
		return c.JSON(http.StatusOK, stats)
	})

	group.GET("/conversations", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		conversations, err := s.ListConversations(userId, 100)
		if err != nil {
			return badRequest("Failed to fetch conversations", err)
		}
		result := make([]map[string]any, 0, len(conversations))
		for _, conv := range conversations {
			count, _ := chatService.GetConversationMessageCount(conv.ID)
			result = append(result, map[string]any{"id": conv.ID, "title": conv.Title, "created": conv.Created, "updated": conv.Updated, "message_count": count})
		}
		return c.JSON(http.StatusOK, result)
	})

	group.POST("/conversations", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		var body struct {
			Title string `json:"title"`
		}
		_ = c.Bind(&body)
		conv, err := s.CreateConversation(userId, body.Title)
		if err != nil {
			return badRequest("Failed to create conversation", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"id": conv.ID, "title": conv.Title, "created": conv.Created, "updated": conv.Updated})
	})

	group.GET("/conversations/:id", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		conv, err := s.GetConversation(c.PathParam("id"), userId)
		if err != nil {
			return notFound("Conversation not found")
		}
		messages, err := s.ListMessages(conv.ID, 500)
		if err != nil {
			return badRequest("Failed to fetch messages", err)
		}
		msgList := make([]map[string]any, 0, len(messages))
		for _, msg := range messages {
			msgList = append(msgList, map[string]any{"id": msg.ID, "role": msg.Role, "content": msg.Content, "referenced_diaries": msg.ReferencedDiaries, "created": msg.Created})
		}
		return c.JSON(http.StatusOK, map[string]any{"conversation": map[string]any{"id": conv.ID, "title": conv.Title, "created": conv.Created, "updated": conv.Updated}, "messages": msgList})
	})

	group.DELETE("/conversations/:id", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		if err := s.DeleteConversation(c.PathParam("id"), userId); err != nil {
			return notFound("Conversation not found")
		}
		recordAudit(c, audit.ActionConvDelete, c.PathParam("id"), nil)
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	group.PUT("/conversations/:id", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		var body struct {
			Title string `json:"title"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		conv, err := s.UpdateConversationTitle(c.PathParam("id"), userId, body.Title)
		if err != nil {
			return notFound("Conversation not found")
		}
		return c.JSON(http.StatusOK, map[string]any{"id": conv.ID, "title": conv.Title, "updated": conv.Updated})
	})

	group.POST("/conversations/:id/title", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		conv, err := s.GetConversation(c.PathParam("id"), userId)
		if err != nil {
			return notFound("Conversation not found")
		}
		messages, err := s.ListMessages(conv.ID, 4)
		if err != nil || len(messages) == 0 {
			return badRequest("No messages available for title generation", err)
		}
		var firstUser, firstAssistant string
		for _, m := range messages {
			if m.Role == "user" && firstUser == "" {
				firstUser = m.Content
			} else if m.Role == "assistant" && firstAssistant == "" {
				firstAssistant = m.Content
			}
		}
		if firstUser == "" {
			return badRequest("No user message found for title generation", nil)
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
		defer cancel()
		title, err := chatService.GenerateTitle(ctx, userId, firstUser, firstAssistant)
		if err != nil || strings.TrimSpace(title) == "" {
			return badRequest("Failed to generate title", err)
		}
		updated, err := s.UpdateConversationTitle(conv.ID, userId, title)
		if err != nil {
			return badRequest("Failed to save generated title", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"id": updated.ID, "title": updated.Title, "updated": updated.Updated})
	})

	group.DELETE("/conversations/:id/messages/:messageId", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		convID := c.PathParam("id")
		if _, err := s.GetConversation(convID, userId); err != nil {
			return notFound("Conversation not found")
		}
		deleted, err := s.DeleteMessagesFrom(userId, convID, c.PathParam("messageId"))
		if err != nil {
			return notFound("Message not found")
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "deleted": deleted})
	})

	group.POST("/chat", func(c echo.Context) error {
		userId := auth.CurrentUser(c).ID
		var body struct {
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
			Scope          string `json:"scope"`
			Regenerate     bool   `json:"regenerate"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.ConversationID == "" {
			return badRequest("conversation_id is required", nil)
		}
		if !body.Regenerate && strings.TrimSpace(body.Content) == "" {
			return badRequest("conversation_id and content are required", nil)
		}
		conv, err := s.GetConversation(body.ConversationID, userId)
		if err != nil {
			return notFound("Conversation not found")
		}

		var (
			userMessageID  string
			promptContent  = body.Content
			isFirstMessage bool
			currentTitle   = conv.Title
		)

		if body.Regenerate {
			recent, err := s.ListMessages(body.ConversationID, 20)
			if err != nil || len(recent) == 0 {
				return badRequest("No message to regenerate", err)
			}
			last := recent[len(recent)-1]
			if last.Role != "user" {
				return badRequest("Last message must be a user message to regenerate", nil)
			}
			userMessageID = last.ID
			promptContent = last.Content
		} else {
			messageCount, _ := chatService.GetConversationMessageCount(body.ConversationID)
			isFirstMessage = messageCount == 0
			userMsg, err := chatService.SaveMessage(userId, body.ConversationID, "user", body.Content, nil)
			if err != nil {
				logger.Error("[POST /api/v1/ai/chat] failed to save user message: %v", err)
			} else {
				userMessageID = userMsg.ID
				logger.Info("[POST /api/v1/ai/chat] saved user message: %s", userMsg.ID)
			}
		}

		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().Header().Set("X-Accel-Buffering", "no")
		c.Response().WriteHeader(http.StatusOK)
		writer := &sseWriter{w: c.Response()}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Minute)
		defer cancel()

		var newTitle string
		if isFirstMessage && currentTitle == "" {
			title, err := chatService.GenerateTitleFromUserMessage(ctx, userId, promptContent)
			if err == nil {
				newTitle = title
				if err := chatService.UpdateConversationTitle(body.ConversationID, title); err == nil {
					titleData, _ := json.Marshal(map[string]any{"type": "title", "title": newTitle})
					_, _ = writer.Write([]byte("data: " + string(titleData) + "\n\n"))
					writer.Flush()
				}
			}
		}

		var references []chat.DiaryReference
		fullResponse, referencedDiaries, err := chatService.StreamChatWithOptions(ctx, userId, body.ConversationID, promptContent, writer, chat.ChatOptions{
			Scope:            body.Scope,
			ExcludeMessageID: userMessageID,
			ReferencesOut:    &references,
		})
		if err != nil {
			// If the client cancelled mid-stream after partial text arrived, persist it.
			if ctx.Err() == context.Canceled && strings.TrimSpace(fullResponse) != "" {
				if _, saveErr := chatService.SaveMessage(userId, body.ConversationID, "assistant", fullResponse, referencedDiaries); saveErr != nil {
					logger.Error("[POST /api/v1/ai/chat] failed to save partial assistant message: %v", saveErr)
				}
				return nil
			}
			logger.Error("[POST /api/v1/ai/chat] stream chat error: %v", err)
			errData, _ := json.Marshal(map[string]any{"type": "error", "error": err.Error()})
			_, _ = writer.Write([]byte("data: " + string(errData) + "\n\n"))
			writer.Flush()
			return nil
		}

		var assistantMessageID string
		assistantMsg, err := chatService.SaveMessage(userId, body.ConversationID, "assistant", fullResponse, referencedDiaries)
		if err != nil {
			logger.Error("[POST /api/v1/ai/chat] failed to save assistant message: %v", err)
		} else {
			assistantMessageID = assistantMsg.ID
			logger.Info("[POST /api/v1/ai/chat] saved assistant message: %s", assistantMsg.ID)
		}
		doneData, _ := json.Marshal(map[string]any{
			"type":               "done",
			"done":               true,
			"message_id":         assistantMessageID,
			"user_message_id":    userMessageID,
			"referenced_diaries": referencedDiaries,
			"references":         references,
			"title":              newTitle,
		})
		_, _ = writer.Write([]byte("data: " + string(doneData) + "\n\n"))
		writer.Flush()
		return nil
	})
}

func fetchModels(baseURL, apiKey string) ([]ModelInfo, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	url := baseURL + "/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
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

type sseWriter struct{ w http.ResponseWriter }

func (s *sseWriter) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *sseWriter) Flush() {
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}
