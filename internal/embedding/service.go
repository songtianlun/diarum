package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	chromem "github.com/philippgille/chromem-go"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/songtianlun/journitalia/internal/config"
	"github.com/songtianlun/journitalia/internal/logger"
)

// EmbeddingService handles diary embedding operations
type EmbeddingService struct {
	app           *pocketbase.PocketBase
	vectorDB      *VectorDB
	configService *config.ConfigService
}

// BuildResult represents the result of a build operation
type BuildResult struct {
	Success      int      `json:"success"`
	Failed       int      `json:"failed"`
	Total        int      `json:"total"`
	Errors       []string `json:"errors,omitempty"`
	ErrorDetails []string `json:"error_details,omitempty"`
}

// VectorStats represents statistics about the vector index
type VectorStats struct {
	DiaryCount    int `json:"diary_count"`
	IndexedCount  int `json:"indexed_count"`
	OutdatedCount int `json:"outdated_count"`
	PendingCount  int `json:"pending_count"`
}

// EmbeddingRequest represents a request to the embedding API
type EmbeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

// EmbeddingResponse represents the response from the embedding API
type EmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// NewEmbeddingService creates a new EmbeddingService
func NewEmbeddingService(app *pocketbase.PocketBase, vectorDB *VectorDB) *EmbeddingService {
	return &EmbeddingService{
		app:           app,
		vectorDB:      vectorDB,
		configService: config.NewConfigService(app),
	}
}

// createEmbeddingFunc creates an embedding function for the given user's configuration
func (s *EmbeddingService) createEmbeddingFunc(userID string) (chromem.EmbeddingFunc, error) {
	apiKey, err := s.configService.GetString(userID, "ai.api_key")
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("AI API key not configured")
	}

	baseURL, err := s.configService.GetString(userID, "ai.base_url")
	if err != nil || baseURL == "" {
		return nil, fmt.Errorf("AI base URL not configured")
	}

	embeddingModel, err := s.configService.GetString(userID, "ai.embedding_model")
	if err != nil || embeddingModel == "" {
		return nil, fmt.Errorf("embedding model not configured")
	}

	// Normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Debug log configuration (mask API key)
	maskedKey := "***"
	if len(apiKey) > 8 {
		maskedKey = apiKey[:4] + "***" + apiKey[len(apiKey)-4:]
	}
	logger.Debug("[EmbeddingService] config: baseURL=%s, model=%s, apiKey=%s", baseURL, embeddingModel, maskedKey)

	return func(ctx context.Context, text string) ([]float32, error) {
		return s.generateEmbedding(ctx, baseURL, apiKey, embeddingModel, text)
	}, nil
}

// generateEmbedding calls the OpenAI-compatible embedding API
func (s *EmbeddingService) generateEmbedding(ctx context.Context, baseURL, apiKey, model, text string) ([]float32, error) {
	url := baseURL + "/v1/embeddings"

	reqBody := EmbeddingRequest{
		Input: text,
		Model: model,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("[EmbeddingService] embedding API error: status=%d, url=%s, response=%s", resp.StatusCode, url, string(body))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return embResp.Data[0].Embedding, nil
}

// BuildAllVectors rebuilds vectors for ALL diaries (full rebuild)
func (s *EmbeddingService) BuildAllVectors(ctx context.Context, userID string) (*BuildResult, error) {
	logger.Info("[EmbeddingService] starting full vector rebuild for user: %s", userID)

	// Check if AI is enabled
	enabled, _ := s.configService.GetBool(userID, "ai.enabled")
	if !enabled {
		return nil, fmt.Errorf("AI features are not enabled")
	}

	// Create embedding function
	embeddingFunc, err := s.createEmbeddingFunc(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding function: %w", err)
	}

	// Delete existing collection and create a new one
	if err := s.vectorDB.DeleteCollection(userID); err != nil {
		logger.Warn("[EmbeddingService] failed to delete existing collection: %v", err)
	}

	collection, err := s.vectorDB.GetOrCreateCollection(ctx, userID, embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	// Get all diaries for the user
	diaries, err := s.app.Dao().FindRecordsByFilter(
		"diaries",
		"owner = {:owner}",
		"-date",
		-1, // No limit
		0,
		map[string]any{"owner": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diaries: %w", err)
	}

	result := &BuildResult{
		Total:        len(diaries),
		Errors:       make([]string, 0),
		ErrorDetails: make([]string, 0),
	}

	if len(diaries) == 0 {
		logger.Info("[EmbeddingService] no diaries found for user: %s", userID)
		return result, nil
	}

	// Process all diaries
	for _, diary := range diaries {
		if err := s.processDiary(ctx, collection, diary, embeddingFunc); err != nil {
			result.Failed++
			dateStr := extractDate(diary.GetString("date"))
			errMsg := fmt.Sprintf("Diary %s: %v", dateStr, err)
			result.Errors = append(result.Errors, dateStr)
			result.ErrorDetails = append(result.ErrorDetails, errMsg)
			logger.Error("[EmbeddingService] %s", errMsg)
		} else {
			result.Success++
		}
	}

	logger.Info("[EmbeddingService] full rebuild completed for user %s: %d success, %d failed",
		userID, result.Success, result.Failed)

	return result, nil
}

// BuildIncrementalVectors builds vectors only for new and outdated diaries
func (s *EmbeddingService) BuildIncrementalVectors(ctx context.Context, userID string) (*BuildResult, error) {
	logger.Info("[EmbeddingService] starting incremental vector build for user: %s", userID)

	// Check if AI is enabled
	enabled, _ := s.configService.GetBool(userID, "ai.enabled")
	if !enabled {
		return nil, fmt.Errorf("AI features are not enabled")
	}

	// Create embedding function
	embeddingFunc, err := s.createEmbeddingFunc(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding function: %w", err)
	}

	// Get or create collection (keep existing)
	collection, err := s.vectorDB.GetOrCreateCollection(ctx, userID, embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	// Get all diaries for the user
	diaries, err := s.app.Dao().FindRecordsByFilter(
		"diaries",
		"owner = {:owner}",
		"-date",
		-1,
		0,
		map[string]any{"owner": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diaries: %w", err)
	}

	result := &BuildResult{
		Total:        len(diaries),
		Errors:       make([]string, 0),
		ErrorDetails: make([]string, 0),
	}

	if len(diaries) == 0 {
		logger.Info("[EmbeddingService] no diaries found for user: %s", userID)
		return result, nil
	}

	// Process only new and outdated diaries
	skipped := 0
	for _, diary := range diaries {
		if !s.needsBuildVector(ctx, collection, diary) {
			skipped++
			continue
		}

		if err := s.processDiary(ctx, collection, diary, embeddingFunc); err != nil {
			result.Failed++
			dateStr := extractDate(diary.GetString("date"))
			errMsg := fmt.Sprintf("Diary %s: %v", dateStr, err)
			result.Errors = append(result.Errors, dateStr)
			result.ErrorDetails = append(result.ErrorDetails, errMsg)
			logger.Error("[EmbeddingService] %s", errMsg)
		} else {
			result.Success++
		}
	}

	logger.Info("[EmbeddingService] incremental build completed for user %s: %d built, %d skipped, %d failed",
		userID, result.Success, skipped, result.Failed)

	return result, nil
}

// processDiary processes a single diary entry
func (s *EmbeddingService) processDiary(ctx context.Context, collection *chromem.Collection, diary *models.Record, embeddingFunc chromem.EmbeddingFunc) error {
	content := diary.GetString("content")
	if content == "" {
		return nil // Skip empty diaries
	}

	diaryID := diary.GetId()
	dateStr := extractDate(diary.GetString("date"))
	mood := diary.GetString("mood")
	weather := diary.GetString("weather")
	builtAt := time.Now().UTC().Format(time.RFC3339Nano)

	// Generate embedding directly to avoid issues with collection's embeddingFunc
	embedding, err := embeddingFunc(ctx, content)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Create document with metadata and pre-generated embedding
	doc := chromem.Document{
		ID:        diaryID,
		Content:   content,
		Embedding: embedding,
		Metadata: map[string]string{
			"date":     dateStr,
			"mood":     mood,
			"weather":  weather,
			"built_at": builtAt,
		},
	}

	// Add document to collection
	if err := collection.AddDocument(ctx, doc); err != nil {
		return fmt.Errorf("failed to add document: %w", err)
	}

	return nil
}

// extractDate extracts the date part from a timestamp string
func extractDate(dateTime string) string {
	if len(dateTime) >= 10 {
		return dateTime[:10]
	}
	return dateTime
}

// needsBuildVector checks if a diary needs its vector rebuilt
func (s *EmbeddingService) needsBuildVector(ctx context.Context, collection *chromem.Collection, diary *models.Record) bool {
	if collection == nil {
		return true
	}

	diaryID := diary.GetId()
	diaryUpdated := diary.Updated.Time()

	doc, err := collection.GetByID(ctx, diaryID)
	if err != nil {
		return true // Not found, needs build
	}

	builtAtStr, ok := doc.Metadata["built_at"]
	if !ok || builtAtStr == "" {
		return true
	}

	builtAt, err := time.Parse(time.RFC3339Nano, builtAtStr)
	if err != nil {
		// Fallback to RFC3339 for backward compatibility
		builtAt, err = time.Parse(time.RFC3339, builtAtStr)
		if err != nil {
			return true
		}
	}

	return diaryUpdated.After(builtAt)
}

// DiarySearchResult represents a diary found by vector search
type DiarySearchResult struct {
	ID      string  `json:"id"`
	Date    string  `json:"date"`
	Content string  `json:"content"`
	Mood    string  `json:"mood,omitempty"`
	Weather string  `json:"weather,omitempty"`
	Score   float32 `json:"score"`
}

// QuerySimilar finds diaries similar to the given query
func (s *EmbeddingService) QuerySimilar(ctx context.Context, userID, query string, limit int) ([]DiarySearchResult, error) {
	logger.Info("[EmbeddingService] querying similar diaries for user: %s", userID)

	// Check if AI is enabled
	enabled, _ := s.configService.GetBool(userID, "ai.enabled")
	if !enabled {
		return nil, fmt.Errorf("AI features are not enabled")
	}

	// Create embedding function
	embeddingFunc, err := s.createEmbeddingFunc(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding function: %w", err)
	}

	// Get collection
	collection, err := s.vectorDB.GetOrCreateCollection(ctx, userID, embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}

	// Check collection count and adjust limit if necessary
	// chromem-go requires nResults <= number of documents in collection
	docCount := collection.Count()
	if docCount == 0 {
		logger.Info("[EmbeddingService] collection is empty, no documents to query")
		return []DiarySearchResult{}, nil
	}
	if limit > docCount {
		logger.Debug("[EmbeddingService] adjusting limit from %d to %d (collection size)", limit, docCount)
		limit = docCount
	}

	// Query similar documents
	results, err := collection.Query(ctx, query, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}

	// Convert to DiarySearchResult
	searchResults := make([]DiarySearchResult, 0, len(results))
	for _, result := range results {
		searchResults = append(searchResults, DiarySearchResult{
			ID:      result.ID,
			Date:    result.Metadata["date"],
			Content: result.Content,
			Mood:    result.Metadata["mood"],
			Weather: result.Metadata["weather"],
			Score:   result.Similarity,
		})
	}

	logger.Info("[EmbeddingService] found %d similar diaries", len(searchResults))
	return searchResults, nil
}

// GetVectorStats returns statistics about the vector index for a user
func (s *EmbeddingService) GetVectorStats(ctx context.Context, userID string) (*VectorStats, error) {
	stats := &VectorStats{}

	// Get all diaries for the user
	diaries, err := s.app.Dao().FindRecordsByFilter(
		"diaries",
		"owner = {:owner}",
		"-updated",
		-1,
		0,
		map[string]any{"owner": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diaries: %w", err)
	}
	stats.DiaryCount = len(diaries)

	// Get collection
	collection := s.vectorDB.GetCollection(userID)

	// Compare each diary with its vector
	for _, diary := range diaries {
		diaryID := diary.GetId()
		diaryUpdated := diary.Updated.Time()

		// Try to get the vector document
		if collection == nil {
			stats.PendingCount++
			continue
		}

		doc, err := collection.GetByID(ctx, diaryID)
		if err != nil {
			// Document not found - pending
			stats.PendingCount++
			continue
		}

		// Check build time from metadata
		builtAtStr, ok := doc.Metadata["built_at"]
		if !ok || builtAtStr == "" {
			// No build time - treat as outdated
			stats.OutdatedCount++
			continue
		}

		builtAt, err := time.Parse(time.RFC3339Nano, builtAtStr)
		if err != nil {
			// Fallback to RFC3339 for backward compatibility
			builtAt, err = time.Parse(time.RFC3339, builtAtStr)
			if err != nil {
				stats.OutdatedCount++
				continue
			}
		}

		// Compare times
		if diaryUpdated.After(builtAt) {
			stats.OutdatedCount++
		} else {
			stats.IndexedCount++
		}
	}

	return stats, nil
}
