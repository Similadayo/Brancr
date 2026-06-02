package ai

// This file is a thin compatibility shim over internal/llm.
//
// The low-level LLM transport (HTTP, vision, model selection) now lives in
// internal/llm. AIClient embeds *llm.Client so the transport methods
// (Generate, GenerateWithUsage, AnalyzeImage, vision) are promoted, while the
// domain-specific methods (ParseMenuText, GenerateBusinessProfile,
// GenerateBusinessKnowledge, ...) remain defined on *AIClient in their own files.
//
// The type aliases below keep existing call sites (ai.Message, ai.TokenUsage,
// ai.ContentPart, ...) compiling unchanged. New code should depend on internal/llm
// directly; these aliases are an interim bridge during AI-layer consolidation.

import (
	"context"

	"github.com/similadayo/brancr/internal/llm"
)

// Model tier constants (re-exported from internal/llm).
const (
	ModelNano = llm.ModelNano
	ModelMini = llm.ModelMini
	ModelFull = llm.ModelFull
)

// Transport type aliases (the underlying types live in internal/llm).
type (
	Message         = llm.Message
	ContentPart     = llm.ContentPart
	TokenUsage      = llm.TokenUsage
	GenerateResult  = llm.GenerateResult
	ModerationError = llm.ModerationError
	ModelSelector   = llm.ModelSelector
)

// Function re-exports.
var (
	IsModerationError = llm.IsModerationError
	ModelForIntent    = llm.ModelForIntent
	NewModelSelector  = llm.NewModelSelector
)

// AIClientInterface abstracts the AI client for easier testing and mocking.
type AIClientInterface interface {
	Generate(ctx context.Context, messages []Message) (string, error)
	GenerateWithUsage(ctx context.Context, messages []Message) (*GenerateResult, error)
	GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error)
	GenerateWithVisionWithUsage(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, *TokenUsage, error)
	AnalyzeImage(ctx context.Context, imageURL string) (string, error)
	ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ParseMenuTextResult, error)
	ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ParseMenuTextResult, error)
	GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*BusinessProfileResult, error)
	GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*KnowledgeResult, error)
}

// AIClient wraps the low-level llm.Client transport and carries the
// domain-specific generation methods defined across the ai package.
type AIClient struct {
	*llm.Client
}

// NewAIClient builds an AIClient backed by a transport client from the environment.
func NewAIClient() *AIClient {
	return &AIClient{Client: llm.NewClient()}
}

// WithModel returns a copy of the client targeting a different model, preserving
// the *AIClient type so domain methods remain available on the result.
func (c *AIClient) WithModel(model string) *AIClient {
	return &AIClient{Client: c.Client.WithModel(model)}
}
