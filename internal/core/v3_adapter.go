package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	v3 "github.com/similadayo/brancr/internal/ai/v3"
)

// v3PipelineAdapter adapts v3.Pipeline to the MessageHandler's Responder interface
// This allows MessageHandler to use V3Pipeline directly instead of CustomerResponder wrapper
type v3PipelineAdapter struct {
	pipeline v3.Pipeline
}

// GenerateCustomerReply implements the Responder interface using V3Pipeline
// Converts the simple string-based interface to V3's rich request/response model
func (a *v3PipelineAdapter) GenerateCustomerReply(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	message string,
	intent string,
	platform string,
	conversationID ...uuid.UUID,
) (string, error) {
	// Get conversation ID
	var convID uuid.UUID
	if len(conversationID) > 0 {
		convID = conversationID[0]
	} else {
		convID = uuid.New()
	}

	// Build V3 request
	req := v3.ConversationRequest{
		TenantID:       tenantID,
		CustomerID:     customerID,
		ConversationID: convID,
		Message:        message,
		Platform:       platform,
		Timestamp:      time.Now(),
	}

	// Call V3 Pipeline
	aiResp, err := a.pipeline.Process(ctx, req)
	if err != nil {
		return "", err
	}

	// Extract human text from V3 response
	// TODO: In the future, we could return the full AIResponse to access metadata
	return aiResp.HumanText, nil
}

// NewV3PipelineAdapter creates a new adapter that wraps v3.Pipeline
func NewV3PipelineAdapter(pipeline v3.Pipeline) *v3PipelineAdapter {
	return &v3PipelineAdapter{pipeline: pipeline}
}
