package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	chromem "github.com/philippgille/chromem-go"

	"github.com/songtianlun/diarum/internal/store"
)

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

func newTestVectorDB(t *testing.T) *VectorDB {
	t.Helper()

	db, err := NewVectorDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewVectorDB: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func configureAISettings(t *testing.T, s *store.Store, userID, baseURL string) {
	t.Helper()

	for key, value := range map[string]any{
		"ai.enabled":         true,
		"ai.api_key":         "test-key",
		"ai.base_url":        strings.TrimRight(baseURL, "/"),
		"ai.embedding_model": "embed-model",
		"ai.chat_model":      "chat-model",
	} {
		if err := s.SetSetting(userID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}
}

func embeddingVector(input string) []float32 {
	normalized := strings.ToLower(input)
	switch {
	case strings.Contains(normalized, "sun"), strings.Contains(normalized, "happy"):
		return []float32{1, 0}
	case strings.Contains(normalized, "rain"), strings.Contains(normalized, "sad"):
		return []float32{0, 1}
	default:
		return []float32{0.5, 0.5}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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

func newEmbeddingTransport(t *testing.T, statusCode int, customBody string) roundTripFunc {
	t.Helper()

	return func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/embeddings" {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("not found")),
				Header:     make(http.Header),
			}, nil
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q, want Bearer test-key", got)
		}
		if statusCode != http.StatusOK {
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader(customBody)),
				Header:     make(http.Header),
			}, nil
		}
		if customBody != "" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(customBody)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}
		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		payload := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"index":     0,
					"embedding": embeddingVector(req.Input),
				},
			},
			"model": req.Model,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal embedding response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
}

func TestCreateEmbeddingFuncAndGenerateEmbedding(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if _, err := service.createEmbeddingFunc(user.ID); err == nil {
		t.Fatal("createEmbeddingFunc should fail when config is missing")
	}

	baseURL := "https://mock.local"
	withMockTransport(t, newEmbeddingTransport(t, http.StatusOK, ""))
	configureAISettings(t, s, user.ID, baseURL+"/")

	embeddingFunc, err := service.createEmbeddingFunc(user.ID)
	if err != nil {
		t.Fatalf("createEmbeddingFunc: %v", err)
	}

	vector, err := embeddingFunc(context.Background(), "happy and sunny")
	if err != nil {
		t.Fatalf("embeddingFunc: %v", err)
	}
	if len(vector) != 2 || vector[0] != 1 || vector[1] != 0 {
		t.Fatalf("embedding vector = %#v, want [1 0]", vector)
	}

	directVector, err := service.generateEmbedding(context.Background(), baseURL, "test-key", "embed-model", "rainy")
	if err != nil {
		t.Fatalf("generateEmbedding: %v", err)
	}
	if len(directVector) != 2 || directVector[0] != 0 || directVector[1] != 1 {
		t.Fatalf("direct embedding vector = %#v, want [0 1]", directVector)
	}
}

func TestGenerateEmbeddingFailures(t *testing.T) {
	s := newTestStore(t)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	withMockTransport(t, newEmbeddingTransport(t, http.StatusBadGateway, "upstream failed"))
	if _, err := service.generateEmbedding(context.Background(), "https://mock.local", "test-key", "embed-model", "hello"); err == nil || !strings.Contains(err.Error(), "API returned status 502") {
		t.Fatalf("generateEmbedding status error = %v", err)
	}

	withMockTransport(t, newEmbeddingTransport(t, http.StatusOK, `{"object":"list","data":[],"model":"m"}`))
	if _, err := service.generateEmbedding(context.Background(), "https://mock.local", "test-key", "embed-model", "hello"); err == nil || !strings.Contains(err.Error(), "no embedding data") {
		t.Fatalf("generateEmbedding empty data error = %v", err)
	}

	withMockTransport(t, newEmbeddingTransport(t, http.StatusOK, `not-json`))
	if _, err := service.generateEmbedding(context.Background(), "https://mock.local", "test-key", "embed-model", "hello"); err == nil || !strings.Contains(err.Error(), "failed to decode response") {
		t.Fatalf("generateEmbedding invalid JSON error = %v", err)
	}
}

func TestBuildQueryAndStats(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	withMockTransport(t, newEmbeddingTransport(t, http.StatusOK, ""))
	configureAISettings(t, s, user.ID, "https://mock.local")

	diary1, err := s.InsertImportedDiary(user.ID, "d1", "2024-01-01", "happy sun walk", "happy", "sunny")
	if err != nil {
		t.Fatalf("InsertImportedDiary d1: %v", err)
	}
	_, err = s.InsertImportedDiary(user.ID, "d2", "2024-01-02", "sad rain day", "sad", "rainy")
	if err != nil {
		t.Fatalf("InsertImportedDiary d2: %v", err)
	}

	buildResult, err := service.BuildAllVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildAllVectors: %v", err)
	}
	if buildResult.Success != 2 || buildResult.Failed != 0 || buildResult.Total != 2 {
		t.Fatalf("BuildAllVectors result = %#v", buildResult)
	}

	results, err := service.QuerySimilar(context.Background(), user.ID, "sun", 10)
	if err != nil {
		t.Fatalf("QuerySimilar: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("QuerySimilar result count = %d, want 2", len(results))
	}
	if results[0].ID != diary1.ID {
		t.Fatalf("QuerySimilar top result ID = %q, want %q", results[0].ID, diary1.ID)
	}

	stats, err := service.GetVectorStats(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetVectorStats: %v", err)
	}
	if stats.DiaryCount != 2 || stats.IndexedCount != 2 || stats.OutdatedCount != 0 || stats.PendingCount != 0 {
		t.Fatalf("GetVectorStats = %#v", stats)
	}

	embeddingFunc, err := service.createEmbeddingFunc(user.ID)
	if err != nil {
		t.Fatalf("createEmbeddingFunc before update: %v", err)
	}
	collection, err := vectorDB.GetOrCreateCollection(context.Background(), user.ID, embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection before update: %v", err)
	}
	doc, err := collection.GetByID(context.Background(), diary1.ID)
	if err != nil {
		t.Fatalf("collection.GetByID: %v", err)
	}
	builtAt, err := time.Parse(time.RFC3339Nano, doc.Metadata["built_at"])
	if err != nil {
		t.Fatalf("parse built_at: %v", err)
	}

	updatedDiary, _, err := s.UpsertDiary(user.ID, "2024-01-01", "happy sun walk updated", "happy", "sunny")
	if err != nil {
		t.Fatalf("UpsertDiary update: %v", err)
	}
	outdatedAt := builtAt.Add(time.Nanosecond).Format(time.RFC3339Nano)
	if _, err := s.DB.Exec(`UPDATE diaries SET updated = ? WHERE id = ?`, outdatedAt, updatedDiary.ID); err != nil {
		t.Fatalf("force diary updated time: %v", err)
	}
	updatedDiary, err = s.GetDiaryByID(updatedDiary.ID)
	if err != nil {
		t.Fatalf("reload updated diary: %v", err)
	}
	stats, err = service.GetVectorStats(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetVectorStats after update: %v", err)
	}
	if stats.OutdatedCount == 0 {
		t.Fatalf("GetVectorStats after update = %#v, want outdated diary", stats)
	}

	buildResult, err = service.BuildIncrementalVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildIncrementalVectors: %v", err)
	}
	if buildResult.Total != 2 || buildResult.Success == 0 {
		t.Fatalf("BuildIncrementalVectors result = %#v", buildResult)
	}

	if collection.Count() != 2 {
		t.Fatalf("collection.Count() = %d, want 2", collection.Count())
	}
	if service.needsBuildVector(context.Background(), collection, updatedDiary) {
		t.Fatal("needsBuildVector should be false right after incremental rebuild")
	}
}

func TestHelpersAndEdgeCases(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if _, err := service.BuildAllVectors(context.Background(), user.ID); err == nil || !strings.Contains(err.Error(), "AI features are not enabled") {
		t.Fatalf("BuildAllVectors disabled error = %v", err)
	}
	if _, err := service.QuerySimilar(context.Background(), user.ID, "hello", 5); err == nil || !strings.Contains(err.Error(), "AI features are not enabled") {
		t.Fatalf("QuerySimilar disabled error = %v", err)
	}

	withMockTransport(t, newEmbeddingTransport(t, http.StatusOK, ""))
	configureAISettings(t, s, user.ID, "https://mock.local")

	embeddingFunc, err := service.createEmbeddingFunc(user.ID)
	if err != nil {
		t.Fatalf("createEmbeddingFunc: %v", err)
	}
	collection, err := vectorDB.GetOrCreateCollection(context.Background(), user.ID, embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection: %v", err)
	}

	emptyResult, err := service.BuildIncrementalVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildIncrementalVectors empty: %v", err)
	}
	if emptyResult.Total != 0 || emptyResult.Success != 0 {
		t.Fatalf("BuildIncrementalVectors empty result = %#v", emptyResult)
	}
	fullEmptyResult, err := service.BuildAllVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildAllVectors empty: %v", err)
	}
	if fullEmptyResult.Total != 0 || fullEmptyResult.Success != 0 {
		t.Fatalf("BuildAllVectors empty result = %#v", fullEmptyResult)
	}
	queryResults, err := service.QuerySimilar(context.Background(), user.ID, "hello", 5)
	if err != nil {
		t.Fatalf("QuerySimilar empty collection: %v", err)
	}
	if len(queryResults) != 0 {
		t.Fatalf("QuerySimilar empty collection results = %#v", queryResults)
	}

	emptyDiary := &store.Diary{ID: "empty", Content: ""}
	if err := service.processDiary(context.Background(), collection, emptyDiary, embeddingFunc); err != nil {
		t.Fatalf("processDiary empty content: %v", err)
	}

	diary := &store.Diary{
		ID:      "doc-1",
		Date:    "2024-01-03 00:00:00.000Z",
		Content: "plain text",
		Updated: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := service.processDiary(context.Background(), collection, diary, embeddingFunc); err != nil {
		t.Fatalf("processDiary: %v", err)
	}
	if service.needsBuildVector(context.Background(), nil, diary) == false {
		t.Fatal("needsBuildVector should require build when collection is nil")
	}
	if service.needsBuildVector(context.Background(), collection, &store.Diary{ID: "missing", Updated: diary.Updated}) == false {
		t.Fatal("needsBuildVector should require build for missing document")
	}
	if service.needsBuildVector(context.Background(), collection, &store.Diary{ID: diary.ID, Updated: "bad-time"}) == false {
		t.Fatal("needsBuildVector should require build for invalid updated time")
	}
	if err := collection.AddDocument(context.Background(), chromem.Document{
		ID:        "no-built-at",
		Content:   "plain text",
		Embedding: []float32{1, 0},
		Metadata:  map[string]string{},
	}); err != nil {
		t.Fatalf("AddDocument no-built-at: %v", err)
	}
	if service.needsBuildVector(context.Background(), collection, &store.Diary{ID: "no-built-at", Updated: diary.Updated}) == false {
		t.Fatal("needsBuildVector should require build when built_at is missing")
	}
	if err := collection.AddDocument(context.Background(), chromem.Document{
		ID:        "bad-built-at",
		Content:   "plain text",
		Embedding: []float32{1, 0},
		Metadata:  map[string]string{"built_at": "bad-time"},
	}); err != nil {
		t.Fatalf("AddDocument bad-built-at: %v", err)
	}
	if service.needsBuildVector(context.Background(), collection, &store.Diary{ID: "bad-built-at", Updated: diary.Updated}) == false {
		t.Fatal("needsBuildVector should require build when built_at is invalid")
	}
	if err := collection.AddDocument(context.Background(), chromem.Document{
		ID:        "rfc3339-built-at",
		Content:   "plain text",
		Embedding: []float32{1, 0},
		Metadata:  map[string]string{"built_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339)},
	}); err != nil {
		t.Fatalf("AddDocument rfc3339-built-at: %v", err)
	}
	if service.needsBuildVector(context.Background(), collection, &store.Diary{ID: "rfc3339-built-at", Updated: diary.Updated}) {
		t.Fatal("needsBuildVector should accept RFC3339 built_at")
	}
	if service.needsBuildVector(context.Background(), collection, diary) {
		t.Fatal("needsBuildVector should be false for current document")
	}

	if got := extractDate("2024-01-03T10:20:30Z"); got != "2024-01-03" {
		t.Fatalf("extractDate = %q, want 2024-01-03", got)
	}
	if got := extractDate("short"); got != "short" {
		t.Fatalf("extractDate short = %q, want short", got)
	}
	if got, err := parseStoreTime("2024-01-03 00:00:00.000Z"); err != nil || got.IsZero() {
		t.Fatalf("parseStoreTime store format = %v, %v", got, err)
	}
	if got, err := parseStoreTime(time.Now().UTC().Format(time.RFC3339Nano)); err != nil || got.IsZero() {
		t.Fatalf("parseStoreTime RFC3339Nano = %v, %v", got, err)
	}
	if _, err := parseStoreTime("not-a-time"); err == nil {
		t.Fatal("parseStoreTime should reject invalid input")
	}
}

func TestBuildFailuresAndPendingStats(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	withMockTransport(t, func(r *http.Request) (*http.Response, error) {
		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		if strings.Contains(req.Input, "fail") {
			return response(http.StatusBadGateway, "upstream failed"), nil
		}
		body, err := json.Marshal(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"index":     0,
					"embedding": embeddingVector(req.Input),
				},
			},
			"model": req.Model,
		})
		if err != nil {
			t.Fatalf("marshal embedding response: %v", err)
		}
		return response(http.StatusOK, string(body)), nil
	})
	configureAISettings(t, s, user.ID, "https://mock.local")

	if _, err := s.InsertImportedDiary(user.ID, "ok-diary", "2024-05-01", "happy day", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary ok: %v", err)
	}
	if _, err := s.InsertImportedDiary(user.ID, "fail-diary", "2024-05-02", "please fail", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary fail: %v", err)
	}

	buildResult, err := service.BuildAllVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildAllVectors failure case: %v", err)
	}
	if buildResult.Success != 1 || buildResult.Failed != 1 || len(buildResult.Errors) != 1 {
		t.Fatalf("BuildAllVectors failure result = %#v", buildResult)
	}

	pendingUser := newTestUser(t, s)
	configureAISettings(t, s, pendingUser.ID, "https://mock.local")
	if _, err := s.InsertImportedDiary(pendingUser.ID, "pending-diary", "2024-05-03", "pending", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary pending: %v", err)
	}
	stats, err := service.GetVectorStats(context.Background(), pendingUser.ID)
	if err != nil {
		t.Fatalf("GetVectorStats pending: %v", err)
	}
	if stats.PendingCount != 1 || stats.DiaryCount != 1 {
		t.Fatalf("GetVectorStats pending = %#v", stats)
	}
}

func TestIncrementalBuildAndStatsEdges(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if err := s.SetSetting(user.ID, "ai.enabled", true, false); err != nil {
		t.Fatalf("SetSetting ai.enabled: %v", err)
	}
	if _, err := service.BuildIncrementalVectors(context.Background(), user.ID); err == nil || !strings.Contains(err.Error(), "failed to create embedding function") {
		t.Fatalf("BuildIncrementalVectors missing config error = %v", err)
	}

	withMockTransport(t, func(r *http.Request) (*http.Response, error) {
		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		if strings.Contains(req.Input, "fail") {
			return response(http.StatusBadGateway, "upstream failed"), nil
		}
		return response(http.StatusOK, `{"data":[{"embedding":[1,0]}]}`), nil
	})
	configureAISettings(t, s, user.ID, "https://mock.local")

	if _, err := s.InsertImportedDiary(user.ID, "ok-incremental", "2024-06-01", "ok", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary ok: %v", err)
	}
	if _, err := s.InsertImportedDiary(user.ID, "fail-incremental", "2024-06-02", "please fail", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary fail: %v", err)
	}

	buildResult, err := service.BuildIncrementalVectors(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BuildIncrementalVectors failure case: %v", err)
	}
	if buildResult.Total != 2 || buildResult.Success != 1 || buildResult.Failed != 1 || len(buildResult.ErrorDetails) != 1 {
		t.Fatalf("BuildIncrementalVectors failure result = %#v", buildResult)
	}

	statsUser := newTestUser(t, s)
	configureAISettings(t, s, statsUser.ID, "https://mock.local")
	invalidUpdated, err := s.InsertImportedDiary(statsUser.ID, "invalid-updated", "2024-06-03", "indexed", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary invalid-updated: %v", err)
	}
	missingBuiltAt, err := s.InsertImportedDiary(statsUser.ID, "missing-built-at", "2024-06-04", "indexed", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary missing-built-at: %v", err)
	}
	badBuiltAt, err := s.InsertImportedDiary(statsUser.ID, "bad-built-at", "2024-06-05", "indexed", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary bad-built-at: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE diaries SET updated = ? WHERE id = ?`, "bad-time", invalidUpdated.ID); err != nil {
		t.Fatalf("force invalid updated: %v", err)
	}

	embeddingFunc, err := service.createEmbeddingFunc(statsUser.ID)
	if err != nil {
		t.Fatalf("createEmbeddingFunc stats user: %v", err)
	}
	collection, err := vectorDB.GetOrCreateCollection(context.Background(), statsUser.ID, embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection stats user: %v", err)
	}
	for _, doc := range []chromem.Document{
		{ID: missingBuiltAt.ID, Content: "indexed", Embedding: []float32{1, 0}, Metadata: map[string]string{}},
		{ID: badBuiltAt.ID, Content: "indexed", Embedding: []float32{1, 0}, Metadata: map[string]string{"built_at": "bad-time"}},
	} {
		if err := collection.AddDocument(context.Background(), doc); err != nil {
			t.Fatalf("AddDocument %s: %v", doc.ID, err)
		}
	}

	stats, err := service.GetVectorStats(context.Background(), statsUser.ID)
	if err != nil {
		t.Fatalf("GetVectorStats metadata edges: %v", err)
	}
	if stats.OutdatedCount != 3 || stats.DiaryCount != 3 {
		t.Fatalf("GetVectorStats metadata edges = %#v, want 3 outdated of 3", stats)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := service.GetVectorStats(context.Background(), statsUser.ID); err == nil || !strings.Contains(err.Error(), "failed to fetch diaries") {
		t.Fatalf("GetVectorStats closed-store error = %v", err)
	}
}

func TestVectorDBLifecycle(t *testing.T) {
	db := newTestVectorDB(t)
	embeddingFunc := func(ctx context.Context, text string) ([]float32, error) {
		return []float32{1, 0}, nil
	}

	collection, err := db.GetOrCreateCollection(context.Background(), "user-1", embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection first: %v", err)
	}
	if collection == nil {
		t.Fatal("GetOrCreateCollection should return a collection")
	}

	sameCollection, err := db.GetOrCreateCollection(context.Background(), "user-1", embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection second: %v", err)
	}
	if sameCollection == nil {
		t.Fatal("GetOrCreateCollection second should return a collection")
	}

	if got := db.GetCollection("user-1"); got == nil {
		t.Fatal("GetCollection should return collection")
	}
	if err := db.DeleteCollection("user-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCreateEmbeddingFuncMissingConfigs(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if err := s.SetSetting(user.ID, "ai.api_key", "", false); err != nil {
		t.Fatalf("SetSetting empty api_key: %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.base_url", "https://mock.local", false); err != nil {
		t.Fatalf("SetSetting base_url: %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.embedding_model", "model", false); err != nil {
		t.Fatalf("SetSetting embedding_model: %v", err)
	}
	if _, err := service.createEmbeddingFunc(user.ID); err == nil || !strings.Contains(err.Error(), "API key not configured") {
		t.Fatalf("createEmbeddingFunc empty key error = %v", err)
	}

	if err := s.SetSetting(user.ID, "ai.api_key", "key", false); err != nil {
		t.Fatalf("SetSetting api_key: %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.base_url", "", false); err != nil {
		t.Fatalf("SetSetting empty base_url: %v", err)
	}
	if _, err := service.createEmbeddingFunc(user.ID); err == nil || !strings.Contains(err.Error(), "base URL not configured") {
		t.Fatalf("createEmbeddingFunc empty baseURL error = %v", err)
	}

	if err := s.SetSetting(user.ID, "ai.base_url", "https://mock.local", false); err != nil {
		t.Fatalf("SetSetting base_url: %v", err)
	}
	if err := s.SetSetting(user.ID, "ai.embedding_model", "", false); err != nil {
		t.Fatalf("SetSetting empty embedding_model: %v", err)
	}
	if _, err := service.createEmbeddingFunc(user.ID); err == nil || !strings.Contains(err.Error(), "embedding model not configured") {
		t.Fatalf("createEmbeddingFunc empty model error = %v", err)
	}
}

func TestBuildAllVectorsAndQuerySimilarStoreError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if err := s.SetSetting(user.ID, "ai.enabled", true, false); err != nil {
		t.Fatalf("SetSetting ai.enabled: %v", err)
	}

	if _, err := service.BuildAllVectors(context.Background(), user.ID); err == nil || !strings.Contains(err.Error(), "failed to create embedding function") {
		t.Fatalf("BuildAllVectors missing config error = %v", err)
	}
	if _, err := service.BuildIncrementalVectors(context.Background(), user.ID); err == nil || !strings.Contains(err.Error(), "failed to create embedding function") {
		t.Fatalf("BuildIncrementalVectors missing config error = %v", err)
	}
}

func TestGetVectorStatsMissingCollection(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	if _, err := s.InsertImportedDiary(user.ID, "pending-doc", "2024-08-01", "content", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	stats, err := service.GetVectorStats(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetVectorStats no collection: %v", err)
	}
	if stats.PendingCount != 1 || stats.DiaryCount != 1 {
		t.Fatalf("GetVectorStats no collection = %#v", stats)
	}
}

func TestNewVectorDBError(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := NewVectorDB(filepath.Join(notDir, "vectors")); err == nil {
		t.Fatal("NewVectorDB should fail when parent is a file")
	}
}

func TestGetCollectionAfterDelete(t *testing.T) {
	db := newTestVectorDB(t)
	if got := db.GetCollection("nonexistent"); got != nil {
		t.Fatalf("GetCollection nonexistent = %#v, want nil", got)
	}
}

func TestNewVectorDBAndGetCollectionErrors(t *testing.T) {
	db := newTestVectorDB(t)
	embeddingFunc := func(ctx context.Context, text string) ([]float32, error) {
		return []float32{1, 0}, nil
	}
	col, err := db.GetOrCreateCollection(context.Background(), "user-new", embeddingFunc)
	if err != nil {
		t.Fatalf("GetOrCreateCollection: %v", err)
	}
	if col == nil {
		t.Fatal("GetOrCreateCollection should return non-nil")
	}
	if err := db.DeleteCollection("user-new"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if got := db.GetCollection("user-new"); got != nil {
		t.Fatalf("GetCollection after delete should be nil, got %v", got)
	}
}

func TestGenerateEmbeddingMarshalError(t *testing.T) {
	s := newTestStore(t)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `not-json`), nil
	})
	if _, err := service.generateEmbedding(context.Background(), "https://mock.local", "key", "model", "text"); err == nil || !strings.Contains(err.Error(), "failed to decode response") {
		t.Fatalf("generateEmbedding decode error = %v", err)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"object":"list","data":[{"embedding":[]}],"model":"m"}`), nil
	})
	if vec, err := service.generateEmbedding(context.Background(), "https://mock.local", "key", "model", "text"); err != nil || len(vec) != 0 {
		t.Fatalf("generateEmbedding empty embedding = %v, %v", vec, err)
	}
}

var _ chromem.EmbeddingFunc = func(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("not used")
}
