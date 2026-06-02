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
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
)

// ChatService handles AI chat operations with RAG
type ChatService struct {
	app              *pocketbase.PocketBase
	embeddingService *embedding.EmbeddingService
	configService    *config.ConfigService
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call from the model
type ToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// Tool represents a function tool definition
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents the function definition
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// SearchDiariesArgs represents arguments for search_diaries function
type SearchDiariesArgs struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Query     string `json:"query,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// ChatRequest represents a request to the chat API
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
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
			Role      string     `json:"role,omitempty"`
			Content   string     `json:"content,omitempty"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
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

// getTools returns the available tools for the chat
func (s *ChatService) getTools() []Tool {
	return []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_diaries",
				Description: "搜索用户的日记。可以按时间范围筛选，也可以按语义相似度搜索。用于回答关于用户日记内容的问题，如总结、回顾、分析等。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"start_date": map[string]interface{}{
							"type":        "string",
							"description": "开始日期，格式 YYYY-MM-DD。用于筛选该日期之后的日记。",
						},
						"end_date": map[string]interface{}{
							"type":        "string",
							"description": "结束日期，格式 YYYY-MM-DD。用于筛选该日期之前的日记。",
						},
						"query": map[string]interface{}{
							"type":        "string",
							"description": "语义搜索关键词。用于查找与该主题相关的日记。",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "返回的最大日记数量，默认10，最大100。",
						},
					},
					"required": []string{},
				},
			},
		},
	}
}

// QueryRelevantDiaries retrieves diaries relevant to the query
func (s *ChatService) QueryRelevantDiaries(ctx context.Context, userID, query string, limit int) ([]embedding.DiarySearchResult, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service not available")
	}
	return s.embeddingService.QuerySimilar(ctx, userID, query, limit)
}

// SearchDiariesByDateRange searches diaries within a date range
func (s *ChatService) SearchDiariesByDateRange(ctx context.Context, userID string, args SearchDiariesArgs) ([]embedding.DiarySearchResult, error) {
	logger.Info("[ChatService] searching diaries: startDate=%s, endDate=%s, query=%s, limit=%d",
		args.StartDate, args.EndDate, args.Query, args.Limit)

	// Set default limit
	if args.Limit <= 0 {
		args.Limit = 10
	}
	if args.Limit > 100 {
		args.Limit = 100
	}

	// Build filter conditions
	filter := "owner = {:owner}"
	filterParams := map[string]any{"owner": userID}

	if args.StartDate != "" {
		filter += " && date >= {:start_date}"
		filterParams["start_date"] = args.StartDate
	}
	if args.EndDate != "" {
		filter += " && date <= {:end_date}"
		filterParams["end_date"] = args.EndDate + " 23:59:59"
	}

	// Query from database
	diaries, err := s.app.Dao().FindRecordsByFilter(
		"diaries",
		filter,
		"-date",
		args.Limit,
		0,
		filterParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diaries: %w", err)
	}

	// Convert to DiarySearchResult
	results := make([]embedding.DiarySearchResult, 0, len(diaries))
	for _, diary := range diaries {
		dateStr := diary.GetString("date")
		if len(dateStr) >= 10 {
			dateStr = dateStr[:10]
		}
		results = append(results, embedding.DiarySearchResult{
			ID:      diary.GetId(),
			Date:    dateStr,
			Content: diary.GetString("content"),
			Mood:    diary.GetString("mood"),
			Weather: diary.GetString("weather"),
		})
	}

	// If query is provided, use vector search to re-rank or filter
	if args.Query != "" && s.embeddingService != nil {
		// Use semantic search within the date range results
		semanticResults, err := s.embeddingService.QuerySimilar(ctx, userID, args.Query, args.Limit)
		if err != nil {
			logger.Warn("[ChatService] semantic search failed, using date-filtered results: %v", err)
		} else if len(semanticResults) > 0 {
			// Filter semantic results to only include those in date range
			dateFilteredIDs := make(map[string]bool)
			for _, r := range results {
				dateFilteredIDs[r.ID] = true
			}

			filtered := make([]embedding.DiarySearchResult, 0)
			for _, sr := range semanticResults {
				if dateFilteredIDs[sr.ID] {
					filtered = append(filtered, sr)
				}
			}
			if len(filtered) > 0 {
				results = filtered
			}
		}
	}

	logger.Info("[ChatService] found %d diaries", len(results))
	return results, nil
}

// buildSystemPrompt creates the system prompt with diary context
func (s *ChatService) buildSystemPrompt(diaries []embedding.DiarySearchResult) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful AI assistant for a personal diary application called Diarum. ")
	sb.WriteString("You help users reflect on their diary entries, summarize their experiences, ")
	sb.WriteString("and provide insights based on their personal journal.\n\n")

	if len(diaries) > 0 {
		sb.WriteString("Here are relevant diary entries from the user:\n\n")
		for i, diary := range diaries {
			// Parse date and get weekday
			weekday := ""
			if t, err := time.Parse("2006-01-02", diary.Date); err == nil {
				weekday = t.Weekday().String()
			}
			sb.WriteString(fmt.Sprintf("--- Diary Entry %d (Date: %s, %s) ---\n", i+1, diary.Date, weekday))
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

// buildAgentSystemPrompt creates the system prompt for the agent with tools
func (s *ChatService) buildAgentSystemPrompt() string {
	today := time.Now().Format("2006-01-02")
	return fmt.Sprintf(`You are a helpful AI assistant for a personal diary application called Diarum.
You help users reflect on their diary entries, summarize their experiences, and provide insights based on their personal journal.

Today's date is: %s

You have access to the search_diaries tool to find relevant diary entries. Use it when:
- User asks about their diary content, memories, or experiences
- User wants a summary or analysis of a time period (e.g., "summarize this month", "what happened last week")
- User asks about specific topics they may have written about

When using search_diaries:
- For time-based queries (e.g., "this year", "last month"), set appropriate start_date and end_date
- For topic-based queries (e.g., "about travel"), use the query parameter
- Adjust limit based on the scope: use higher limits (30-50) for summaries, lower (5-10) for specific questions

Always reference specific dates when discussing diary entries. Respond in the same language as the user.`, today)
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

	// Build initial messages with system prompt
	systemPrompt := s.buildAgentSystemPrompt()
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
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

	// Get tools
	tools := s.getTools()

	// Call API with tools (first round)
	var referencedDiaryIDs []string
	fullResponse, toolCalls, err := s.callAPIWithTools(ctx, baseURL, apiKey, chatModel, messages, tools, writer)
	if err != nil {
		return "", nil, err
	}

	// Handle tool calls if any
	if len(toolCalls) > 0 {
		// Process tool calls and collect results
		var toolResults []string
		for _, tc := range toolCalls {
			if tc.Function.Name == "search_diaries" {
				var args SearchDiariesArgs
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					logger.Error("[ChatService] failed to parse tool arguments: %v", err)
					continue
				}

				// Execute search
				diaries, err := s.SearchDiariesByDateRange(ctx, userID, args)
				if err != nil {
					logger.Error("[ChatService] search_diaries failed: %v", err)
					continue
				}

				// Collect diary IDs
				for _, d := range diaries {
					referencedDiaryIDs = append(referencedDiaryIDs, d.ID)
				}

				// Build tool result
				toolResult := s.formatDiariesForContext(diaries)
				toolResults = append(toolResults, toolResult)
			}
		}

		// Build new messages with tool results as context
		// Use a simpler format that's more compatible with various models
		if len(toolResults) > 0 {
			contextMsg := "Based on the diary search results:\n\n" + strings.Join(toolResults, "\n\n")
			messages = append(messages, ChatMessage{
				Role:    "user",
				Content: contextMsg + "\n\nPlease summarize the above diary entries for me.",
			})
		}

		// Call API again without tools to get final response
		fullResponse, _, err = s.callAPIWithTools(ctx, baseURL, apiKey, chatModel, messages, nil, writer)
		if err != nil {
			return "", nil, err
		}
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

// callAPIWithTools calls the API with tool support
func (s *ChatService) callAPIWithTools(ctx context.Context, baseURL, apiKey, model string, messages []ChatMessage, tools []Tool, writer StreamWriter) (string, []ToolCall, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	url := baseURL + "/v1/chat/completions"

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Debug log the request
	logger.Debug("[ChatService] API request: %s", string(jsonBody))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return s.processStreamResponseWithTools(resp.Body, writer)
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

// processStreamResponseWithTools processes SSE stream with tool call support
func (s *ChatService) processStreamResponseWithTools(body io.Reader, writer StreamWriter) (string, []ToolCall, error) {
	scanner := bufio.NewScanner(body)
	var fullResponse strings.Builder
	toolCallsMap := make(map[int]*ToolCall) // Use index as key

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
			choice := streamResp.Choices[0]

			// Handle content
			if choice.Delta.Content != "" {
				fullResponse.WriteString(choice.Delta.Content)
				sseData := map[string]string{"content": choice.Delta.Content}
				jsonData, _ := json.Marshal(sseData)
				writer.Write([]byte("data: " + string(jsonData) + "\n\n"))
				writer.Flush()
			}

			// Handle tool calls - accumulate by index
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				if _, exists := toolCallsMap[idx]; !exists {
					toolCallsMap[idx] = &ToolCall{Index: idx}
				}
				toolCall := toolCallsMap[idx]
				if tc.ID != "" {
					toolCall.ID = tc.ID
				}
				if tc.Type != "" {
					toolCall.Type = tc.Type
				}
				if tc.Function.Name != "" {
					toolCall.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					toolCall.Function.Arguments += tc.Function.Arguments
				}
			}
		}
	}

	// Convert map to slice
	toolCalls := make([]ToolCall, 0, len(toolCallsMap))
	for i := 0; i < len(toolCallsMap); i++ {
		if tc, exists := toolCallsMap[i]; exists {
			toolCalls = append(toolCalls, *tc)
		}
	}

	if err := scanner.Err(); err != nil {
		return fullResponse.String(), toolCalls, fmt.Errorf("error reading stream: %w", err)
	}

	logger.Debug("[ChatService] parsed %d tool calls", len(toolCalls))
	for i, tc := range toolCalls {
		logger.Debug("[ChatService] tool call %d: id=%s, name=%s, args=%s", i, tc.ID, tc.Function.Name, tc.Function.Arguments)
	}

	return fullResponse.String(), toolCalls, nil
}

// formatDiariesForContext formats diaries for tool result context
func (s *ChatService) formatDiariesForContext(diaries []embedding.DiarySearchResult) string {
	if len(diaries) == 0 {
		return "No diary entries found for the specified criteria."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d diary entries:\n\n", len(diaries)))

	for i, diary := range diaries {
		// Parse date and get weekday
		weekday := ""
		if t, err := time.Parse("2006-01-02", diary.Date); err == nil {
			weekday = t.Weekday().String()
		}
		sb.WriteString(fmt.Sprintf("--- Diary Entry %d (Date: %s, %s) ---\n", i+1, diary.Date, weekday))
		if diary.Mood != "" {
			sb.WriteString(fmt.Sprintf("Mood: %s\n", diary.Mood))
		}
		if diary.Weather != "" {
			sb.WriteString(fmt.Sprintf("Weather: %s\n", diary.Weather))
		}
		sb.WriteString(fmt.Sprintf("Content:\n%s\n\n", diary.Content))
	}

	return sb.String()
}

// GenerateTitleFromUserMessage generates a title based only on the user's message
// Uses simple text extraction instead of AI to ensure compatibility with all models
func (s *ChatService) GenerateTitleFromUserMessage(ctx context.Context, userID, userMessage string) (string, error) {
	// Simple approach: extract title from user message directly
	title := extractTitleFromMessage(userMessage)
	if title == "" {
		return "", fmt.Errorf("could not extract title from message")
	}
	return title, nil
}

// extractTitleFromMessage extracts a short title from the user's message
func extractTitleFromMessage(message string) string {
	// Remove HTML tags if any
	message = stripHTMLTags(message)

	// Replace newlines with spaces
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")

	// Collapse multiple spaces
	for strings.Contains(message, "  ") {
		message = strings.ReplaceAll(message, "  ", " ")
	}

	// Trim whitespace
	message = strings.TrimSpace(message)

	if message == "" {
		return ""
	}

	// Limit to 50 characters
	maxLen := 50
	if len(message) <= maxLen {
		return message
	}

	// Try to cut at word boundary
	title := message[:maxLen]
	lastSpace := strings.LastIndex(title, " ")
	if lastSpace > maxLen/2 {
		title = title[:lastSpace]
	}

	return strings.TrimSpace(title) + "..."
}

// stripHTMLTags removes HTML tags from a string
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// GenerateTitle generates a title for a conversation based on the first message
func (s *ChatService) GenerateTitle(ctx context.Context, userID, userMessage, assistantResponse string) (string, error) {
	// Get AI configuration
	apiKey, err := s.configService.GetString(userID, "ai.api_key")
	if err != nil || apiKey == "" {
		return "", fmt.Errorf("AI API key not configured")
	}

	baseURL, err := s.configService.GetString(userID, "ai.base_url")
	if err != nil || baseURL == "" {
		return "", fmt.Errorf("AI base URL not configured")
	}

	chatModel, err := s.configService.GetString(userID, "ai.chat_model")
	if err != nil || chatModel == "" {
		return "", fmt.Errorf("chat model not configured")
	}

	// Build messages for title generation
	messages := []ChatMessage{
		{
			Role: "system",
			Content: `Generate a short, concise title (max 50 characters) for this conversation based on the user's message and assistant's response.
The title should capture the main topic or intent of the conversation.
Respond with ONLY the title, no quotes, no explanation, no punctuation at the end.
Use the same language as the user's message.`,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("User message: %s\n\nAssistant response: %s", userMessage, truncateString(assistantResponse, 500)),
		},
	}

	// Call API without streaming
	baseURL = strings.TrimSuffix(baseURL, "/")
	url := baseURL + "/v1/chat/completions"

	reqBody := map[string]interface{}{
		"model":      chatModel,
		"messages":   messages,
		"max_tokens": 60,
		"stream":     false,
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	title := strings.TrimSpace(result.Choices[0].Message.Content)
	// Ensure title is not too long
	if len(title) > 100 {
		title = title[:100]
	}

	return title, nil
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetConversationMessageCount returns the number of messages in a conversation
func (s *ChatService) GetConversationMessageCount(conversationID string) (int, error) {
	messages, err := s.app.Dao().FindRecordsByFilter(
		"ai_messages",
		"conversation = {:conv}",
		"",
		0,
		0,
		map[string]any{"conv": conversationID},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}
	return len(messages), nil
}

// UpdateConversationTitle updates the title of a conversation
func (s *ChatService) UpdateConversationTitle(conversationID, title string) error {
	conv, err := s.app.Dao().FindRecordById("ai_conversations", conversationID)
	if err != nil {
		return fmt.Errorf("failed to find conversation: %w", err)
	}

	conv.Set("title", title)
	if err := s.app.Dao().SaveRecord(conv); err != nil {
		return fmt.Errorf("failed to update conversation: %w", err)
	}

	return nil
}
