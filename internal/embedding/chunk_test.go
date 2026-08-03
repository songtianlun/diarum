package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens(""); got != 0 {
		t.Fatalf("estimateTokens(\"\") = %d, want 0", got)
	}
	if got := estimateTokens("hello world"); got <= 0 || got > 11 {
		t.Fatalf("estimateTokens ascii = %d, want between 1 and 11", got)
	}
	// CJK characters must be counted heavier than ASCII characters.
	if estimateTokens("今天天气很好") <= estimateTokens("abcdef") {
		t.Fatal("estimateTokens should weight CJK higher than ASCII")
	}
}

func TestSplitTextForEmbedding(t *testing.T) {
	if got := splitTextForEmbedding("   \n  ", 100); got != nil {
		t.Fatalf("splitTextForEmbedding blank = %#v, want nil", got)
	}

	short := "a short diary entry"
	chunks := splitTextForEmbedding(short, defaultMaxEmbeddingTokens)
	if len(chunks) != 1 || chunks[0] != short {
		t.Fatalf("short text chunks = %#v, want single unmodified chunk", chunks)
	}

	cases := map[string]string{
		"paragraphs": strings.Repeat("今天天气很好，我去公园散步了。\n\n", 3000),
		"lines":      strings.Repeat("line of diary content\n", 8000),
		"no-breaks":  strings.Repeat("单", 40000),
		"ascii-run":  strings.Repeat("x", 120000),
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			chunks := splitTextForEmbedding(text, defaultMaxEmbeddingTokens)
			if len(chunks) < 2 {
				t.Fatalf("expected long text to be split, got %d chunk(s)", len(chunks))
			}
			var rebuilt strings.Builder
			for i, chunk := range chunks {
				if tokens := estimateTokens(chunk); tokens > defaultMaxEmbeddingTokens {
					t.Fatalf("chunk %d has %d estimated tokens, limit is %d", i, tokens, defaultMaxEmbeddingTokens)
				}
				if strings.TrimSpace(chunk) == "" {
					t.Fatalf("chunk %d is blank", i)
				}
				if !isValidUTF8(chunk) {
					t.Fatalf("chunk %d is not valid UTF-8", i)
				}
				rebuilt.WriteString(chunk)
			}
			if rebuilt.String() != text {
				t.Fatal("chunks do not reassemble into the original text")
			}
		})
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}

func TestAverageVectors(t *testing.T) {
	if got := averageVectors(nil, nil); got != nil {
		t.Fatalf("averageVectors(nil) = %#v, want nil", got)
	}

	single := [][]float32{{3, 4}}
	if got := averageVectors(single, nil); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("averageVectors single = %#v, want unmodified", got)
	}

	got := averageVectors([][]float32{{1, 0}, {0, 1}}, []float64{1, 1})
	want := float32(math.Sqrt2 / 2)
	if len(got) != 2 || math.Abs(float64(got[0]-want)) > 1e-6 || math.Abs(float64(got[1]-want)) > 1e-6 {
		t.Fatalf("averageVectors = %#v, want normalized [%v %v]", got, want, want)
	}

	// Weighted towards the first vector.
	weighted := averageVectors([][]float32{{1, 0}, {0, 1}}, []float64{9, 1})
	if weighted[0] <= weighted[1] {
		t.Fatalf("weighted average = %#v, want first component to dominate", weighted)
	}

	// Mismatched dimensions fall back to the first vector instead of corrupting.
	mismatch := averageVectors([][]float32{{1, 0}, {0, 1, 2}}, nil)
	if len(mismatch) != 2 {
		t.Fatalf("mismatched dimensions = %#v, want first vector", mismatch)
	}
}

func TestIsContextLengthError(t *testing.T) {
	if isContextLengthError(nil) {
		t.Fatal("isContextLengthError(nil) should be false")
	}
	realWorld := fmt.Errorf(`API returned status 400: {"error":{"message":"Provider API error: Invalid 'input': maximum context length is 8192 tokens."}}`)
	if !isContextLengthError(realWorld) {
		t.Fatal("isContextLengthError should match the provider's context length error")
	}
	if isContextLengthError(fmt.Errorf("API returned status 502: upstream failed")) {
		t.Fatal("isContextLengthError should not match unrelated errors")
	}
}

// TestGenerateEmbeddingSplitsLongText verifies a diary far beyond the model's
// context window is embedded via multiple requests instead of failing.
func TestGenerateEmbeddingSplitsLongText(t *testing.T) {
	s := newTestStore(t)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	requests := 0
	withMockTransport(t, func(r *http.Request) (*http.Response, error) {
		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		requests++
		if estimateTokens(req.Input) > 8192 {
			return response(http.StatusBadRequest,
				`{"error":{"message":"Invalid 'input': maximum context length is 8192 tokens."}}`), nil
		}
		return response(http.StatusOK, `{"data":[{"embedding":[1,0]}]}`), nil
	})

	longDiary := strings.Repeat("今天写了很多东西，记录一下当时的心情。\n\n", 2000)
	vector, err := service.generateEmbedding(context.Background(), "https://mock.local", "key", "model", longDiary)
	if err != nil {
		t.Fatalf("generateEmbedding long text: %v", err)
	}
	if len(vector) != 2 {
		t.Fatalf("vector = %#v, want 2 dimensions", vector)
	}
	if requests < 2 {
		t.Fatalf("embedding requests = %d, want the text to be chunked", requests)
	}
}

// TestGenerateEmbeddingRetriesOnContextLengthError verifies we shrink the chunk
// budget when the provider's real tokenizer disagrees with our estimate.
func TestGenerateEmbeddingRetriesOnContextLengthError(t *testing.T) {
	s := newTestStore(t)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	// Pretend the provider only accepts inputs a quarter of our estimated budget.
	limit := defaultMaxEmbeddingTokens / 4
	withMockTransport(t, func(r *http.Request) (*http.Response, error) {
		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		if estimateTokens(req.Input) > limit {
			return response(http.StatusBadRequest,
				`{"error":{"message":"Invalid 'input': maximum context length is 8192 tokens."}}`), nil
		}
		return response(http.StatusOK, `{"data":[{"embedding":[0,1]}]}`), nil
	})

	longDiary := strings.Repeat("a long english diary sentence that keeps going. ", 4000)
	vector, err := service.generateEmbedding(context.Background(), "https://mock.local", "key", "model", longDiary)
	if err != nil {
		t.Fatalf("generateEmbedding retry: %v", err)
	}
	if len(vector) != 2 {
		t.Fatalf("vector = %#v, want 2 dimensions", vector)
	}
}

// TestGenerateEmbeddingDoesNotRetryOtherErrors keeps unrelated failures fast.
func TestGenerateEmbeddingDoesNotRetryOtherErrors(t *testing.T) {
	s := newTestStore(t)
	vectorDB := newTestVectorDB(t)
	service := NewEmbeddingService(s, vectorDB)

	requests := 0
	withMockTransport(t, func(r *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusBadGateway, "upstream failed"), nil
	})

	if _, err := service.generateEmbedding(context.Background(), "https://mock.local", "key", "model", "hello"); err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (no retry for non-context errors)", requests)
	}
}
