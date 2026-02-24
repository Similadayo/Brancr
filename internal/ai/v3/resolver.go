package v3

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// =============================================================================
// ACTION RESOLVER - State Machine + Guards
// =============================================================================

// actionResolver implements ActionResolver interface
type actionResolver struct{}

// NewActionResolver creates a new action resolver
func NewActionResolver() ActionResolver {
	return &actionResolver{}
}

// =============================================================================
// HELPER: Dumb Keyword Check (User Requested)
// =============================================================================
func userMessageMentionsAvailability(msg string) bool {
	keywords := []string{
		"what do you have",
		"menu",
		"what's available",
		"what do you sell",
		"show me",
		"list",
		"catalogue",
		"options",
		"items",
		"something else",
		"other",
		"different",
	}
	lower := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// Decide determines what action to take based on intent and current state
// Uses deterministic state machine - NO LLM decisions here
func (r *actionResolver) Decide(ctx *ConversationContext, intent Intent) Action {
	// =========================================================================
	// AUTHORITY HIERARCHY:
	// 1. Explicit User Message (Keywords/Overrides)
	// 2. Explicit Rejection
	// 3. Pending Question (Obligations)
	// 4. Order State (Locks)
	// 5. Intent (NLU)
	// =========================================================================

	// 1. USER WORDS ARE LAW (Hard Overrides)
	// If tenant has products + user asks availability -> SHOW MENU.
	if len(ctx.RecentMessages) > 0 {
		lastMsg := ctx.RecentMessages[len(ctx.RecentMessages)-1]
		if lastMsg.Role == "user" && userMessageMentionsAvailability(lastMsg.Content) {
			if len(ctx.Products) > 0 || ctx.IndustryHasMenu {
				return Action{
					Type:    ActionShowMenu,
					Allowed: true,
					Reason:  "AUTHORITY: User explicitly requested menu (Override).",
				}
			}
		}

		// Phase 3: Affirmation != Mutation Guard
		if intent.Type == IntentAddItem && IsAffirmationOnly(lastMsg.Content) {
			intent.Type = IntentAffirm
			intent.Entities = Entities{}
		}
	}

	// 2. ORDER STATE LOCK (No Surprise Orders)
	allowedInLocked := intent.Type == IntentPaymentQuery ||
		intent.Type == IntentConfirmPayment ||
		intent.Type == IntentStatusInquiry ||
		intent.Type == IntentContextual ||
		intent.Type == IntentAffirm ||
		intent.Type == IntentReject

	if !allowedInLocked {
		if conflictAction := GuardNoMutationAfterConfirm(ctx); conflictAction != nil {
			return *conflictAction
		}
	}

	// 3. CRITICAL GUARDS (Add Item Proof)
	// (Logic continues...)
	// CRITICAL GUARD: ADD_ITEM requires product entity OR active context
	// This prevents "I want to order" from creating "1x Order"
	// But allows "Make it 2" to update the last item.
	// CRITICAL GUARD: ADD_ITEM requires product entity OR active context
	// This prevents "I want to order" from creating "1x Order"
	// But allows "Make it 2" to update the last item.
	if intent.Type == IntentAddItem {
		// GUARD 3: AFFIRMATION CANNOT MUTATE
		if IsAffirmationOnly(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content) {
			intent.Type = IntentAffirm
			intent.Entities = Entities{}
		} else if intent.Entities.Product == nil {
			// GUARD 2: QUANTITY WITHOUT PRODUCT != NEW ITEM
			if intent.Entities.Quantity > 0 {
				// Check if we have context (recent item in cart or order)
				hasContext := len(ctx.PendingCart) > 0 || (ctx.CurrentOrder != nil && len(ctx.CurrentOrder.Items) > 0)
				if !hasContext {
					return Action{
						Type:    ActionAskClarification,
						Allowed: true,
						Reason:  "Quantity without product scope - asking for clarification",
						Data: ActionData{
							Message: "Which item is that for?",
						},
					}
				}
				// If hasContext, proceed (executor will apply to last item)
			} else {
				// GUARD 1: ADD_ITEM REQUIRES PROOF
				// No product, no quantity -> Pure "I want to order" or hallucination
				return Action{
					Type:    ActionAskClarification,
					Allowed: true,
					Reason:  "No product specified - asking customer what they want",
					Data: ActionData{
						Message: "Sure! What item are you interested in?",
					},
				}
			}
		}
	}

	// ==========================================================================
	// PHASE 12: SILENCE GATE (Social Fluidity)
	// ==========================================================================
	// If input is short/low-value ("Okay", "None", "Note"), don't trigger full flows.
	// Just acknowledge and hold space.
	if len(ctx.RecentMessages) > 0 {
		msg := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content, ".", "")))
		if msg == "none" || msg == "okay" || msg == "ok" || msg == "alright" || msg == "noted" || msg == "done" {
			// Only gate if no explicit intent detected (or weak intent)
			if intent.Confidence < 0.8 && intent.Type != IntentCancel && intent.Type != IntentConfirmPayment {
				return Action{
					Type:    ActionDefer, // Silence / "Got it"
					Allowed: true,
					Reason:  "Silence Gate: Low-value input -> Minimal response",
					Data: ActionData{
						Message: "", // Empty message = Silence or "👍" handled by composer
					},
				}
			}
		}
	}

	// ==========================================================================
	// CONTROL CORRECTION: AUTHORITY HIERARCHY
	// 1. Explicit Rejection > 2. Obligations > 3. Intent > 4. Persona
	// ==========================================================================

	// 1. EXPLICIT REJECTION (Phase 6: Context-Aware Switching)
	// If user rejected the last item, we MUST offer an alternative.
	if ctx.Obligations != nil && ctx.Obligations.UserRejectedLastItem {
		return Action{
			Type:    ActionPitchProduct, // Recommend ONE alternative
			Allowed: true,
			Reason:  "Rejection Logic: User rejected last item -> Suggest alternative",
		}
	}

	// 2. PIDGIN GUARD: "KE" = Confusion/Rejection
	// "4 ke?" -> "Why 4?" / "No".
	if len(ctx.RecentMessages) > 0 {
		last := strings.ToLower(strings.TrimSpace(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content))
		if strings.HasSuffix(last, "ke") || strings.HasSuffix(last, "ke?") || strings.HasSuffix(last, "ke!") {
			return Action{
				Type:    ActionAskClarification, // Clarify what they mean
				Allowed: true,
				Reason:  "Pidgin Guard: 'Ke' detected indicating confusion/rejection",
				Data: ActionData{
					Message: "My apologies if I got that wrong. What did you mean?",
				},
			}
		}
	}

	// 3. HARD DATA OBLIGATIONS (Phase 4: Menu/Price is Absolute)
	// KEYWORD TRAP: If user explicitly asks for "menu" or "list", FORCE it.
	// This overrides NLU misclassification (e.g. tagging "Show menu" as "Recommendation")
	if len(ctx.RecentMessages) > 0 {
		last := strings.ToLower(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content)
		if strings.Contains(last, "menu") ||
			strings.Contains(last, "list") ||
			strings.Contains(last, "have") ||
			strings.Contains(last, "catalogue") ||
			strings.Contains(last, "options") {
			return Action{
				Type:    ActionShowMenu,
				Allowed: true,
				Reason:  "Keyword Override: Explicit Menu Request detected.",
			}
		}
	}

	if ctx.Obligations != nil && len(ctx.Obligations.PendingObligations) > 0 {
		for _, ob := range ctx.Obligations.PendingObligations {
			if !ob.Answered {
				// Menu/Price -> Show Menu
				if ob.QuestionType == "menu_inquiry" || ob.QuestionType == "price_inquiry" {
					return Action{
						Type:    ActionShowMenu,
						Allowed: true,
						Reason:  "Strict Obligation: Customer asked for menu/price.",
					}
				}
				// Recommendation -> Pitch Product
				if ob.QuestionType == "recommendation_request" {
					if len(ctx.Products) > 0 {
						// PHASE 6: Recommendation Kill Switch
						// If order is confirmed, NO MORE PITCHING.
						if ctx.StateSnapshot != nil && ctx.StateSnapshot.ItemsConfirmed {
							// Block pitch, just allow fallback or silence
							// Do nothing, let it fall through
						} else {
							// PHASE 5: Memory Guard
							// Don't pitch rejected items
							// Note: We don't know WHICH product to pitch here yet (Composer decides).
							// But we can Guard the Action Type later or trust Composer?
							// Actually, `ActionPitchProduct` tells Composer to pitch *something*.
							// Composer MUST check RejectedItems.
							// However, if we want to block the *Action* itself:
							// We can't unless we know the product.
							// But we can block the action if ALL products are rejected? Unlikely.
							// We will return ActionPitchProduct, ensuring Composer filters.
							// User says: "Add a resolver-level guard: if order.ItemsConfirmed { disallow(ACTION_PITCH_PRODUCT) }"
							// So specifically checking Confirmed here.

							return Action{
								Type:    ActionPitchProduct,
								Allowed: true,
								Reason:  "Strict Obligation: Customer asked for recommendation.",
							}
						}
					}
				}
			}
		}
	}

	// Get current order state
	orderState := StateIdle
	if ctx.CurrentOrder != nil {
		orderState = ctx.CurrentOrder.State
	}

	// Route based on intent type
	// ECHO GUARD: Check for double-counting on affirmation
	// If the AI thinks "Yes" means "Add Item" but we JUST added it, it's an affirmation.
	if intent.Type == IntentAddItem && len(ctx.RecentMessages) >= 2 {
		lastUserMsg := strings.ToLower(strings.TrimSpace(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content))
		lastAssistantMsg := strings.ToLower(ctx.RecentMessages[len(ctx.RecentMessages)-2].Content)

		// 1. Is the input clearly an affirmation?
		isAffirmation := lastUserMsg == "yes" || lastUserMsg == "yea" || lastUserMsg == "ok" || lastUserMsg == "okay" || lastUserMsg == "confirm" || lastUserMsg == "sure"

		// 2. Did we just add something?
		justAdded := strings.Contains(lastAssistantMsg, "proposal recorded") || strings.Contains(lastAssistantMsg, "got it") || strings.Contains(lastAssistantMsg, "added")

		if isAffirmation && justAdded {
			// OVERRIDE: This is a confirmation, not a new add
			intent.Type = IntentAffirm
			intent.Confidence = 1.0
			// Clear entities to prevent confusion
			intent.Entities = Entities{}
		}
	}

	switch intent.Type {
	case IntentGreeting:
		return r.handleGreeting(ctx, orderState)

	case IntentCancel:
		return r.handleCancel(ctx, orderState)

	case IntentHelp:
		return r.handleHelp(ctx)

	case IntentAddItem:
		return r.handleAddItem(ctx, intent, orderState)

	case IntentRemoveItem:
		return r.handleRemoveItem(ctx, intent, orderState)

	case IntentViewCart:
		return r.handleViewCart(ctx, orderState)

	case IntentClearCart:
		return r.handleClearCart(ctx, orderState)

	case IntentCheckout:
		return r.handleCheckout(ctx, orderState)

	case IntentPaymentQuery:
		return r.handlePaymentQuery(ctx, orderState)

	case IntentConfirmPayment:
		return r.handleConfirmPayment(ctx, orderState)

	case IntentProductInquiry:
		return r.handleProductInquiry(ctx)

	case IntentPriceInquiry:
		// GUARD 5: "HOW MUCH?" != MENU DUMP
		if len(ctx.PendingCart) > 0 || (ctx.CurrentOrder != nil && len(ctx.CurrentOrder.Items) > 0) {
			// If context has items, assume they want the TOTAL.
			return Action{
				Type:    ActionShowCart, // Or Quote total
				Allowed: true,
				Reason:  "Price Inquiry with items -> Show Total",
			}
		}
		// If no items, fall through to handlePriceInquiry (which asks "which item?")
		return r.handlePriceInquiry(ctx)

	case IntentStatusInquiry:
		return r.handleStatusInquiry(ctx)

	case IntentNegotiation:
		return r.handleNegotiation(ctx)

	case IntentAffirm:
		return r.handleAffirm(ctx, orderState)

	case IntentReject:
		return r.handleReject(ctx, intent, orderState)

	case IntentUnknown:
		// PHASE 1: ALIVE ASSISTANT
		if ctx.AllowFreeResponse {
			return Action{
				Type:    ActionFreeResponse,
				Allowed: true,
				Reason:  "Unknown intent but Free Response authorized - answering naturally",
			}
		}
		// PHASE 8: SAFETY NET (Context-Aware Clarification)
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Unknown intent - Safety Net trigger",
			Data: ActionData{
				Message: "No worries — are you looking to see the full menu, or want me to suggest something else?",
			},
		}

	// V2: Grounding-driven intents
	case IntentProductAdvisory:
		return r.handleProductAdvisory(ctx)

	case IntentServiceInquiry:
		return r.handleServiceInquiry(ctx)

	case IntentContextual:
		return r.handleContextual(ctx)

	default:
		// PHASE 1: ALIVE ASSISTANT
		if ctx.AllowFreeResponse {
			return Action{
				Type:    ActionFreeResponse,
				Allowed: true,
				Reason:  "Unhandled intent but Free Response authorized - answering naturally",
			}
		}

		// PHASE 8: FALBACK SAFETY NET
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Fallback Safety Net - asking for menu vs suggestion",
			Data: ActionData{
				Message: "No worries — are you looking to see the full menu, or want me to suggest something else?",
			},
		}
	}
}

// =============================================================================
// INTENT HANDLERS
// =============================================================================

func (r *actionResolver) handleGreeting(ctx *ConversationContext, state OrderState) Action {
	// Phase 9: Industry Awareness
	if ctx.RoleProfile.Behavior.ShowMenuOnGreet {
		// Phase 11 & 12: Strict Menu Fatigue check
		// Don't show menu again if we just showed it in last 3 turns
		if r.recentlyPerformed(ctx, ActionShowProducts, 3) {
			return Action{
				Type:    ActionGreet,
				Allowed: true,
				Reason:  "Menu already shown recently - just greeting",
			}
		}

		return Action{
			Type:    ActionShowProducts,
			Allowed: true,
			Reason:  "Industry behavior: Show menu/products on greeting",
		}
	}

	return Action{
		Type:    ActionGreet,
		Allowed: true,
		Reason:  "Customer said hello",
	}
}

func (r *actionResolver) handleCancel(ctx *ConversationContext, state OrderState) Action {
	// Can only cancel if there's an active order
	if state == StateIdle {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No active order to cancel",
			Data: ActionData{
				Message: "You don't have an active order. Would you like to place one?",
			},
		}
	}

	// Cannot cancel completed orders via chat
	if state == StatePayment || state == StateCompleted {
		return Action{
			Type:    ActionEscalate,
			Allowed: true,
			Reason:  "Completed orders require human support to cancel",
		}
	}

	return Action{
		Type:       ActionCancelOrder,
		Allowed:    true,
		RequiresTx: true,
		Reason:     "Cancelling active order",
	}
}

func (r *actionResolver) handleHelp(ctx *ConversationContext) Action {
	return Action{
		Type:    ActionEscalate,
		Allowed: true,
		Reason:  "Customer requested human help",
	}
}

func (r *actionResolver) handleAddItem(ctx *ConversationContext, intent Intent, state OrderState) Action {
	// SNAPSHOT GUARD: Cannot mutate order after confirmation
	if state == StateConfirmed || state == StatePayment {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Order is locked for payment",
			Data: ActionData{
				Message: "Your order is already confirmed. Would you like to modify it?",
			},
		}
	}

	// Product entity logic
	// If product is missing, check if we can infer it from context (Last Active Item)
	var productID uuid.UUID

	if intent.Entities.Product != nil {
		productID = intent.Entities.Product.ID
	} else if len(ctx.PendingCart) > 0 {
		// Context Inference: Use the last item added
		lastItem := ctx.PendingCart[len(ctx.PendingCart)-1]
		productID = lastItem.ProductID
	} else {
		// Should have been caught by Decide guard, but safe fallback
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No product specified and no context",
			Data: ActionData{
				Message: "What item would you like to add?",
			},
		}
	}

	// NO SURPRISE ORDERS CONTRACT - Phase 2: Anti-Inflation Guard
	// Prevents "Quantity Drift" (4+4+4+4) by blocking re-adds of existing items.
	// Forces user to explicitly explicitly handle quantity changes via clarification.
	if GuardDuplicateAdd(ctx, productID) {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Duplicate add blocked - demanding explicit quantity update",
			Data: ActionData{
				Message: "I already have that item in your order. Want to change the quantity?",
			},
		}
	}

	quantity := intent.Entities.Quantity
	if quantity < 1 {
		quantity = 1
	}

	// Check product availability
	if !r.isProductInStock(ctx, productID) {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Product out of stock",
			Data: ActionData{
				Message: "That item is currently out of stock. Would you like something else?",
			},
		}
	}

	return Action{
		Type:       ActionAddToCart,
		Allowed:    true,
		RequiresTx: true,
		Reason:     "Adding item to cart",
		Data: ActionData{
			ProductID: productID,
			Quantity:  quantity,
			Explicit:  true, // Phase 4: Authorized Mutation
		},
	}
}

func (r *actionResolver) handleRemoveItem(ctx *ConversationContext, intent Intent, state OrderState) Action {
	// SNAPSHOT GUARD
	if state == StateConfirmed || state == StatePayment {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Order is locked for payment",
			Data: ActionData{
				Message: "Your order is confirmed. Do you want to cancel or modify it?",
			},
		}
	}

	// Must have an active cart
	if state == StateIdle {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No cart to remove from",
			Data: ActionData{
				Message: "You don't have any items in your cart yet.",
			},
		}
	}

	product := intent.Entities.Product
	if product == nil {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No product specified",
			Data: ActionData{
				Message: "Which item would you like to remove?",
			},
		}
	}

	return Action{
		Type:       ActionRemoveFromCart,
		Allowed:    true,
		RequiresTx: true,
		Reason:     "Removing item from cart",
		Data: ActionData{
			ProductID: product.ID,
		},
	}
}

func (r *actionResolver) handleViewCart(ctx *ConversationContext, state OrderState) Action {
	if state == StateIdle || ctx.CurrentOrder == nil || len(ctx.CurrentOrder.Items) == 0 {
		return Action{
			Type:    ActionShowCart,
			Allowed: true,
			Reason:  "Cart is empty",
		}
	}

	return Action{
		Type:    ActionShowCart,
		Allowed: true,
		Reason:  "Showing cart contents",
	}
}

func (r *actionResolver) handleClearCart(ctx *ConversationContext, state OrderState) Action {
	// SNAPSHOT GUARD
	if state == StateConfirmed || state == StatePayment {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Order is locked for payment",
			Data: ActionData{
				Message: "Order confirmed. To clear it, you'll need to cancel the order first.",
			},
		}
	}

	if state == StateIdle {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No cart to clear",
			Data: ActionData{
				Message: "Your cart is already empty.",
			},
		}
	}

	return Action{
		Type:       ActionClearCart,
		Allowed:    true,
		RequiresTx: true,
		Reason:     "Clearing cart",
	}
}

func (r *actionResolver) handleCheckout(ctx *ConversationContext, state OrderState) Action {
	// Must have items in cart
	if state == StateIdle || ctx.CurrentOrder == nil || len(ctx.CurrentOrder.Items) == 0 {
		// Checks pending cart too
		if len(ctx.PendingCart) == 0 {
			return Action{
				Type:    ActionAskClarification,
				Allowed: true,
				Reason:  "Cannot checkout empty cart",
				Data: ActionData{
					Message: "Your cart is empty. Add some items first!",
				},
			}
		}
	}

	// COMMITMENT GATE: High bar for checkout
	if ctx.CommitmentGradient < 0.85 {
		return Action{
			Type:    ActionShowCart, // Just show cart, don't trigger payment flow yet
			Allowed: true,
			Reason:  "Commitment too low for checkout flow - enforcing confirmation",
			Data: ActionData{
				Message: "I hav these items noted. Ready to wrap this up?",
			},
		}
	}

	// If already in payment state, show payment info
	if state == StatePayment {
		return Action{
			Type:    ActionShowPayment,
			Allowed: true,
			Reason:  "Already in payment state",
		}
	}

	// Need to collect address if not already done
	if ctx.CurrentOrder != nil && ctx.CurrentOrder.Address == "" {
		return Action{
			Type:    ActionCollectAddress,
			Allowed: true,
			Reason:  "Need delivery address",
		}
	}

	// Ready to show payment
	return Action{
		Type:       ActionShowPayment,
		Allowed:    true,
		RequiresTx: true, // Updates order status to payment
		Reason:     "Ready for payment",
	}
}

func (r *actionResolver) handlePaymentQuery(ctx *ConversationContext, state OrderState) Action {
	// Payment query requires items in pending cart (in-memory)
	// This is when customer asks "how do I pay?" or "account details"
	if len(ctx.PendingCart) == 0 && ctx.CurrentOrder == nil {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "No items in cart to pay for",
			Data: ActionData{
				Message: "You don't have any items in your cart yet. What would you like to order?",
			},
		}
	}

	// COMMITMENT GATE: STRICT CONFIRMATION REQUIRED
	// If order is not explicitly in PAYMENT or CONFIRMED state, force a review.
	// We do NOT show payment details based on "how much" or "account please" until summary is seen.
	if state != StatePayment && state != StateConfirmed && state != StateCheckout {
		return Action{
			Type:    ActionShowCart,
			Allowed: true,
			Reason:  "Enforcing confirmation gate before payment",
			Data: ActionData{
				Message: "Here is the summary of what you've picked. Does this look right to you?",
			},
		}
	}

	// Ready to show payment and CREATE ORDER IN DB
	// This is the trigger point where PendingCart becomes a real order
	return Action{
		Type:       ActionShowPayment,
		Allowed:    true,
		RequiresTx: true, // CREATES ORDER IN DB from PendingCart
		Reason:     "Customer asked for payment - creating order and showing details",
	}
}

func (r *actionResolver) handleConfirmPayment(ctx *ConversationContext, state OrderState) Action {
	if state != StatePayment {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Not in payment state",
			Data: ActionData{
				Message: "I don't have a pending payment to confirm. Do you have an order?",
			},
		}
	}

	return Action{
		Type:       ActionConfirmOrder,
		Allowed:    true,
		RequiresTx: true,
		Reason:     "Customer says they paid - confirming order",
	}
}

func (r *actionResolver) handleProductInquiry(ctx *ConversationContext) Action {
	// STRICT OBLIGATION: If customer asks about products/menu, SHOW THE MENU.
	// No fatigue checks. No "I already showed you".
	// The prompt builder will handle whether to just list top items or full menu.

	// If context has "specific" nuance, maybe pitch?
	// But `ProductInquiry` usually means "What do you have?".

	return Action{
		Type:    ActionShowMenu, // Explicitly show menu (list)
		Allowed: true,
		Reason:  "Customer asked for menu/products - STRICT OBLIGATION",
	}
}

func (r *actionResolver) handleProductAdvisory(ctx *ConversationContext) Action {
	// PHASE 5: RECOMMENDATION KILL SWITCH
	// Recommendation is allowed ONLY if:
	// ctx.State == OrderIdle && !ctx.HasSelection && !ctx.HasRejection

	hasSelection := len(ctx.PendingCart) > 0 || (ctx.CurrentOrder != nil && len(ctx.CurrentOrder.Items) > 0)
	hasRejection := ctx.StateSnapshot != nil && len(ctx.StateSnapshot.RejectedItems) > 0

	// Note: We use "StateIdle" or "StateBrowsing" as acceptable for recommendations.
	// If "Draft" or "Confirmed", we strictly BLOCK.
	// User said: "if ctx.State == OrderIdle".
	state := StateIdle
	if ctx.CurrentOrder != nil {
		state = ctx.CurrentOrder.State
	}
	// Also check PendingCart -> Implies "Draft"
	if len(ctx.PendingCart) > 0 {
		state = StateDraft
	}

	canRecommend := (state == StateIdle || state == StateBrowsing) && !hasSelection && !hasRejection

	if !canRecommend {
		// BLOCKED: Fallback to passive menu or clarification
		return Action{
			Type:    ActionShowProducts, // Passive list, not a pitch
			Allowed: true,
			Reason:  "Recommendation Kill Switch: State/Selection/Rejection exists -> BLOCK PITCH",
		}
	}

	// PHASE 4: ONE-PITCH-PER-DECISION
	// If we successfully pitched before, and user accepted/ignored (didn't reject),
	// do NOT pitch again. Ask for clarification/feedback instead.
	if ctx.Obligations != nil && ctx.Obligations.RecommendationGiven && !ctx.Obligations.UserRejectedLastItem {
		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Already made a recommendation. Asking for feedback instead of re-pitching.",
			Data: ActionData{
				Message: fmt.Sprintf("I suggested the %s earlier. Did that sound good to you, or are you looking for something else?", ctx.Obligations.LastRecommendation),
			},
		}
	}

	return Action{
		Type:    ActionPitchProduct, // Explicitly pitch/recommend
		Allowed: true,
		Reason:  "Customer asked for recommendation/advice (Kill Switch Passed)",
	}
}

func (r *actionResolver) handlePriceInquiry(ctx *ConversationContext) Action {
	return Action{
		Type:    ActionShowProducts,
		Allowed: true,
		Reason:  "Customer asking about prices",
	}
}

func (r *actionResolver) handleStatusInquiry(ctx *ConversationContext) Action {
	return Action{
		Type:    ActionShowStatus,
		Allowed: true,
		Reason:  "Customer asking about order status",
	}
}

func (r *actionResolver) handleNegotiation(ctx *ConversationContext) Action {
	// Route to LLM-based negotiation response
	// The composer will generate a natural response about negotiation
	return Action{
		Type:    ActionNegotiate,
		Allowed: true,
		Reason:  "Customer negotiating price",
	}
}

func (r *actionResolver) handleUnknown(ctx *ConversationContext, intent Intent) Action {
	// =========================================================================
	// PHASE 8: NO SAFETY SILENCE ("If the business can answer -> answer")
	// If low confidence, we DO NOT defer or stall.
	// We check if we have grounded data (Products/Services). If yes -> Show it.
	// =========================================================================

	// 1. Fallback: Contextual Data
	if len(ctx.Products) > 0 || ctx.IndustryHasMenu {
		return Action{
			Type:    ActionShowMenu,
			Allowed: true,
			Reason:  "Unknown intent fallback - showing menu/products (Data Default)",
		}
	}

	// 2. Fallback: Services
	if len(ctx.Services) > 0 {
		return Action{
			Type:    ActionShowProducts, // Use general show for services
			Allowed: true,
			Reason:  "Unknown intent fallback - showing services (Data Default)",
		}
	}

	// 3. Last Resort: Ask Clarification (Active, not Passive Deferral)
	return Action{
		Type:    ActionAskClarification,
		Allowed: true,
		Reason:  "Unknown intent & No Data - asking clarification",
		Data: ActionData{
			Message: "I'm here to help! Could you tell me what you're looking for?",
		},
	}
}

// V2: Grounding-driven intent handlers

func (r *actionResolver) handleServiceInquiry(ctx *ConversationContext) Action {
	// Customer asking about services
	return Action{
		Type:    ActionShowProducts,
		Allowed: true,
		Reason:  "Customer asking about services",
	}
}

func (r *actionResolver) handleContextual(ctx *ConversationContext) Action {
	// Contextual intent - we know the context, so we respond based on available data
	if len(ctx.Products) > 0 {
		return Action{
			Type:    ActionShowProducts,
			Allowed: true,
			Reason:  "Contextual - showing available products",
		}
	}
	if len(ctx.Services) > 0 {
		return Action{
			Type:    ActionShowProducts,
			Allowed: true,
			Reason:  "Contextual - showing available services",
		}
	}
	// Last resort - greet
	return Action{
		Type:    ActionGreet,
		Allowed: true,
		Reason:  "Contextual - greeting with business context",
	}
}

func (r *actionResolver) isProductInStock(ctx *ConversationContext, productID interface{}) bool {
	// Find product in context
	for _, p := range ctx.Products {
		if p.ID == productID {
			return p.InStock
		}
	}
	// If not found, assume in stock (fallback)
	return true
}

// recentlyPerformed checks if a specific action type was performed in the last N turns
func (r *actionResolver) recentlyPerformed(ctx *ConversationContext, actionType ActionType, turns int) bool {
	if len(ctx.RecentMessages) == 0 {
		return false
	}

	// Heuristic: Check recent assistant messages for keywords associated with the action
	// Look back N turns (approx 2*N messages if alternating)
	limit := turns * 2
	if limit > len(ctx.RecentMessages) {
		limit = len(ctx.RecentMessages)
	}

	recent := ctx.RecentMessages[len(ctx.RecentMessages)-limit:]

	for _, msg := range recent {
		if msg.Role == "assistant" {
			switch actionType {
			case ActionShowProducts:
				if strings.Contains(msg.Content, "Here's what we have") || strings.Contains(msg.Content, "See anything you like?") {
					return true
				}
			case ActionShowCart:
				if strings.Contains(msg.Content, "Your cart:") {
					return true
				}
			}
		}
	}

	return false
}

// handleAffirm transitions from Summary/Building to Payment
func (r *actionResolver) handleAffirm(ctx *ConversationContext, state OrderState) Action {
	// 1. CONFIRMATION GATE: If we presented a summary/cart, and user says "Yes", CONFIRM & LOCK.
	// This maps "Yes" -> "Confirm Order" (creating Snapshot)
	if state == StateBuilding || state == StateDraft {
		if len(ctx.PendingCart) > 0 || (ctx.CurrentOrder != nil && len(ctx.CurrentOrder.Items) > 0) {
			return Action{
				Type:       ActionConfirmOrder, // CRITICAL: Triggers Lock + Snapshot
				Allowed:    true,
				RequiresTx: true,
				Reason:     "User affirmed cart summary - confirming order",
			}
		}
	}

	// 2. Generic Affirmation
	return Action{
		Type:    ActionAck,
		Allowed: true,
		Reason:  "User agreed",
		Data: ActionData{
			Message: "Great! Let me know what you'd like to do next.",
		},
	}
}

// handleReject handles disapproval ("No", "Change it")
func (r *actionResolver) handleReject(ctx *ConversationContext, intent Intent, state OrderState) Action {
	// If user says "No" during checkout/building
	if state == StateBuilding || state == StateDraft {
		// PHASE 5: Rejected Item Memory
		// If explicit item is mentioned ("No cake"), reject it specifically.
		if intent.Entities.Product != nil {
			return Action{
				Type:       ActionRejectItem,
				Allowed:    true,
				RequiresTx: true,
				Reason:     "User explicitly rejected item - persisting memory",
				Data: ActionData{
					ProductID: intent.Entities.Product.ID,
					Message:   fmt.Sprintf("Removed expectation for %s", intent.Entities.Product.Name),
				},
			}
		}

		return Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "User rejected summary",
			Data: ActionData{
				Message: "No problem. What would you like to change? You can add, remove, or clear items.",
			},
		}
	}

	return Action{
		Type:    ActionAck,
		Allowed: true,
		Reason:  "User disagreed",
		Data: ActionData{
			Message: "Understood.",
		},
	}
}
