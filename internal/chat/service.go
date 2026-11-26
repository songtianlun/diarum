package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	maxToolRounds          = 4
	maxContextBudgetRunes  = 24000
	sseScannerMaxTokenSize = 2 * 1024 * 1024
)

// ChatService handles AI chat operations with RAG and diary tools.
type ChatService struct {
	store            *store.Store
	embeddingService *embedding.EmbeddingService
	configService    *config.ConfigService
}

// ChatMessage represents a message in the chat completion payload.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call from the model.
type ToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// Tool represents a function tool definition.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents the function definition.
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// SearchDiariesArgs represents arguments for search_diaries.
type SearchDiariesArgs struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Query     string `json:"query,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// GetDiaryArgs represents arguments for get_diary.
type GetDiaryArgs struct {
	Date string `json:"date"`
}

// ListDiariesArgs represents arguments for list_diaries.
type ListDiariesArgs struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Mood      string `json:"mood,omitempty"`
	Weather   string `json:"weather,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// KeywordSearchArgs represents arguments for search_diaries_keyword.
type KeywordSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// DiaryReference is lightweight reference metadata streamed to the client.
type DiaryReference struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Mood    string `json:"mood,omitempty"`
	Weather string `json:"weather,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// ChatOptions configures a single chat turn.
type ChatOptions struct {
	// Scope restricts how the assistant accesses diaries:
	// "" or "all" (default), "this_month", "this_year", or "none" (pure chat).
	Scope string
	// ExcludeMessageID skips a message (typically the just-saved user message)
	// when loading conversation history so it is never sent twice.
	ExcludeMessageID string
	// ReferencesOut, when non-nil, receives deduplicated diary references used.
	ReferencesOut *[]DiaryReference
}

// ChatRequest represents a request to the OpenAI-compatible chat API.
type ChatRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []Tool        `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream"`
}

// ChatStreamResponse represents a streaming response chunk.
type ChatStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string     `json:"role,omitempty"`
			Content          string     `json:"content,omitempty"`
			ReasoningContent string     `json:"reasoning_content,omitempty"`
			Reasoning        string     `json:"reasoning,omitempty"`
			ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamWriter is an interface for writing streaming responses.
type StreamWriter interface {
	Write([]byte) (int, error)
	Flush()
}

// NewChatService creates a new ChatService.
func NewChatService(store *store.Store, embeddingService *embedding.EmbeddingService) *ChatService {
	return &ChatService{
		store:            store,
		embeddingService: embeddingService,
		configService:    config.NewConfigService(store),
	}
}

func writeSSEEvent(writer StreamWriter, payload map[string]any) {
	if writer == nil {
		return
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = writer.Write([]byte("data: " + string(jsonData) + "\n\n"))
	writer.Flush()
}

// getTools returns the available diary tools for the agent.
func (s *ChatService) getTools() []Tool {
	return []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_diaries",
				Description: "Search the user's diaries by semantic similarity and/or date range. Use this for topic questions, reflections, memories, or summarizing a period.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"start_date": map[string]interface{}{
							"type":        "string",
							"description": "Inclusive start date in YYYY-MM-DD format.",
						},
						"end_date": map[string]interface{}{
							"type":        "string",
							"description": "Inclusive end date in YYYY-MM-DD format.",
						},
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Semantic search query describing the topic, feeling, or event.",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of diary entries to return (default 10, max 50).",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_diary",
				Description: "Read the exact diary entry for a specific date (YYYY-MM-DD).",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"date": map[string]interface{}{
							"type":        "string",
							"description": "Exact date in YYYY-MM-DD format.",
						},
					},
					"required": []string{"date"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "list_diaries",
				Description: "List diary entries within a date range (newest first), optionally filtered by mood or weather. Ideal for weekly/monthly reviews or mood analysis.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"start_date": map[string]interface{}{
							"type":        "string",
							"description": "Inclusive start date in YYYY-MM-DD format.",
						},
						"end_date": map[string]interface{}{
							"type":        "string",
							"description": "Inclusive end date in YYYY-MM-DD format.",
						},
						"mood": map[string]interface{}{
							"type":        "string",
							"description": "Optional mood filter (case-insensitive substring match).",
						},
						"weather": map[string]interface{}{
							"type":        "string",
							"description": "Optional weather filter (case-insensitive substring match).",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of diary entries to return (default 15, max 50).",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_diaries_keyword",
				Description: "Exact keyword/substring search across all diary entries. Best for finding specific names, places, book titles, or exact phrases.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Exact keyword or phrase to search for.",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of diary entries to return (default 15, max 50).",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_days",
				Description: "List the user's tracked important dates, countdowns, anniversaries, and their milestones.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"include_milestones": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether to include milestones for each tracked day (default true).",
						},
					},
				},
			},
		},
	}
}

// QueryRelevantDiaries retrieves diaries relevant to the query.
func (s *ChatService) QueryRelevantDiaries(ctx context.Context, userID, query string, limit int) ([]embedding.DiarySearchResult, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service not available")
	}
	return s.embeddingService.QuerySimilar(ctx, userID, query, limit)
}

func diaryToSearchResult(d *store.Diary) embedding.DiarySearchResult {
	dateStr := d.Date
	if len(dateStr) >= 10 {
		dateStr = dateStr[:10]
	}
	return embedding.DiarySearchResult{
		ID:      d.ID,
		Date:    dateStr,
		Content: d.Content,
		Mood:    d.Mood,
		Weather: d.Weather,
	}
}

func inDateBounds(date, startDate, endDate string) bool {
	if len(date) >= 10 {
		date = date[:10]
	}
	if startDate != "" && date < startDate {
		return false
	}
	if endDate != "" && date > endDate {
		return false
	}
	return true
}

// SearchDiariesByDateRange searches diaries using semantic search (when query
// is provided), keyword fallback, and optional date range filtering.
func (s *ChatService) SearchDiariesByDateRange(ctx context.Context, userID string, args SearchDiariesArgs) ([]embedding.DiarySearchResult, error) {
	logger.Info("[ChatService] searching diaries: startDate=%s, endDate=%s, query=%s, limit=%d",
		args.StartDate, args.EndDate, args.Query, args.Limit)

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	query := strings.TrimSpace(args.Query)
	if query != "" {
		results := make([]embedding.DiarySearchResult, 0, limit)
		seen := make(map[string]bool)

		// 1. Semantic search over the full vector index, then apply date filter.
		if s.embeddingService != nil {
			poolLimit := limit * 3
			if poolLimit < 30 {
				poolLimit = 30
			}
			semanticResults, err := s.embeddingService.QuerySimilar(ctx, userID, query, poolLimit)
			if err != nil {
				logger.Warn("[ChatService] semantic search failed, falling back to keyword/date search: %v", err)
			} else {
				for _, sr := range semanticResults {
					if !inDateBounds(sr.Date, args.StartDate, args.EndDate) {
						continue
					}
					if seen[sr.ID] {
						continue
					}
					seen[sr.ID] = true
					results = append(results, sr)
					if len(results) >= limit {
						break
					}
				}
			}
		}

		// 2. Supplement with exact keyword search if we still have room.
		if len(results) < limit {
			kwDiaries, err := s.store.SearchDiaries(userID, query, limit*2)
			if err != nil {
				logger.Warn("[ChatService] keyword search failed: %v", err)
			} else {
				for _, d := range kwDiaries {
					sr := diaryToSearchResult(d)
					if !inDateBounds(sr.Date, args.StartDate, args.EndDate) || seen[sr.ID] {
						continue
					}
					seen[sr.ID] = true
					results = append(results, sr)
					if len(results) >= limit {
						break
					}
				}
			}
		}

		if len(results) > 0 {
			logger.Info("[ChatService] found %d diaries for query", len(results))
			return results, nil
		}
	}

	// Pure date-range query (or fallback when query had no semantic/keyword hits).
	start := ""
	end := ""
	if args.StartDate != "" {
		start = args.StartDate + " 00:00:00.000Z"
	}
	if args.EndDate != "" {
		end = args.EndDate + " 23:59:59.999Z"
	}

	diaries, err := s.store.ListDiaries(userID, start, end, "-date", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diaries: %w", err)
	}

	results := make([]embedding.DiarySearchResult, 0, len(diaries))
	for _, diary := range diaries {
		results = append(results, diaryToSearchResult(diary))
	}

	logger.Info("[ChatService] found %d diaries", len(results))
	return results, nil
}

// buildSystemPrompt creates a system prompt with pre-fetched diary context.
func (s *ChatService) buildSystemPrompt(diaries []embedding.DiarySearchResult) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful AI assistant for a personal diary application called Diarum. ")
	sb.WriteString("You help users reflect on their diary entries, summarize their experiences, ")
	sb.WriteString("and provide insights based on their personal journal.\n\n")

	if len(diaries) > 0 {
		sb.WriteString("Here are relevant diary entries from the user:\n\n")
		sb.WriteString(s.formatDiariesForContext(diaries))
		sb.WriteString("Use these diary entries to provide personalized and relevant responses. ")
		sb.WriteString("When referencing specific entries, mention the exact date in YYYY-MM-DD format.\n")
	} else {
		sb.WriteString("No relevant diary entries were found for this query. ")
		sb.WriteString("You can still help the user with general questions about journaling.\n")
	}

	return sb.String()
}

// buildAgentSystemPrompt creates the system prompt for the agent with tools.
func (s *ChatService) buildAgentSystemPrompt() string {
	return s.buildAgentSystemPromptWithScope("")
}

func (s *ChatService) buildAgentSystemPromptWithScope(scope string) string {
	return s.buildAgentSystemPromptWithContext(scope, nil, "", false)
}

func (s *ChatService) buildAgentSystemPromptWithContext(scope string, prefetchedDiaries []embedding.DiarySearchResult, prefetchedDays string, didPrefetchDiaries bool) string {
	now := time.Now()
	today := now.Format("2006-01-02")
	weekday := now.Weekday().String()

	if scope == "none" {
		return fmt.Sprintf(`You are a thoughtful, warm AI assistant inside Diarum, a personal diary app.
Today's date is: %s (%s).
The user selected General Chat mode without diary search. Answer directly, concisely, and empathetically in the same language as the user. Use clean Markdown formatting when helpful.`, today, weekday)
	}

	scopeHint := ""
	switch scope {
	case "this_month":
		monthStart := now.Format("2006-01") + "-01"
		scopeHint = fmt.Sprintf("\nContext scope preference: Focus on diary entries from this month (%s to %s) unless the user explicitly asks about another date.", monthStart, today)
	case "this_year":
		yearStart := now.Format("2006") + "-01-01"
		scopeHint = fmt.Sprintf("\nContext scope preference: Focus on diary entries from this year (%s to %s) unless the user explicitly asks about another date.", yearStart, today)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`You are a thoughtful, empathetic AI assistant for a personal diary application called Diarum.
You help the user reflect on their diary entries, recall memories, spot mood/habit patterns, and organize their thoughts.

Today's date is: %s (%s).%s

Available tools:
- search_diaries: Semantic + keyword search with optional start_date/end_date. Best for topics, memories, feelings, or open-ended questions.
- get_diary: Read the exact diary entry for a single YYYY-MM-DD date.
- list_diaries: List diaries in a date range (optional mood/weather filter). Best for weekly/monthly/yearly summaries or trend analysis.
- search_diaries_keyword: Exact text match across all diaries. Best for people's names, places, book/movie titles, or specific phrases.
- get_days: List the user's countdowns, anniversaries, and milestones.

STRICT GROUNDING RULES (MUST FOLLOW):
1. You ONLY know diary entries that explicitly appear inside <retrieved_diaries> below or inside a tool result returned during this conversation.
2. NEVER invent, imagine, guess, or fabricate any diary entry, date, mood, weather, person, or event.
3. If <retrieved_diaries> is empty or missing what the user asked for, call the appropriate tool first. If no matching entries exist after searching, honestly tell the user that no matching diary entries were found in Diarum—never make up fictional examples or summaries.
4. Always cite specific real diary dates in YYYY-MM-DD format (e.g., 2026-10-15) when referencing entries.
5. Respond in the same language as the user with clear, readable Markdown.`, today, weekday, scopeHint))

	if didPrefetchDiaries {
		sb.WriteString("\n\n<retrieved_diaries count=\"")
		sb.WriteString(fmt.Sprintf("%d", len(prefetchedDiaries)))
		sb.WriteString("\">\n")
		if len(prefetchedDiaries) > 0 {
			sb.WriteString(s.formatDiariesForContext(prefetchedDiaries))
		} else {
			sb.WriteString("No diary entries were found in the user's database for the automatically queried date range/topic. Call a tool with broader criteria if appropriate, or honestly tell the user no entries were found.\n")
		}
		sb.WriteString("</retrieved_diaries>")
	}

	if prefetchedDays != "" {
		sb.WriteString("\n\n<tracked_days>\n")
		sb.WriteString(prefetchedDays)
		sb.WriteString("\n</tracked_days>")
	}

	return sb.String()
}

type prefetchResult struct {
	diaries          []embedding.DiarySearchResult
	daysContext      string
	didFetchDiaries  bool
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// prefetchGroundedContext automatically fetches real diaries/days for explicit
// scopes and time/memory intents before calling the LLM, preventing models that
// ignore or lack tool calling from hallucinating fictional diary entries.
func (s *ChatService) prefetchGroundedContext(ctx context.Context, userID, message, scope string, writer StreamWriter) prefetchResult {
	var res prefetchResult
	if scope == "none" || s.store == nil {
		return res
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	lower := strings.ToLower(message)

	// 1. Check if the user asks about tracked days / anniversaries / milestones.
	if containsAny(lower, "纪念日", "倒数日", "里程碑", "重要日子", "anniversar", "countdown", "milestone", "tracked day") {
		toolID := "prefetch_days"
		writeSSEEvent(writer, map[string]any{
			"type": "tool_start",
			"tool": map[string]any{"id": toolID, "name": "get_days"},
		})
		days, err := s.store.ListDays(userID)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			milestones, _ := s.store.ListAllDayMilestones(userID)
			res.daysContext = formatDaysForContext(days, milestones)
		}
		writeSSEEvent(writer, map[string]any{
			"type": "tool_end",
			"tool": map[string]any{"id": toolID, "name": "get_days", "count": len(days), "error": errMsg},
		})
	}

	// 2. Determine diary retrieval strategy based on explicit scope or query intent.
	var (
		toolName   string
		toolArgs   map[string]any
		startDate  string
		endDate    string
		limit      int
		onThisDay  bool
		recentHint bool
	)

	switch {
	case scope == "this_month":
		toolName = "list_diaries"
		startDate = now.Format("2006-01") + "-01"
		endDate = today
		limit = 31
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case scope == "this_year":
		toolName = "list_diaries"
		startDate = now.Format("2006") + "-01-01"
		endDate = today
		limit = 30
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "那年今日", "去年今天", "往年", "过去同期", "on this day", "previous months or years", "earlier months or years"):
		toolName = "search_diaries"
		onThisDay = true
		toolArgs = map[string]any{"query": now.Format("01-02")}

	case containsAny(lower, "最近 7 天", "最近7天", "过去 7 天", "过去7天", "近7天", "近一周", "最近一周", "本周", "这周", "一周", "past 7 days", "last 7 days", "this week", "past week", "last week", "weekly"):
		toolName = "list_diaries"
		startDate = now.AddDate(0, 0, -7).Format("2006-01-02")
		endDate = today
		limit = 15
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "最近一个月", "近一个月", "最近 30 天", "最近30天", "近30天", "本月", "这个月", "this month", "past month", "last month", "past 30 days", "monthly"):
		toolName = "list_diaries"
		startDate = now.AddDate(0, 0, -30).Format("2006-01-02")
		endDate = today
		limit = 31
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "今年", "这一年", "this year", "yearly"):
		toolName = "list_diaries"
		startDate = now.Format("2006") + "-01-01"
		endDate = today
		limit = 30
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "昨天", "昨日", "yesterday"):
		toolName = "list_diaries"
		y := now.AddDate(0, 0, -1).Format("2006-01-02")
		startDate = y
		endDate = y
		limit = 5
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "今天", "今日", "today"):
		toolName = "list_diaries"
		startDate = today
		endDate = today
		limit = 5
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}

	case containsAny(lower, "最近", "近期", "recent", "lately"):
		toolName = "list_diaries"
		recentHint = true
		startDate = now.AddDate(0, 0, -14).Format("2006-01-02")
		endDate = today
		limit = 15
		toolArgs = map[string]any{"start_date": startDate, "end_date": endDate}
	}

	if toolName == "" {
		return res
	}

	res.didFetchDiaries = true
	toolID := "prefetch_diaries"
	writeSSEEvent(writer, map[string]any{
		"type": "tool_start",
		"tool": map[string]any{"id": toolID, "name": toolName, "args": toolArgs},
	})

	var err error
	if onThisDay {
		monthDay := now.Format("01-02")
		raw, qErr := s.store.ListDiariesByMonthDay(userID, monthDay, 1970, now.Year()-1, 20)
		err = qErr
		for _, d := range raw {
			res.diaries = append(res.diaries, diaryToSearchResult(d))
		}
		// If nothing on exact MM-DD in earlier years, also check same day in earlier months this year.
		if err == nil && len(res.diaries) == 0 {
			for m := 1; m <= 6; m++ {
				target := now.AddDate(0, -m, 0).Format("2006-01-02")
				if d, dErr := s.store.GetDiaryByDate(userID, target+" 00:00:00.000Z", target+" 23:59:59.999Z"); dErr == nil && d != nil {
					res.diaries = append(res.diaries, diaryToSearchResult(d))
				}
			}
		}
	} else {
		raw, qErr := s.store.ListDiaries(userID, startDate+" 00:00:00.000Z", endDate+" 23:59:59.999Z", "-date", limit)
		err = qErr
		for _, d := range raw {
			res.diaries = append(res.diaries, diaryToSearchResult(d))
		}
		// For broad "recent/近期" queries, if nothing in last 14 days, fall back to latest 10 entries.
		if err == nil && recentHint && len(res.diaries) == 0 {
			if latest, lErr := s.store.ListDiaries(userID, "", "", "-date", 10); lErr == nil {
				for _, d := range latest {
					res.diaries = append(res.diaries, diaryToSearchResult(d))
				}
			}
		}
	}

	dates := make([]string, 0, len(res.diaries))
	for _, d := range res.diaries {
		if d.Date != "" {
			dates = append(dates, d.Date)
		}
	}
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	writeSSEEvent(writer, map[string]any{
		"type": "tool_end",
		"tool": map[string]any{
			"id":    toolID,
			"name":  toolName,
			"count": len(res.diaries),
			"dates": dates,
			"error": errMsg,
		},
	})

	return res
}

// GetConversationHistory retrieves recent message history for a conversation.
func (s *ChatService) GetConversationHistory(conversationID string, limit int) ([]ChatMessage, error) {
	return s.getConversationHistoryExcluding(conversationID, limit, "")
}

func (s *ChatService) getConversationHistoryExcluding(conversationID string, limit int, excludeID string) ([]ChatMessage, error) {
	fetchLimit := limit
	if excludeID != "" && fetchLimit > 0 {
		fetchLimit++
	}
	messages, err := s.store.ListMessages(conversationID, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	history := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if excludeID != "" && msg.ID == excludeID {
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		history = append(history, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	return history, nil
}

// SaveMessage saves a message to the database.
func (s *ChatService) SaveMessage(userID, conversationID, role, content string, referencedDiaries []string) (*store.Message, error) {
	record, err := s.store.CreateMessage(userID, conversationID, role, content, referencedDiaries)
	if err != nil {
		return nil, fmt.Errorf("failed to save message: %w", err)
	}
	return record, nil
}

// StreamChat performs streaming chat with RAG context using default options.
func (s *ChatService) StreamChat(ctx context.Context, userID, conversationID, message string, writer StreamWriter) (string, []string, error) {
	return s.StreamChatWithOptions(ctx, userID, conversationID, message, writer, ChatOptions{})
}

// StreamChatWithOptions performs multi-round streaming chat with tools, thinking support, and structured SSE events.
func (s *ChatService) StreamChatWithOptions(ctx context.Context, userID, conversationID, message string, writer StreamWriter, opts ChatOptions) (string, []string, error) {
	logger.Info("[ChatService] starting stream chat for user: %s, conversation: %s, scope: %s", userID, conversationID, opts.Scope)

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

	var (
		fullResponse       strings.Builder
		referencedDiaryIDs []string
		references         []DiaryReference
		seenDiaryIDs       = make(map[string]bool)
	)

	addRef := func(d embedding.DiarySearchResult) {
		if d.ID == "" || seenDiaryIDs[d.ID] {
			return
		}
		seenDiaryIDs[d.ID] = true
		referencedDiaryIDs = append(referencedDiaryIDs, d.ID)
		references = append(references, DiaryReference{
			ID:      d.ID,
			Date:    d.Date,
			Mood:    d.Mood,
			Weather: d.Weather,
			Preview: truncateString(strings.Join(strings.Fields(htmlToPlainText(d.Content)), " "), 120),
		})
	}

	prefetched := s.prefetchGroundedContext(ctx, userID, message, opts.Scope, writer)
	for _, d := range prefetched.diaries {
		addRef(d)
	}
	if len(references) > 0 {
		writeSSEEvent(writer, map[string]any{
			"type":       "references",
			"references": references,
		})
	}

	systemPrompt := s.buildAgentSystemPromptWithContext(opts.Scope, prefetched.diaries, prefetched.daysContext, prefetched.didFetchDiaries)
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}

	history, err := s.getConversationHistoryExcluding(conversationID, 24, opts.ExcludeMessageID)
	if err != nil {
		logger.Warn("[ChatService] failed to get conversation history: %v", err)
	} else {
		messages = append(messages, history...)
	}

	messages = append(messages, ChatMessage{Role: "user", Content: message})

	var tools []Tool
	if opts.Scope != "none" {
		tools = s.getTools()
	}

	for round := 0; round <= maxToolRounds; round++ {
		roundTools := tools
		if round == maxToolRounds {
			roundTools = nil
		}

		roundText, toolCalls, err := s.callAPIWithTools(ctx, baseURL, apiKey, chatModel, messages, roundTools, writer)
		if roundText != "" {
			fullResponse.WriteString(roundText)
		}
		if err != nil {
			if opts.ReferencesOut != nil {
				*opts.ReferencesOut = references
			}
			return fullResponse.String(), referencedDiaryIDs, err
		}

		if len(toolCalls) == 0 {
			break
		}

		// Ensure every tool call has an ID & type for strict OpenAI protocol compliance.
		for i := range toolCalls {
			if toolCalls[i].ID == "" {
				toolCalls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
			}
			if toolCalls[i].Type == "" {
				toolCalls[i].Type = "function"
			}
		}

		// Append assistant message containing tool_calls per standard OpenAI protocol.
		messages = append(messages, ChatMessage{
			Role:      "assistant",
			Content:   roundText,
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			resultText, diaries, toolErr := s.executeToolCall(ctx, userID, tc, writer)
			for _, d := range diaries {
				addRef(d)
			}
			if toolErr != nil {
				resultText = "Tool error: " + toolErr.Error()
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    resultText,
			})
		}

		if len(references) > 0 {
			writeSSEEvent(writer, map[string]any{
				"type":       "references",
				"references": references,
			})
		}
	}

	if opts.ReferencesOut != nil {
		*opts.ReferencesOut = references
	}

	return fullResponse.String(), referencedDiaryIDs, nil
}

func (s *ChatService) executeToolCall(ctx context.Context, userID string, tc ToolCall, writer StreamWriter) (string, []embedding.DiarySearchResult, error) {
	var parsedArgs map[string]any
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &parsedArgs)

	writeSSEEvent(writer, map[string]any{
		"type": "tool_start",
		"tool": map[string]any{
			"id":   tc.ID,
			"name": tc.Function.Name,
			"args": parsedArgs,
		},
	})

	var (
		resultText string
		diaries    []embedding.DiarySearchResult
		execErr    error
	)

	switch tc.Function.Name {
	case "search_diaries":
		var args SearchDiariesArgs
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			execErr = fmt.Errorf("invalid arguments: %w", err)
		} else {
			diaries, execErr = s.SearchDiariesByDateRange(ctx, userID, args)
			if execErr == nil {
				resultText = s.formatDiariesForContext(diaries)
			}
		}

	case "get_diary":
		var args GetDiaryArgs
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			execErr = fmt.Errorf("invalid arguments: %w", err)
		} else if strings.TrimSpace(args.Date) == "" {
			execErr = fmt.Errorf("date is required (YYYY-MM-DD)")
		} else {
			dateStr := strings.TrimSpace(args.Date)
			diary, err := s.store.GetDiaryByDate(userID, dateStr+" 00:00:00.000Z", dateStr+" 23:59:59.999Z")
			if err != nil {
				resultText = fmt.Sprintf("No diary entry found for %s.", dateStr)
			} else {
				diaries = []embedding.DiarySearchResult{diaryToSearchResult(diary)}
				resultText = s.formatDiariesForContext(diaries)
			}
		}

	case "list_diaries":
		var args ListDiariesArgs
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			execErr = fmt.Errorf("invalid arguments: %w", err)
		} else {
			limit := args.Limit
			if limit <= 0 {
				limit = 15
			}
			if limit > 50 {
				limit = 50
			}
			start := ""
			end := ""
			if args.StartDate != "" {
				start = args.StartDate + " 00:00:00.000Z"
			}
			if args.EndDate != "" {
				end = args.EndDate + " 23:59:59.999Z"
			}
			fetchLimit := limit
			if args.Mood != "" || args.Weather != "" {
				fetchLimit = 200
			}
			rawDiaries, err := s.store.ListDiaries(userID, start, end, "-date", fetchLimit)
			if err != nil {
				execErr = fmt.Errorf("failed to list diaries: %w", err)
			} else {
				moodFilter := strings.ToLower(strings.TrimSpace(args.Mood))
				weatherFilter := strings.ToLower(strings.TrimSpace(args.Weather))
				for _, d := range rawDiaries {
					if moodFilter != "" && !strings.Contains(strings.ToLower(d.Mood), moodFilter) {
						continue
					}
					if weatherFilter != "" && !strings.Contains(strings.ToLower(d.Weather), weatherFilter) {
						continue
					}
					diaries = append(diaries, diaryToSearchResult(d))
					if len(diaries) >= limit {
						break
					}
				}
				resultText = s.formatDiariesForContext(diaries)
			}
		}

	case "search_diaries_keyword":
		var args KeywordSearchArgs
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			execErr = fmt.Errorf("invalid arguments: %w", err)
		} else if strings.TrimSpace(args.Query) == "" {
			execErr = fmt.Errorf("query is required")
		} else {
			limit := args.Limit
			if limit <= 0 {
				limit = 15
			}
			if limit > 50 {
				limit = 50
			}
			rawDiaries, err := s.store.SearchDiaries(userID, strings.TrimSpace(args.Query), limit)
			if err != nil {
				execErr = fmt.Errorf("failed to search diaries: %w", err)
			} else {
				for _, d := range rawDiaries {
					diaries = append(diaries, diaryToSearchResult(d))
				}
				resultText = s.formatDiariesForContext(diaries)
			}
		}

	case "get_days":
		days, err := s.store.ListDays(userID)
		if err != nil {
			execErr = fmt.Errorf("failed to list days: %w", err)
		} else {
			milestones, _ := s.store.ListAllDayMilestones(userID)
			resultText = formatDaysForContext(days, milestones)
		}

	default:
		execErr = fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}

	dates := make([]string, 0, len(diaries))
	for _, d := range diaries {
		if d.Date != "" {
			dates = append(dates, d.Date)
		}
	}
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
		logger.Error("[ChatService] tool %s failed: %v", tc.Function.Name, execErr)
	}

	writeSSEEvent(writer, map[string]any{
		"type": "tool_end",
		"tool": map[string]any{
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"count": len(diaries),
			"dates": dates,
			"error": errMsg,
		},
	})

	return resultText, diaries, execErr
}

func formatDaysForContext(days []*store.Day, milestones []*store.DayMilestone) string {
	if len(days) == 0 {
		return "No tracked countdowns or anniversaries found."
	}
	byDay := make(map[string][]*store.DayMilestone)
	for _, m := range milestones {
		byDay[m.Day] = append(byDay[m.Day], m)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d tracked days/anniversaries:\n\n", len(days)))
	for _, d := range days {
		sb.WriteString(fmt.Sprintf("- %s %s (Date: %s", strings.TrimSpace(d.Emoji), d.Name, d.Date))
		if d.Repeat != "" {
			sb.WriteString(", repeat: " + d.Repeat)
		}
		if d.End != "" {
			sb.WriteString(", end: " + d.End)
		}
		sb.WriteString(")")
		if d.Note != "" {
			sb.WriteString(" — " + d.Note)
		}
		sb.WriteString("\n")
		for _, m := range byDay[d.ID] {
			sb.WriteString(fmt.Sprintf("  * Milestone: %s (%s)", m.Name, m.Date))
			if m.Note != "" {
				sb.WriteString(" — " + m.Note)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// callStreamingAPI calls the OpenAI-compatible streaming API.
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

// callAPIWithTools calls the API with optional tool definitions.
func (s *ChatService) callAPIWithTools(ctx context.Context, baseURL, apiKey, model string, messages []ChatMessage, tools []Tool, writer StreamWriter) (string, []ToolCall, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	url := baseURL + "/v1/chat/completions"

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.ToolChoice = "auto"
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}

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
		bodyStr := string(body)
		// Some OpenAI-compatible models/relays reject `tools` with 400/422.
		// Fall back cleanly without tools so grounded pre-fetched context still works.
		if len(tools) > 0 && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity) &&
			containsAny(strings.ToLower(bodyStr), "tool", "function", "schema") {
			logger.Warn("[ChatService] provider rejected tools (%d: %s), retrying without tools", resp.StatusCode, bodyStr)
			return s.callAPIWithTools(ctx, baseURL, apiKey, model, messages, nil, writer)
		}
		return "", nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, bodyStr)
	}

	return s.processStreamResponseWithTools(resp.Body, writer)
}

// thinkTagFilter separates inline <think>...</think> blocks emitted in content
// streams (e.g. local DeepSeek-R1 / QwQ) from normal answer text.
type thinkTagFilter struct {
	inThink bool
	pending string
}

func (f *thinkTagFilter) push(chunk string) (contentOut string, thinkOut string) {
	data := f.pending + chunk
	f.pending = ""

	var contentBuf, thinkBuf strings.Builder
	const openTag = "<think>"
	const closeTag = "</think>"

	for len(data) > 0 {
		if f.inThink {
			if idx := strings.Index(data, closeTag); idx >= 0 {
				thinkBuf.WriteString(data[:idx])
				data = data[idx+len(closeTag):]
				f.inThink = false
				continue
			}
			if keep := partialSuffixLen(data, closeTag); keep > 0 {
				thinkBuf.WriteString(data[:len(data)-keep])
				f.pending = data[len(data)-keep:]
			} else {
				thinkBuf.WriteString(data)
			}
			break
		}

		if idx := strings.Index(data, openTag); idx >= 0 {
			contentBuf.WriteString(data[:idx])
			data = data[idx+len(openTag):]
			f.inThink = true
			continue
		}
		if keep := partialSuffixLen(data, openTag); keep > 0 {
			contentBuf.WriteString(data[:len(data)-keep])
			f.pending = data[len(data)-keep:]
		} else {
			contentBuf.WriteString(data)
		}
		break
	}

	return contentBuf.String(), thinkBuf.String()
}

func (f *thinkTagFilter) flush() (contentOut string, thinkOut string) {
	if f.pending == "" {
		return "", ""
	}
	rem := f.pending
	f.pending = ""
	if f.inThink {
		return "", rem
	}
	return rem, ""
}

func partialSuffixLen(s, tag string) int {
	maxCheck := len(tag) - 1
	if len(s) < maxCheck {
		maxCheck = len(s)
	}
	for n := maxCheck; n >= 1; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}
	return 0
}

// processStreamResponse processes the SSE stream and writes to the client.
func (s *ChatService) processStreamResponse(body io.Reader, writer StreamWriter) (string, error) {
	fullResponse, _, err := s.processStreamResponseWithTools(body, writer)
	return fullResponse, err
}

// processStreamResponseWithTools processes SSE stream with thinking and tool call support.
func (s *ChatService) processStreamResponseWithTools(body io.Reader, writer StreamWriter) (string, []ToolCall, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), sseScannerMaxTokenSize)

	var fullResponse strings.Builder
	toolCallsMap := make(map[int]*ToolCall)
	maxToolIdx := -1
	filter := &thinkTagFilter{}

	emitContent := func(text string) {
		if text == "" {
			return
		}
		fullResponse.WriteString(text)
		writeSSEEvent(writer, map[string]any{
			"type":    "content",
			"content": text,
		})
	}

	emitThinking := func(text string) {
		if text == "" {
			return
		}
		writeSSEEvent(writer, map[string]any{
			"type":     "thinking",
			"thinking": text,
		})
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var streamResp ChatStreamResponse
		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			logger.Warn("[ChatService] failed to parse stream chunk: %v", err)
			continue
		}

		if len(streamResp.Choices) == 0 {
			continue
		}
		choice := streamResp.Choices[0]

		// Handle explicit reasoning fields (DeepSeek-R1, OpenRouter, etc.).
		if choice.Delta.ReasoningContent != "" {
			emitThinking(choice.Delta.ReasoningContent)
		} else if choice.Delta.Reasoning != "" {
			emitThinking(choice.Delta.Reasoning)
		}

		// Handle content (and split inline <think>...</think> tags if present).
		if choice.Delta.Content != "" {
			cOut, tOut := filter.push(choice.Delta.Content)
			emitThinking(tOut)
			emitContent(cOut)
		}

		// Accumulate streamed tool calls by index.
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if idx > maxToolIdx {
				maxToolIdx = idx
			}
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

	cRem, tRem := filter.flush()
	emitThinking(tRem)
	emitContent(cRem)

	toolCalls := make([]ToolCall, 0, len(toolCallsMap))
	for i := 0; i <= maxToolIdx; i++ {
		if tc, exists := toolCallsMap[i]; exists {
			toolCalls = append(toolCalls, *tc)
		}
	}

	if err := scanner.Err(); err != nil {
		return fullResponse.String(), toolCalls, fmt.Errorf("error reading stream: %w", err)
	}

	logger.Debug("[ChatService] parsed %d tool calls", len(toolCalls))
	return fullResponse.String(), toolCalls, nil
}

// formatDiariesForContext formats diaries as clean plain text within a token-safe budget.
func (s *ChatService) formatDiariesForContext(diaries []embedding.DiarySearchResult) string {
	if len(diaries) == 0 {
		return "No diary entries found for the specified criteria."
	}

	perEntryLimit := 4000
	if len(diaries) > 10 {
		perEntryLimit = 1500
	}
	if len(diaries) > 25 {
		perEntryLimit = 800
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d diary entries:\n\n", len(diaries)))
	usedRunes := 0

	for i, diary := range diaries {
		weekday := ""
		if t, err := time.Parse("2006-01-02", diary.Date); err == nil {
			weekday = t.Weekday().String()
		}

		cleanContent := htmlToPlainText(diary.Content)
		cleanContent = truncateString(cleanContent, perEntryLimit)

		var entry strings.Builder
		entry.WriteString(fmt.Sprintf("--- Diary Entry %d (Date: %s", i+1, diary.Date))
		if weekday != "" {
			entry.WriteString(", " + weekday)
		}
		entry.WriteString(") ---\n")
		if diary.Mood != "" {
			entry.WriteString(fmt.Sprintf("Mood: %s\n", diary.Mood))
		}
		if diary.Weather != "" {
			entry.WriteString(fmt.Sprintf("Weather: %s\n", diary.Weather))
		}
		entry.WriteString(fmt.Sprintf("Content:\n%s\n\n", cleanContent))

		entryStr := entry.String()
		entryRunes := len([]rune(entryStr))
		if usedRunes+entryRunes > maxContextBudgetRunes && i > 0 {
			sb.WriteString(fmt.Sprintf("... (%d additional diary entries omitted to fit context budget)\n", len(diaries)-i))
			break
		}
		sb.WriteString(entryStr)
		usedRunes += entryRunes
	}

	return sb.String()
}

// GenerateTitleFromUserMessage extracts a clean, rune-safe title from the user's message.
func (s *ChatService) GenerateTitleFromUserMessage(ctx context.Context, userID, userMessage string) (string, error) {
	title := extractTitleFromMessage(userMessage)
	if title == "" {
		return "", fmt.Errorf("could not extract title from message")
	}
	return title, nil
}

// extractTitleFromMessage extracts a short, UTF-8-safe title from the user's message.
func extractTitleFromMessage(message string) string {
	message = stripHTMLTags(message)
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		return ""
	}

	const maxRunes = 45
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}

	cut := string(runes[:maxRunes])
	if lastSpace := strings.LastIndex(cut, " "); lastSpace > len(cut)/2 {
		cut = cut[:lastSpace]
	}

	return strings.TrimSpace(cut) + "..."
}

// htmlToPlainText converts TipTap/HTML diary content into readable plain text.
func htmlToPlainText(s string) string {
	replacer := strings.NewReplacer(
		"</p>", "\n",
		"</div>", "\n",
		"</li>", "\n",
		"</h1>", "\n",
		"</h2>", "\n",
		"</h3>", "\n",
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
	)
	normalized := replacer.Replace(s)
	stripped := html.UnescapeString(stripHTMLTags(normalized))
	lines := strings.Split(stripped, "\n")
	cleaned := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevBlank && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			prevBlank = true
			continue
		}
		cleaned = append(cleaned, trimmed)
		prevBlank = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// stripHTMLTags removes HTML tags from a string.
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

// GenerateTitle generates a concise title for a conversation via the configured LLM.
func (s *ChatService) GenerateTitle(ctx context.Context, userID, userMessage, assistantResponse string) (string, error) {
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

	messages := []ChatMessage{
		{
			Role: "system",
			Content: `Generate a short, concise title (max 30 characters) for this conversation based on the user's message and assistant's response.
Respond with ONLY the title, no quotes, no markdown, no explanation, and no trailing punctuation.
Use the same language as the user's message.`,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("User message: %s\n\nAssistant response: %s", truncateString(userMessage, 300), truncateString(assistantResponse, 500)),
		},
	}

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
	// Strip any <think>...</think> block if a reasoning model responded.
	if endIdx := strings.Index(title, "</think>"); endIdx >= 0 {
		title = strings.TrimSpace(title[endIdx+len("</think>"):])
	}
	title = strings.Trim(title, "\"'「」《》")
	runes := []rune(title)
	if len(runes) > 100 {
		title = string(runes[:100])
	}

	return title, nil
}

// truncateString truncates a string to maxRunes runes (UTF-8 safe).
func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// GetConversationMessageCount returns the number of messages in a conversation.
func (s *ChatService) GetConversationMessageCount(conversationID string) (int, error) {
	return s.store.CountMessages(conversationID)
}

// UpdateConversationTitle updates the title of a conversation.
func (s *ChatService) UpdateConversationTitle(conversationID, title string) error {
	conv, err := s.store.GetConversation(conversationID, "")
	if err != nil {
		return fmt.Errorf("failed to find conversation: %w", err)
	}
	if _, err := s.store.UpdateConversationTitle(conversationID, conv.Owner, title); err != nil {
		return fmt.Errorf("failed to update conversation: %w", err)
	}

	return nil
}
