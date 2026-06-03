package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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
	if len(tools) != 1 || tools[0].Function.Name != "search_diaries" {
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

	if got := extractTitleFromMessage("<p>Hello</p>\nworld and everyone reading this message today"); got != "Hello world and everyone reading this message..." {
		t.Fatalf("extractTitleFromMessage = %q", got)
	}
	if got := stripHTMLTags("<strong>Hello</strong> world"); got != "Hello world" {
		t.Fatalf("stripHTMLTags = %q", got)
	}
	if got := truncateString("abcdef", 3); got != "abc..." {
		t.Fatalf("truncateString = %q, want abc...", got)
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
		if len(payload.Tools) != 0 {
			t.Fatal("second chat request should not include tools")
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
