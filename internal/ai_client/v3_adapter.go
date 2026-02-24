package ai_client

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	v3 "github.com/similadayo/brancr/internal/ai/v3"
	"github.com/similadayo/brancr/internal/ai_contract"
	"go.uber.org/zap"
)

// =============================================================================
// V3 ADAPTER - Temporary Bridge to Existing Logic
// =============================================================================
// This adapter wraps the existing V3 AI logic behind the new Client interface.
//
// PHASE-1: This is a temporary adapter. Future phases will replace this with
// a Python service client.
//
// IMPORTANT: This adapter does NOT change behavior - it only restructures
// how we call the existing code.
// =============================================================================

// v3Adapter implements the Client interface by delegating to existing V3 logic.
type v3Adapter struct {
	intentExtractor v3.IntentExtractor
	logger          *zap.Logger
}

// NewV3Adapter creates a new adapter that delegates to V3 logic.
// PHASE-1: This is temporary - will be replaced with Python client.
func NewV3Adapter(intentExtractor v3.IntentExtractor, logger *zap.Logger) Client {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &v3Adapter{
		intentExtractor: intentExtractor,
		logger:          logger,
	}
}

// Analyze implements the Client interface by delegating to V3 intent extraction.
// PHASE-1 NOTE: This currently only wraps intent detection.
// Full reasoning will be integrated in subsequent steps.
func (a *v3Adapter) Analyze(ctx context.Context, input AnalyzeInput) (*ai_contract.AISuggestion, error) {
	// PHASE-1: Convert AnalyzeInput to V3 ConversationContext
	convCtx := a.convertToV3Context(input)

	// PHASE-1: Call existing V3 intent detector
	intent := a.intentExtractor.Classify(convCtx, input.Message)

	// PHASE-1: Wrap V3 intent into AISuggestion contract
	suggestion := a.wrapIntentAsSuggestion(intent, input.Message)

	// PHASE-1 LOGGING: Log the AI suggestion at the boundary
	a.logger.Info("ai_client_suggestion_generated",
		zap.String("intent", suggestion.Intent),
		zap.Float64("confidence", suggestion.Confidence),
		zap.String("method", suggestion.DetectionMethod),
		zap.Int("num_actions", len(suggestion.Actions)),
	)

	return suggestion, nil
}

// Generate is a stub for v3Adapter - not implemented for legacy fallback.
// PHASE-8: The v3Adapter is only used as a fallback for Analyze.
// Response generation should use the Python service.
func (a *v3Adapter) Generate(ctx context.Context, input interface{}) (string, error) {
	a.logger.Warn("v3_adapter_generate_not_implemented",
		zap.String("note", "v3Adapter is for analysis only, not response generation"),
	)
	return "", fmt.Errorf("generate not implemented in v3 adapter - use Python AI service")
}

// convertToV3Context converts the new AnalyzeInput to V3's ConversationContext.
// PHASE-1: This is a temporary adapter function.
func (a *v3Adapter) convertToV3Context(input AnalyzeInput) *v3.ConversationContext {
	ctx := &v3.ConversationContext{
		TenantID:       input.TenantID,
		CustomerID:     input.CustomerID,
		ConversationID: input.ConversationID,
		Products:       make([]v3.Product, 0),
		Services:       make([]v3.Service, 0),
		RecentMessages: make([]v3.Message, 0),
	}

	// Convert recent messages
	for _, msg := range input.Context.RecentMessages {
		ctx.RecentMessages = append(ctx.RecentMessages, v3.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Convert available products
	for _, p := range input.Context.AvailableProducts {
		ctx.Products = append(ctx.Products, v3.Product{
			ID:       p.ID,
			Name:     p.Name,
			Price:    p.Price,
			Currency: p.Currency,
		})
	}

	// Convert available services
	for _, s := range input.Context.AvailableServices {
		ctx.Services = append(ctx.Services, v3.Service{
			ID:   s.ID,
			Name: s.Name,
		})
	}

	// Convert payment info
	if input.Context.PaymentInfo != nil {
		ctx.PaymentInfo = &v3.PaymentConfig{
			BankName:      input.Context.PaymentInfo.BankName,
			AccountNumber: input.Context.PaymentInfo.AccountNumber,
			AccountName:   input.Context.PaymentInfo.AccountName,
		}
	}

	// Convert order summary if present
	if input.Context.OrderSummary != nil {
		ctx.PendingCart = make([]v3.PendingCartItem, 0)
		for _, item := range input.Context.OrderSummary.Items {
			ctx.PendingCart = append(ctx.PendingCart, v3.PendingCartItem{
				ProductID:   item.ProductID,
				ProductName: item.ProductName,
				Quantity:    item.Quantity,
				UnitPrice:   item.UnitPrice,
			})
		}

		// If there's an active order, set it
		if input.Context.OrderSummary.OrderID != (uuid.UUID{}) {
			ctx.CurrentOrder = &v3.Order{
				ID:    input.Context.OrderSummary.OrderID,
				State: v3.OrderState(input.Context.OrderSummary.Status),
			}
		}
	}

	return ctx
}

// wrapIntentAsSuggestion wraps V3 Intent into the new AISuggestion contract.
// PHASE-1: This creates the boundary between old and new types.
func (a *v3Adapter) wrapIntentAsSuggestion(intent v3.Intent, originalMessage string) *ai_contract.AISuggestion {
	suggestion := &ai_contract.AISuggestion{
		Intent:          string(intent.Type),
		Confidence:      intent.Confidence,
		DetectionMethod: intent.Source,
		Explanation:     a.generateExplanation(intent, originalMessage),
		Actions:         make([]ai_contract.AISuggestedAction, 0),
	}

	// Convert emotion if present
	if intent.Emotion.Type != "" {
		suggestion.Emotion = ai_contract.EmotionSuggestion{
			Type:      string(intent.Emotion.Type),
			Intensity: intent.Emotion.Intensity,
		}
	}

	// Convert entities to map
	if intent.Entities.Product != nil || intent.Entities.Quantity > 0 {
		suggestion.Entities = make(map[string]interface{})
		if intent.Entities.Product != nil {
			suggestion.Entities["product_id"] = intent.Entities.Product.ID
			suggestion.Entities["product_name"] = intent.Entities.Product.Name
			suggestion.Entities["product_price"] = intent.Entities.Product.Price
		}
		if intent.Entities.Quantity > 0 {
			suggestion.Entities["quantity"] = intent.Entities.Quantity
		}
	}

	// Generate suggested action based on intent
	// PHASE-1: This is where we translate intent to suggested actions
	action := a.intentToAction(intent)
	if action != nil {
		suggestion.Actions = append(suggestion.Actions, *action)
	}

	return suggestion
}

// intentToAction converts a V3 Intent to an AISuggestedAction.
// PHASE-1: This mapping will be refined as we integrate more V3 logic.
func (a *v3Adapter) intentToAction(intent v3.Intent) *ai_contract.AISuggestedAction {
	action := &ai_contract.AISuggestedAction{
		Type:      string(intent.Type),
		Score:     intent.Confidence,
		Params:    make(map[string]interface{}),
		Reasoning: a.generateActionReasoning(intent),
	}

	// Add entity parameters
	if intent.Entities.Product != nil {
		action.Params["product_id"] = intent.Entities.Product.ID.String()
		action.Params["product_name"] = intent.Entities.Product.Name
	}
	if intent.Entities.Quantity > 0 {
		action.Params["quantity"] = intent.Entities.Quantity
	}

	return action
}

// generateExplanation creates a human-readable explanation of the AI's decision.
func (a *v3Adapter) generateExplanation(intent v3.Intent, message string) string {
	method := "heuristic pattern matching"
	if intent.Source == "llm_discovery" {
		method = "LLM analysis"
	}

	return "Detected intent '" + string(intent.Type) + "' via " + method +
		" with " + formatConfidence(intent.Confidence) + " confidence"
}

// generateActionReasoning creates reasoning for why an action was suggested.
func (a *v3Adapter) generateActionReasoning(intent v3.Intent) string {
	if intent.Entities.Product != nil {
		return "User mentioned product '" + intent.Entities.Product.Name + "'"
	}
	return "Based on detected intent: " + string(intent.Type)
}

// formatConfidence formats a confidence score as a percentage string.
func formatConfidence(conf float64) string {
	pct := int(conf * 100)
	if pct >= 90 {
		return "high"
	} else if pct >= 70 {
		return "medium"
	}
	return "low"
}
