package embedding

import (
	"math"
	"strings"
)

const (
	// defaultMaxEmbeddingTokens is the token budget used for a single embedding
	// request. Most OpenAI-compatible embedding models cap the input at 8192
	// tokens; we keep headroom because token counts are only estimated here.
	defaultMaxEmbeddingTokens = 6000

	// minEmbeddingChunkTokens is the smallest budget we are willing to fall back
	// to when the provider still reports a context-length error.
	minEmbeddingChunkTokens = 512

	// maxEmbeddingSplitRetries limits how many times we halve the chunk budget
	// after a context-length error from the provider.
	maxEmbeddingSplitRetries = 4
)

// asciiTokenWeight / wideTokenWeight approximate a cl100k-style tokenizer:
// roughly 3 ASCII characters per token, and up to 1.5 tokens per CJK character.
// Both values intentionally overestimate so chunks stay under the real limit.
const (
	asciiTokenWeight = 1.0 / 3.0
	wideTokenWeight  = 1.5
)

// chunkSeparators lists split points from coarse to fine, so chunks break on
// natural boundaries (paragraphs, then lines, then sentences, then words).
var chunkSeparators = []string{
	"\n\n",
	"\n",
	"。", "！", "？", "；",
	". ", "! ", "? ", "; ",
	"，", "、",
	", ",
	" ",
}

// tokenWeight returns the estimated token cost of a single rune.
func tokenWeight(r rune) float64 {
	if r < 128 {
		return asciiTokenWeight
	}
	return wideTokenWeight
}

// estimateTokens approximates how many tokens the given text will consume.
// It deliberately errs on the high side.
func estimateTokens(text string) int {
	total := 0.0
	for _, r := range text {
		total += tokenWeight(r)
	}
	return int(math.Ceil(total))
}

// splitTextForEmbedding splits text into chunks that each fit within maxTokens.
// It returns a single-element slice when the text already fits.
func splitTextForEmbedding(text string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = defaultMaxEmbeddingTokens
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if estimateTokens(text) <= maxTokens {
		return []string{text}
	}
	return mergeChunks(splitRecursive(text, maxTokens, 0), maxTokens)
}

// splitRecursive breaks text down using progressively finer separators until
// every piece fits within maxTokens.
func splitRecursive(text string, maxTokens, sepIndex int) []string {
	if text == "" {
		return nil
	}
	if estimateTokens(text) <= maxTokens {
		return []string{text}
	}
	if sepIndex >= len(chunkSeparators) {
		return splitByRunes(text, maxTokens)
	}

	parts := strings.SplitAfter(text, chunkSeparators[sepIndex])
	if len(parts) <= 1 {
		// Separator not present, try the next one.
		return splitRecursive(text, maxTokens, sepIndex+1)
	}

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, splitRecursive(part, maxTokens, sepIndex+1)...)
	}
	return out
}

// splitByRunes is the last-resort splitter for text without any usable
// separator (a single very long "word"). It never splits a UTF-8 rune.
func splitByRunes(text string, maxTokens int) []string {
	var (
		chunks []string
		buf    strings.Builder
		tokens float64
	)
	budget := float64(maxTokens)

	for _, r := range text {
		weight := tokenWeight(r)
		if buf.Len() > 0 && tokens+weight > budget {
			chunks = append(chunks, buf.String())
			buf.Reset()
			tokens = 0
		}
		buf.WriteRune(r)
		tokens += weight
	}
	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}

// mergeChunks greedily recombines adjacent pieces so we issue as few embedding
// requests as possible while staying under the budget.
func mergeChunks(pieces []string, maxTokens int) []string {
	if len(pieces) == 0 {
		return nil
	}

	merged := make([]string, 0, len(pieces))
	current := ""
	currentTokens := 0

	for _, piece := range pieces {
		pieceTokens := estimateTokens(piece)
		if current != "" && currentTokens+pieceTokens > maxTokens {
			merged = append(merged, current)
			current = ""
			currentTokens = 0
		}
		current += piece
		currentTokens += pieceTokens
	}
	if current != "" {
		merged = append(merged, current)
	}

	// Drop chunks that carry no actual content (pure whitespace).
	out := make([]string, 0, len(merged))
	for _, chunk := range merged {
		if strings.TrimSpace(chunk) != "" {
			out = append(out, chunk)
		}
	}
	return out
}

// averageVectors mean-pools chunk embeddings with the given weights and
// L2-normalizes the result, so long diaries map to a single vector.
func averageVectors(vectors [][]float32, weights []float64) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	if len(vectors) == 1 {
		return vectors[0]
	}

	dim := len(vectors[0])
	sum := make([]float64, dim)
	totalWeight := 0.0

	for i, vec := range vectors {
		if len(vec) != dim {
			// Dimension mismatch means something is wrong upstream; fall back to
			// the first vector rather than producing a corrupt embedding.
			return vectors[0]
		}
		weight := 1.0
		if i < len(weights) && weights[i] > 0 {
			weight = weights[i]
		}
		for j, v := range vec {
			sum[j] += float64(v) * weight
		}
		totalWeight += weight
	}

	if totalWeight == 0 {
		return vectors[0]
	}

	norm := 0.0
	for j := range sum {
		sum[j] /= totalWeight
		norm += sum[j] * sum[j]
	}
	norm = math.Sqrt(norm)

	out := make([]float32, dim)
	for j, v := range sum {
		if norm > 0 {
			v /= norm
		}
		out[j] = float32(v)
	}
	return out
}

// isContextLengthError reports whether an embedding API error was caused by the
// input exceeding the model's context window.
func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"maximum context length",
		"context length",
		"context_length_exceeded",
		"too many tokens",
		"input is too long",
		"string too long",
		"reduce the length",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
