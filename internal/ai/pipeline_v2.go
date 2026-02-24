package ai

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PipelineV2 orchestrates the full v2.0 customer responder pipeline.
// All 5 stages run in sequence, each with clear responsibility.
type PipelineV2 struct {
	// Stage components
	MessageIngestion  *MessageIngestion
	IntentExtractor   *IntentExtractorV2
	DecisionResolver  *DecisionResolver
	ActionExecutor    *ActionExecutor
	ResponseTemplates *ResponseTemplates

	// Shared dependencies
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewPipelineV2 creates a fully wired v2 pipeline.
func NewPipelineV2(db *gorm.DB, aiClient *AIClient, logger *zap.Logger, brandName string) *PipelineV2 {
	paymentProvider := NewHardcodedPaymentProvider(db, logger)

	return &PipelineV2{
		MessageIngestion:  NewMessageIngestion(db, logger),
		IntentExtractor:   NewIntentExtractorV2(aiClient, logger),
		DecisionResolver:  NewDecisionResolver(db, logger),
		ActionExecutor:    NewActionExecutor(db, logger, paymentProvider),
		ResponseTemplates: NewResponseTemplates(brandName, "NGN"),
		DB:                db,
		Logger:            logger,
	}
}

// PipelineInput is the entry point to the pipeline.
type PipelineInput struct {
	Raw            *RawMessage        // Use this for full pipeline from raw input
	Normalized     *NormalizedMessage // Use this if already normalized
	Message        string             // Shortcut: just the message text
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	ConversationID uuid.UUID
	Platform       string
}

// PipelineOutput is the final result.
type PipelineOutput struct {
	Reply          string
	ReplyParts     []string // For multi-message platforms
	Order          *ActionResult
	UsedFallback   bool
	IntentDetected CanonicalIntent
	ActionTaken    V2ActionType
}

// Process runs a message through all 5 stages.
func (p *PipelineV2) Process(ctx context.Context, input *PipelineInput) (*PipelineOutput, error) {
	output := &PipelineOutput{}

	// =============================================================
	// STAGE 1: MESSAGE INGESTION
	// =============================================================
	var normalized *NormalizedMessage
	var err error

	if input.Raw != nil {
		normalized, err = p.MessageIngestion.Ingest(ctx, input.Raw)
		if err != nil {
			p.Logger.Error("Stage 1 failed", zap.Error(err))
			output.Reply = p.ResponseTemplates.SystemError()
			output.UsedFallback = true
			return output, nil
		}
	} else if input.Normalized != nil {
		normalized = input.Normalized
	} else {
		// Build from shortcut fields
		normalized = &NormalizedMessage{
			TenantID:       input.TenantID,
			CustomerID:     input.CustomerID,
			ConversationID: input.ConversationID,
			Platform:       input.Platform,
			Text:           input.Message,
		}
	}

	p.Logger.Info("📥 Stage 1: Message ingested",
		zap.Stringer("conversation_id", normalized.ConversationID))

	// =============================================================
	// STAGE 2: INTENT EXTRACTION
	// =============================================================

	// Try quick classification first (no AI call)
	intentResult := p.IntentExtractor.QuickClassify(normalized.Text)

	if intentResult == nil {
		// Fall back to AI extraction
		intentResult, err = p.IntentExtractor.ExtractIntent(ctx, normalized.TenantID, normalized.Text, nil, nil)
		if err != nil {
			p.Logger.Warn("Stage 2 AI failed, using fallback", zap.Error(err))
			intentResult = &IntentExtractionResult{
				Intent:     IntentUnknown,
				Confidence: 0.0,
				Entities:   make(map[string]interface{}),
			}
		}
	}

	output.IntentDetected = intentResult.Intent

	p.Logger.Info("🎯 Stage 2: Intent extracted",
		zap.String("intent", string(intentResult.Intent)),
		zap.Float64("confidence", intentResult.Confidence))

	// =============================================================
	// STAGE 3: DECISION RESOLVER
	// =============================================================
	resolverInput := ResolverInput{
		TenantID:       normalized.TenantID,
		CustomerID:     normalized.CustomerID,
		ConversationID: normalized.ConversationID,
		Platform:       normalized.Platform,
		Intent:         intentResult.Intent,
		Confidence:     intentResult.Confidence,
		Entities:       intentResult.Entities,
	}

	decision, err := p.DecisionResolver.Resolve(ctx, resolverInput)
	if err != nil {
		p.Logger.Error("Stage 3 failed", zap.Error(err))
		output.Reply = p.ResponseTemplates.SystemError()
		output.UsedFallback = true
		return output, nil
	}

	output.ActionTaken = decision.ActionType

	p.Logger.Info("🧠 Stage 3: Decision resolved",
		zap.String("action", string(decision.ActionType)),
		zap.String("current_state", string(decision.CurrentState)),
		zap.String("target_state", string(decision.TargetState)))

	// =============================================================
	// STAGE 4: ACTION EXECUTOR
	// =============================================================
	var actionResult *ActionResult

	if decision.ActionType != V2ActionNone && decision.ActionType != V2ActionAskClarification {
		actionResult, err = p.ActionExecutor.Execute(ctx, decision, normalized.TenantID, normalized.CustomerID, normalized.ConversationID, normalized.Platform)
		if err != nil {
			p.Logger.Warn("Stage 4 failed", zap.Error(err))
			output.Reply = p.ResponseTemplates.SystemError()
			output.UsedFallback = true
			return output, nil
		}

		output.Order = actionResult

		p.Logger.Info("⚡ Stage 4: Action executed",
			zap.String("action", string(decision.ActionType)),
			zap.Bool("success", actionResult.Success),
			zap.Bool("state_changed", actionResult.StateChanged))
	}

	// =============================================================
	// STAGE 5: RESPONSE COMPOSER
	// =============================================================

	// Use template-based response (fallback-first approach)
	reply := p.ResponseTemplates.GetTemplateForAction(decision.ActionType, actionResult)

	if reply != "" {
		output.Reply = reply
		output.UsedFallback = true
		p.Logger.Info("📝 Stage 5: Using fallback template",
			zap.String("action", string(decision.ActionType)))
	} else {
		// No template for this action - AI would compose here
		// For now, provide a generic response
		output.Reply = p.composeGenericResponse(decision, actionResult)
		p.Logger.Info("📝 Stage 5: Generic response",
			zap.String("action", string(decision.ActionType)))
	}

	return output, nil
}

// composeGenericResponse provides a basic response when no template exists.
func (p *PipelineV2) composeGenericResponse(decision *ResolvedAction, result *ActionResult) string {
	switch decision.ActionType {
	case V2ActionNone:
		return "How can I help you today?"
	case V2ActionAskClarification:
		return p.ResponseTemplates.LowConfidence(nil)
	case V2ActionRejectIntent:
		if decision.ValidationErr != nil {
			return fmt.Sprintf("I can't do that right now. %s", decision.FallbackReason)
		}
		return "I can't do that right now."
	default:
		return "Done! Anything else?"
	}
}

// ProcessSimple is a convenience method for quick processing.
func (p *PipelineV2) ProcessSimple(ctx context.Context, tenantID, customerID, conversationID uuid.UUID, platform, message string) (string, error) {
	input := &PipelineInput{
		TenantID:       tenantID,
		CustomerID:     customerID,
		ConversationID: conversationID,
		Platform:       platform,
		Message:        message,
	}

	output, err := p.Process(ctx, input)
	if err != nil {
		return "", err
	}

	return output.Reply, nil
}
