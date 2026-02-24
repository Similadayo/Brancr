package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	v3 "github.com/similadayo/brancr/internal/ai/v3"
	"github.com/similadayo/brancr/internal/executor"
	"github.com/similadayo/brancr/internal/rules"
	"github.com/similadayo/brancr/pkg/metrics"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// llmClientAdapter bridges ai.AIClient with v3.LLMClient interface
type llmClientAdapter struct {
	client *AIClient
}

// Generate implements v3.LLMClient interface
func (a *llmClientAdapter) Generate(ctx context.Context, messages []v3.LLMMessage) (string, error) {
	// Convert v3.LLMMessage to ai.Message
	aiMessages := make([]Message, len(messages))
	for i, m := range messages {
		aiMessages[i] = Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	return a.client.Generate(ctx, aiMessages)
}

// CustomerResponder generates AI replies for customer conversations
// V3 PIPELINE ONLY - All V2 code removed
type CustomerResponder struct {
	Client                   *AIClient
	Memory                   *MemoryService
	DB                       *gorm.DB
	Logger                   *zap.Logger
	Metrics                  *metrics.Metrics
	V3Pipeline               v3.Pipeline
	RetrievalService         interface{} // Stub for social package compatibility
	UsageRecorder            interface{} // Stub for core package compatibility
	QuotaService             interface{} // Stub for core package compatibility
	SalesConversationManager interface{} // Stub for core package compatibility
	PaymentInstructionGen    interface{} // Stub for core package compatibility
	OrderService             interface{} // Stub for core package compatibility
	CustomerExtractor        interface{} // Stub for core package compatibility
	IndustryService          interface{} // Stub for core package compatibility
	TenantNotifier           interface{} // Stub for core package compatibility
}

// SetPublicURL is a stub for compatibility
func (r *CustomerResponder) SetPublicURL(url string) {
	// V3 pipeline doesn't use public URL
}

// NewCustomerResponder creates a new CustomerResponder
// V3 PIPELINE ONLY - All V2 initialization removed
func NewCustomerResponder(client *AIClient, db *gorm.DB, logger *zap.Logger, memory *MemoryService, metricsSvc *metrics.Metrics, quotaSvc interface{}) *CustomerResponder {
	// Create LLM adapter
	var llmClient v3.LLMClient
	if client != nil {
		llmClient = &llmClientAdapter{client: client}
	}

	// Initialize V3 pipeline with minimal config
	// V3 will handle all internal initialization
	v3Pipeline := v3.NewPipeline(v3.PipelineConfig{
		DB:             db,
		Logger:         logger,
		LLMClient:      llmClient,
		Negotiator:     NewNegotiationAdapter(),
		RulesEngine:    rules.NewBaseEngine(),
		ActionRegistry: executor.NewRegistry(),
	})

	if logger != nil {
		logger.Info("v3_pipeline_initialized",
			zap.Bool("has_llm", llmClient != nil),
		)
	}

	return &CustomerResponder{
		Client:     client,
		Memory:     memory,
		DB:         db,
		Logger:     logger,
		Metrics:    metricsSvc,
		V3Pipeline: v3Pipeline,
	}
}

// GenerateCustomerReply generates an AI reply for a customer message
// V3 PIPELINE ONLY - All V2 and legacy code removed
func (r *CustomerResponder) GenerateCustomerReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (string, error) {
	// Test-safety: allow lightweight operation without DB
	if r == nil {
		return "", fmt.Errorf("customer responder is nil")
	}

	// Get conversation ID
	var convID uuid.UUID
	if len(conversationID) > 0 {
		convID = conversationID[0]
	} else {
		convID = uuid.New()
	}

	// V3 PIPELINE ONLY
	// Build request for V3 pipeline
	req := v3.ConversationRequest{
		TenantID:       tenantID,
		CustomerID:     customerID,
		ConversationID: convID,
		Message:        message,
		Platform:       platform,
		Timestamp:      time.Now(),
	}

	// Call V3 Pipeline
	aiResp, err := r.V3Pipeline.Process(ctx, req)
	if err != nil {
		if r.Logger != nil {
			r.Logger.Error("v3_pipeline_failed", zap.Error(err))
		}
		return "", err
	}

	// Extract human text from V3 response
	return aiResp.HumanText, nil
}
