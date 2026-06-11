package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/store"
)

func zipEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	entries := make(map[string][]byte)
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip file %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip file %s: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	return entries
}

func buildImportZip(t *testing.T, data exportData, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	exportJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal exportData: %v", err)
	}
	w, err := zw.Create("diarum_export.json")
	if err != nil {
		t.Fatalf("Create diarum_export.json: %v", err)
	}
	if _, err := w.Write(exportJSON); err != nil {
		t.Fatalf("Write diarum_export.json: %v", err)
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip writer: %v", err)
	}
	return buf.Bytes()
}

func configureAIRouteSettings(t *testing.T, s *store.Store, userID string) {
	t.Helper()

	cfg := config.NewConfigService(s)
	for key, value := range map[string]any{
		"ai.enabled":         true,
		"ai.api_key":         "test-key",
		"ai.base_url":        "https://mock.local",
		"ai.chat_model":      "chat-model",
		"ai.embedding_model": "embed-model",
	} {
		if err := cfg.Set(userID, key, value); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
}

func embeddingTransport(t *testing.T) roundTripFunc {
	t.Helper()

	return func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/embeddings" {
			return httpResponse(http.StatusNotFound, "not found"), nil
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		input, _ := payload["input"].(string)
		base := float32(len(input)%7 + 1)
		body := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[` + strings.TrimRight(strings.TrimRight(jsonFloat(base), "0"), ".") + `,0.2,0.3]}],"model":"embed-model"}`
		return httpResponse(http.StatusOK, body), nil
	}
}

func jsonFloat(value float32) string {
	bytes, _ := json.Marshal(value)
	return string(bytes)
}

func TestExportImportRoutesAndHelpers(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterExportImportRoutes(e, s, authMiddlewareFor(user), nil)

	diary, err := s.InsertImportedDiary(user.ID, "diary-1", "2024-02-01", "Exported diary", "happy", "sunny")
	if err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	media, err := s.InsertImportedMedia(user.ID, "media-1", "photo.png", "Photo", "Alt", []string{diary.ID})
	if err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if err := s.SaveUploadedMedia(media, bytes.NewReader(pngBytes())); err != nil {
		t.Fatalf("SaveUploadedMedia: %v", err)
	}
	conv, err := s.InsertImportedConversation(user.ID, "conv-1", "Conversation")
	if err != nil {
		t.Fatalf("InsertImportedConversation: %v", err)
	}
	if _, err := s.InsertImportedMessage(user.ID, "msg-1", conv.ID, "user", "hello", []string{diary.ID}); err != nil {
		t.Fatalf("InsertImportedMessage: %v", err)
	}

	rec := performRequest(t, e, http.MethodPost, "/api/v1/export", strings.NewReader(`{"date_range":"all","include_diaries":true,"include_media":true,"include_conversations":true}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/export status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export content type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Export-Stats") == "" {
		t.Fatal("export should include X-Export-Stats header")
	}
	entries := zipEntries(t, rec.Body.Bytes())
	if _, ok := entries["diarum_export.json"]; !ok {
		t.Fatalf("export zip entries = %#v", entries)
	}
	if _, ok := entries["markdown/2024-02-01_happy.md"]; !ok {
		t.Fatalf("export markdown entries = %#v", entries)
	}
	if _, ok := entries["media/photo.png"]; !ok {
		t.Fatalf("export media entries = %#v", entries)
	}

	if !isValidZipPath("media/photo.png") || isValidZipPath("../evil") {
		t.Fatal("isValidZipPath results unexpected")
	}
	if markdown := generateMarkdown(exportDiary{Date: "2024-02-01", Content: "Hello", Mood: "happy", Weather: "sunny"}); !strings.Contains(markdown, "**Mood:** happy") {
		t.Fatalf("generateMarkdown = %q", markdown)
	}
	if _, _, err := calculateDateRange(ExportRequest{DateRange: "custom", StartDate: "2024-01-02", EndDate: "2024-01-01"}); err == nil {
		t.Fatal("calculateDateRange should reject reversed custom range")
	}
	if _, _, err := calculateDateRange(ExportRequest{DateRange: "custom"}); err == nil {
		t.Fatal("calculateDateRange should require custom dates")
	}
	if _, _, err := calculateDateRange(ExportRequest{DateRange: "custom", StartDate: "bad", EndDate: "2024-01-01"}); err == nil {
		t.Fatal("calculateDateRange should reject invalid start date")
	}
	if _, _, err := calculateDateRange(ExportRequest{DateRange: "custom", StartDate: "2024-01-01", EndDate: "bad"}); err == nil {
		t.Fatal("calculateDateRange should reject invalid end date")
	}
	if start, end, err := calculateDateRange(ExportRequest{DateRange: "1m"}); err != nil || !end.After(start) {
		t.Fatalf("calculateDateRange 1m = %v, %v, %v", start, end, err)
	}
	for _, dateRange := range []string{"3m", "6m", "1y", "unknown"} {
		start, end, err := calculateDateRange(ExportRequest{DateRange: dateRange})
		if err != nil || !end.After(start) {
			t.Fatalf("calculateDateRange %s = %v, %v, %v", dateRange, start, end, err)
		}
	}
	if !isDateInRange("2024-02-01", mustDate(t, "2024-02-01"), mustDate(t, "2024-02-28")) {
		t.Fatal("isDateInRange should include in-range dates")
	}
	if isDateInRange("", mustDate(t, "2024-02-01"), mustDate(t, "2024-02-28")) || isDateInRange("bad-date", mustDate(t, "2024-02-01"), mustDate(t, "2024-02-28")) {
		t.Fatal("isDateInRange should reject empty and invalid dates")
	}
	if serverError("oops", nil).(*echo.HTTPError).Code != http.StatusInternalServerError {
		t.Fatal("serverError without wrapped error should return 500")
	}

	importStore := newTestStore(t)
	importUser := newTestUser(t, importStore)
	importEcho := echo.New()
	RegisterExportImportRoutes(importEcho, importStore, authMiddlewareFor(importUser), nil)

	zipBytes := buildImportZip(t, exportData{
		Version:    1,
		ExportedAt: "2024-02-01T00:00:00Z",
		Diaries: []exportDiary{
			{ID: "old-diary", Date: "2024-02-03", Content: "Imported diary", Mood: "calm", Weather: "cloudy"},
		},
		Media: []exportMedia{
			{ID: "old-media", File: "photo.png", Name: "Photo", Alt: "Alt", Diary: []string{"old-diary"}},
		},
		Conversations: []exportConversation{
			{ID: "old-conv", Title: "Imported Conversation", Messages: []exportMessage{{ID: "old-msg", Role: "user", Content: "hi", ReferencedDiaries: []string{"old-diary"}}}},
		},
	}, map[string][]byte{"media/photo.png": pngBytes()})
	body, contentType := multipartRequestBody(t, "file", "import.zip", zipBytes, nil)
	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/import status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	if payload["diaries"].(map[string]any)["imported"] != float64(1) {
		t.Fatalf("import payload diaries = %#v", payload)
	}
	if importStore.CountDiaries(importUser.ID) != 1 {
		t.Fatalf("imported diary count = %d, want 1", importStore.CountDiaries(importUser.ID))
	}
	items, total, err := importStore.ListMedia(importUser.ID, 1, 10)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("imported media list = %#v total=%d err=%v", items, total, err)
	}
	conversations, err := importStore.ListConversations(importUser.ID, 10)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("imported conversations = %#v err=%v", conversations, err)
	}

	body, contentType = multipartRequestBody(t, "file", "import.zip", zipBytes, nil)
	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/import repeat status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["diaries"].(map[string]any)["skipped"] == float64(0) {
		t.Fatalf("repeat import payload = %#v", payload)
	}

	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", strings.NewReader(""), map[string]string{"Content-Type": "multipart/form-data; boundary=missing"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/v1/import missing file status = %d", rec.Code)
	}

	var invalidBuf bytes.Buffer
	invalidZipWriter := zip.NewWriter(&invalidBuf)
	w, err := invalidZipWriter.Create("media/photo.txt")
	if err != nil {
		t.Fatalf("Create invalid zip entry: %v", err)
	}
	if _, err := w.Write([]byte("not-an-image")); err != nil {
		t.Fatalf("Write invalid zip entry: %v", err)
	}
	if err := invalidZipWriter.Close(); err != nil {
		t.Fatalf("Close invalid zip writer: %v", err)
	}
	body, contentType = multipartRequestBody(t, "file", "invalid.zip", invalidBuf.Bytes(), nil)
	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/v1/import missing export json status = %d body=%s", rec.Code, rec.Body.String())
	}

	disallowedZip := buildImportZip(t, exportData{
		Version:    1,
		ExportedAt: "2024-02-01T00:00:00Z",
		Diaries: []exportDiary{
			{ID: "old-diary-2", Date: "2024-02-04", Content: "Imported diary 2"},
		},
		Media: []exportMedia{
			{ID: "old-media-2", File: "photo.txt", Name: "Text file", Diary: []string{"old-diary-2"}},
		},
	}, map[string][]byte{"media/photo.txt": []byte("not-an-image")})
	body, contentType = multipartRequestBody(t, "file", "disallowed.zip", disallowedZip, nil)
	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/import disallowed media status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["media"].(map[string]any)["failed"] == float64(0) {
		t.Fatalf("disallowed media payload = %#v", payload)
	}

	malformedZip := buildImportZip(t, exportData{
		Version:    1,
		ExportedAt: "2024-02-01T00:00:00Z",
		Diaries: []exportDiary{
			{ID: "missing-date", Content: "no date"},
		},
		Media: []exportMedia{
			{ID: "empty-file", File: "", Name: "Empty"},
			{ID: "missing-media", File: "missing.png", Name: "Missing"},
		},
		Conversations: []exportConversation{
			{ID: "conv-no-messages", Title: "No Messages"},
			{ID: "conv-bad-ref", Title: "Bad Ref", Messages: []exportMessage{{ID: "msg", Role: "assistant", Content: "hello", ReferencedDiaries: []string{"missing-date"}}}},
		},
	}, map[string][]byte{"../ignored.png": pngBytes()})
	body, contentType = multipartRequestBody(t, "file", "malformed.zip", malformedZip, nil)
	rec = performRequest(t, importEcho, http.MethodPost, "/api/v1/import", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/import malformed branches status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["diaries"].(map[string]any)["failed"] != float64(1) || payload["media"].(map[string]any)["failed"] != float64(2) || payload["conversations"].(map[string]any)["imported"] != float64(2) {
		t.Fatalf("malformed import payload = %#v", payload)
	}
}

func TestAIRoutesAndFetchModels(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterAIRoutes(e, s, authMiddlewareFor(user), nil)

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/models":
			if strings.Contains(req.Header.Get("Authorization"), "bad") {
				return httpResponse(http.StatusUnauthorized, "unauthorized"), nil
			}
			return httpResponse(http.StatusOK, `{"object":"list","data":[{"id":"gpt-test","object":"model"}]}`), nil
		case "/v1/chat/completions":
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode ai request: %v", err)
			}
			if stream, _ := payload["stream"].(bool); stream {
				tools, _ := payload["tools"].([]any)
				if len(tools) > 0 {
					return httpResponse(http.StatusOK, strings.Join([]string{
						"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search_diaries\",\"arguments\":\"{\\\"start_date\\\":\\\"2024-04-01\\\",\\\"end_date\\\":\\\"2024-04-01\\\",\\\"limit\\\":5}\"}}]}}]}",
						"data: [DONE]",
					}, "\n")), nil
				}
				return httpResponse(http.StatusOK, strings.Join([]string{
					"data: {\"choices\":[{\"delta\":{\"content\":\"Summary\"}}]}",
					"data: {\"choices\":[{\"delta\":{\"content\":\" complete\"}}]}",
					"data: [DONE]",
				}, "\n")), nil
			}
			return httpResponse(http.StatusOK, `{"choices":[{"message":{"content":"unused title"}}]}`), nil
		default:
			return httpResponse(http.StatusNotFound, "not found"), nil
		}
	})

	rec := performRequest(t, e, http.MethodGet, "/api/v1/ai/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ai/settings status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodPut, "/api/v1/ai/settings", strings.NewReader(`{"api_key":"test-key","base_url":"https://mock.local","chat_model":"chat-model","embedding_model":"embed-model","enabled":true}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /ai/settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	configureAIRouteSettings(t, s, user.ID)

	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/models", strings.NewReader(`{"api_key":"test-key","base_url":"https://mock.local"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ai/models status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/models", strings.NewReader(`{"api_key":"","base_url":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /ai/models missing config status = %d", rec.Code)
	}

	if _, err := fetchModels("https://mock.local", "bad-key"); err == nil {
		t.Fatal("fetchModels should fail on non-200 response")
	}

	for _, path := range []string{
		"/api/v1/ai/vectors/build",
		"/api/v1/ai/vectors/build-incremental",
		"/api/v1/ai/vectors/stats",
	} {
		method := http.MethodPost
		if strings.HasSuffix(path, "/stats") {
			method = http.MethodGet
		}
		rec = performRequest(t, e, method, path, nil, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, rec.Code)
		}
	}

	convRec := performRequest(t, e, http.MethodPost, "/api/v1/ai/conversations", strings.NewReader(`{"title":""}`), map[string]string{"Content-Type": "application/json"})
	if convRec.Code != http.StatusOK {
		t.Fatalf("POST /ai/conversations status = %d body=%s", convRec.Code, convRec.Body.String())
	}
	convPayload := decodeJSONBody(t, convRec)
	convID := convPayload["id"].(string)

	if _, err := s.InsertImportedDiary(user.ID, "", "2024-04-01", "AI diary entry", "happy", "sunny"); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/ai/conversations", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ai/conversations status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/ai/conversations/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /ai/conversations/missing status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"conversation_id":"","content":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /ai/chat missing fields status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"conversation_id":"missing","content":"hello"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /ai/chat missing conversation status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"conversation_id":"`+convID+`","content":"Summarize April 1st"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ai/chat status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"done":true`) || !strings.Contains(rec.Body.String(), `"content":"Summary"`) || !strings.Contains(rec.Body.String(), `"content":" complete"`) {
		t.Fatalf("POST /ai/chat body = %s", rec.Body.String())
	}

	messageCount, err := s.CountMessages(convID)
	if err != nil || messageCount != 2 {
		t.Fatalf("CountMessages after chat = %d err=%v", messageCount, err)
	}
	conv, err := s.GetConversation(convID, user.ID)
	if err != nil || conv.Title == "" {
		t.Fatalf("conversation title after chat = %#v err=%v", conv, err)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/ai/conversations/"+convID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ai/conversations/:id status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/ai/conversations/"+convID, strings.NewReader(`{"title":"Renamed"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /ai/conversations/:id status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/ai/conversations/"+convID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /ai/conversations/:id status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/ai/conversations/"+convID, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /ai/conversations/:id missing status = %d", rec.Code)
	}
}

func TestAIVectorRoutesWithEmbeddingService(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB, err := embedding.NewVectorDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewVectorDB: %v", err)
	}
	embeddingService := embedding.NewEmbeddingService(s, vectorDB)
	e := echo.New()
	RegisterAIRoutes(e, s, authMiddlewareFor(user), embeddingService)

	configureAIRouteSettings(t, s, user.ID)
	if _, err := s.InsertImportedDiary(user.ID, "ai-vector-1", "2024-05-01", "vector route diary", "calm", "sunny"); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	withMockTransport(t, embeddingTransport(t))

	rec := performRequest(t, e, http.MethodPost, "/api/v1/ai/vectors/build", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ai/vectors/build status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["success"] != float64(1) || payload["failed"] != float64(0) || payload["total"] != float64(1) {
		t.Fatalf("build vectors payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/ai/vectors/stats", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ai/vectors/stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["diary_count"] != float64(1) || payload["indexed_count"] != float64(1) {
		t.Fatalf("vector stats payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/ai/vectors/build-incremental", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ai/vectors/build-incremental status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["total"] != float64(1) || payload["failed"] != float64(0) {
		t.Fatalf("incremental vectors payload = %#v", payload)
	}
}

func mustDate(t *testing.T, value string) time.Time {
	t.Helper()

	result, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", value, err)
	}
	return result
}
