package v3

import (
	"strings"

	"github.com/google/uuid"
)

// =============================================================================
// CONVERSATION GROUNDER V2 - Full Tenant Reality
// =============================================================================

// ConversationGrounderV2 returns GroundedContext instead of GroundingResult
type ConversationGrounderV2 interface {
	GroundFull(ctx *ConversationContext, message string) *GroundedContext
}

// FullConversationGrounder implements V2 grounding with full context
type FullConversationGrounder struct{}

// NewFullConversationGrounder creates a new V2 grounder
func NewFullConversationGrounder() *FullConversationGrounder {
	return &FullConversationGrounder{}
}

// GroundFull returns a complete GroundedContext for response generation
func (g *FullConversationGrounder) GroundFull(ctx *ConversationContext, message string) *GroundedContext {
	// Safety: tenant must exist
	if ctx == nil || ctx.TenantID == uuid.Nil {
		return &GroundedContext{
			ConversationType: ConversationGreeting,
			Answerable:       false,
			ResponseMode:     GroundingEscalate,
		}
	}

	lower := strings.ToLower(strings.TrimSpace(message))

	// Build grounded context from ConversationContext
	grounded := &GroundedContext{
		TenantID:     ctx.TenantID,
		BusinessName: "", // Will be populated from tenant context
		Industry:     ctx.RoleProfile.Industry,
		Products:     ctx.Products,
		Services:     ctx.Services,
		FAQs:         ctx.FAQs,
		Policies:     ctx.Policies,
		PaymentInfo:  ctx.PaymentInfo,
		Voice:        buildVoice(ctx),
	}

	// Infer conversation type
	grounded.ConversationType = inferConversationType(lower, grounded)

	// Determine answerability
	answerable, missing := canAnswerFromDB(lower, grounded)
	grounded.Answerable = answerable
	grounded.MissingData = missing

	// Set response mode
	grounded.ResponseMode = GroundingDirect
	if !answerable && len(missing) > 0 {
		grounded.ResponseMode = GroundingClarify
	}
	if !answerable && len(missing) == 0 && len(grounded.Products) == 0 && len(grounded.Services) == 0 {
		grounded.ResponseMode = GroundingEscalate
	}

	return grounded
}

// =============================================================================
// INFERENCE FUNCTIONS
// =============================================================================

// inferConversationType determines what kind of conversation this is
func inferConversationType(lower string, g *GroundedContext) ConversationType {
	// Support signals override everything
	if isSupportLanguage(lower) {
		return ConversationSupport
	}

	// Greeting patterns
	if isGreetingLanguage(lower) {
		return ConversationGreeting
	}

	// Advisory patterns (recommendations, comparisons)
	if isAdvisoryLanguage(lower) {
		return ConversationAdvisory
	}

	// Default: if products exist, it's Sales
	if len(g.Products) > 0 || len(g.Services) > 0 {
		return ConversationSales
	}

	// No data, still need to respond
	return ConversationGreeting
}

// canAnswerFromDB checks if we can answer from available data
func canAnswerFromDB(lower string, g *GroundedContext) (bool, []MissingDatum) {
	// If we have products/services, we can always answer
	if len(g.Products) > 0 || len(g.Services) > 0 {
		return true, nil
	}

	// If asking about payment and we have payment info
	if isPaymentLanguage(lower) && g.PaymentInfo != nil && g.PaymentInfo.BankName != "" {
		return true, nil
	}

	// If support question and we have FAQs/policies
	if isSupportLanguage(lower) && (len(g.FAQs) > 0 || len(g.Policies) > 0) {
		return true, nil
	}

	// Cannot answer - determine what's missing
	missing := []MissingDatum{}
	if len(g.Products) == 0 && len(g.Services) == 0 {
		// No products/services configured
		// Don't ask user for this - it's a tenant setup issue
	}

	return false, missing
}

// buildVoice compiles persona into actionable voice fingerprint
func buildVoice(ctx *ConversationContext) TenantVoiceFingerprint {
	voice := TenantVoiceFingerprint{
		Tone:     "friendly",
		Language: "en",
	}

	if ctx.RoleProfile.Tone != "" {
		voice.Tone = ctx.RoleProfile.Tone
	}
	if ctx.RoleProfile.Tone != "" {
		voice.Tone = ctx.RoleProfile.Tone
	}

	// Infer emoji bias from industry
	switch ctx.RoleProfile.Industry {
	case "food", "retail", "fashion":
		voice.EmojiBias = EmojiLight
	case "legal", "finance", "healthcare":
		voice.EmojiBias = EmojiNone
	default:
		voice.EmojiBias = EmojiLight
	}

	return voice
}

// =============================================================================
// LANGUAGE DETECTION HELPERS
// =============================================================================

func isGreetingLanguage(lower string) bool {
	greetings := []string{
		"hi", "hello", "hey", "good morning", "good evening", "good afternoon",
		"yo", "sup", "how far", "how body", "e kaaro", "sannu", "kedu",
	}
	for _, g := range greetings {
		if lower == g || strings.HasPrefix(lower, g+" ") || strings.HasPrefix(lower, g+",") || strings.HasPrefix(lower, g+"!") {
			return true
		}
	}
	return len(lower) < 10 && (lower == "hi" || lower == "hey" || lower == "hello")
}

func isSupportLanguage(lower string) bool {
	return strings.Contains(lower, "problem") ||
		strings.Contains(lower, "issue") ||
		strings.Contains(lower, "not working") ||
		strings.Contains(lower, "trouble") ||
		strings.Contains(lower, "complain") ||
		(strings.Contains(lower, "help") && !strings.Contains(lower, "choose"))
}

func isAdvisoryLanguage(lower string) bool {
	return strings.Contains(lower, "recommend") ||
		strings.Contains(lower, "suggest") ||
		strings.Contains(lower, "which should") ||
		strings.Contains(lower, "which one") ||
		strings.Contains(lower, "best") ||
		strings.Contains(lower, "favorite") ||
		strings.Contains(lower, "confused") ||
		strings.Contains(lower, "difference") ||
		strings.Contains(lower, "better")
}

func isPaymentLanguage(lower string) bool {
	return strings.Contains(lower, "pay") ||
		strings.Contains(lower, "payment") ||
		strings.Contains(lower, "bank") ||
		strings.Contains(lower, "account") ||
		strings.Contains(lower, "transfer")
}

// =============================================================================
// LEGACY INTERFACE (Keep for backwards compatibility)
// =============================================================================

// ConversationGrounder determines why the customer is speaking right now
type ConversationGrounder interface {
	Ground(ctx *ConversationContext, message string) GroundingResult
}

// DefaultConversationGrounder implements standard grounding logic
type DefaultConversationGrounder struct {
	v2 *FullConversationGrounder
}

// NewConversationGrounder creates a new grounder
func NewConversationGrounder() ConversationGrounder {
	return &DefaultConversationGrounder{
		v2: NewFullConversationGrounder(),
	}
}

// Ground determines the conversational context (legacy wrapper)
func (g *DefaultConversationGrounder) Ground(ctx *ConversationContext, message string) GroundingResult {
	grounded := g.v2.GroundFull(ctx, message)

	// Convert GroundedContext to GroundingResult for compatibility
	gType := GroundingBrowsing
	switch grounded.ConversationType {
	case ConversationGreeting:
		gType = GroundingGreeting
	case ConversationSupport:
		gType = GroundingSupport
	case ConversationAdvisory:
		gType = GroundingDecisionHelp
	case ConversationSales:
		gType = GroundingBrowsing
	}

	return GroundingResult{
		Type:        gType,
		Confidence:  0.9,
		Reason:      string(grounded.ConversationType),
		MustRespond: true,
	}
}
