package social

import (
	"context"
	"fmt"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/decision"
	"github.com/similadayo/brancr/internal/ai_v4/executor"
	"github.com/similadayo/brancr/internal/ai_v4/safety"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// Renderer generates the customer-facing response.
type Renderer struct {
	llm    types.LLMClient
	logger *zap.Logger
}

// RenderInput contains all data needed for rendering
type RenderInput struct {
	Context   *v4ctx.ContextSnapshot
	Decision  *decision.Decision
	Execution *executor.ExecutionResult
	Intent    *types.IntentResult
	Semantic  *types.SemanticUnderstanding
}

// SocialReply represents the final text and segment content
type SocialReply struct {
	Text     string
	Segments []string
}

// NewRenderer creates a new social reply renderer.
func NewRenderer(
	llm types.LLMClient,
	logger *zap.Logger,
) *Renderer {
	return &Renderer{
		llm:    llm,
		logger: logger,
	}
}

// Render constructs the final response text.
func (r *Renderer) Render(
	ctx context.Context,
	snapshot *v4ctx.ContextSnapshot,
	dec *decision.Decision,
	exec *executor.ExecutionResult,
	intent *types.IntentResult,
	semantic *types.SemanticUnderstanding,
) (*types.AIResponse, error) {

	// 1. ESCALATION HANDLE
	if dec.Escalation != nil {
		return &types.AIResponse{
			ReplyText:     "I'm going to pass this to a human agent who can help you better. Please hold on.",
			ReplySegments: []string{"I'm going to pass this to a human agent who can help you better.", "Please hold on."},
			NewMode:       types.ModeEscalation,
		}, nil
	}

	// PART 3: ENFORCE KNOWLEDGE AT REPLY TIME
	k := snapshot.TenantKnowledge
	if k == nil {
		panic("TenantKnowledgeSnapshot is missing — refusing to generate reply")
	}

	// Prepare Input
	input := RenderInput{
		Context:   snapshot,
		Decision:  dec,
		Execution: exec,
		Intent:    intent,
		Semantic:  semantic,
	}

	var reply SocialReply

	// PRIORITY LOGIC
	// 1. Semantic-first (assistant behavior)
	// Guard: Only if NO execution messages. If we performed an action (Execute), we must report it (Decision/Execution driven).
	hasExec := exec != nil && (len(exec.Messages) > 0 || exec.OrderSnapshot != nil || exec.PaymentDetails != nil)

	if semantic != nil && semantic.Meaning != "" && !hasExec {
		reply = r.RenderFromSemantic(input)
	} else if dec != nil || hasExec {
		// 2. Decision-reason driven (authoritative)
		reply = r.RenderFromDecision(input)

		// If decision renderer yielded nothing, fall through to intent
		if reply.Text == "" {
			reply = r.RenderFromIntent(input)
		}
	} else {
		// 3. Intent fallback (chatbot-level)
		reply = r.RenderFromIntent(input)
	}

	// 3. LLM TONE POLISH
	// SAFETY: Check KillSwitch
	ks := safety.LoadKillSwitch()
	polished := reply.Text
	if !ks.DisableLLM && reply.Text != "" {
		var err error
		polished, err = r.polishWeb(ctx, reply.Text, snapshot)
		if err != nil {
			r.logger.Warn("llm_tone_polish_failed", zap.Error(err))
			polished = reply.Text
		}
	} else {
		if reply.Text != "" {
			r.logger.Info("llm_polish_skipped_killswitch_active")
		}
	}

	// PART 3: HARD-GATE GENERIC REPLIES
	// Check if knowledge exists but we are still sounding generic
	if (len(k.MenuItems) > 0 || len(k.Products) > 0 || len(k.Services) > 0) &&
		strings.Contains(strings.ToLower(polished), "we specialize") {
		panic("Generic business reply attempted despite tenant knowledge")
	}

	// 4. PLATFORM SPLIT
	segments := reply.Segments
	if len(segments) == 0 && polished != "" {
		segments = r.splitMessage(polished, snapshot.Platform)
	}

	// Log Fallback Debugging
	// Check if we hit RenderFromIntent logic by inferring?
	// Ideally we'd know which path we took.
	// For now, logging general state.
	if reply.Text == r.RenderClarification().Text {
		r.logger.Warn("renderer_fallback_used",
			zap.String("intent", string(intent.Primary)),
			zap.Bool("has_semantic", semantic != nil),
			zap.String("decision_reason", dec.Reason),
		)
	}

	return &types.AIResponse{
		ReplyText:     polished,
		ReplySegments: segments,
	}, nil
}

// RenderFromDecision checks execution results and decision reasoning
func (r *Renderer) RenderFromDecision(input RenderInput) SocialReply {
	var parts []string
	exec := input.Execution

	// 1. Check Execution Results (Actions taken)
	if exec != nil {
		// System messages (e.g. "Added to cart")
		for _, msg := range exec.Messages {
			if msg == "payment_proof_received" {
				parts = append(parts, "I've received your payment proof. Checking it now.")
			} else {
				parts = append(parts, msg)
			}
		}

		// Order Snapshot
		if exec.OrderSnapshot != nil {
			// Describe order state briefly
			if exec.OrderSnapshot.Status == "confirmed" {
				parts = append(parts, fmt.Sprintf("Order #%s is confirmed.", exec.OrderSnapshot.OrderID.String()[:8]))
			} else {
				parts = append(parts, fmt.Sprintf("Your cart has %d items.", len(exec.OrderSnapshot.Items)))
			}
		}

		// Payment Details
		if exec.PaymentDetails != nil {
			parts = append(parts, "Here are the payment details:")
			if bank, ok := exec.PaymentDetails["bank_name"]; ok {
				parts = append(parts, fmt.Sprintf("Bank: %v", bank))
			}
			if acc, ok := exec.PaymentDetails["account_number"]; ok {
				parts = append(parts, fmt.Sprintf("Account: %v", acc))
			}
		}
	}

	// If we have content from execution, return it
	if len(parts) > 0 {
		text := strings.Join(parts, "\n")
		return SocialReply{Text: text, Segments: parts}
	}

	return SocialReply{}
}

// RenderFromIntent handles callbacks and fallbacks
func (r *Renderer) RenderFromIntent(input RenderInput) SocialReply {
	// STEP 3: Fallback Safeguard
	if input.Semantic != nil {
		// NEVER clarify if we have semantic meaning
		return r.SafeAcknowledge(input.Context)
	}

	// 1. Inquiry Strategy (DB-BACKED)
	if input.Intent != nil && input.Intent.Primary == types.IntentInquiry {
		text := r.BuildKnowledgeReply(input.Context.TenantKnowledge)
		return SocialReply{Text: text, Segments: []string{text}}
	}

	// 2. Greeting
	if input.Intent != nil && input.Intent.Primary == types.IntentGreeting {
		text := r.BuildGreetingReply(input.Context.TenantKnowledge)
		return SocialReply{Text: text, Segments: []string{text}}
	}

	// 3. Known Intents (No Clarification)
	if input.Intent != nil && input.Intent.Primary != types.IntentUnknown {
		text := "I understand. How would you like to proceed?"
		return SocialReply{Text: text, Segments: []string{text}}
	}

	// 4. Unknown Intent -> Business Overview (Part 5: Deterministic)
	text := r.DeterministicBusinessReply(input.Context.TenantKnowledge, "")
	return SocialReply{Text: text, Segments: []string{text}}
}

func (r *Renderer) SafeAcknowledge(ctx *v4ctx.ContextSnapshot) SocialReply {
	// Generic safe acknowledgment
	text := "I hear you. Let me check on that."
	return SocialReply{Text: text, Segments: []string{text}}
}

func (r *Renderer) RenderClarification() SocialReply {
	text := "Could you say that again? I want to make sure I help you correctly."
	return SocialReply{Text: text, Segments: []string{text}}
}

// Deterministic Builders (Step 6)

func (r *Renderer) BuildGreetingReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return "Hello! How can I help you?"
	}
	base := "Welcome to " + knowledge.BusinessName + "! 👋"
	if knowledge.Description != "" {
		return base + " " + knowledge.Description
	}
	return base + " How can I help you today?"
}

func (r *Renderer) BuildKnowledgeReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return "How can I help you correctly?"
	}
	if knowledge.HasProducts || knowledge.HasMenu {
		return r.BuildMenuReply(knowledge)
	}
	if knowledge.HasServices {
		return r.BuildServiceReply(knowledge)
	}
	return r.BuildBusinessOverviewReply(knowledge)
}

func (r *Renderer) BuildBusinessOverviewReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return "How can I help you?"
	}
	var sb strings.Builder
	sb.WriteString("Welcome to " + knowledge.BusinessName + "! 👋\n")

	if knowledge.Description != "" {
		sb.WriteString(knowledge.Description + "\n")
	}

	if knowledge.HasProducts || knowledge.HasMenu {
		sb.WriteString("\nWould you like to see our menu?")
	} else if knowledge.HasServices {
		sb.WriteString("\nWe offer professional services. How can we help?")
	} else {
		sb.WriteString("\nHow can I help you regarding " + knowledge.Industry + "?")
	}

	return sb.String()
}

func (r *Renderer) BuildMenuReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	var sb strings.Builder
	sb.WriteString("Here’s what we have available 👇\n\n")

	// Combine Products and MenuItems
	count := 0

	// Products
	for _, p := range knowledge.Products {
		if count >= 10 {
			break
		}
		price := "Price on request"
		if p.Price > 0 {
			price = fmt.Sprintf("₦%.0f", p.Price)
		}
		sb.WriteString("• " + p.Name + " – " + price + "\n")
		count++
	}

	// MenuItems
	if count < 10 {
		for _, m := range knowledge.MenuItems {
			if count >= 10 {
				break
			}
			price := "Price on request"
			if m.Price > 0 {
				price = fmt.Sprintf("₦%.0f", m.Price)
			}
			sb.WriteString("• " + m.Name + " – " + price + "\n")
			count++
		}
	}

	if count == 0 {
		return "Feel free to ask about our products."
	}

	sb.WriteString("\nWhat would you like to order?")
	return sb.String()
}

func (r *Renderer) BuildServiceReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	var sb strings.Builder
	sb.WriteString("Here are our services:\n\n")

	for i, s := range knowledge.Services {
		if i >= 5 {
			break
		}
		sb.WriteString("🔹 " + s.Name + "\n")
		if s.Description != "" {
			sb.WriteString("  " + s.Description + "\n")
		}
	}

	sb.WriteString("\nWould you like to book a consultation?")
	return sb.String()
}

func (r *Renderer) polishWeb(ctx context.Context, draft string, snap *v4ctx.ContextSnapshot) (string, error) {
	// Step 7: Restrict LLM to polish only
	sysPrompt := `You are an assistant speaking for a business.
You MUST NOT invent facts.
You may ONLY rephrase the provided draft.
Do not ask follow up questions unless the draft asks one.
Keep it concise.`

	fullPrompt := sysPrompt + "\n\nDraft: " + draft + "\n\nRewrite:"

	if r.llm != nil {
		return r.llm.Generate(fullPrompt)
	}
	return draft, nil
}

func (r *Renderer) splitMessage(text string, platform string) []string {
	// Simple split by newline for now
	return strings.Split(text, "\n")
}

// STEP 2: RenderFromSemantic implementation
func (r *Renderer) RenderFromSemantic(input RenderInput) SocialReply {
	reason := ""
	if input.Decision != nil {
		reason = input.Decision.Reason
	}

	switch reason {
	case "semantic_business_info", "semantic_business_intro":
		// Use Deterministic Resolver (Part 5)
		text := r.DeterministicBusinessReply(input.Context.TenantKnowledge, "")
		return SocialReply{Text: text, Segments: []string{text}}

	case "semantic_support":
		return r.RenderSupportIntro(input.Context)

	case "semantic_greeting":
		return r.RenderGreeting()

	default:
		// semantic exists but no mapping — still better than fallback
		// We use the Meaning from the input if available
		meaning := ""
		if input.Semantic != nil {
			meaning = input.Semantic.Meaning
		}
		return SocialReply{
			Text: meaning,
		}
	}
}

func (r *Renderer) RenderBusinessIntro(ctx *v4ctx.ContextSnapshot) SocialReply {
	desc := ctx.Business.Description
	if desc == "" {
		desc = "providing great products and services"
	}
	text := fmt.Sprintf("We distinguish ourselves by %s.", desc)
	return SocialReply{Text: text, Segments: []string{text}}
}

func (r *Renderer) RenderSupportIntro(ctx *v4ctx.ContextSnapshot) SocialReply {
	text := "I understand you need assistance. Could you elaborate on what you need?"
	return SocialReply{Text: text, Segments: []string{text}}
}

func (r *Renderer) RenderGreeting() SocialReply {
	text := "Hi! I'm here to help."
	return SocialReply{Text: text, Segments: []string{text}}
}

// PART 5: DETERMINISTIC BUSINESS REPLY (NO INTENTS)
func (r *Renderer) DeterministicBusinessReply(
	k *v4ctx.TenantKnowledgeSnapshot,
	userMessage string,
) string {

	switch {
	case len(k.MenuItems) > 0:
		return r.BuildMenuReply(k)

	case len(k.Products) > 0:
		return r.BuildMenuReply(k) // Reusing BuildMenuReply for Products as they share logic in previous impl

	case len(k.Services) > 0:
		return r.BuildServiceReply(k)

	case k.Description != "":
		return fmt.Sprintf(
			"%s\n\nLocation: %s\nHours: %s",
			k.Description,
			k.Location,
			k.OperatingHours,
		)

	default:
		return "We’re setting up our business details. Please check back shortly."
	}
}
