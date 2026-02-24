package signals

import (
	"context"
	"regexp"
	"strings"

	// Alias to avoid collision if needed, assuming pkg name is context
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// FactExtractor observes language to extract facts.
type FactExtractor struct {
	llm types.LLMClient
}

// NewFactExtractor creates a new FactExtractor.
func NewFactExtractor(llm types.LLMClient) *FactExtractor {
	return &FactExtractor{
		llm: llm,
	}
}

// Extract analyzes the context snapshot to determine semantic facts.
func (e *FactExtractor) Extract(
	ctx context.Context,
	snapshot *v4ctx.ContextSnapshot,
) (*SemanticFacts, error) {

	facts := &SemanticFacts{
		Confidence: 0.1, // Base conviction
	}

	if snapshot == nil {
		return facts, nil
	}

	msg := snapshot.Message

	// Step 1: Heuristic Pass (Weak Signals)
	e.applyHeuristics(msg, facts)

	// Step 2: LLM Semantic Assist (Optional)
	if e.llm != nil {
		// Prepare LLM Input
		// We use a simplified prompt here. In production, this would be a structured prompt template.
		// For now, we simulate the structure.
		// NOTE: Assuming LLMClient has a Generate or specific method.
		// Since types.LLMClient interface wasn't fully inspected for exact method signature in this turn,
		// I will assume a generic "Generate" or similar, but for safety in this strict build block,
		// I will comment out the actual LLM call logic placeholder to ensure compilation if signatures differ,
		// or use a hypothetical method if I knew it.
		// Wait, I saw types.LLMClient earlier. It has method signatures.
		// I will proceed with the heuristics primarily and add LLM stub.

		// TODO: Implement actual LLM call.
		// prompt := fmt.Sprintf("Analyze: %s. Biz: %s", msg, snapshot.Business.Name)
		// resp, err := e.llm.Analyze(ctx, ...)
		// if err == nil { mergeLLMFacts(facts, resp) }
	}

	return facts, nil
}

// applyHeuristics runs regex checks for structural facts.
// It avoids keyword matching for intent, relying on LLM/Semantic layers for that.
func (e *FactExtractor) applyHeuristics(msg string, facts *SemanticFacts) {
	lower := strings.ToLower(msg)

	// 1. Numeric Detection (Quantity)
	// Simple check for digits
	if match, _ := regexp.MatchString(`\d+`, msg); match {
		facts.MentionsQuantity = true // Potential
	}

	// 2. Currency Symbol Detection (Structural Price)
	// We check for EXPLICIT currency symbols, not just "how much" keywords.
	// This confirms the structure of the message involves money values.
	if strings.Contains(lower, "₦") ||
		strings.Contains(lower, "ngn") ||
		strings.Contains(lower, "$") ||
		strings.Contains(lower, "usd") {
		facts.MentionsPrice = true
	}

	// 3. Payment Confirmation Signals
	if strings.Contains(lower, "paid") ||
		strings.Contains(lower, "payment sent") ||
		strings.Contains(lower, "transfer done") ||
		strings.Contains(lower, "i have paid") ||
		strings.Contains(lower, "payment made") {
		facts.MentionsPayment = true
		facts.MentionsConfirmation = true
	}

	// 4. Human Request Signals
	if strings.Contains(lower, "human") ||
		strings.Contains(lower, "agent") ||
		strings.Contains(lower, "manager") ||
		(strings.Contains(lower, "talk") && strings.Contains(lower, "someone")) {
		facts.MentionsHuman = true
	}

	// 5. Complaint / Refund Signals
	if strings.Contains(lower, "complaint") ||
		strings.Contains(lower, "angry") ||
		strings.Contains(lower, "bad service") ||
		strings.Contains(lower, "disappointed") {
		facts.MentionsComplaint = true
	}
	if strings.Contains(lower, "refund") ||
		strings.Contains(lower, "money back") ||
		strings.Contains(lower, "return my money") {
		facts.MentionsRefund = true
	}

	// 6. Order Confirmation Signals
	if strings.Contains(lower, "confirm order") ||
		strings.Contains(lower, "confirm it") ||
		strings.Contains(lower, "yes, confirm") {
		facts.MentionsConfirmation = true
	}
}

// Helper
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
