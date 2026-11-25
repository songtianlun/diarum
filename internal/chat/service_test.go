package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type mockStreamWriter struct {
	bytes.Buffer
	flushes int
}

func (w *mockStreamWriter) Flush() {
	w.flushes++
}

func withMockTransport(t *testing.T, fn roundTripFunc) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() {
		http.DefaultTransport = original
	})
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func newTestUser(t *testing.T, s *store.Store) *store.User {
	t.Helper()

	id, err := store.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

func configureAISettings(t *testing.T, s *store.Store, userID string) {
	t.Helper()

	for key, value := range map[string]any{
		"ai.api_key":    "test-key",
		"ai.base_url":   "https://mock.local",
		"ai.chat_model": "chat-model",
	} {
		if err := s.SetSetting(userID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}
}

func TestSearchPromptsAndHelpers(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)

	first, err := s.InsertImportedDiary(user.ID, "", "2024-01-01", "A very sunny day", "happy", "sunny")
	if err != nil {
		t.Fatalf("InsertImportedDiary first: %v", err)
	}
	_, err = s.InsertImportedDiary(user.ID, "", "2024-01-02", "A rainy day", "sad", "rainy")
	if err != nil {
		t.Fatalf("InsertImportedDiary second: %v", err)
	}

	tools := service.getTools()
	if len(tools) != 5 || tools[0].Function.Name != "search_diaries" {
		t.Fatalf("getTools = %#v", tools)
	}

	results, err := service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{
		StartDate: "2024-01-01",
		EndDate:   "2024-01-01",
	})
	if err != nil {
		t.Fatalf("SearchDiariesByDateRange: %v", err)
	}
	if len(results) != 1 || results[0].ID != first.ID {
		t.Fatalf("SearchDiariesByDateRange results = %#v", results)
	}

	if _, err := service.QueryRelevantDiaries(context.Background(), user.ID, "hello", 5); err == nil || !strings.Contains(err.Error(), "embedding service not available") {
		t.Fatalf("QueryRelevantDiaries nil embedding error = %v", err)
	}

	systemPrompt := service.buildSystemPrompt(results)
	if !strings.Contains(systemPrompt, "A very sunny day") || !strings.Contains(systemPrompt, "Mood: happy") {
		t.Fatalf("buildSystemPrompt = %q", systemPrompt)
	}
	if !strings.Contains(service.buildSystemPrompt(nil), "No relevant diary entries") {
		t.Fatal("buildSystemPrompt should mention missing diary entries")
	}
	if !strings.Contains(service.buildAgentSystemPrompt(), "Today's date is:") {
		t.Fatal("buildAgentSystemPrompt should contain today's date")
	}

	formatted := service.formatDiariesForContext(results)
	if !strings.Contains(formatted, "Found 1 diary entries") || !strings.Contains(formatted, "A very sunny day") {
		t.Fatalf("formatDiariesForContext = %q", formatted)
	}
	if got := service.formatDiariesForContext(nil); got != "No diary entries found for the specified criteria." {
		t.Fatalf("formatDiariesForContext empty = %q", got)
	}

	if title, err := service.GenerateTitleFromUserMessage(context.Background(), user.ID, "<b>Hello</b>\nworld"); err != nil || title != "Hello world" {
		t.Fatalf("GenerateTitleFromUserMessage = %q, %v", title, err)
	}
	if _, err := service.GenerateTitleFromUserMessage(context.Background(), user.ID, "   "); err == nil {
		t.Fatal("GenerateTitleFromUserMessage should fail on blank message")
	}

	if got := extractTitleFromMessage("<p>Hello</p>\nworld and everyone reading this message today"); got != "Hello world and everyone reading this..." {
		t.Fatalf("extractTitleFromMessage = %q", got)
	}
	if got := extractTitleFromMessage("今天去了西湖散步，天气特别晴朗，还顺路喝了一杯很好喝的桂花拿铁，心情超级愉快的一天啊真的太棒了"); !strings.HasSuffix(got, "...") || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("extractTitleFromMessage chinese = %q", got)
	}
	if got := stripHTMLTags("<strong>Hello</strong> world"); got != "Hello world" {
		t.Fatalf("stripHTMLTags = %q", got)
	}
	if got := htmlToPlainText("<p>Hello &amp; welcome</p><p>Line 2<br/>Line 3</p>"); got != "Hello & welcome\nLine 2\nLine 3" {
		t.Fatalf("htmlToPlainText = %q", got)
	}
	if got := truncateString("abcdef", 3); got != "abc..." {
		t.Fatalf("truncateString = %q, want abc...", got)
	}
	if got := truncateString("你好世界测试", 3); got != "你好世..." {
		t.Fatalf("truncateString chinese = %q, want 你好世...", got)
	}
}

func TestConversationPersistenceHelpers(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)

	conv, err := s.CreateConversation(user.ID, "")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := service.SaveMessage(user.ID, conv.ID, "user", "hello", nil); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if _, err := service.SaveMessage(user.ID, conv.ID, "assistant", "hi", []string{"d1"}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}

	history, err := service.GetConversationHistory(conv.ID, 10)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("GetConversationHistory = %#v", history)
	}

	count, err := service.GetConversationMessageCount(conv.ID)
	if err != nil {
		t.Fatalf("GetConversationMessageCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("GetConversationMessageCount = %d, want 2", count)
	}

	if err := service.UpdateConversationTitle(conv.ID, "A title"); err != nil {
		t.Fatalf("UpdateConversationTitle: %v", err)
	}
	updated, err := s.GetConversation(conv.ID, user.ID)
	if err != nil {
		t.Fatalf("GetConversation after update: %v", err)
	}
	if updated.Title != "A title" {
		t.Fatalf("updated title = %q, want A title", updated.Title)
	}
	if err := service.UpdateConversationTitle("missing", "nope"); err == nil {
		t.Fatal("UpdateConversationTitle should fail for missing conversation")
	}
}

func TestProcessStreamResponses(t *testing.T) {
	service := &ChatService{}
	writer := &mockStreamWriter{}

	fullResponse, err := service.processStreamResponse(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}",
		"data: not-json",
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}",
		"data: [DONE]",
	}, "\n")), writer)
	if err != nil {
		t.Fatalf("processStreamResponse: %v", err)
	}
	if fullResponse != "Hello world" {
		t.Fatalf("processStreamResponse fullResponse = %q, want Hello world", fullResponse)
	}
	if writer.flushes != 2 || !strings.Contains(writer.String(), `"content":"Hello "`) {
		t.Fatalf("processStreamResponse writer = %q, flushes=%d", writer.String(), writer.flushes)
	}

	writer = &mockStreamWriter{}
	fullResponse, toolCalls, err := service.processStreamResponseWithTools(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Summary \",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search_diaries\",\"arguments\":\"{\\\"start_date\\\":\\\"2024-01-01\\\"\"}}]}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"done\",\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\",\\\"end_date\\\":\\\"2024-01-31\\\"}\"}}]}}]}",
		"data: [DONE]",
	}, "\n")), writer)
	if err != nil {
		t.Fatalf("processStreamResponseWithTools: %v", err)
	}
	if fullResponse != "Summary done" {
		t.Fatalf("processStreamResponseWithTools fullResponse = %q", fullResponse)
	}
	if len(toolCalls) != 1 || toolCalls[0].Function.Name != "search_diaries" || !strings.Contains(toolCalls[0].Function.Arguments, `"end_date":"2024-01-31"`) {
		t.Fatalf("processStreamResponseWithTools toolCalls = %#v", toolCalls)
	}
}

func TestCallStreamingAPIAndGenerateTitle(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)
	configureAISettings(t, s, user.ID)

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/chat/completions" {
			return response(http.StatusNotFound, "not found"), nil
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if stream, _ := payload["stream"].(bool); stream {
			return response(http.StatusOK, strings.Join([]string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}",
				"data: {\"choices\":[{\"delta\":{\"content\":\" there\"}}]}",
				"data: [DONE]",
			}, "\n")), nil
		}
		return response(http.StatusOK, `{"choices":[{"message":{"content":"Generated Title"}}]}`), nil
	})

	writer := &mockStreamWriter{}
	fullResponse, err := service.callStreamingAPI(context.Background(), "https://mock.local", "test-key", "chat-model", []ChatMessage{{Role: "user", Content: "hello"}}, writer)
	if err != nil {
		t.Fatalf("callStreamingAPI: %v", err)
	}
	if fullResponse != "Hello there" {
		t.Fatalf("callStreamingAPI fullResponse = %q, want Hello there", fullResponse)
	}

	title, err := service.GenerateTitle(context.Background(), user.ID, "Need a title", "Long assistant response")
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if title != "Generated Title" {
		t.Fatalf("GenerateTitle = %q, want Generated Title", title)
	}
}

func TestStreamChatWithToolCall(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)
	configureAISettings(t, s, user.ID)

	conv, err := s.CreateConversation(user.ID, "")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	diary, err := s.InsertImportedDiary(user.ID, "", "2024-01-01", "First diary", "happy", "sunny")
	if err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	callCount := 0
	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/chat/completions" {
			return response(http.StatusNotFound, "not found"), nil
		}
		callCount++
		var payload ChatRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		if callCount == 1 {
			if len(payload.Tools) == 0 {
				t.Fatal("first chat request should include tools")
			}
			return response(http.StatusOK, strings.Join([]string{
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search_diaries\",\"arguments\":\"{\\\"start_date\\\":\\\"2024-01-01\\\",\\\"end_date\\\":\\\"2024-01-01\\\",\\\"limit\\\":5}\"}}]}}]}",
				"data: [DONE]",
			}, "\n")), nil
		}
		// Verify standard OpenAI assistant(tool_calls) + tool(tool_call_id) protocol.
		hasToolMsg := false
		for _, m := range payload.Messages {
			if m.Role == "tool" && m.ToolCallID == "call_1" {
				hasToolMsg = true
			}
		}
		if !hasToolMsg {
			t.Fatalf("second chat request missing standard tool result message: %#v", payload.Messages)
		}
		return response(http.StatusOK, strings.Join([]string{
			"data: {\"choices\":[{\"delta\":{\"content\":\"Summary\"}}]}",
			"data: {\"choices\":[{\"delta\":{\"content\":\" ready\"}}]}",
			"data: [DONE]",
		}, "\n")), nil
	})

	writer := &mockStreamWriter{}
	fullResponse, referencedDiaries, err := service.StreamChat(context.Background(), user.ID, conv.ID, "Summarize my day", writer)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if fullResponse != "Summary ready" {
		t.Fatalf("StreamChat fullResponse = %q, want Summary ready", fullResponse)
	}
	if len(referencedDiaries) != 1 || referencedDiaries[0] != diary.ID {
		t.Fatalf("StreamChat referencedDiaries = %#v", referencedDiaries)
	}
	if callCount != 2 {
		t.Fatalf("StreamChat callCount = %d, want 2", callCount)
	}
	if !strings.Contains(writer.String(), `"content":"Summary"`) {
		t.Fatalf("StreamChat writer = %q", writer.String())
	}
}

func TestSearchAndQueryRelevantDiariesEdges(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB, err := embedding.NewVectorDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewVectorDB: %v", err)
	}
	embeddingService := embedding.NewEmbeddingService(s, vectorDB)
	service := NewChatService(s, embeddingService)

	for key, value := range map[string]any{
		"ai.enabled":         true,
		"ai.api_key":         "test-key",
		"ai.base_url":        "https://mock.local/",
		"ai.embedding_model": "embed-model",
	} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	for i := 0; i < 12; i++ {
		if _, err := s.InsertImportedDiary(user.ID, "", fmt.Sprintf("2024-02-%02d", i+1), "sunny topic", "", ""); err != nil {
			t.Fatalf("InsertImportedDiary %d: %v", i, err)
		}
	}

	results, err := service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{Limit: 0})
	if err != nil {
		t.Fatalf("SearchDiariesByDateRange default limit: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("default-limited results = %d, want 10", len(results))
	}

	results, err = service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{Limit: 500})
	if err != nil {
		t.Fatalf("SearchDiariesByDateRange capped limit: %v", err)
	}
	if len(results) != 12 {
		t.Fatalf("capped results = %d, want all 12 under cap", len(results))
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":[{"embedding":[1,0]}]}`), nil
	})
	if _, err := embeddingService.BuildAllVectors(context.Background(), user.ID); err != nil {
		t.Fatalf("BuildAllVectors: %v", err)
	}
	relevant, err := service.QueryRelevantDiaries(context.Background(), user.ID, "sunny", 3)
	if err != nil {
		t.Fatalf("QueryRelevantDiaries: %v", err)
	}
	if len(relevant) != 3 {
		t.Fatalf("QueryRelevantDiaries count = %d, want 3", len(relevant))
	}

	results, err = service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{Query: "sunny", Limit: 3})
	if err != nil {
		t.Fatalf("SearchDiariesByDateRange semantic: %v", err)
	}
	if len(results) == 0 || len(results) > 3 {
		t.Fatalf("semantic filtered results = %d, want 1..3", len(results))
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, "embedding down"), nil
	})
	results, err = service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{Query: "sunny", Limit: 3})
	if err != nil {
		t.Fatalf("SearchDiariesByDateRange semantic failure fallback: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("semantic failure fallback results = %d, want date results", len(results))
	}

	if _, err := service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{}); err != nil {
		t.Fatalf("SearchDiariesByDateRange before close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := service.SearchDiariesByDateRange(context.Background(), user.ID, SearchDiariesArgs{}); err == nil || !strings.Contains(err.Error(), "failed to fetch diaries") {
		t.Fatalf("SearchDiariesByDateRange closed-store error = %v", err)
	}
}

func TestChatAPIErrorBranches(t *testing.T) {
	service := &ChatService{}
	writer := &mockStreamWriter{}
	messages := []ChatMessage{{Role: "user", Content: "hello"}}

	if _, err := service.callStreamingAPI(context.Background(), "://bad", "key", "model", messages, writer); err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Fatalf("callStreamingAPI create request error = %v", err)
	}
	if _, _, err := service.callAPIWithTools(context.Background(), "://bad", "key", "model", messages, nil, writer); err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Fatalf("callAPIWithTools create request error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})
	if _, err := service.callStreamingAPI(context.Background(), "https://mock.local", "key", "model", messages, writer); err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Fatalf("callStreamingAPI send error = %v", err)
	}
	if _, _, err := service.callAPIWithTools(context.Background(), "https://mock.local", "key", "model", messages, nil, writer); err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Fatalf("callAPIWithTools send error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusTeapot, "short and stout"), nil
	})
	if _, err := service.callStreamingAPI(context.Background(), "https://mock.local", "key", "model", messages, writer); err == nil || !strings.Contains(err.Error(), "API returned status 418") {
		t.Fatalf("callStreamingAPI status error = %v", err)
	}
	if _, _, err := service.callAPIWithTools(context.Background(), "https://mock.local", "key", "model", messages, nil, writer); err == nil || !strings.Contains(err.Error(), "API returned status 418") {
		t.Fatalf("callAPIWithTools status error = %v", err)
	}
}

func TestStreamChatConfigurationAndAPIErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)
	writer := &mockStreamWriter{}

	if _, _, err := service.StreamChat(context.Background(), user.ID, "missing", "hello", writer); err == nil || !strings.Contains(err.Error(), "AI API key not configured") {
		t.Fatalf("StreamChat missing api key error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.api_key", "test-key", false); err != nil {
		t.Fatalf("SetSetting ai.api_key: %v", err)
	}
	if _, _, err := service.StreamChat(context.Background(), user.ID, "missing", "hello", writer); err == nil || !strings.Contains(err.Error(), "AI base URL not configured") {
		t.Fatalf("StreamChat missing base URL error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.base_url", "https://mock.local", false); err != nil {
		t.Fatalf("SetSetting ai.base_url: %v", err)
	}
	if _, _, err := service.StreamChat(context.Background(), user.ID, "missing", "hello", writer); err == nil || !strings.Contains(err.Error(), "chat model not configured") {
		t.Fatalf("StreamChat missing model error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.chat_model", "chat-model", false); err != nil {
		t.Fatalf("SetSetting ai.chat_model: %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "boom"), nil
	})
	if _, _, err := service.StreamChat(context.Background(), user.ID, "missing", "hello", writer); err == nil || !strings.Contains(err.Error(), "API returned status 500") {
		t.Fatalf("StreamChat API error = %v", err)
	}
}

func TestGenerateTitleErrorAndEdgePaths(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)

	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "AI API key not configured") {
		t.Fatalf("GenerateTitle missing api key error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.api_key", "test-key", false); err != nil {
		t.Fatalf("SetSetting ai.api_key: %v", err)
	}
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "AI base URL not configured") {
		t.Fatalf("GenerateTitle missing base URL error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.base_url", "https://mock.local", false); err != nil {
		t.Fatalf("SetSetting ai.base_url: %v", err)
	}
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "chat model not configured") {
		t.Fatalf("GenerateTitle missing model error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.chat_model", "chat-model", false); err != nil {
		t.Fatalf("SetSetting ai.chat_model: %v", err)
	}

	if err := s.SetSetting(user.ID, "ai.base_url", "://bad", false); err != nil {
		t.Fatalf("SetSetting bad base URL: %v", err)
	}
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Fatalf("GenerateTitle create request error = %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.base_url", "https://mock.local", false); err != nil {
		t.Fatalf("SetSetting good base URL: %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Fatalf("GenerateTitle send error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, "upstream"), nil
	})
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "API returned status 502") {
		t.Fatalf("GenerateTitle status error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, "not-json"), nil
	})
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "failed to decode response") {
		t.Fatalf("GenerateTitle decode error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"choices":[]}`), nil
	})
	if _, err := service.GenerateTitle(context.Background(), user.ID, "user", "assistant"); err == nil || !strings.Contains(err.Error(), "no response from API") {
		t.Fatalf("GenerateTitle no choices error = %v", err)
	}

	longTitle := strings.Repeat("x", 120)
	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"choices":[{"message":{"content":"`+longTitle+`"}}]}`), nil
	})
	title, err := service.GenerateTitle(context.Background(), user.ID, "user", strings.Repeat("assistant ", 100))
	if err != nil {
		t.Fatalf("GenerateTitle long title: %v", err)
	}
	if len(title) != 100 {
		t.Fatalf("GenerateTitle long title len = %d, want 100", len(title))
	}

	if got := truncateString("short", 10); got != "short" {
		t.Fatalf("truncateString short = %q, want short", got)
	}
}

func TestSaveMessageAndHistoryErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)

	if _, err := service.SaveMessage(user.ID, "missing-conversation", "user", "hello", nil); err == nil || !strings.Contains(err.Error(), "failed to save message") {
		t.Fatalf("SaveMessage missing conversation error = %v", err)
	}

	conv, err := s.CreateConversation(user.ID, "test")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := service.SaveMessage("missing-user", conv.ID, "user", "hello", nil); err == nil || !strings.Contains(err.Error(), "failed to save message") {
		t.Fatalf("SaveMessage missing user error = %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := service.GetConversationHistory(conv.ID, 10); err == nil || !strings.Contains(err.Error(), "failed to fetch messages") {
		t.Fatalf("GetConversationHistory closed-store error = %v", err)
	}
}

func TestUpdateConversationTitleErrors(t *testing.T) {
	s := newTestStore(t)
	service := NewChatService(s, nil)

	if err := service.UpdateConversationTitle("missing", "title"); err == nil || !strings.Contains(err.Error(), "failed to find conversation") {
		t.Fatalf("UpdateConversationTitle missing conv error = %v", err)
	}

	user := newTestUser(t, s)
	conv, err := s.CreateConversation(user.ID, "test")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := service.UpdateConversationTitle(conv.ID, "new title"); err != nil {
		t.Fatalf("UpdateConversationTitle success: %v", err)
	}
}

func TestFormatDiariesForContextWeekdayParse(t *testing.T) {
	service := &ChatService{}
	result := service.formatDiariesForContext([]embedding.DiarySearchResult{
		{ID: "d1", Date: "2024-06-01", Content: "test", Mood: "happy", Weather: "sunny"},
	})
	if !strings.Contains(result, "Saturday") {
		t.Fatalf("formatDiariesForContext weekday missing: %s", result)
	}
}

func TestGenerateTitleLongTitle(t *testing.T) {
	_ = truncateString("Hello World This Is A Very Long Title That Exceeds Fifty Characters By Far", 50)
}

func TestChatServiceNew(t *testing.T) {
	s := newTestStore(t)
	service := NewChatService(s, nil)
	if service == nil {
		t.Fatal("NewChatService should return non-nil")
	}
}

func TestThinkingStreamAndThinkTagFilter(t *testing.T) {
	service := &ChatService{}
	writer := &mockStreamWriter{}

	// Test explicit reasoning_content, reasoning, and chunked <think>...</think> tags.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"step 1 "}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"step 2"}}]}`,
		`data: {"choices":[{"delta":{"content":"<thi"}}]}`,
		`data: {"choices":[{"delta":{"content":"nk>inner thought</th"}}]}`,
		`data: {"choices":[{"delta":{"content":"ink>Final answer"}}]}`,
		`data: [DONE]`,
	}, "\n")

	fullResponse, _, err := service.processStreamResponseWithTools(strings.NewReader(stream), writer)
	if err != nil {
		t.Fatalf("processStreamResponseWithTools: %v", err)
	}
	if fullResponse != "Final answer" {
		t.Fatalf("fullResponse = %q, want Final answer", fullResponse)
	}
	out := writer.String()
	if !strings.Contains(out, `"thinking":"step 1 "`) || !strings.Contains(out, `"thinking":"step 2"`) || !strings.Contains(out, `"thinking":"inner thought"`) {
		t.Fatalf("expected thinking events in stream output: %s", out)
	}

	// Verify flush handles dangling partial tag in both normal and think states.
	f1 := &thinkTagFilter{}
	c1, t1 := f1.push("hello <thi")
	c2, t2 := f1.flush()
	if c1+c2 != "hello <thi" || t1+t2 != "" {
		t.Fatalf("normal flush = (%q,%q), (%q,%q)", c1, t1, c2, t2)
	}
	f2 := &thinkTagFilter{inThink: true}
	_, tt1 := f2.push("thinking </th")
	_, tt2 := f2.flush()
	if tt1+tt2 != "thinking </th" {
		t.Fatalf("think flush = %q", tt1+tt2)
	}
}

func TestAllDiaryToolsAndScopes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)
	configureAISettings(t, s, user.ID)

	if _, err := s.InsertImportedDiary(user.ID, "d-jan", "2024-01-15", "<p>Visited Kyoto temple</p>", "happy", "sunny"); err != nil {
		t.Fatalf("InsertImportedDiary jan: %v", err)
	}
	if _, err := s.InsertImportedDiary(user.ID, "d-feb", "2024-02-20", "<p>Rainy coding day</p>", "calm", "rainy"); err != nil {
		t.Fatalf("InsertImportedDiary feb: %v", err)
	}
	day, err := s.SaveDay(&store.Day{Owner: user.ID, Name: "Anniversary", Date: "2020-05-20", Emoji: "🎉", Repeat: store.DayRepeatYear, Note: "Special day"})
	if err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	if _, err := s.SaveDayMilestone(&store.DayMilestone{Owner: user.ID, Day: day.ID, Name: "5 Years", Date: "2025-05-20", Note: "Big milestone"}); err != nil {
		t.Fatalf("SaveDayMilestone: %v", err)
	}

	writer := &mockStreamWriter{}
	// 1. get_diary found & not found & invalid args
	res, diaries, err := service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c1", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "get_diary", Arguments: `{"date":"2024-01-15"}`},
	}, writer)
	if err != nil || len(diaries) != 1 || !strings.Contains(res, "Visited Kyoto temple") {
		t.Fatalf("get_diary found = %q, %d, %v", res, len(diaries), err)
	}
	res, _, err = service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c2", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "get_diary", Arguments: `{"date":"2099-01-01"}`},
	}, writer)
	if err != nil || !strings.Contains(res, "No diary entry found") {
		t.Fatalf("get_diary missing = %q, %v", res, err)
	}
	if _, _, err := service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c2b", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "get_diary", Arguments: `{"date":""}`},
	}, writer); err == nil {
		t.Fatal("get_diary empty date should error")
	}

	// 2. list_diaries with mood/weather filter
	res, diaries, err = service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c3", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "list_diaries", Arguments: `{"start_date":"2024-01-01","end_date":"2024-12-31","mood":"calm","weather":"rainy","limit":10}`},
	}, writer)
	if err != nil || len(diaries) != 1 || diaries[0].ID != "d-feb" || !strings.Contains(res, "Rainy coding day") {
		t.Fatalf("list_diaries filtered = %q, %#v, %v", res, diaries, err)
	}

	// 3. search_diaries_keyword
	res, diaries, err = service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c4", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "search_diaries_keyword", Arguments: `{"query":"Kyoto"}`},
	}, writer)
	if err != nil || len(diaries) != 1 || diaries[0].ID != "d-jan" {
		t.Fatalf("search_diaries_keyword = %q, %#v, %v", res, diaries, err)
	}
	if _, _, err := service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c4b", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "search_diaries_keyword", Arguments: `{"query":""}`},
	}, writer); err == nil {
		t.Fatal("search_diaries_keyword empty query should error")
	}

	// 4. get_days & empty days formatting
	res, _, err = service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c5", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "get_days", Arguments: `{}`},
	}, writer)
	if err != nil || !strings.Contains(res, "Anniversary") || !strings.Contains(res, "5 Years") {
		t.Fatalf("get_days = %q, %v", res, err)
	}
	if got := formatDaysForContext(nil, nil); !strings.Contains(got, "No tracked") {
		t.Fatalf("formatDaysForContext empty = %q", got)
	}

	// 5. unknown tool & invalid JSON args
	if _, _, err := service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c6", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "nonexistent", Arguments: `{}`},
	}, writer); err == nil {
		t.Fatal("unknown tool should error")
	}
	if _, _, err := service.executeToolCall(context.Background(), user.ID, ToolCall{
		ID: "c7", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "search_diaries", Arguments: `{bad`},
	}, writer); err == nil {
		t.Fatal("invalid json args should error")
	}

	// 6. Scope prompts & scope=none skips tools
	for _, scope := range []string{"this_month", "this_year", "none"} {
		prompt := service.buildAgentSystemPromptWithScope(scope)
		if prompt == "" {
			t.Fatalf("empty prompt for scope %s", scope)
		}
	}

	conv, _ := s.CreateConversation(user.ID, "scope test")
	savedUser, _ := service.SaveMessage(user.ID, conv.ID, "user", "hi there", nil)

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		var payload ChatRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(payload.Tools) != 0 {
			t.Fatalf("scope=none should not send tools, got %d", len(payload.Tools))
		}
		userMsgCount := 0
		for _, m := range payload.Messages {
			if m.Role == "user" && m.Content == "hi there" {
				userMsgCount++
			}
		}
		if userMsgCount != 1 {
			t.Fatalf("expected user message once (deduped via ExcludeMessageID), got %d", userMsgCount)
		}
		return response(http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello back\"}}]}\ndata: [DONE]\n"), nil
	})

	var refs []DiaryReference
	reply, _, err := service.StreamChatWithOptions(context.Background(), user.ID, conv.ID, "hi there", &mockStreamWriter{}, ChatOptions{
		Scope:            "none",
		ExcludeMessageID: savedUser.ID,
		ReferencesOut:    &refs,
	})
	if err != nil || reply != "Hello back" {
		t.Fatalf("StreamChatWithOptions scope=none = %q, %v", reply, err)
	}
}

func TestHistoryLatestOrderAndContextBudget(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewChatService(s, nil)

	conv, err := s.CreateConversation(user.ID, "long")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	var thirdID string
	for i := 1; i <= 5; i++ {
		msg, err := service.SaveMessage(user.ID, conv.ID, "user", fmt.Sprintf("msg-%d", i), nil)
		if err != nil {
			t.Fatalf("SaveMessage %d: %v", i, err)
		}
		if i == 3 {
			thirdID = msg.ID
		}
	}

	// Requesting limit=2 must return the LATEST 2 messages (msg-4, msg-5) in chronological order.
	history, err := service.GetConversationHistory(conv.ID, 2)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(history) != 2 || history[0].Content != "msg-4" || history[1].Content != "msg-5" {
		t.Fatalf("expected [msg-4, msg-5], got %#v", history)
	}

	// DeleteMessagesFrom(thirdID) deletes msg-3, msg-4, msg-5.
	deleted, err := s.DeleteMessagesFrom(user.ID, conv.ID, thirdID)
	if err != nil || deleted != 3 {
		t.Fatalf("DeleteMessagesFrom = %d, %v", deleted, err)
	}
	remaining, err := s.ListMessages(conv.ID, 0)
	if err != nil || len(remaining) != 2 || remaining[1].Content != "msg-2" {
		t.Fatalf("remaining after delete = %#v, %v", remaining, err)
	}

	// Verify context budget omits excess entries when huge diaries are formatted.
	huge := make([]embedding.DiarySearchResult, 0, 30)
	for i := 0; i < 30; i++ {
		huge = append(huge, embedding.DiarySearchResult{
			ID:      fmt.Sprintf("d-%d", i),
			Date:    fmt.Sprintf("2024-03-%02d", (i%28)+1),
			Content: strings.Repeat("长日记内容测试", 250),
		})
	}
	formatted := service.formatDiariesForContext(huge)
	if !strings.Contains(formatted, "omitted to fit context budget") {
		t.Fatalf("expected budget omission notice in formatted diaries, len=%d", len([]rune(formatted)))
	}
}
