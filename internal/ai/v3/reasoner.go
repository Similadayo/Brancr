package v3

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/similadayo/brancr/internal/ai/authority"
	"github.com/similadayo/brancr/internal/ai/guard"
	"github.com/similadayo/brancr/internal/ai/prompt"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/rules"
	"github.com/similadayo/brancr/internal/types"
	"go.uber.org/zap"
)

// Reasoner is the intelligence layer that wraps the low-level transport.
// It enforces all architectural guarantees before and after the LLM call.
type Reasoner struct {
	Client LLMClient
	Logger *zap.Logger // PHASE-0: Added for observability
}

func NewReasoner(client LLMClient) *Reasoner {
	// PHASE-0: Logger will be set via SetLogger if available
	return &Reasoner{Client: client, Logger: zap.NewNop()}
}

// SetLogger sets the logger for observability (PHASE-0)
func (r *Reasoner) SetLogger(logger *zap.Logger) {
	if logger != nil {
		r.Logger = logger
	}
}

// Generate executes the reasoning process:
// 1. Build Strict Input
// 2. Call LLM (Transport)
// 3. Validate Output (JSON Schema + Allowed Actions)
// 4. Enforce Confidence
func (r *Reasoner) Generate(
	ctx context.Context,
	resolved types.ResolvedContext,
	intentResult intent.IntentResult,
	allowed []rules.ActionType,
	goal conversation.Goal,
	platform string,
) (GPTOutput, error) {

	// STEP 1: AUTHORITY BUILD (Fix 7.4)
	auth := authority.BuildAuthority(&resolved, string(intentResult.Intent))

	// STEP 6: PHRASING PROFILE (Fix 12.5)
	// Calculate profile based on Confidence & Emotion (already updated in ResolvedContext by Pipeline)
	phraseProfile := conversation.PhraseStyle(resolved.Conversation.ConfidenceLevel, domain.Emotion(resolved.Conversation.Emotion))

	// Build Dynamic System Prompt
	sysPrompt := prompt.AuthoritySystemPrompt(auth) + "\n\n" +
		"### IDENTITY CONTEXT ###\n" +
		prompt.IdentityBlock(resolved.Merchant) + "\n\n" +
		"### GUARDRAILS ###\n" +
		prompt.GuardrailBlock(resolved.Merchant) + "\n\n" +
		"### STYLE & MANNERISMS ###\n" +
		prompt.StyleMirroringBlock(resolved.Conversation.DetectedStyle) + "\n\n" +
		"### HISTORY & MEMORY ###\n" +
		prompt.ContextualRecallBlock(&resolved) + "\n\n" +
		"### PRODUCT CATALOG (STRICT RECALL) ###\n" +
		prompt.CatalogBlock(resolved.Catalog) + "\n\n" +
		"### BUSINESS FACTS ###\n" +
		prompt.FactsBlock(&resolved, auth.AllowedFacts) + "\n\n" +
		"### PHRASING PROFILE ###\n" +
		prompt.PhrasingBlock(phraseProfile)

	// 1. Build Strict Input (Context Summary)
	// In a real impl, this would serialize the ResolvdContext safely (no raw ID leakage).
	snap := BuildContextSnapshot(resolved)
	mode := ResolveResponseMode(goal)

	input := GPTInput{
		ContextSummary: fmt.Sprintf("Recent context:\n- Last topic: %s\n- Goal: %s\n- Response tone: %s",
			snap.LastTopic,
			goal,
			mode,
		),
		Intent:         string(intentResult.Intent),
		AllowedActions: allowed,
		Platform:       platform,
		Language:       "en",      // Default for now
		SystemPrompt:   sysPrompt, // Pass dynamic prompt
	}

	// 2. Select Model Tier
	// Note: Model customization currently disabled during refactor to interface.
	// Future: Pass model hint in context or extend interface.

	// 3. Build Messages
	messages := buildMessages(input)

	// 4. Call LLM (Transport)
	// Map []Message to []LLMMessage
	llmMessages := make([]LLMMessage, len(messages))
	for i, m := range messages {
		llmMessages[i] = LLMMessage{Role: m.Role, Content: m.Content}
	}

	rawOutput, err := r.Client.Generate(ctx, llmMessages)
	if err != nil {
		return GPTOutput{}, err
	}

	// 5. Unmarshal & Validate JSON
	var out GPTOutput
	if err := json.Unmarshal([]byte(rawOutput), &out); err != nil {
		return GPTOutput{}, errors.New("invalid GPT JSON output")
	}

	// STEP 6.5: OUTPUT POLICING (Fix 7.3)
	if !guard.ValidateOutput(out.MessageToUser) {
		// Panic as requested for dev visibility
		panic(fmt.Sprintf("AUTHORITY VIOLATION: LLM OVERSTEPPED. Output: %s", out.MessageToUser))
	}

	// 6. Validate Allowed Actions
	if !isAllowed(rules.ActionType(out.SuggestedAction), allowed) {
		return GPTOutput{}, fmt.Errorf("GPT suggested forbidden action: %s", out.SuggestedAction)
	}

	// 3. Validation Chain
	// A. Static Output Guard (Forbidden Phrases)
	if !guard.ValidateOutput(out.MessageToUser) {
		return GPTOutput{}, errors.New("output content violation")
	}

	// B. Phrasing Guard (Style Profile)
	if !guard.ValidatePhrasing(out.MessageToUser, phraseProfile) {
		return GPTOutput{}, errors.New("phrasing violation detected (too assertive for current confidence)")
	}

	// 7. Enforce Confidence
	if out.Confidence < 0.6 {
		return GPTOutput{
			MessageToUser:   "Can you clarify that a bit for me?",
			SuggestedAction: string(rules.ActionClarifyIntent),
			Confidence:      1.0, // System confidence in the clarification request is high
		}, nil
	}

	// PHASE-0 LOGGING: Reasoner Output Boundary
	// Log AI-produced output before it's passed to executor
	// PHASE-0 NOTE: AI output directly influences execution here.
	// This path will be refactored in later phases.
	r.Logger.Info("reasoner_output_generated",
		zap.String("message_to_user", out.MessageToUser),
		zap.String("suggested_action", out.SuggestedAction),
		zap.Float64("confidence", out.Confidence),
		zap.String("reasoning", out.Reasoning),
		zap.Int("num_commands", len(out.Commands)),
		zap.String("originating_intent", string(intentResult.Intent)),
	)

	return out, nil
}

func buildMessages(input GPTInput) []Message {
	// Serialize inputs for the prompt
	allowedJSON, _ := json.Marshal(input.AllowedActions)

	userPrompt := fmt.Sprintf(`
CONTEXT: %s
INTENT: %s
ALLOWED ACTIONS: %s
PLATFORM: %s
`, input.ContextSummary, input.Intent, string(allowedJSON), input.Platform)

	return []Message{
		{Role: "system", Content: input.SystemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

func isAllowed(suggested rules.ActionType, allowed []rules.ActionType) bool {
	for _, a := range allowed {
		if a == suggested {
			return true
		}
	}
	// Case: clarify_intent is always safe fallback?
	if suggested == rules.ActionClarifyIntent {
		return true
	}
	return false
}

// Helpers to safely extract strings from potentially nil pointers
func safeNegotiationPhase(order *domain.OrderState) string {
	if order == nil || order.Negotiation.Phase == domain.NegotiationNone {
		return "NONE"
	}
	return string(order.Negotiation.Phase)
}

func safePaymentStatus(order *domain.OrderState) string {
	if order == nil {
		return "UNKNOWN"
	}
	return string(order.Payment.Status)
}

func safeOrderTotal(order *domain.OrderState) float64 {
	if order == nil {
		return 0
	}
	return float64(order.TotalPrice.Amount) / 100.0
}

func safeOrderCurrency(order *domain.OrderState) string {
	if order == nil {
		return "NGN"
	}
	return order.TotalPrice.Currency
}

// GPTInput / GPTOutput structs might need to be defined here if not in types.go
// Checking unused types...
// They are likely in reasoner.go original file but unexported or private.
// I will include them here.

type GPTInput struct {
	ContextSummary string             `json:"context_summary"`
	Intent         string             `json:"intent"`
	AllowedActions []rules.ActionType `json:"allowed_actions"`
	Platform       string             `json:"platform"`
	Language       string             `json:"language"`
	SystemPrompt   string             `json:"-"`
}

type GPTOutput struct {
	MessageToUser   string          `json:"message_to_user"`
	SuggestedAction string          `json:"suggested_action"`
	Confidence      float64         `json:"confidence"`
	Reasoning       string          `json:"reasoning"`
	Commands        []HiddenCommand `json:"commands,omitempty"`

	// Phase 4: Commitment Detection (Semantic, LLM-Assisted)
	CommitmentSignal *conversation.CommitmentSignal `json:"commitment_signal,omitempty"`

	// Phase 5: Commitment Reversal & Cool-Off
	CommitmentReversalSignal *conversation.CommitmentReversalSignal `json:"commitment_reversal_signal,omitempty"`

	// Phase 4.75C: Structured Product Referencing (Anti-Hallucination)
	ProductReferences []ProductReference `json:"product_references,omitempty"`
}

// ProductReference represents a structured reference to a catalog product
// PHASE 4.75C: LLM outputs product IDs, Go renders facts
type ProductReference struct {
	ProductID string `json:"product_id"`
}
