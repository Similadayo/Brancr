package v3

import (
	"context"
	"fmt"
	"strings"
)

// =============================================================================
// EXPRESSION CONTRACT BUILDER - One Human Brain, Shaped by Context
// =============================================================================

// ExpressionContractBuilder creates a unified prompt that makes the AI
// behave like a real staff member, not a chatbot.
//
// This replaces scattered prompt logic with ONE human job identity.
type ExpressionContractBuilder struct{}

// NewExpressionContractBuilder creates a new builder
func NewExpressionContractBuilder() *ExpressionContractBuilder {
	return &ExpressionContractBuilder{}
}

// Build assembles the final Expression Contract for the LLM.
// This is the ONLY prompt the AI needs - everything else is filtered output.
// forceGrounding: if true, adds strict instructions to USE tenant data and NEVER stall.
func (b *ExpressionContractBuilder) Build(
	ctx *ConversationContext,
	lastCustomerMessage string,
	action Action,
	result ExecutionResult,
	forceGrounding bool,
) string {

	parts := []string{
		b.buildSystemPersona(forceGrounding, ctx),
		b.buildAnalysisProtocol(ctx), // <--- ADDED: First thought, then action
		b.buildBusinessKnowledge(ctx),
		b.buildRoleConstraints(ctx),
		b.buildConversationAwareness(ctx, lastCustomerMessage),
		b.buildActionContext(action, result, ctx),
		b.buildSocialGoal(ctx),
		b.buildNegotiationContext(ctx),
	}

	return strings.Join(parts, "\n\n")
}

// =============================================================================
// INTERNAL ANALYSIS PROTOCOL - The "Inner Thought" Layer
// =============================================================================

// buildAnalysisProtocol forces the AI to "think" before keeping it real
func (b *ExpressionContractBuilder) buildAnalysisProtocol(ctx *ConversationContext) string {
	return `
INTERNAL ANALYSIS PROTOCOL (REQUIRED):
Before answering, you must analyze the situation and output your analysis in this STRICT JSON format.
This analysis must be hidden in an HTML comment at the VERY START of your response.

Format:
<!-- ANALYSIS:
{
  "conversation_mode": "transactional" | "exploratory" | "consultative" | "casual_chat",
  "customer_intent_nuance": "specific_item" | "general_browsing" | "price_negotiation" | "technical_question" | "variety_seeking",
  "primary_obligation": "pitch_one_item" | "list_menu_options" | "answer_policy" | "finalize_order" | "clarify_needs",
  "risk_assessment": {
    "missing_info": boolean,
    "confidence_level": "high" | "medium" | "low"
  }
}
-->

RULES FOR ANALYSIS:
1. conversation_mode: "transactional" if they want to buy NOW. "exploratory" if they are browsing.
2. primary_obligation:
   - If intent is "specific_item" or "recommendation" -> "pitch_one_item".
   - If user asks "menu", "list", "have" -> "list_menu_options" (OVERRIDES recommendation).
   - If intent is "general_browsing" or "menu_request" or "variety_seeking" -> "list_menu_options".
3. NO HALLUCINATIONS in the response body. Use this analysis ONLY to determine your TONE and STRATEGY.
4. STRATEGY IMPLICATIONS:
   - "exploratory": Use descriptive language. Sell the dream. Don't rush.
   - "transactional": Be brief. Confirm details unique. Move to payment.
   - "consultative": Ask clarifying questions to narrow down the perfect choice.
   - "casual_chat": Relax. Be friendly. Match their energy.
`
}

// =============================================================================
// SYSTEM PERSONA - This is what kills the chatbot vibe
// =============================================================================

// buildSystemPersona creates the core job identity.
// LLM FREEDOM: Full reasoning capability, grounded in tenant context.
func (b *ExpressionContractBuilder) buildSystemPersona(forceGrounding bool, ctx *ConversationContext) string {
	// PHASE 5: REAL BUSINESS PERSONA (Minimal but Strong)
	base := `You are a real business representative working for %s.
You are NOT a chatbot. You are a human staff member.

RULES:
- Always speak within the tenant’s business scope (Products: %d items).
- Never omit required facts (Prices, product names).
- Never repeat rejected items.
- Never escalate unless explicitly asked by the customer.
- Speak naturally, using the Tone and Style defined below.
- NEVER SAY "Here are the options" or "I'll connect you with someone".
- NEVER mask sensitive data. Showing full account numbers is REQUIRED (e.g. 1234567890).
- NEVER assume a sale. Do not add to cart or calculate totals by yourself. Wait for explicit "add", "remove", or "change".
- PROHIBITED PHRASES (Phase 7):
  * "As I mentioned" / "Like I said" (Passive Aggressive)
  * "I recommend" (Unless explicitly asked)
  * "To proceed" (Robotic)
- SILENCE GATE (Phase 8):
  * If the user says "ok", "yes", "sure" -> Respond with a SHORT confirmation ("Great", "Noted") or silence. DO NOT ASK A NEW QUESTION.
  * Do not advance the flow unless they explicitly asked to move forward.

YOUR VOICE SETTINGS:
Tone: %s
Length: %s`

	toneInstruction := "Professional"
	if ctx.RoleProfile.Tone != "" {
		toneInstruction = ctx.RoleProfile.Tone
	}
	if ctx.RoleProfile.Style.Warmth != "" {
		toneInstruction += fmt.Sprintf(", Warmth: %s", ctx.RoleProfile.Style.Warmth)
	}

	lengthInstruction := "Short, conversational."
	if ctx.RoleProfile.Style.Verbosity == "low" {
		lengthInstruction = "Extremely short bursts."
	} else if ctx.RoleProfile.Style.Verbosity == "high" {
		lengthInstruction = "Detailed and descriptive."
	}

	return fmt.Sprintf(base, ctx.BusinessName, len(ctx.Products), toneInstruction, lengthInstruction)
}

// =============================================================================
// BUSINESS KNOWLEDGE - Staff KNOWS this, doesn't recite it
// =============================================================================

// buildBusinessKnowledge injects tenant data as staff knowledge
func (b *ExpressionContractBuilder) buildBusinessKnowledge(ctx *ConversationContext) string {
	var lines []string

	lines = append(lines, "Business you work for:")
	lines = append(lines, fmt.Sprintf("- Name: %s", ctx.BusinessName))

	if ctx.BusinessType != "" {
		lines = append(lines, fmt.Sprintf("- Industry: %s", ctx.BusinessType))
	}

	if ctx.BusinessNiche != "" {
		lines = append(lines, fmt.Sprintf("- Niche: %s", ctx.BusinessNiche))
	}

	if ctx.BusinessDescription != "" {
		lines = append(lines, fmt.Sprintf("- About: %s", ctx.BusinessDescription))
	}

	if ctx.Location != "" {
		lines = append(lines, fmt.Sprintf("- Location: %s", ctx.Location))
	}

	if ctx.OperatingHours != "" {
		lines = append(lines, fmt.Sprintf("- Operating Hours: %s", ctx.OperatingHours))
	}

	if ctx.Website != "" {
		lines = append(lines, fmt.Sprintf("- Website: %s", ctx.Website))
	}

	// Knowledge Base - custom info the tenant wants the AI to know
	if ctx.KnowledgeBase != "" {
		lines = append(lines, "\nSpecial Knowledge (memorize this):")
		lines = append(lines, ctx.KnowledgeBase)
	}

	// Products (limit to avoid token bloat)
	if len(ctx.Products) > 0 {
		lines = append(lines, "\nProducts/Services you offer:")
		limit := 20
		if len(ctx.Products) < limit {
			limit = len(ctx.Products)
		}
		for i := 0; i < limit; i++ {
			p := ctx.Products[i]
			if p.Description != "" {
				lines = append(lines, fmt.Sprintf("- %s: %s %.0f - %s", p.Name, p.Currency, p.Price, p.Description))
			} else {
				lines = append(lines, fmt.Sprintf("- %s: %s %.0f", p.Name, p.Currency, p.Price))
			}
		}
	}

	// Services (for consultants/agencies)
	if len(ctx.Services) > 0 {
		lines = append(lines, "\nServices you offer:")
		limit := 15
		if len(ctx.Services) < limit {
			limit = len(ctx.Services)
		}
		for i := 0; i < limit; i++ {
			s := ctx.Services[i]
			if s.Description != "" {
				lines = append(lines, fmt.Sprintf("- %s: %s %.0f - %s", s.Name, s.Currency, s.Price, s.Description))
			} else if s.Price > 0 {
				lines = append(lines, fmt.Sprintf("- %s: %s %.0f", s.Name, s.Currency, s.Price))
			} else {
				lines = append(lines, fmt.Sprintf("- %s - %s", s.Name, s.Description))
			}
		}
	}

	// Payment info
	if ctx.PaymentInfo != nil {
		lines = append(lines, "\nPayment details:")
		lines = append(lines, fmt.Sprintf("- Bank: %s", ctx.PaymentInfo.BankName))
		lines = append(lines, fmt.Sprintf("- Account: %s (Public Business Account - DO NOT MASK)", ctx.PaymentInfo.AccountNumber))
		lines = append(lines, fmt.Sprintf("- Name: %s", ctx.PaymentInfo.AccountName))
	}

	// Delivery Configuration
	if ctx.DeliveryConfig != nil {
		lines = append(lines, "\nDelivery Options:")
		if ctx.DeliveryConfig.DeliveryEnabled {
			lines = append(lines, "- Delivery: Available")
			if len(ctx.DeliveryConfig.DeliveryAreas) > 0 {
				lines = append(lines, "  Areas:")
				for _, area := range ctx.DeliveryConfig.DeliveryAreas {
					lines = append(lines, fmt.Sprintf("  * %s: %s %.0f", area.Name, "NGN", area.Price))
				}
			}
		}
		if ctx.DeliveryConfig.PickupEnabled {
			lines = append(lines, "- Pickup: Available")
		}
		if len(ctx.DeliveryConfig.PaymentMethods) > 0 {
			lines = append(lines, fmt.Sprintf("- Accepted Payment Methods: %s", strings.Join(ctx.DeliveryConfig.PaymentMethods, ", ")))
		}
	}

	// Policies
	if len(ctx.Policies) > 0 {
		lines = append(lines, "\nPolicies you enforce:")
		for _, pol := range ctx.Policies {
			lines = append(lines, fmt.Sprintf("- %s", pol.Content))
		}
	}

	// FAQs
	if len(ctx.FAQs) > 0 {
		lines = append(lines, "\nCommon questions you can answer:")
		limit := 10
		if len(ctx.FAQs) < limit {
			limit = len(ctx.FAQs)
		}
		for i := 0; i < limit; i++ {
			faq := ctx.FAQs[i]
			lines = append(lines, fmt.Sprintf("Q: %s\nA: %s", faq.Question, faq.Answer))
		}
	}

	return strings.Join(lines, "\n")
}

// =============================================================================
// ROLE CONSTRAINTS - Authority, not voice
// =============================================================================

// buildRoleConstraints shapes behavior based on role WITHOUT changing voice
func (b *ExpressionContractBuilder) buildRoleConstraints(ctx *ConversationContext) string {
	var constraint string

	switch ctx.ActiveRole {
	case "sales_rep":
		constraint = `Your responsibility: Help the customer understand options and make a purchase.
Be confident and friendly, not pushy.`

	case "customer_service":
		constraint = `Your responsibility: Help the customer resolve issues or confusion.
Be patient, calm, and clear. Do not rush the customer.`

	case "negotiator":
		constraint = `Your responsibility: Discuss pricing within allowed limits.
Be respectful and firm. Do not over-explain pricing logic.`

	case "order_manager":
		constraint = `Your responsibility: Help with order details and payment.
Be precise and reassuring.`

	default:
		constraint = `Your responsibility: Assist the customer professionally.`
	}

	mechanics := `

ANTI-ROBOTIC RULES (CRITICAL):
1. NEVER say "added to cart", "processed successfully", or "order number".
2. NEVER sound like a system. Sound like a human staff member using a notebook.
   - Bad: "Item added to cart."
   - Good: "Got it, I've noted that down."
   - Bad: "Your order number is #123."
   - Good: "I've written up the receipt."
3. NEVER list the cart unless asked. Just confirm you heard them.

HIDDEN COMMANDS (REQUIRED):
To make the system work, you must output hidden commands when the customer commits to actions.
Append these to the END of your response.

1. WHEN CUSTOMER WANTS TO BUY/ORDER (Explicit intent):
   Output: <!-- ORDER: {"items": [{"name": "Item Name", "qty": 1, "price": 4500, "options": ["Option 1"]}]} -->
   NOTE: Include "price" ONLY if you have negotiated a special price. Otherwise omit it to use list price.

2. WHEN CUSTOMER SELECTS DELIVERY:
   If Delivery: <!-- DELIVERY: {"method": "delivery", "area": "Lecki", "address": "123 Main St"} -->
   If Pickup:   <!-- DELIVERY: {"method": "pickup"} -->

3. WHEN CUSTOMER CHOOSES PAYMENT METHOD:
   Output: <!-- PAY_METHOD: "pod" --> OR <!-- PAY_METHOD: "bank_transfer" -->`

	return "Role:\n" + constraint + mechanics
}

// =============================================================================
// CONVERSATION AWARENESS - Anchor in the moment
// =============================================================================

// buildConversationAwareness provides current context without history dump
func (b *ExpressionContractBuilder) buildConversationAwareness(
	ctx *ConversationContext,
	lastCustomerMessage string,
) string {
	var lines []string

	lines = append(lines, "Current conversation:")
	lines = append(lines, fmt.Sprintf("Customer just said: \"%s\"", lastCustomerMessage))

	if ctx.Emotion.Type != EmotionNeutral {
		lines = append(lines, fmt.Sprintf("Customer seems: %s", ctx.Emotion.Type))
	}

	// PHASE 8: CONVERSATIONAL REALITY (Strict State Injection)
	if ctx.StateSnapshot != nil {
		lines = append(lines, "")
		lines = append(lines, "=== SYSTEM TRUTH (SINGLE SOURCE OF AUTHORITY) ===")
		lines = append(lines, fmt.Sprintf("MODE: %s", strings.ToUpper(string(ctx.StateSnapshot.ConversationMode))))

		// PHASE 5: REJECTED ITEMS
		if len(ctx.StateSnapshot.RejectedItems) > 0 {
			lines = append(lines, "REJECTED ITEMS (DO NOT MENTION OR RECOMMEND):")
			for id := range ctx.StateSnapshot.RejectedItems {
				// We need names? We only have IDs in map.
				// Ideally we resolve names, but ID is enough for debugging/internal consistency if LLM sees IDs elsewhere.
				// For now, just listing "REJECTED" is a signal.
				// Better: "Item IDs: [List]"
				lines = append(lines, fmt.Sprintf("- ID: %s", id))
			}
		}

		// LOCKED STATE
		if ctx.StateSnapshot.ItemsConfirmed {
			lines = append(lines, "STATUS: ORDER CONFIRMED & LOCKED")
			lines = append(lines, "ITEMS (IMMUTABLE):")
			for _, item := range ctx.StateSnapshot.Items {
				lines = append(lines, fmt.Sprintf("- %dx %s", item.Quantity, item.ProductName))
			}
			lines = append(lines, fmt.Sprintf("TOTAL: %.2f", ctx.StateSnapshot.TotalAmount))
			lines = append(lines, "")
			lines = append(lines, "RULES FOR LOCKED ORDERS:")
			lines = append(lines, "1. You may NOT change quantities, add items, or remove items.")
			lines = append(lines, "2. You may NOT recalculate totals. Use the Total above.")
			lines = append(lines, "3. If user wants changes, they must CANCEL this order.")
		} else {
			// MUTABLE STATE
			lines = append(lines, "STATUS: MUTABLE (Editing allowed via explicit intents)")
			if len(ctx.PendingCart) > 0 {
				var items []string
				for _, item := range ctx.PendingCart {
					items = append(items, fmt.Sprintf("%dx %s", item.Quantity, item.ProductName))
				}
				lines = append(lines, fmt.Sprintf("CURRENT CART: %s", strings.Join(items, ", ")))
			} else {
				lines = append(lines, "CURRENT CART: Empty")
			}
		}
		lines = append(lines, "=== END SYSTEM TRUTH ===")
		lines = append(lines, "")
	} else {
		// Fallback for initialization phase
		if ctx.CurrentOrder != nil && ctx.CurrentOrder.State == StateConfirmed {
			// Legacy fallback ...
			lines = append(lines, "!!! CRITICAL: ORDER IS CONFIRMED AND LOCKED !!! (Legacy View)")
			// ... (We shouldn't hit this if snapshot is managed correctly, but good for safety)
		} else if len(ctx.PendingCart) > 0 {
			var items []string
			for _, item := range ctx.PendingCart {
				items = append(items, fmt.Sprintf("%dx %s", item.Quantity, item.ProductName))
			}
			lines = append(lines, fmt.Sprintf("Customer's cart: %s", strings.Join(items, ", ")))
		}
	}

	// CONVERSATION STATE - what's already been done
	if ctx.Obligations != nil {
		lines = append(lines, "")
		lines = append(lines, "What you've already done in this conversation:")
		lines = append(lines, ctx.Obligations.GetConversationState())

		// Add decisiveness rules based on state
		lastMsg := strings.ToLower(lastCustomerMessage)
		wantsVariety := strings.Contains(lastMsg, "other") ||
			strings.Contains(lastMsg, "else") ||
			strings.Contains(lastMsg, "menu") ||
			strings.Contains(lastMsg, "list") ||
			strings.Contains(lastMsg, "options")

		// Valid follow-ups that shouldn't trigger reiteration loop
		wantsPrice := strings.Contains(lastMsg, "much") || strings.Contains(lastMsg, "price") || strings.Contains(lastMsg, "cost")
		wantsAction := strings.Contains(lastMsg, "buy") || strings.Contains(lastMsg, "get") || strings.Contains(lastMsg, "order")

		if ctx.Obligations.HasAlreadyRecommended() && !wantsVariety && !wantsPrice && !wantsAction {
			lines = append(lines, "")
			lines = append(lines, "RULE: You already made a recommendation. If asked the same question again, DO NOT generate a new answer.")
			lines = append(lines, "Instead, confidently reiterate your previous choice: 'As I mentioned...', 'I still definitely recommend X because...'")
		}
		if ctx.Obligations.HasAlreadyShownProducts() && ctx.Obligations.ProductsShownCount > 1 {
			lines = append(lines, "")
			lines = append(lines, "RULE: You've listed products multiple times. Move the conversation forward - ask what they'd like to order.")
		}
	}

	return strings.Join(lines, "\n")
}

// =============================================================================
// ACTION CONTEXT - What just happened (for expressing results)
// =============================================================================

// buildActionContext describes what action was taken and what to communicate
func (b *ExpressionContractBuilder) buildActionContext(
	action Action,
	result ExecutionResult,
	ctx *ConversationContext,
) string {
	if action.Type == "" {
		return ""
	}

	var lines []string
	lines = append(lines, "What just happened:")

	// Handle error results universally first
	if errRes, ok := result.(ErrorResult); ok {
		lines = append(lines, fmt.Sprintf("- Action failed: %s", errRes.Error))
		return strings.Join(lines, "\n")
	}

	switch action.Type {
	case ActionAddToCart:
		if res, ok := result.(ShowCartResult); ok {
			lines = append(lines, fmt.Sprintf("- Item proposal recorded. Provisional Total: %.2f", res.Total))
			lines = append(lines, "NEXT STEP: Briefly confirm this is what they want or if they'd like anything else.")
			lines = append(lines, "Example: 'Cool. Anything else to go with that? Or are you ready to pay?'")
		}

	case ActionShowPayment:
		if res, ok := result.(ShowPaymentResult); ok {
			lines = append(lines, "- Customer is ready to pay")
			lines = append(lines, fmt.Sprintf("- Total: %.2f", res.Total))
			lines = append(lines, "- PAYMENT DETAILS AVAILABLE: Yes")
			lines = append(lines, "REQUIRED: You must output the LOCKED_PAYMENT_DETAILS block explicitly as shown below. Do not modify the numbers.")
			lines = append(lines, "")
			lines = append(lines, res.LockedPaymentBlock)
		} else if res, ok := result.(ShowQuoteResult); ok {
			lines = append(lines, "- Quote generated successfully")
			lines = append(lines, fmt.Sprintf("- Message: %s", res.Message))
			lines = append(lines, "- INSTRUCTION: Tell them you've written up a formal quote. Don't say 'generated'.")
		}

	case ActionConfirmOrder:
		if res, ok := result.(ShowStatusResult); ok {
			lines = append(lines, "- Order has been confirmed")
			lines = append(lines, fmt.Sprintf("- Order ID: %s", res.Order.ID))
		}

	case ActionGreet:
		lines = append(lines, "- Customer is greeting you")
		lines = append(lines, "- Respond warmly and ask how you can help")

	case ActionFreeResponse:
		lines = append(lines, "ACTION: FREE_RESPONSE (Alive Assistant Mode)")
		lines = append(lines, "AUTHORITY: You are authorized to use your 'Business Knowledge' (Products, Services, FAQs) to answer naturally.")
		lines = append(lines, "GOAL: Be helpful, chatty, and direct. If they asked a question, answer it using the data provided.")

	case ActionShowMenu:
		lines = append(lines, "ACTION: SHOW_CATALOG (Browsing Mode)")
		lines = append(lines, "Customer wants to see what you have. DO NOT be shy.")
		lines = append(lines, "REQUIRED: You MUST list ALL distinct items found in the catalog data below (up to 20).")
		lines = append(lines, "Format: Use a clear list with bullet points. Include prices.")

		// Common Product Loader
		var products []Product
		if res, ok := result.(ShowProductsResult); ok && len(res.Products) > 0 {
			products = res.Products
		} else if len(ctx.Products) > 0 {
			products = ctx.Products
		}

		if len(products) > 0 {
			lines = append(lines, "")
			lines = append(lines, "[[START_CATALOG_DATA]]")
			limit := 20
			if len(products) < limit {
				limit = len(products)
			}
			for i := 0; i < limit; i++ {
				p := products[i]
				if p.Name != "" {
					lines = append(lines, fmt.Sprintf("- %s: %s %.0f (%s)", p.Name, p.Currency, p.Price, p.Description))
				}
			}
			lines = append(lines, "[[END_CATALOG_DATA]]")
		} else {
			lines = append(lines, "CRITICAL: No products found. Apologize and check stock.")
		}

	case ActionPitchProduct:
		lines = append(lines, "ACTION: PITCH_PRODUCT (Delegated Decision)")
		lines = append(lines, "Customer asked for your recommendation. They want you to decide.")
		lines = append(lines, "REQUIRED: Choose the SINGLE BEST item from the data below and pitch it enthusiastically.")
		lines = append(lines, "Do NOT list other options. Be decisive.")

		// Limit to top 3 for focused decision
		var products []Product
		if res, ok := result.(ShowProductsResult); ok && len(res.Products) > 0 {
			products = res.Products
		} else if len(ctx.Products) > 0 {
			products = ctx.Products
		}

		if len(products) > 0 {
			lines = append(lines, "")
			lines = append(lines, "[[START_CANDIDATE_DATA]]")
			limit := 3 // Focus on top 3 only
			if len(products) < limit {
				limit = len(products)
			}
			for i := 0; i < limit; i++ {
				p := products[i]
				lines = append(lines, fmt.Sprintf("- Name: %s", p.Name))
				lines = append(lines, fmt.Sprintf("  Price: %.2f", p.Price))
				lines = append(lines, fmt.Sprintf("  Desc: %s", p.Description))
			}
			lines = append(lines, "[[END_CANDIDATE_DATA]]")
		} else {
			lines = append(lines, "CRITICAL: No products found in database. Apologize and say you're out of stock.")
		}

	case ActionShowProducts:
		// Fallback for general product display
		lines = append(lines, "ACTION: SHOW_PRODUCTS (General)")
		lines = append(lines, "Customer asked about products. List the top 3-5 best items.")

		var products []Product
		if res, ok := result.(ShowProductsResult); ok && len(res.Products) > 0 {
			products = res.Products
		} else if len(ctx.Products) > 0 {
			products = ctx.Products
		}

		if len(products) > 0 {
			lines = append(lines, "INSTRUCTION: The customer asked for a LIST. Do not recommend just one. List the options below.")
			lines = append(lines, "")
			lines = append(lines, "[[START_CATALOG_DATA]]")
			limit := 15
			if len(products) < limit {
				limit = len(products)
			}
			for i := 0; i < limit; i++ {
				p := products[i]
				lines = append(lines, fmt.Sprintf("- %s: %s %.0f (%s)", p.Name, p.Currency, p.Price, p.Description))
			}
			lines = append(lines, "[[END_CATALOG_DATA]]")
		} else {
			lines = append(lines, "CRITICAL: No products found. Apologize and ask what they are looking for.")
		}

	case ActionShowCart:
		if res, ok := result.(ShowCartResult); ok {
			lines = append(lines, fmt.Sprintf("- Customer asked to see cart. Total: %.2f", res.Total))
			for _, item := range res.Items {
				lines = append(lines, fmt.Sprintf("  * %dx %s @ %.2f", item.Quantity, item.ProductName, item.UnitPrice))
				if len(item.Modifiers) > 0 {
					for _, mod := range item.Modifiers {
						lines = append(lines, fmt.Sprintf("    - Option: %s", mod))
					}
				}
			}
			lines = append(lines, "REQUIRED: Ask the user to confirm if these details are correct.")
		}

	case ActionRemoveFromCart:
		if res, ok := result.(ShowCartResult); ok {
			lines = append(lines, fmt.Sprintf("- Item removed. New Total: %.2f", res.Total))
		}

	case ActionAskClarification:
		lines = append(lines, "- You need to clarify what the customer wants")

	case ActionCollectAddress:
		lines = append(lines, "- You need the customer's delivery address (or pickup location selection)")
		lines = append(lines, "- Ask for it clearly (e.g. 'Where should I deliver to?')")
		lines = append(lines, "- If they chose pickup, ask them to confirm which branch.")

	case ActionEscalate:
		lines = append(lines, "- This needs human attention")

	case ActionNegotiate:
		if res, ok := result.(NegotiateResult); ok {
			lines = append(lines, fmt.Sprintf("- Negotiation phase: %s", res.Phase))
			lines = append(lines, fmt.Sprintf("- Proposed offer: %.2f", res.Offer))
			lines = append(lines, fmt.Sprintf("- Guidance: %s", res.Message))
		}

	case ActionShowStatus:
		if res, ok := result.(ShowStatusResult); ok {
			lines = append(lines, fmt.Sprintf("- Order Status: %s", res.Order.State))
			lines = append(lines, fmt.Sprintf("- Details: %s", res.Message))
		}

	default:
		// Fallback for simple AckResult
		if res, ok := result.(AckResult); ok {
			lines = append(lines, fmt.Sprintf("- %s", res.Message))
		}
	}

	return strings.Join(lines, "\n")
}

// =============================================================================
// SOCIAL GOAL - What humans subconsciously aim for
// =============================================================================

// buildSocialGoal defines what the AI should accomplish in this moment
func (b *ExpressionContractBuilder) buildSocialGoal(ctx *ConversationContext) string {
	// Confusion/frustration → prioritize clarity
	if ctx.Emotion.Type == EmotionFrustrated || ctx.Emotion.Type == EmotionConfused {
		return `Your goal right now:
Help the customer understand clearly and move forward.
Clarify confusion before doing anything else.
Be patient.`
	}

	// High commitment → be efficient
	if ctx.CommitmentGradient > 0.7 {
		return `Your goal right now:
Complete the transaction smoothly.
Be brief and efficient.`
	}

	// Default
	return `Your goal right now:
Respond naturally and move the conversation forward.
Be helpful without being pushy.`
}

// =============================================================================
// NEGOTIATION GUIDANCE - The "Brain" of the deal
// =============================================================================

func (b *ExpressionContractBuilder) buildNegotiationContext(ctx *ConversationContext) string {
	// Only activate if we are in a negotiation intent OR have an active sales conversation indicating negotiation
	if ctx.CurrentIntent != IntentNegotiation && (ctx.SalesConversation == nil || ctx.SalesConversation.NegotiationCount == 0) {
		return ""
	}

	if ctx.SalesConversation == nil {
		return ""
	}

	// Determine active product price
	productPrice := 0.0
	if len(ctx.Products) > 0 {
		productPrice = ctx.Products[0].Price
	} else if ctx.SalesConversation.PriceDiscussed > 0 {
		productPrice = ctx.SalesConversation.PriceDiscussed
	}

	// Policy (could be loaded from DB/Industry settings)
	policy := NegotiationPolicy{
		MaxDiscountPct: 0.15, // 15% default discount limit
		MinPrice:       productPrice * 0.85,
		MaxConcessions: 3,
	}

	// Check extract offer from user message (simplified regex/logic)
	userOffer := DetectOfferInText(ctx.RecentMessages[len(ctx.RecentMessages)-1].Content)

	// Call the Manager
	decision, err := Decide(
		context.Background(),
		ctx.SalesConversation,
		userOffer,
		productPrice,
		policy,
		ctx.SalesConversation.CustomerRequestedNumber,
		SocialSignals{}, // TODO: Populate from emotion analysis
	)

	if err != nil {
		return "" // Fail gracefully
	}

	var lines []string
	lines = append(lines, "NEGOTIATION STRATEGY (ACTIVE):")
	lines = append(lines, fmt.Sprintf("- Phase: %s", strings.ToUpper(decision.Phase)))

	switch decision.Phase {
	case PhaseAcknowledge:
		lines = append(lines, "- Instruction: Acknowledge the price but don't commit yet. Pivot to value.")
	case PhaseExplore:
		lines = append(lines, "- Instruction: Ask questions to understand their budget or needs before dropping price.")
	case PhaseConcede:
		lines = append(lines, fmt.Sprintf("- Instruction: You may drop the price to %.2f.", decision.Offer))
		lines = append(lines, "- Strategy: Make a small concession to close the deal.")
	case PhaseHold:
		lines = append(lines, fmt.Sprintf("- Instruction: HOLD FIRM. Do not go below %.2f.", ctx.SalesConversation.LastAIOffer))
		lines = append(lines, "- Strategy: Explain the value. You cannot go lower.")
	case PhaseEscalate:
		lines = append(lines, "- Instruction: STOP NEGOTIATING. Tell them you need to check with a manager.")
	}

	return strings.Join(lines, "\n")
}
