package ai_client

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_contract"
	"go.uber.org/zap"
)

// =============================================================================
// AI CLIENT BOUNDARY - The Only AI Entry Point
// =============================================================================
// This package represents the ONLY place the system is allowed to "talk to AI".
//
// PHASE-1: This client currently delegates to existing V3 logic.
// Future phases will replace this with Python service calls.
//
// RULES:
// - This is the single entry point for all AI operations
// - Input must be structured (no raw DB models)
// - Output must be ai_contract.AISuggestion (advisory only)
// - No direct OpenAI calls from other packages
// =============================================================================

// Client is the interface for AI analysis operations.
// This is the boundary between the business system and AI reasoning.
type Client interface {
	// Analyze takes structured input and returns an AI suggestion.
	// The suggestion is advisory - the system decides whether to act on it.
	Analyze(ctx context.Context, input AnalyzeInput) (*ai_contract.AISuggestion, error)

	// Generate creates a customer-facing response based on decision context.
	// PHASE-8: Uses memory HUD for context-aware response generation.
	Generate(ctx context.Context, input interface{}) (string, error)
}

// AnalyzeInput contains all context needed for AI analysis.
// This struct must NOT contain raw database models - only lightweight data.
type AnalyzeInput struct {
	// TenantID identifies the business
	TenantID uuid.UUID `json:"tenant_id"`

	// ConversationID identifies the conversation thread
	ConversationID uuid.UUID `json:"conversation_id"`

	// CustomerID identifies the customer
	CustomerID uuid.UUID `json:"customer_id"`

	// Message is the raw user input
	Message string `json:"message"`

	// Context contains structured conversation context
	Context ConversationContext `json:"context"`

	// Platform indicates the source (e.g., "whatsapp", "telegram")
	Platform string `json:"platform"`

	// IntentHint is an optional hint from Go regex-based detection
	// PHASE-3: Python uses this as fallback if LLM classification fails
	IntentHint string `json:"intent_hint,omitempty"`
}

// ConversationContext provides structured context for AI analysis.
// This is a lightweight summary, not raw DB models.
type ConversationContext struct {
	// RecentMessages is the conversation history
	RecentMessages []Message `json:"recent_messages,omitempty"`

	// OrderSummary describes the current order state (if any)
	OrderSummary *OrderSummary `json:"order_summary,omitempty"`

	// AvailableProducts lists products the customer can order
	AvailableProducts []ProductSummary `json:"available_products,omitempty"`

	// AvailableServices lists services the customer can request
	AvailableServices []ServiceSummary `json:"available_services,omitempty"`

	// PaymentInfo describes how the customer can pay
	PaymentInfo *PaymentInfo `json:"payment_info,omitempty"`

	// BusinessProfile describes the business
	BusinessProfile *BusinessProfile `json:"business_profile,omitempty"`

	// CustomerProfile describes the customer
	CustomerProfile *CustomerProfile `json:"customer_profile,omitempty"`
}

// Message represents a single message in the conversation.
type Message struct {
	Role      string `json:"role"` // "user" or "assistant"
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

// OrderSummary describes the current order state.
type OrderSummary struct {
	OrderID     uuid.UUID          `json:"order_id,omitempty"`
	OrderNumber string             `json:"order_number,omitempty"`
	Status      string             `json:"status"` // "draft", "confirmed", "paid", etc.
	Items       []OrderItemSummary `json:"items"`
	Total       float64            `json:"total"`
	Currency    string             `json:"currency"`
}

// OrderItemSummary describes a single item in an order.
type OrderItemSummary struct {
	ProductID   uuid.UUID `json:"product_id"`
	ProductName string    `json:"product_name"`
	Quantity    int       `json:"quantity"`
	UnitPrice   float64   `json:"unit_price"`
	TotalPrice  float64   `json:"total_price"`
}

// ProductSummary describes a product available for purchase.
type ProductSummary struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Price       float64   `json:"price"`
	Currency    string    `json:"currency"`
	Available   bool      `json:"available"`
}

// ServiceSummary describes a service available for request.
type ServiceSummary struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	BasePrice   float64   `json:"base_price,omitempty"`
	Currency    string    `json:"currency,omitempty"`
}

// PaymentInfo describes payment options.
type PaymentInfo struct {
	BankName      string `json:"bank_name"`
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
}

// BusinessProfile describes the business.
type BusinessProfile struct {
	Name        string `json:"name"`
	Industry    string `json:"industry,omitempty"`
	Description string `json:"description,omitempty"`
}

// CustomerProfile describes the customer.
type CustomerProfile struct {
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`
}

// =============================================================================
// CONTRACT ADAPTER - Bridges ai_client.Client to ai_contract.AIClient
// =============================================================================

// ContractAdapter wraps ai_client.Client to implement ai_contract.AIClient interface.
// This allows the pipeline to accept interface{} input and convert internally.
type ContractAdapter struct {
	client Client
	logger *zap.Logger
}

// NewContractAdapter creates an adapter that bridges Client to ai_contract.AIClient.
func NewContractAdapter(client Client, logger *zap.Logger) ai_contract.AIClient {
	return &ContractAdapter{
		client: client,
		logger: logger,
	}
}

// Analyze implements ai_contract.AIClient interface.
// Converts interface{} input to AnalyzeInput and calls the underlying Client.
func (a *ContractAdapter) Analyze(ctx context.Context, input interface{}) (*ai_contract.AISuggestion, error) {
	// Convert input to AnalyzeInput
	var analyzeInput AnalyzeInput

	switch v := input.(type) {
	case AnalyzeInput:
		analyzeInput = v
	case *AnalyzeInput:
		analyzeInput = *v
	case map[string]interface{}:
		// Build AnalyzeInput from map
		if tenantID, ok := v["tenant_id"].(string); ok {
			if uid, err := uuid.Parse(tenantID); err == nil {
				analyzeInput.TenantID = uid
			}
		}
		if convID, ok := v["conversation_id"].(string); ok {
			if uid, err := uuid.Parse(convID); err == nil {
				analyzeInput.ConversationID = uid
			}
		}
		if custID, ok := v["customer_id"].(string); ok {
			if uid, err := uuid.Parse(custID); err == nil {
				analyzeInput.CustomerID = uid
			}
		}
		if msg, ok := v["message"].(string); ok {
			analyzeInput.Message = msg
		}
		if platform, ok := v["platform"].(string); ok {
			analyzeInput.Platform = platform
		}
		if hint, ok := v["intent_hint"].(string); ok {
			analyzeInput.IntentHint = hint
		}
		// Context is optional - if provided, try to extract
		if ctxMap, ok := v["context"].(map[string]interface{}); ok {
			analyzeInput.Context = buildContextFromMap(ctxMap)
		}
	default:
		if a.logger != nil {
			a.logger.Warn("unknown_input_type_for_analyze", zap.String("type", fmt.Sprintf("%T", input)))
		}
		return nil, fmt.Errorf("unknown input type: %T", input)
	}

	return a.client.Analyze(ctx, analyzeInput)
}

// Generate implements ai_contract.AIClient interface.
// PHASE-8: Passes through to underlying Client's Generate method.
func (a *ContractAdapter) Generate(ctx context.Context, input interface{}) (string, error) {
	// The input is already a map[string]interface{} from pipeline.go
	// Just pass it through to the underlying client
	return a.client.Generate(ctx, input)
}

// buildContextFromMap builds ConversationContext from a map.
func buildContextFromMap(m map[string]interface{}) ConversationContext {
	ctx := ConversationContext{}
	// Products
	if prods, ok := m["available_products"]; ok {
		ctx.AvailableProducts = convertToProductSummaries(prods)
	}
	// Services
	if svcs, ok := m["available_services"]; ok {
		ctx.AvailableServices = convertToServiceSummaries(svcs)
	}
	return ctx
}

// convertToProductSummaries converts interface to []ProductSummary.
func convertToProductSummaries(v interface{}) []ProductSummary {
	// Handle various input types
	switch products := v.(type) {
	case []ProductSummary:
		return products
	case []interface{}:
		result := make([]ProductSummary, 0, len(products))
		for _, p := range products {
			if pm, ok := p.(map[string]interface{}); ok {
				ps := ProductSummary{}
				if name, ok := pm["name"].(string); ok {
					ps.Name = name
				}
				if price, ok := pm["price"].(float64); ok {
					ps.Price = price
				}
				if currency, ok := pm["currency"].(string); ok {
					ps.Currency = currency
				}
				result = append(result, ps)
			}
		}
		return result
	default:
		return nil
	}
}

// convertToServiceSummaries converts interface to []ServiceSummary.
func convertToServiceSummaries(v interface{}) []ServiceSummary {
	switch services := v.(type) {
	case []ServiceSummary:
		return services
	case []interface{}:
		result := make([]ServiceSummary, 0, len(services))
		for _, s := range services {
			if sm, ok := s.(map[string]interface{}); ok {
				ss := ServiceSummary{}
				if name, ok := sm["name"].(string); ok {
					ss.Name = name
				}
				result = append(result, ss)
			}
		}
		return result
	default:
		return nil
	}
}
