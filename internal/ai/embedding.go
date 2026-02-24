package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// EmbeddingClient handles text → vector encoding for semantic memory.
type EmbeddingClient struct {
	APIKey  string
	Model   string
	BaseURL string
}

// EmbeddingResponse mirrors OpenAI embedding structure.
type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewEmbeddingClient creates a client for embeddings (OpenAI only)
func NewEmbeddingClient() *EmbeddingClient {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		key = "" // Will fail at runtime with clear error
	}

	base := os.Getenv("OPENAI_API_BASE")
	if base == "" {
		base = "https://api.openai.com/v1"
	}

	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = "text-embedding-3-small" // default model
	}

	return &EmbeddingClient{
		APIKey:  key,
		Model:   model,
		BaseURL: base,
	}
}

// GenerateEmbedding encodes text into a dense vector representation.
func (e *EmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("text is empty")
	}

	payload := map[string]any{
		"model": e.Model,
		"input": text,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/embeddings", e.BaseURL), bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// Increased timeout to 60 seconds for longer texts
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding API call failed: %w", err)
	}
	defer resp.Body.Close()

	var data EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid embedding response: %w", err)
	}

	if data.Error != nil {
		return nil, fmt.Errorf("embedding API error: %s", data.Error.Message)
	}

	if len(data.Data) == 0 || len(data.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding vector returned")
	}

	return data.Data[0].Embedding, nil
}

// GenerateBatchEmbeddings handles multiple texts at once (optimized for training / recall preloads)
func (e *EmbeddingClient) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided for embedding")
	}

	payload := map[string]any{
		"model": e.Model,
		"input": texts,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/embeddings", e.BaseURL), bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding API error: %w", err)
	}
	defer resp.Body.Close()

	var data EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	if len(data.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	vectors := make([][]float32, len(data.Data))
	for i, d := range data.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}
