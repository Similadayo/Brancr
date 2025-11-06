package interfaces

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
)

// AIClient defines the interface for AI/LLM operations
type AIClient interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

// Message represents a chat message for AI generation
type Message struct {
	Role    string
	Content string
}

// IntentDetector detects customer intent from messages
type IntentDetector interface {
	DetectIntent(ctx context.Context, tenantID uint, message string) (string, float64, error)
}

// CustomerResponder generates AI responses for customer interactions
type CustomerResponder interface {
	Respond(ctx context.Context, tenantID, customerID uint, message, platform string) (string, error)
}

// MemoryService manages conversation memory and context
type MemoryService interface {
	Store(ctx context.Context, tenantID, customerID uint, text string, category string) error
	Recall(ctx context.Context, tenantID, customerID uint, query string) ([]string, error)
}

// CaptionOptimizer optimizes captions for social media posts
type CaptionOptimizer interface {
	OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error)
}

// OptimizeCaptionRequest represents a caption optimization request
type OptimizeCaptionRequest struct {
	OriginalCaption string
	Platform        string
	MediaType       string
	TenantID        uint
}

// OptimizeCaptionResult contains the optimized caption and metadata
type OptimizeCaptionResult struct {
	OptimizedCaption string
	Hashtags         []string
	SuggestedTime    string
	Improvements     string
}

// Responder generates conversational AI responses
type Responder interface {
	GenerateConversationalReply(ctx context.Context, tenantID uint, userMsg string) (string, error)
}

// PersonaService retrieves tenant persona information
type PersonaService interface {
	GetPersona(tenantID uint) (storage.Persona, error)
}

