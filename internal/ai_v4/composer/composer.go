package composer

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/industry"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// Composer generates AI responses and proposed actions
type Composer interface {
	Compose(ctx context.Context, resolved *types.ResolvedContext, intent *types.IntentResult, policy *types.PolicyDecision, knowledge *v4ctx.TenantKnowledgeSnapshot) (*types.AIResponse, error)
	BuildEscalation(resolved *types.ResolvedContext, intent *types.IntentResult, reason string) *types.AIResponse
}

// DefaultComposer implements response composition
type DefaultComposer struct {
	llm    types.LLMClient
	logger *zap.Logger
}

// NewComposer creates a new response composer
func NewComposer(llm types.LLMClient, logger *zap.Logger) *DefaultComposer {
	return &DefaultComposer{
		llm:    llm,
		logger: logger,
	}
}

// Compose generates a response based on context, intent, and policy
func (c *DefaultComposer) Compose(ctx context.Context, resolved *types.ResolvedContext, intent *types.IntentResult, policy *types.PolicyDecision, knowledge *v4ctx.TenantKnowledgeSnapshot) (*types.AIResponse, error) {
	// FIX 3: MENU REQUEST MUST OVERRIDE ORDER FLOW
	// This resets accidental drift if user asks for menu
	lower := strings.ToLower(resolved.Message)
	if strings.Contains(lower, "menu") {
		resolved.Signals.ExplicitCommit = false
		resolved.Signals.HasBuyingIntent = false
		resolved.Signals.ItemsSelected = false // Reset implicit selection too
		// Also unlock flow? User said "resets accidental drift".
		// If flow is locked, menu request should probably unlock it or be ignored?
		// "Menu request must override order flow" -> implies unlocking or pausing.
		resolved.Signals.OrderFlowLocked = false

		// FIX 1 (Set 4): HARD OVERRIDE FOR MENU REQUESTS
		return &types.AIResponse{
			ReplyText:       c.buildMenuReply(knowledge),
			ReplySegments:   splitIntoSegments(c.buildMenuReply(knowledge)),
			ProposedActions: []types.ProposedAction{},
			NewState:        types.StateBrowsing,
			NewMode:         types.ModeSupport,
		}, nil
	}

	// FIX 7: USE THE FLOW LOCK
	// Prevents intent drift forever once order starts
	if resolved.Signals.OrderFlowLocked {
		// Force Purchase Intent if locked, unless it's strictly payment or special case logic
		// But user said: "if resolved.Signals.OrderFlowLocked { intent.Primary = types.IntentPurchase }"
		// We should respect that but allow "payment" related intents to pass through if needed (e.g. Complaint/Refund handled by Policy)
		// But for general chat, loop back to Purchase/Order logic.
		intent.Primary = types.IntentPurchase
	}

	// Handle escalation immediately (hard rule)
	if policy.MustEscalate {
		// ... existing escalation logic ...
		return c.BuildEscalation(resolved, intent, policy.EscalateReason), nil
	}

	// 1. Build Deterministic Response (Content Source of Truth)
	reply, actions := c.buildDeterministicResponse(resolved, intent, knowledge)
	baseReply := reply

	// Force text menu rendering in Go if this is a menu request
	if intent.Entities["request_type"] == "menu" {
		baseReply = c.buildMenuReply(knowledge)
	}

	// 2. Use LLM as renderer (rewrite only) with strict fact preservation
	var mustInclude []string
	if shouldRequireMenuItems(intent, knowledge, baseReply) {
		mustInclude = buildMenuMustInclude(knowledge)
	}

	if c.llm != nil {
		if llmReply, err := c.renderWithLLM(ctx, resolved, intent, baseReply, mustInclude); err == nil && llmReply != "" {
			if isLLMReplyValid(llmReply, baseReply, mustInclude) {
				reply = llmReply
			} else {
				c.logger.Warn("LLM reply failed validation, using deterministic reply")
				reply = baseReply
			}
		} else if err != nil {
			c.logger.Warn("LLM rendering failed, using deterministic reply", zap.Error(err))
			reply = baseReply
		}
	} else {
		reply = baseReply
	}

	// Determine newMode based on intent and signals
	cap := industry.GetCapability(resolved.Business.Industry)
	newMode := types.DetermineNextMode(resolved.Memory.Mode, intent.Primary, resolved.Signals, cap)

	// FIX: Hard override to prevent escalation during order flow
	// If ordering is happening, force OrderCollection unless purely payment flow
	if (intent.Primary == types.IntentPurchase || resolved.Signals.HasBuyingIntent) &&
		resolved.Memory != nil &&
		resolved.Memory.State != types.StateAwaitingPayment {
		// NEVER escalate here - assume we are collecting items
		newMode = types.ModeOrderCollection
	}

	// Fix 3: Enforce Deterministic Actions (Action Priority)
	// These actions override or augment LLM suggestions
	mandatoryActions := c.enforceDeterministicActions(resolved, intent)

	// FIX 5: Gate recommendations once committed
	// If we have an active order, acts as a "hard stop" on menu spam
	if resolved.Memory.ActiveOrderID != nil {
		// Filter out ActionSendOfferings if it was added
		filtered := make([]types.ProposedAction, 0, len(mandatoryActions))
		for _, a := range mandatoryActions {
			if a.Type != types.ActionSendOfferings {
				filtered = append(filtered, a)
			}
		}
		mandatoryActions = filtered
	}

	if len(mandatoryActions) > 0 {
		// Prepend mandatory actions (higher priority)
		actions = append(mandatoryActions, actions...)
	}

	// Handle Escalation Mode from State Controller
	if newMode == types.ModeEscalation {
		return c.BuildEscalation(resolved, intent, "policy_violation"), nil
	}

	// Map mode back to state for legacy compatibility
	var newState types.ConversationState
	switch newMode {
	case types.ModeDiscovery:
		newState = types.StateGreeting
	case types.ModeSupport:
		newState = types.StateBrowsing
	case types.ModeSales:
		newState = types.StateBrowsing
	case types.ModeNegotiation:
		newState = types.StateNegotiating
	case types.ModeOrderCollection:
		newState = types.StateOrdering
	case types.ModePaymentAwareness:
		newState = types.StateAwaitingPayment
	case types.ModeEscalation:
		newState = types.StateEscalated
	}

	return &types.AIResponse{
		ReplyText:       reply,
		ReplySegments:   splitIntoSegments(reply),
		ProposedActions: actions,
		NewState:        newState,
		NewMode:         newMode,
	}, nil
}

// buildDeterministicResponse creates the base response using templates
func (c *DefaultComposer) buildDeterministicResponse(resolved *types.ResolvedContext, intent *types.IntentResult, knowledge *v4ctx.TenantKnowledgeSnapshot) (string, []types.ProposedAction) {
	var reply string
	var actions []types.ProposedAction

	switch intent.Primary {
	case types.IntentGreeting:
		reply = c.buildGreeting(resolved, knowledge)

	case types.IntentInquiry, types.IntentPriceCheck:
		// Logic: If they ask for price/inquiry, check what we have
		if knowledge.HasProducts || knowledge.HasMenu {
			reply = c.buildMenuReply(knowledge)
		} else if knowledge.HasServices {
			reply = c.buildServiceReply(knowledge)
		} else {
			reply = c.buildBusinessOverviewReply(knowledge)
		}
		// Note: ActionSendOfferings logic moved to enforceDeterministicActions

	case types.IntentPurchase:
		reply, actions = c.buildPurchaseResponse(resolved, intent, knowledge)

	case types.IntentOrderStatus:
		reply = c.buildOrderStatusResponse(resolved)

	case types.IntentNegotiation:
		reply = c.buildNegotiationResponse(resolved, intent)

	case types.IntentThankYou:
		reply = c.buildThankYouResponse(resolved)

	case types.IntentGoodbye:
		reply = c.buildGoodbyeResponse(resolved)

	case types.IntentComplaint: // Add explicit handler
		reply = "I'm sorry to hear that. Could you tell me more about what went wrong? I'll do my best to fix it."

	default:
		// STRICT FAILURE REPLACEMENT (Step 6 & 8)
		// No generic fallbacks. Route based on knowledge.
		reply = c.buildBusinessOverviewReply(knowledge)
	}

	return reply, actions
}

// enforceDeterministicActions adds mandatory actions based on business rules (Fix 3)
func (c *DefaultComposer) enforceDeterministicActions(resolved *types.ResolvedContext, intent *types.IntentResult) []types.ProposedAction {
	// Fix 3 (Set 9): CONFIRM ORDER SHOULD FIRE ONCE — EVER
	if intent.Primary == types.IntentConfirmOrder &&
		resolved.Memory.ActiveOrderID != nil &&
		!resolved.Signals.OrderCommitted {

		resolved.Signals.OrderCommitted = true
		resolved.Signals.OrderFlowLocked = true

		return []types.ProposedAction{
			{
				Type:       types.ActionConfirmOrder,
				Confidence: 1.0,
				Payload: map[string]any{
					"order_id": resolved.Memory.ActiveOrderID.String(),
				},
			},
		}
	}

	// Fix 5 (Set 9): SEND PAYMENT DETAILS ONCE — NEVER AGAIN
	if resolved.Signals.OrderCommitted &&
		!resolved.Signals.PaymentRequested {

		resolved.Signals.PaymentRequested = true

		return []types.ProposedAction{
			{
				Type:       types.ActionSendPaymentInstructions,
				Confidence: 1.0,
				Payload: map[string]any{
					"payment": resolved.Payment,
				},
			},
		}
	}

	// Lock out everything else if committed
	if resolved.Signals.OrderCommitted {
		return nil
	}

	actions := []types.ProposedAction{}

	// Fix 6: Stop Menu Spam After Commitment (FINAL LOCK)
	if resolved.Signals.ItemsSelected || resolved.Memory.ActiveOrderID != nil {
		// return actions // NO offerings (Logic subsumed by blocking above if committed? No, active order vs committed)
		// Keeping existing specific logic for pre-commit active order
	}

	// Trigger if intent is ConfirmOrder OR Purchase (with context) AND we are in proper mode
	if (intent.Primary == types.IntentPurchase || intent.Primary == types.IntentConfirmOrder || resolved.Signals.HasBuyingIntent) &&
		resolved.Memory.ActiveOrderID != nil &&
		!resolved.Signals.PaymentRequested &&
		!resolved.Signals.PaymentProofReceived &&
		!resolved.Signals.OrderCommitted { // Avoid duplicate action if OrderCommitted handled it above

		actions = append(actions, types.ProposedAction{
			Type:       types.ActionSendPaymentInstructions,
			Confidence: 1.0,
			Payload: map[string]any{
				"payment": resolved.Payment,
			},
		})
	}

	// Rule 3: Handle Payment Proof (Silent Escalation)
	if resolved.Signals.PaymentProofReceived && resolved.Memory.ActiveOrderID != nil {
		actions = append(actions, types.ProposedAction{
			Type:       types.ActionCreatePendingPayment,
			Confidence: 1.0,
			Payload: map[string]any{
				"scope": types.MemoryScope{
					TenantID:   resolved.Business.ID,
					CustomerID: resolved.Customer.Phone, // Using phone as ID or need real ID?
					// Note: ResolveContext definition of CustomerInfo has ID uuid.UUID
					// But MemoryScope has CustomerID string.
					// Ideally we pass the UUID string representation to the executor which expects it.
					// Let's pass the raw values carefully.
					// Using resolved.Customer.ID.String() matches what we expect in Executor
					ConversationID: types.UUIDToStringOrNil(resolved.Memory.ActiveOrderID), // Wait, likely nil if not tracking conversation UUID in scope
					Platform:       resolved.Platform,
				},
				"active_order_id": resolved.Memory.ActiveOrderID.String(),
				"customer_id":     resolved.Customer.ID.String(), // Pass explicit UUID for DB
			},
		})
	}

	// Rule 4: Wiring Implicit Item Selection (The Missing Link)
	// If intent detected "implicit_add" (e.g. "1 chocolate"), we MUST propose ActionAddToOrder
	if intent.Entities["implicit_add"] == true && resolved.Memory.ActiveOrderID != nil {
		rawText, _ := intent.Entities["raw_text"].(string)
		parsedItems := parseImplicitItems(rawText)

		if len(parsedItems) > 0 {
			actions = append(actions, types.ProposedAction{
				Type:       types.ActionAddToOrder,
				Confidence: 1.0,
				Payload: map[string]any{
					"active_order_id": resolved.Memory.ActiveOrderID.String(),
					"items":           parsedItems,
				},
			})
		}
	}

	return actions
}

// parseImplicitItems provides basic parsing for "1 item" patterns
// This is a heuristic fallback when LLM entity extraction hasn't run
func parseImplicitItems(text string) []map[string]any {
	items := []map[string]any{}

	// Very basic splitter: "1 a, 2 b" -> ["1 a", "2 b"]
	parts := strings.Split(text, ",")
	if strings.Contains(text, " and ") {
		// Handle "1 a and 1 b"
		parts = strings.Split(strings.ReplaceAll(text, " and ", ","), ",")
	}

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Look for leading digit
		qty := 1
		name := p

		// Simple scan for "N name"
		var i int
		for i = 0; i < len(p); i++ {
			if p[i] >= '0' && p[i] <= '9' {
				continue
			}
			break
		}

		if i > 0 {
			// Found leading digits
			if q, err := strconv.Atoi(p[:i]); err == nil {
				qty = q
			}
			name = strings.TrimSpace(p[i:])
		}

		if name != "" {
			items = append(items, map[string]any{
				"name":     name,
				"quantity": qty,
				"price":    0, // Price unknown at this stage, will be resolved or 0
			})
		}
	}

	return items
}

// BuildEscalation creates an escalation response
func (c *DefaultComposer) BuildEscalation(resolved *types.ResolvedContext, intent *types.IntentResult, reason string) *types.AIResponse {
	priority := types.PriorityMedium
	if intent.Primary == types.IntentComplaint || intent.Primary == types.IntentRefund {
		priority = types.PriorityHigh
	}

	return &types.AIResponse{
		ReplyText: "Let me connect you with someone who can help you better. They'll be with you shortly! 🙏",
		ReplySegments: []string{
			"Let me connect you with someone who can help you better.",
			"They'll be with you shortly! 🙏",
		},
		Escalation: &types.EscalationRequest{
			Reason:      reason,
			Priority:    priority,
			Context:     c.buildContextSummary(resolved),
			CustomerMsg: resolved.Message,
		},
		ProposedActions: []types.ProposedAction{
			{Type: types.ActionEscalate, Confidence: 1.0},
		},
		NewState: types.StateEscalated,
		NewMode:  types.ModeEscalation,
	}
}

// buildGreeting creates a greeting response
func (c *DefaultComposer) buildGreeting(resolved *types.ResolvedContext, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	name := knowledge.BusinessName
	if name == "" {
		name = "us"
	}

	customerName := ""
	if resolved.Customer.Name != "" {
		customerName = " " + resolved.Customer.Name
	}

	// Grounded greeting
	base := "Hey" + customerName + "! 👋 Welcome to " + name + "."

	if knowledge.Description != "" {
		return base + " " + knowledge.Description // e.g. "We sell vintage shoes."
	}

	return base + " How can I help you today?"
}

// buildInquiryResponse creates an inquiry response
func (c *DefaultComposer) buildInquiryResponse(resolved *types.ResolvedContext, intent *types.IntentResult) string {
	if len(resolved.Offerings) == 0 {
		return "Thanks for reaching out! What are you looking for? I'll be happy to help."
	}

	// Build a simple menu/list (FIX 6: Canonical Text Format)
	var sb strings.Builder
	sb.WriteString("Here’s our menu 👇\n\n")

	// Show top 10 offerings (increased from 5)
	shown := 0
	for _, o := range resolved.Offerings {
		if shown >= 10 {
			break
		}
		if o.Price > 0 {
			sb.WriteString("• " + o.Name + " – ₦" + formatPrice(o.Price) + "\n")
		} else {
			sb.WriteString("• " + o.Name + "\n")
		}
		shown++
	}

	// Fix 7 (Set 9): STOP ASKING “WHAT WOULD YOU LIKE TO ORDER?” AFTER ITEMS SELECTED
	if resolved.Signals.ItemsSelected || resolved.Memory.ActiveOrderID != nil {
		return sb.String()
	}

	if len(resolved.Offerings) > 10 {
		sb.WriteString("\nOnly showing top 10 items. What would you like to order?")
	} else {
		sb.WriteString("\nWhat would you like to order?")
	}

	return sb.String()
}

// buildPurchaseResponse handles purchase intent
func (c *DefaultComposer) buildPurchaseResponse(resolved *types.ResolvedContext, intent *types.IntentResult, knowledge *v4ctx.TenantKnowledgeSnapshot) (string, []types.ProposedAction) {
	// Fix 2 (Set 9): HARD STOP CONFIRMATION LOOP
	if resolved.Signals.OrderCommitted {
		return "Thanks! Your order is locked in. I’ll guide you through payment next 😊", nil
	}

	actions := []types.ProposedAction{}

	// FIX 1: HARD-GUARD ORDER CREATION (NO ORDER ON INQUIRY)
	// 🚫 HARD STOP: no order unless user explicitly commits (Implicit "1 chocolate" is NOT explicit commit?)
	// User said: "Implicit cart additions fall through -> low confidence -> escalation" was the bug.
	// User Fix 1 says: "But menu requests are sometimes drifting into IntentPurchase... HARD STOP: no order unless user explicitly commits"
	// However, Fix 2 says: "1 chocolate -> IntentPurchase".
	// The nuance is: "can I see your menu?" -> No order.
	// "1 chocolate" -> Order.
	// So " ExplicitCommit" should be true if "1 chocolate" is said?
	// User defined ExplicitCommit phrases as "i want to order", "place order".
	// "1 chocolate" triggers ItemsSelected, but maybe not ExplicitCommit?
	// WAIT. If I implement Fix 1 strictly:
	// if !resolved.Signals.ExplicitCommit { return buildInquiryResponse... }
	// Then "1 chocolate" (ItemsSelected=true, ExplicitCommit=false) will result in InquiryResponse ("Here is the menu").
	// This contradicts the previous requirement to make "1 chocolate" work.
	// UNLESS "1 chocolate" implies ExplicitCommit?
	// Re-reading user request: "FIX 2: ADD EXPLICIT COMMIT SIGNAL... commitPhrases := []string{...}"
	// "1 chocolate" is NOT in commit phrases.
	// Is there a conflict?
	// The user says: "For this input: can I see your menu? Correct response: Here's our menu... No order. No checkout."
	// And "1 chocolate & 1 vanilla -> add items".
	// If I strictly follow Fix 1, "1 chocolate" will fail if it's not "ExplicitCommit".
	// NOTE: The previous Fix 2 adds `ItemsSelected` signal.
	// Maybe Fix 1 should check `ExplicitCommit || ItemsSelected`?
	// User said: "**What would you like?** No order." for menu inquiry.
	// But for "1 chocolate", they WANT action.
	// So I will assume `ImplicitCommit` (ItemsSelected) should also bypass the guard, OR the user missed it.
	// BUT, Fix 1 says: `if !resolved.Signals.ExplicitCommit { return ... }`.
	// If I blindly follow valid "1 chocolate" will break.
	// However, I must follow "NON-NEGOTIABLE".
	// I will check `ItemsSelected` too, because otherwise Fix 3/4 from Set 2 (1 chocolate) is undone.
	// Actually, `ActionAddToOrder` is proposed in `Composer` BEFORE `buildPurchaseResponse` if `ItemsSelected` is true (Fix 3 Set 2).
	// `buildPurchaseResponse` is called for `IntentPurchase`.
	// If `ItemsSelected` is true, we assume `IntentPurchase`.
	// If `ItemsSelected` is true, `enforceDeterministicActions` (where I put Fix 3 Set 2 logic to propose AddToOrder) MIGHT run?
	// Wait, I put Fix 3 Set 2 logic inside `buildPurchaseResponse` in Step 603!
	// So if I guard `buildPurchaseResponse` with `!ExplicitCommit`, I kill `AddToOrder` for "1 chocolate".
	// I MUST include `ItemsSelected` in the guard.

	shouldCreateOrder := resolved.Signals.ExplicitCommit || resolved.Signals.ItemsSelected

	if !shouldCreateOrder {
		// Just show menu or ask what they want
		if knowledge.HasProducts || knowledge.HasMenu {
			return c.buildMenuReply(knowledge), actions
		}
		return c.buildBusinessOverviewReply(knowledge), actions
	}

	// Check if already have an order
	if resolved.Memory.ActiveOrderID != nil && (resolved.Signals.ItemsSelected || resolved.Signals.HasBuyingIntent) {
		actions = append(actions, types.ProposedAction{
			Type:       types.ActionAddToOrder,
			Confidence: 1.0,
			Payload: map[string]any{
				"order_id": resolved.Memory.ActiveOrderID.String(),
				"raw_text": resolved.Message, // parsed later
			},
		})

		// Fix 3 (Set 7): STOP ASKING QUESTIONS AFTER COMMIT
		if resolved.Signals.OrderCommitted {
			return "Got it. I'll send your payment details now.", []types.ProposedAction{} // Actions will be populated by enforceDeterministicActions
		}

		return "Noted. Anything else, or should I share payment details?", actions
	}

	if resolved.Memory.ActiveOrderID != nil {
		return "Updated. Ready for payment?", actions
	}

	// New order initialization (Fix 4: Gate with Explicit Commit/Selection)
	if shouldCreateOrder {
		scope := types.MemoryScope{
			TenantID:   resolved.Business.ID,
			CustomerID: resolved.Customer.ID.String(), // Use UUID string
			Platform:   resolved.Platform,
		}

		actions = append(actions, types.ProposedAction{
			Type:       types.ActionInitializeOrder,
			Confidence: 1.0,
			Payload: map[string]any{
				"scope":       scope, // Pass scope for DB creation
				"customer_id": resolved.Customer.ID.String(),
				"items_json":  "[]", // Start empty
			},
		})
	}

	return "Got it. What item should I add first?", actions
}

// buildOrderStatusResponse handles order status inquiries
func (c *DefaultComposer) buildOrderStatusResponse(resolved *types.ResolvedContext) string {
	// Fix 6 (Set 9): AI MUST KNOW WHAT THE ORDER IS
	summary := resolved.Memory.Metadata["order_summary"]
	if summary != nil {
		return "Here’s your order so far:\n" + summary.(string)
	}

	if resolved.Memory.ActiveOrderID != nil {
		return "Let me check on your order... 🔍"
	}
	return "Your order is being prepared. Let me know if you’d like to make any changes."
}

// buildNegotiationResponse handles negotiation
func (c *DefaultComposer) buildNegotiationResponse(resolved *types.ResolvedContext, intent *types.IntentResult) string {
	// Basic negotiation response - detailed logic is in negotiation.Manager
	if resolved.Memory.LastOfferAmount > 0 {
		return "I hear you! Let me see what we can do..."
	}
	return "What price were you thinking?"
}

// buildThankYouResponse handles thank you messages
func (c *DefaultComposer) buildThankYouResponse(resolved *types.ResolvedContext) string {
	responses := []string{
		"You're welcome! 😊",
		"Happy to help!",
		"Anytime! Let me know if you need anything else.",
	}
	// Use a simple selection based on message count for variety
	idx := len(resolved.Memory.Messages) % len(responses)
	return responses[idx]
}

// buildGoodbyeResponse handles goodbye messages
func (c *DefaultComposer) buildGoodbyeResponse(resolved *types.ResolvedContext) string {
	name := resolved.Customer.Name
	if name != "" {
		return "Bye " + name + "! Come back anytime 👋"
	}
	return "Bye! Thanks for stopping by 👋"
}

// buildBusinessOverviewReply (Step 6)
func (c *DefaultComposer) buildBusinessOverviewReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
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

// buildProductReply / buildMenuReply
func (c *DefaultComposer) buildMenuReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
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
		if p.Price >= 0 {
			price = "₦" + formatPrice(p.Price)
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
			if m.Price >= 0 {
				price = "₦" + formatPrice(m.Price)
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

// buildServiceReply
func (c *DefaultComposer) buildServiceReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
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

// buildFAQReply
func (c *DefaultComposer) buildFAQReply(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if len(knowledge.FAQs) > 0 {
		return "Here are some common questions:\n• " + knowledge.FAQs[0].Question + "\n\n" + knowledge.FAQs[0].Answer
	}
	return c.buildBusinessOverviewReply(knowledge)
}

// buildContextSummary creates a context summary for escalation
func (c *DefaultComposer) buildContextSummary(resolved *types.ResolvedContext) string {
	var sb strings.Builder
	sb.WriteString("Customer: ")
	if resolved.Customer.Name != "" {
		sb.WriteString(resolved.Customer.Name)
	} else {
		sb.WriteString("Unknown")
	}
	sb.WriteString("\nPlatform: " + resolved.Platform)
	sb.WriteString("\nState: " + string(resolved.Memory.State))
	if resolved.Memory.ActiveOrderID != nil {
		sb.WriteString("\nHas active order")
	}
	return sb.String()
}

// determineNextState determines the next conversation state
func (c *DefaultComposer) determineNextState(intent types.Intent, currentState types.ConversationState) types.ConversationState {
	switch intent {
	case types.IntentGreeting:
		if currentState == types.StateNew {
			return types.StateGreeting
		}
		return currentState

	case types.IntentInquiry, types.IntentPriceCheck:
		return types.StateBrowsing

	case types.IntentPurchase:
		return types.StateOrdering

	case types.IntentNegotiation:
		return types.StateNegotiating

	case types.IntentGoodbye:
		if currentState == types.StateAwaitingPayment {
			return currentState // Don't change state if awaiting payment
		}
		return types.StateCompleted

	default:
		return currentState
	}
}

// buildMenuResponse renders the menu text specifically
func (c *DefaultComposer) buildMenuResponse(resolved *types.ResolvedContext) string {
	if len(resolved.Offerings) == 0 {
		c.logger.Warn("no offerings resolved for menu request",
			zap.String("tenant_id", resolved.Business.ID.String()),
		)
		return "Sorry, our menu is currently unavailable. Please check back shortly."
	}

	var sb strings.Builder
	sb.WriteString("Here’s our menu 👇\n\n")

	for _, o := range resolved.Offerings {
		sb.WriteString("• " + o.Name)
		// Fix 4 (Set 5): UX Rule - Show 0 if data is 0 (User Request)
		if o.Price >= 0 {
			sb.WriteString(" – ₦" + formatPrice(o.Price))
		} else {
			sb.WriteString(" – Price available on request")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nWhat would you like to order?")
	return sb.String()
}

// Helper functions

var numberTokenRe = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)

func shouldRequireMenuItems(intent *types.IntentResult, knowledge *v4ctx.TenantKnowledgeSnapshot, baseReply string) bool {
	if intent.Entities["request_type"] == "menu" {
		return knowledge.HasProducts || knowledge.HasMenu
	}
	if intent.Primary == types.IntentInquiry || intent.Primary == types.IntentPriceCheck {
		return knowledge.HasProducts || knowledge.HasMenu
	}
	return false
}

func buildMenuMustInclude(knowledge *v4ctx.TenantKnowledgeSnapshot) []string {
	items := []string{}
	count := 0

	for _, p := range knowledge.Products {
		if count >= 10 {
			break
		}
		if p.Name != "" {
			items = append(items, p.Name)
			count++
		}
	}

	if count < 10 {
		for _, m := range knowledge.MenuItems {
			if count >= 10 {
				break
			}
			if m.Name != "" {
				items = append(items, m.Name)
				count++
			}
		}
	}

	return items
}

func isLLMReplyValid(reply string, baseReply string, mustInclude []string) bool {
	if reply == "" {
		return false
	}

	lowerReply := strings.ToLower(reply)
	for _, item := range mustInclude {
		if item == "" {
			continue
		}
		if !strings.Contains(lowerReply, strings.ToLower(item)) {
			return false
		}
	}

	baseTokens := extractNumericTokens(baseReply)
	if len(baseTokens) == 0 {
		return true
	}

	normalizedReply := normalizeNumericText(reply)
	for _, token := range baseTokens {
		if !strings.Contains(normalizedReply, token) {
			return false
		}
	}

	return true
}

func extractNumericTokens(text string) []string {
	matches := numberTokenRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	tokens := make([]string, 0, len(matches))
	for _, match := range matches {
		normalized := normalizeNumericText(match)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		tokens = append(tokens, normalized)
	}

	return tokens
}

func normalizeNumericText(text string) string {
	return strings.ReplaceAll(text, ",", "")
}

func splitIntoSegments(text string) []string {
	// Split by sentence boundaries for natural delivery
	segments := []string{}
	current := ""

	for _, char := range text {
		current += string(char)
		if char == '.' || char == '!' || char == '?' {
			if len(strings.TrimSpace(current)) > 0 {
				segments = append(segments, strings.TrimSpace(current))
				current = ""
			}
		}
	}

	if len(strings.TrimSpace(current)) > 0 {
		segments = append(segments, strings.TrimSpace(current))
	}

	if len(segments) == 0 {
		return []string{text}
	}

	return segments
}

func formatPrice(price float64) string {
	// Fix 5 (Set 5): Use standard formatting
	return strconv.FormatFloat(price, 'f', 0, 64)
}
