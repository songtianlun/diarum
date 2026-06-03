package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	if service.needsBuildVector(context.Background(), collection, diary) {
		t.Fatal("needsBuildVector should be false for current document")
	}

	if got := extractDate("2024-01-03T10:20:30Z"); got != "2024-01-03" {
		t.Fatalf("extractDate = %q, want 2024-01-03", got)
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

var _ chromem.EmbeddingFunc = func(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("not used")
}
