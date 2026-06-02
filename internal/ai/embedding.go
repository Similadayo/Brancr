package ai

// This file is a thin compatibility shim over internal/llm.
//
// The embeddings transport now lives in internal/llm. These aliases keep existing
// call sites (ai.EmbeddingClient, ai.NewEmbeddingClient) compiling unchanged during
// AI-layer consolidation. New code should depend on internal/llm directly.

import "github.com/similadayo/brancr/internal/llm"

type (
	EmbeddingClient   = llm.EmbeddingClient
	EmbeddingResponse = llm.EmbeddingResponse
)

var NewEmbeddingClient = llm.NewEmbeddingClient
