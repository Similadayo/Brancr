package python

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// =============================================================================
// CIRCUIT BREAKER STATE
// =============================================================================

type circuitState int

const (
	circuitClosed   circuitState = iota // Normal operation
	circuitOpen                         // Service unavailable, return fallback
	circuitHalfOpen                     // Testing if service recovered
)

const (
	maxFailures     = 3                // Open circuit after 3 consecutive failures
	circuitOpenTime = 30 * time.Second // Stay open for 30 seconds before retry
	healthCheckPath = "/health"
)

// Client is an HTTP client for the brancr-ai Python service with circuit breaker
type Client struct {
	baseURL    string
	httpClient *http.Client

	// Circuit breaker state
	mu              sync.RWMutex
	state           circuitState
	failures        int
	lastFailure     time.Time
	lastHealthCheck time.Time
}

// NewClient creates a new Python client with circuit breaker
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:8001" // Default Python AI service URL
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Reduced from 90s for faster failure detection
		},
		state:    circuitClosed,
		failures: 0,
	}
}

// HealthCheck pings the Python service to verify availability
func (c *Client) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+healthCheckPath, nil)
	if err != nil {
		return fmt.Errorf("health check request error: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordFailure()
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return fmt.Errorf("health check returned status: %d", resp.StatusCode)
	}

	c.recordSuccess()
	return nil
}

// IsAvailable checks if the circuit is closed (service available)
func (c *Client) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch c.state {
	case circuitClosed:
		return true
	case circuitOpen:
		// Check if enough time has passed to try again
		if time.Since(c.lastFailure) > circuitOpenTime {
			return true // Allow a probe request
		}
		return false
	case circuitHalfOpen:
		return true // Allow probe request
	}
	return true
}

// recordFailure increments failure count and possibly opens circuit
func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failures++
	c.lastFailure = time.Now()

	if c.failures >= maxFailures {
		c.state = circuitOpen
	}
}

// recordSuccess resets failure count and closes circuit
func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failures = 0
	c.state = circuitClosed
}

// GetCircuitState returns current circuit state for monitoring
func (c *Client) GetCircuitState() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch c.state {
	case circuitClosed:
		return "closed"
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	}
	return "unknown"
}

func (c *Client) buildFallbackResponse(text, mode, intent string, confidence float64) *AnalyzeTurnResponse {
	if strings.TrimSpace(intent) == "" {
		intent = "fallback"
	}
	if strings.TrimSpace(mode) == "" {
		mode = "fallback"
	}
	return &AnalyzeTurnResponse{
		Decision: struct {
			Domain     string  `json:"domain"`
			Stage      string  `json:"stage"`
			Confidence float64 `json:"confidence"`
		}{
			Domain:     "general",
			Stage:      "fallback",
			Confidence: confidence,
		},
		Reply: struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
		}{
			Text: text,
			Mode: mode,
		},
		Actions: []types.ProposedAction{},
		ConversationUpdate: ConversationUpdate{
			Stage:              "fallback",
			LastDomain:         "general",
			TurnCountIncrement: 1,
		},
		Semantic: SemanticResult{
			Items:            []SemanticItem{},
			DeliveryIntent:   "",
			Address:          "",
			Ambiguities:      []string{},
			DetectedIntent:   intent,
			CustomerOffer:    0,
			TargetItem:       "",
			MessageType:      "general",
			IntentConfidence: confidence,
			IsQuestion:       false,
		},
		LLMMeta: &LLMMeta{
			Model: mode,
			TokenUsage: LLMTokenUsage{
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			},
		},
	}
}

func templateFallbackText(message, businessName string) (string, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return "", "", false
	}
	prefix := "We are experiencing a brief delay right now."
	if strings.TrimSpace(businessName) != "" {
		prefix = fmt.Sprintf("%s is experiencing a brief delay right now.", businessName)
	}
	if strings.HasPrefix(lower, "hi") || strings.HasPrefix(lower, "hello") || strings.HasPrefix(lower, "hey") {
		return prefix + " Hi, how can we help you today?", "greeting", true
	}
	if strings.Contains(lower, "price") || strings.Contains(lower, "cost") || strings.Contains(lower, "how much") {
		return prefix + " Please share the exact model and variant so we can confirm current pricing.", "price_request", true
	}
	if strings.Contains(lower, "buy") || strings.Contains(lower, "order") || strings.Contains(lower, "i want") {
		return prefix + " Please share the exact item and quantity, and we will continue immediately.", "menu_request", true
	}
	if strings.Contains(lower, "manager") || strings.Contains(lower, "human") || strings.Contains(lower, "agent") {
		return prefix + " We will connect you with our team shortly.", "human_request", true
	}
	return "", "", false
}

func humanHandoffFallbackText(businessName string) string {
	if strings.TrimSpace(businessName) == "" {
		return "Let me connect you with our team so we can help quickly."
	}
	return fmt.Sprintf("Let me connect you with %s's team so we can help quickly.", businessName)
}

func (c *Client) analyzeTurnSimplified(ctx context.Context, req *AnalyzeTurnRequest) (*AnalyzeTurnResponse, error) {
	res, err := c.postJSON(ctx, "/api/v4/analyze-turn-simplified", req)
	if err != nil {
		return nil, err
	}
	if res != nil {
		if strings.TrimSpace(res.Reply.Mode) == "" {
			res.Reply.Mode = "fallback_simplified"
		}
		if strings.TrimSpace(res.Decision.Stage) == "" {
			res.Decision.Stage = "fallback"
		}
	}
	return res, nil
}

// GenerateFallbackResponse handles lower fallback layers:
// 1) Go template fallback
// 2) Human handoff fallback
func (c *Client) GenerateFallbackResponse(message string, businessName string) *AnalyzeTurnResponse {
	if text, intent, ok := templateFallbackText(message, businessName); ok {
		return c.buildFallbackResponse(text, "fallback_template", intent, 0.45)
	}
	return c.buildFallbackResponse(humanHandoffFallbackText(businessName), "fallback_handoff", "human_request", 0.4)
}

// AnalyzeTurnRequest payload
type AnalyzeTurnRequest struct {
	TenantID           uuid.UUID                      `json:"tenant_id"`
	Message            string                         `json:"message"`
	TenantKnowledge    *v4ctx.TenantKnowledgeSnapshot `json:"tenant_knowledge"`
	Conversation       *v4ctx.ConversationSnapshot    `json:"conversation"`
	Order              *v4ctx.OrderSnapshot           `json:"order"`
	FulfillmentTask    *FulfillmentTask               `json:"fulfillment_task"`
	CatalogKind        string                         `json:"catalog_kind"`
	CatalogMode        string                         `json:"catalog_mode"`
	FocusItem          string                         `json:"focus_item"`
	NegotiationContext *NegotiationContext            `json:"negotiation_context,omitempty"`
}

type NegotiationContext struct {
	IsActive          bool    `json:"is_active"`
	NegotiationCount  int     `json:"negotiation_count"`
	LastAIOffer       float64 `json:"last_ai_offer"`
	LastCustomerOffer float64 `json:"last_customer_offer"`
	EscalationPending bool    `json:"escalation_pending"`
}

type FulfillmentTask struct {
	TargetItem       string `json:"target_item"`
	RequirementLabel string `json:"requirement_label"`
	FieldType        string `json:"field_type"`
	Instruction      string `json:"instruction"`
}

// ConversationUpdate represents state changes dictated by Python
type ConversationUpdate struct {
	Stage                  string `json:"stage"`
	LastDomain             string `json:"last_domain"`
	TurnCountIncrement     int    `json:"turn_count_increment"`
	NegotiationRoundUpdate *int   `json:"negotiation_round_update"` // NEW
}

// SemanticItem represents a single extracted item from customer message
type SemanticItem struct {
	Raw      string `json:"raw"`      // Raw text as customer said it
	Quantity int    `json:"quantity"` // Quantity (default 1)
}

// SemanticResult represents pure semantic extraction from Python
type SemanticResult struct {
	Items            []SemanticItem `json:"items"`
	DeliveryIntent   string         `json:"delivery_intent"` // "delivery", "pickup", or ""
	Address          string         `json:"address"`         // Extracted address if present
	Ambiguities      []string       `json:"ambiguities"`
	CancelIntent     bool           `json:"cancel_intent"`     // True if customer wants to cancel
	DetectedIntent   string         `json:"detected_intent"`   // e.g. menu_request, price_request (Step 1)
	CustomerOffer    float64        `json:"customer_offer"`    // NEW: Extracted offer amount
	TargetItem       string         `json:"target_item"`       // NEW: Contextual target item
	BuyingConfidence float64        `json:"buying_confidence"` // LLM-scored buying readiness (0.0-1.0)

	// Phase 2: Python is source of truth for these
	MessageType      string  `json:"message_type"`      // "question" | "offer" | "correction" | "general" | ...
	IntentConfidence float64 `json:"intent_confidence"` // 0.0-1.0
	IsQuestion       bool    `json:"is_question"`       // Explicit question flag
}

type LLMTokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type LLMMeta struct {
	Model      string        `json:"model"`
	TokenUsage LLMTokenUsage `json:"token_usage"`
}

type StructuredReply struct {
	Preface     string   `json:"preface"`
	FollowUp    string   `json:"follow_up"`
	Intent      string   `json:"intent"`
	TargetItems []string `json:"target_items"`
	Confidence  string   `json:"confidence"`
}

// AnalyzeTurnResponse payload
type AnalyzeTurnResponse struct {
	Decision struct {
		Domain     string  `json:"domain"`
		Stage      string  `json:"stage"`
		Confidence float64 `json:"confidence"`
	} `json:"decision"`

	Reply struct {
		Text string `json:"text"`
		Mode string `json:"mode"`
	} `json:"reply"`

	StructuredReply *StructuredReply `json:"structured_reply"`

	Actions []types.ProposedAction `json:"actions"`

	ConversationUpdate ConversationUpdate   `json:"conversation_update"`
	OrderUpdate        *v4ctx.OrderSnapshot `json:"order_update"`
	Semantic           SemanticResult       `json:"semantic"` // NEW: Pure semantic extraction
	LLMMeta            *LLMMeta             `json:"llm_meta"`
	StateUpdates       map[string]any       `json:"state_updates,omitempty"`
	Debug              map[string]any       `json:"debug,omitempty"`
}

// AnalyzeTurn calls the python endpoint with circuit breaker protection
func (c *Client) AnalyzeTurn(
	ctx context.Context,
	req *AnalyzeTurnRequest,
) (*AnalyzeTurnResponse, error) {
	// Check circuit breaker state
	if !c.IsAvailable() {
		// Circuit is open; skip remote calls and use Go fallback layers.
		businessName := ""
		if req.TenantKnowledge != nil {
			businessName = req.TenantKnowledge.BusinessName
		}
		return c.GenerateFallbackResponse(req.Message, businessName), nil
	}

	result, err := c.postJSON(ctx, "/api/v4/analyze-turn", req)
	if err != nil {
		// Layer 2: simplified Python fallback (deterministic, no heavy LLM dependency).
		if simplified, sErr := c.analyzeTurnSimplified(ctx, req); sErr == nil && simplified != nil {
			return simplified, nil
		}

		// Layer 3/4: Go template fallback -> human handoff fallback.
		businessName := ""
		if req.TenantKnowledge != nil {
			businessName = req.TenantKnowledge.BusinessName
		}
		return c.GenerateFallbackResponse(req.Message, businessName), nil
	}

	return result, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload interface{}) (*AnalyzeTurnResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Record failure for circuit breaker
		c.recordFailure()
		return nil, fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Record failure for non-200 responses
		c.recordFailure()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result AnalyzeTurnResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("decode error: %w", err)
	}

	// Record success - circuit breaker happy path
	c.recordSuccess()
	return &result, nil
}

// ========================================
// LLM-Generated Reply Types & Methods
// ========================================

// OrderItemSummary represents an item for order confirmation
type OrderItemSummary struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Price    int    `json:"price"`
}

// IndustryContext provides industry-specific context for LLM
type IndustryContext struct {
	Name           string   `json:"name"`
	HasDelivery    bool     `json:"has_delivery"`
	HasPickup      bool     `json:"has_pickup"`
	PaymentMethods []string `json:"payment_methods"`
}

// OrderConfirmationData is the payload for generating order confirmations
type OrderConfirmationData struct {
	Items    []OrderItemSummary `json:"items"`
	Total    int64              `json:"total"`
	Currency string             `json:"currency"`
	Stage    string             `json:"stage"`
	Industry IndustryContext    `json:"industry"`
}

// PaymentInstructionData is the payload for generating payment instructions
type PaymentInstructionData struct {
	OrderNumber      string   `json:"order_number"`
	Total            float64  `json:"total"`
	Currency         string   `json:"currency"`
	BankName         string   `json:"bank_name"`
	AccountNumber    string   `json:"account_number"`
	AccountName      string   `json:"account_name"`
	PaymentReference string   `json:"payment_reference"`
	PaymentMethods   []string `json:"payment_methods"`
	TermLabel        string   `json:"term_label"` // e.g., "Order", "Service"
}

// GenerateOrderReply calls Python LLM to generate natural order confirmation
func (c *Client) GenerateOrderReply(ctx context.Context, data OrderConfirmationData) (string, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/generate-order-reply", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}

// GeneratePaymentInstructions calls Python LLM to generate natural payment message
func (c *Client) GeneratePaymentInstructions(ctx context.Context, data PaymentInstructionData) (string, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/generate-payment-instructions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}

// KnowledgeRequest is the payload for generating knowledge-based replies
type KnowledgeRequest struct {
	BusinessName        string                   `json:"business_name"`
	KnowledgeBase       string                   `json:"knowledge_base"`
	FAQs                []map[string]interface{} `json:"faqs"`
	ConversationContext []map[string]string      `json:"conversation_context"`
	Message             string                   `json:"message"`
	MessageIntent       string                   `json:"message_intent"`
}

type AvailabilityDescriptionRequest struct {
	BusinessName    string `json:"business_name"`
	ItemName        string `json:"item_name"`
	ItemDescription string `json:"item_description"`
}

type GuardrailReplyRequest struct {
	Reason        string `json:"reason"`
	BusinessName  string `json:"business_name"`
	CurrentItem   string `json:"current_item"`
	RequestedItem string `json:"requested_item"`
}

// GenerateKnowledgeReply calls Python endpoint for conversational fallback
func (c *Client) GenerateKnowledgeReply(ctx context.Context, req KnowledgeRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	// Note: Endpoint matching the one registered in app.py (/api/knowledge-reply)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/knowledge-reply", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}

// GenerateAvailabilityDescription calls Python endpoint for item descriptions
func (c *Client) GenerateAvailabilityDescription(ctx context.Context, req AvailabilityDescriptionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/generate-availability-description", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}

// GenerateGuardrailReply calls Python endpoint to render guardrail text while Go keeps authority on policy decisions.
func (c *Client) GenerateGuardrailReply(ctx context.Context, req GuardrailReplyRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/render-guardrail-reply", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}

// ========================================
// Negotiation Rendering
// ========================================

// NegotiationRenderContext represents context for deterministic negotiation rendering
type NegotiationRenderContext struct {
	Phase         string  `json:"phase"`          // accept | concede | hold | escalate
	CustomerOffer float64 `json:"customer_offer"` // Extracted number
	AIOffer       float64 `json:"ai_offer"`       // Calculated counter-offer
	Currency      string  `json:"currency"`       // e.g. NGN
	TargetItem    string  `json:"target_item"`    // Item name
	TermLabel     string  `json:"term_label"`     // e.g., "Order", "Service"
	BusinessName  string  `json:"business_name"`  // ???? FIX: Pass business name for human reply
	Reason        string  `json:"reason"`         // Decision reason for rendering guidance
	AskedForBest  bool    `json:"asked_for_best"` // Customer asked for best price without a number
}

// GenerateNegotiationReply calls Python to render negotiation text
func (c *Client) GenerateNegotiationReply(ctx context.Context, data NegotiationRenderContext) (string, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/generate-negotiation-reply", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Reply, nil
}
