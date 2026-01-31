package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/songtianlun/diaria/internal/config"
	"github.com/songtianlun/diaria/internal/embedding"
	"github.com/songtianlun/diaria/internal/logger"
)

// ChatService handles AI chat operations with RAG
type ChatService struct {
	app              *pocketbase.PocketBase
	embeddingService *embedding.EmbeddingService
	configService    *config.ConfigService
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to the chat API
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatStreamResponse represents a streaming response chunk
type ChatStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamWriter is an interface for writing streaming responses
type StreamWriter interface {
	Write([]byte) (int, error)
	Flush()
}

// NewChatService creates a new ChatService
func NewChatService(app *pocketbase.PocketBase, embeddingService *embedding.EmbeddingService) *ChatService {
	return &ChatService{
		app:              app,
		embeddingService: embeddingService,
		configService:    config.NewConfigService(app),
	}
}

// QueryRelevantDiaries retrieves diaries relevant to the query
func (s *ChatService) QueryRelevantDiaries(ctx context.Context, userID, query string, limit int) ([]embedding.DiarySearchResult, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service not available")
	}
	return s.embeddingService.QuerySimilar(ctx, userID, query, limit)
}

// buildSystemPrompt creates the system prompt with diary context
func (s *ChatService) buildSystemPrompt(diaries []embedding.DiarySearchResult) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful AI assistant for a personal diary application called Diaria. ")
	sb.WriteString("You help users reflect on their diary entries, summarize their experiences, ")
	sb.WriteString("and provide insights based on their personal journal.\n\n")

	if len(diaries) > 0 {
		sb.WriteString("Here are relevant diary entries from the user:\n\n")
		for i, diary := range diaries {
			sb.WriteString(fmt.Sprintf("--- Diary Entry %d (Date: %s) ---\n", i+1, diary.Date))
			if diary.Mood != "" {
				sb.WriteString(fmt.Sprintf("Mood: %s\n", diary.Mood))
			}
			if diary.Weather != "" {
				sb.WriteString(fmt.Sprintf("Weather: %s\n", diary.Weather))
			}
			sb.WriteString(fmt.Sprintf("Content:\n%s\n\n", diary.Content))
		}
		sb.WriteString("Use these diary entries to provide personalized and relevant responses. ")
		sb.WriteString("When referencing specific entries, mention the date.\n")
	} else {
		sb.WriteString("No relevant diary entries were found for this query. ")
		sb.WriteString("You can still help the user with general questions about journaling.\n")
	}

	return sb.String()
}

// GetConversationHistory retrieves message history for a conversation
func (s *ChatService) GetConversationHistory(conversationID string, limit int) ([]ChatMessage, error) {
	messages, err := s.app.Dao().FindRecordsByFilter(
		"ai_messages",
		"conversation = {:conv}",
		"created",
		limit,
		0,
		map[string]any{"conv": conversationID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	history := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		history = append(history, ChatMessage{
			Role:    msg.GetString("role"),
			Content: msg.GetString("content"),
		})
	}
	return history, nil
}

// SaveMessage saves a message to the database
func (s *ChatService) SaveMessage(userID, conversationID, role, content string, referencedDiaries []string) (*models.Record, error) {
	collection, err := s.app.Dao().FindCollectionByNameOrId("ai_messages")
	if err != nil {
		return nil, fmt.Errorf("failed to find messages collection: %w", err)
	}

	record := models.NewRecord(collection)
	record.Set("conversation", conversationID)
	record.Set("role", role)
	record.Set("content", content)
	record.Set("owner", userID)
	if len(referencedDiaries) > 0 {
		record.Set("referenced_diaries", referencedDiaries)
	}

	if err := s.app.Dao().SaveRecord(record); err != nil {
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	return record, nil
}

// StreamChat performs streaming chat with RAG context
func (s *ChatService) StreamChat(ctx context.Context, userID, conversationID, message string, writer StreamWriter) (string, []string, error) {
	logger.Info("[ChatService] starting stream chat for user: %s, conversation: %s", userID, conversationID)

	// Get AI configuration
	apiKey, err := s.configService.GetString(userID, "ai.api_key")
	if err != nil || apiKey == "" {
		return "", nil, fmt.Errorf("AI API key not configured")
	}

	baseURL, err := s.configService.GetString(userID, "ai.base_url")
	if err != nil || baseURL == "" {
		return "", nil, fmt.Errorf("AI base URL not configured")
	}

	chatModel, err := s.configService.GetString(userID, "ai.chat_model")
	if err != nil || chatModel == "" {
		return "", nil, fmt.Errorf("chat model not configured")
	}

	// Query relevant diaries
	var diaries []embedding.DiarySearchResult
	var referencedDiaryIDs []string
	if s.embeddingService != nil {
		diaries, err = s.embeddingService.QuerySimilar(ctx, userID, message, 5)
		if err != nil {
			logger.Warn("[ChatService] failed to query similar diaries: %v", err)
		} else {
			for _, d := range diaries {
				referencedDiaryIDs = append(referencedDiaryIDs, d.ID)
			}
		}
	}

	// Build messages
	messages := []ChatMessage{
		{Role: "system", Content: s.buildSystemPrompt(diaries)},
	}

	// Add conversation history
	history, err := s.GetConversationHistory(conversationID, 20)
	if err != nil {
		logger.Warn("[ChatService] failed to get conversation history: %v", err)
	} else {
		messages = append(messages, history...)
	}

	// Add current message
	messages = append(messages, ChatMessage{Role: "user", Content: message})

	// Call streaming API
	fullResponse, err := s.callStreamingAPI(ctx, baseURL, apiKey, chatModel, messages, writer)
	if err != nil {
		return "", nil, err
	}

	return fullResponse, referencedDiaryIDs, nil
}

// callStreamingAPI calls the OpenAI-compatible streaming API
func (s *ChatService) callStreamingAPI(ctx context.Context, baseURL, apiKey, model string, messages []ChatMessage, writer StreamWriter) (string, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	url := baseURL + "/v1/chat/completions"

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return s.processStreamResponse(resp.Body, writer)
}

// processStreamResponse processes the SSE stream and writes to the client
func (s *ChatService) processStreamResponse(body io.Reader, writer StreamWriter) (string, error) {
	scanner := bufio.NewScanner(body)
	var fullResponse strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp ChatStreamResponse
		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			logger.Warn("[ChatService] failed to parse stream chunk: %v", err)
			continue
		}

		if len(streamResp.Choices) > 0 {
			content := streamResp.Choices[0].Delta.Content
			if content != "" {
				fullResponse.WriteString(content)

				// Write SSE event to client
				sseData := map[string]string{"content": content}
				jsonData, _ := json.Marshal(sseData)
				writer.Write([]byte("data: " + string(jsonData) + "\n\n"))
				writer.Flush()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fullResponse.String(), fmt.Errorf("error reading stream: %w", err)
	}

	return fullResponse.String(), nil
}
