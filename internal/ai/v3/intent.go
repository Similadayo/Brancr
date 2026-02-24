package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// =============================================================================
// INTENT EXTRACTOR - Heuristic-First Classification
// =============================================================================

// intentExtractor implements IntentExtractor interface
type intentExtractor struct {
	Client LLMClient
	Logger *zap.Logger // PHASE-0: Added for observability
}

// NewIntentExtractor creates a new intent extractor
func NewIntentExtractor(client LLMClient) IntentExtractor {
	// PHASE-0: Logger will be set via SetLogger if available
	return &intentExtractor{Client: client, Logger: zap.NewNop()}
}

// SetLogger sets the logger for observability (PHASE-0)
func (e *intentExtractor) SetLogger(logger *zap.Logger) {
	if logger != nil {
		e.Logger = logger
	}
}

// Classify determines user intent using heuristics first, LLM fallback
// Priority order prevents bugs like "cancel order" matching "order" → add_item
func (e *intentExtractor) Classify(ctx *ConversationContext, message string) Intent {
	// PHASE-0 LOGGING: Intent Detection Boundary - Entry
	// Log raw user message for traceability
	e.Logger.Info("intent_detection_start",
		zap.String("raw_message", message),
		zap.Int("message_length", len(message)),
		zap.Int("products_available", len(ctx.Products)),
		zap.Int("services_available", len(ctx.Services)),
	)

	lower := strings.ToLower(strings.TrimSpace(message))

	// Detect emotion first (affects response style, not intent)
	emotion := e.detectEmotion(lower)

	// Context Extraction
	var lastAssistantMsg string
	if len(ctx.RecentMessages) > 0 {
		for i := len(ctx.RecentMessages) - 1; i >= 0; i-- {
			if ctx.RecentMessages[i].Role == "assistant" {
				lastAssistantMsg = strings.ToLower(ctx.RecentMessages[i].Content)
				break
			}
		}
	}

	// ==========================================================================
	// PRIORITY 1: LLM-BASED DISCOVERY (The New Default)
	// ==========================================================================
	// User Requirement: "The default should be the new one [LLM]"
	// We ask the Brain first. If it succeeds, we trust it.
	if e.Client != nil {
		llmContext := context.Background()
		llmIntent, err := e.classifyWithLLM(llmContext, message, ctx)
		// Accept if valid and confidence is decent (e.g. > 0.6)
		if err == nil && llmIntent.Confidence > 0.6 {
			llmIntent.Emotion = emotion // Preserve locally detected emotion
			return llmIntent
		}
		// If LLM fails or is unsure, fall through to Heuristics
	}

	// ==========================================================================
	// PRIORITY 2: EXACT COMMANDS (Fallback/Safety)
	// ==========================================================================

	// Cancel - MUST come before any "order" pattern
	if e.isCancel(lower) {
		return Intent{
			Type:       IntentCancel,
			Confidence: 0.95,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Help
	if e.isHelp(lower) {
		return Intent{
			Type:       IntentHelp,
			Confidence: 0.95,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Greeting
	if e.isGreeting(lower) {
		return Intent{
			Type:       IntentGreeting,
			Confidence: 0.95,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// ==========================================================================
	// PRIORITY 2: VIEW/STATUS QUERIES (no cart mutation)
	// ==========================================================================

	// Status inquiry (Priority over View Cart to avoid "my order" overlap)
	if e.isStatusInquiry(lower) {
		return Intent{
			Type:       IntentStatusInquiry,
			Confidence: 0.85,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// View cart
	if e.isViewCart(lower) {
		return Intent{
			Type:       IntentViewCart,
			Confidence: 0.90,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Payment query
	if e.isPaymentQuery(lower) {
		return Intent{
			Type:       IntentPaymentQuery,
			Confidence: 0.90,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// ==========================================================================
	// PRIORITY 3: CART MUTATIONS (require entity extraction)
	// ==========================================================================

	// Remove item
	if product, _ := e.extractRemoveItem(lower, ctx.Products); product != nil {
		return Intent{
			Type:       IntentRemoveItem,
			Confidence: 0.85,
			Entities:   Entities{Product: product},
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Clear cart
	if e.isClearCart(lower) {
		return Intent{
			Type:       IntentClearCart,
			Confidence: 0.85,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Add item - CRITICAL: only if we can extract a REAL product OR context implies update
	product, quantity := e.extractAddItem(lower, ctx.Products)
	if product != nil {
		return Intent{
			Type:       IntentAddItem,
			Confidence: 0.85,
			Entities:   Entities{Product: product, Quantity: quantity},
			Source:     "heuristic",
			Emotion:    emotion,
		}
	} else if quantity > 0 && lastAssistantMsg != "" {
		// CONTEXTUAL QUANTITY: "I said 2" -> AddItem(2, nil)
		// We only allow this if the assistant was talking about products/orders recently.
		// Check signal words in assistant msg: "order", "want", "product", "item", "many", "price"
		if strings.Contains(lastAssistantMsg, "order") || strings.Contains(lastAssistantMsg, "want") ||
			strings.Contains(lastAssistantMsg, "item") || strings.Contains(lastAssistantMsg, "many") ||
			strings.Contains(lastAssistantMsg, "price") || strings.Contains(lastAssistantMsg, "add") {
			return Intent{
				Type:       IntentAddItem,
				Confidence: 0.80,
				Entities:   Entities{Quantity: quantity}, // Product is nil, Resolver will fill it
				Source:     "heuristic_context",
				Emotion:    emotion,
			}
		}
	}

	// ==========================================================================
	// PRIORITY 4: CHECKOUT FLOW
	// ==========================================================================

	// Checkout intent
	if e.isCheckout(lower) {
		return Intent{
			Type:       IntentCheckout,
			Confidence: 0.85,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Payment confirmation
	if e.isPaymentConfirmation(lower) {
		return Intent{
			Type:       IntentConfirmPayment,
			Confidence: 0.85,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Generic Affirmation (Context Aware)
	if (lower == "yes" || lower == "yeah" || lower == "yep" || lower == "ok" || lower == "okay" || lower == "confirm" || lower == "correct" || lower == "sure") && lastAssistantMsg != "" {
		// Only affirm if assistant asked a question or sought confirmation
		if strings.Contains(lastAssistantMsg, "?") || strings.Contains(lastAssistantMsg, "confirm") || strings.Contains(lastAssistantMsg, "right") {
			return Intent{
				Type:       IntentAffirm,
				Confidence: 0.90,
				Source:     "heuristic_context",
				Emotion:    emotion,
			}
		}
	}

	// Generic Rejection (Context Aware)
	if (lower == "no" || lower == "nah" || lower == "nope" || lower == "wrong" || lower == "cancel") && lastAssistantMsg != "" {
		// Only reject if assistant asked a question
		if strings.Contains(lastAssistantMsg, "?") || strings.Contains(lastAssistantMsg, "mean") || strings.Contains(lastAssistantMsg, "want") {
			return Intent{
				Type:       IntentReject,
				Confidence: 0.90,
				Source:     "heuristic_context",
				Emotion:    emotion,
			}
		}
	}

	// ==========================================================================
	// PRIORITY 5: INQUIRIES (no cart mutation, just information)
	// ==========================================================================

	// Price inquiry
	if e.isPriceInquiry(lower) {
		return Intent{
			Type:       IntentPriceInquiry,
			Confidence: 0.80,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// Product inquiry - generic "what do you have"
	if e.isProductInquiry(lower) {
		return Intent{
			Type:       IntentProductInquiry,
			Confidence: 0.75,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// ==========================================================================
	// PRIORITY 6: NEGOTIATION (price haggling)
	// ==========================================================================

	if e.isNegotiation(lower) {
		return Intent{
			Type:       IntentNegotiation,
			Confidence: 0.85,
			Source:     "heuristic",
			Emotion:    emotion,
		}
	}

	// ==========================================================================
	// RESOLUTION: Contextual Fallback (Staff Simulation)
	// ==========================================================================

	// If we have products, default to Advisory
	if len(ctx.Products) > 0 {
		return Intent{
			Type:       IntentProductAdvisory,
			Confidence: 0.85,
			Source:     "context_resolution",
			Emotion:    emotion,
		}
	}

	// If we have services, default to Inquiry
	if len(ctx.Services) > 0 {
		return Intent{
			Type:       IntentServiceInquiry,
			Confidence: 0.85,
			Source:     "context_resolution",
			Emotion:    emotion,
		}
	}

	// Check if RoleProfile has a default
	if ctx.RoleProfile.DefaultResponse != "" {
		return Intent{
			Type:       IntentType(ctx.RoleProfile.DefaultResponse),
			Confidence: 0.75,
			Source:     "role_default",
			Emotion:    emotion,
		}
	}

	// Absolute last resort - but distinct from "Unknown"
	intent := Intent{
		Type:       IntentContextual, // "We know the context, just not the specific command"
		Confidence: 0.40,
		Source:     "fallback_contextual",
		Emotion:    emotion,
	}

	finalIntent := e.calibrateConfidence(intent, ctx)

	// PHASE-0 LOGGING: Intent Detection Boundary - Exit
	// Log detected intent, confidence, and method for observability
	e.Logger.Info("intent_detection_complete",
		zap.String("detected_intent", string(finalIntent.Type)),
		zap.Float64("confidence", finalIntent.Confidence),
		zap.String("detection_method", finalIntent.Source),
		zap.String("emotion", string(finalIntent.Emotion.Type)),
		zap.Float64("emotion_intensity", finalIntent.Emotion.Intensity),
		zap.Bool("has_product_entity", finalIntent.Entities.Product != nil),
		zap.Int("quantity", finalIntent.Entities.Quantity),
	)

	return finalIntent
}

func (e *intentExtractor) classifyWithLLM(ctx context.Context, message string, convCtx *ConversationContext) (Intent, error) {
	// Simple Prompt for Intent Classification
	prompt := fmt.Sprintf(`
You are an INTENT CLASSIFIER.
Analyze the user message and map it to one of these intents:
- add_item (if buying something)
- remove_item
- view_cart
- checkout
- cancel
- help
- greeting
- price_inquiry
- product_inquiry (what do you have?)
- status_inquiry (where is order?)
- negotiation (haggling)
- payment_query (how to pay)

Input: "%s"

Output JSON ONLY:
{
  "intent": "intent_type",
  "confidence": 0.0-1.0,
  "product_name": "optional extracted product name",
  "quantity": optional int
}
`, message)

	inputs := []LLMMessage{{Role: "system", Content: prompt}}
	raw, err := e.Client.Generate(ctx, inputs)
	if err != nil {
		return Intent{}, err
	}

	// Strip Markdown Code Blocks if present
	raw = cleanJSON(raw)

	var res struct {
		Intent      string  `json:"intent"`
		Confidence  float64 `json:"confidence"`
		ProductName string  `json:"product_name"`
		Quantity    int     `json:"quantity"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		// Log the error in a real system, here we just return error to trigger fallback
		return Intent{}, fmt.Errorf("failed to unmarshal intent json: %w | raw: %s", err, raw)
	}

	// Map Result
	out := Intent{
		Type:       IntentType(res.Intent),
		Confidence: res.Confidence,
		Source:     "llm_discovery",
	}

	// Map Entities if present
	if res.ProductName != "" {
		// Naive entity mapping, ideally we fuzzy match catalog here again
		// But for now, we pass the raw name for the Reasoner to resolve later if needed
		// OR we can try to find it in ctx.Products
		for _, p := range convCtx.Products {
			if strings.Contains(strings.ToLower(p.Name), strings.ToLower(res.ProductName)) {
				out.Entities.Product = &ProductEntity{
					ID: p.ID, Name: p.Name, Price: p.Price, Currency: p.Currency, Matched: res.ProductName,
				}
				break
			}
		}
	}
	if res.Quantity > 0 {
		out.Entities.Quantity = res.Quantity
	}

	// Default Qty 1 if product found but no qty
	if out.Entities.Product != nil && out.Entities.Quantity == 0 {
		out.Entities.Quantity = 1
	}

	return out, nil
}

// calibrateConfidence boosts confidence if we have the data to answer.
// "Data-backed confidence beats linguistic uncertainty."
func (e *intentExtractor) calibrateConfidence(intent Intent, ctx *ConversationContext) Intent {
	// If we are scientifically sure we can answer, we are confident.
	isAnswerable := false

	switch intent.Type {
	case IntentProductInquiry, IntentPriceInquiry:
		// If we have products or services, we can definitively answer "what do you have"
		if len(ctx.Products) > 0 {
			isAnswerable = true
		}

	case IntentGreeting:
		// We always have a business name or default, so we can always greet
		isAnswerable = true

	case IntentPaymentQuery:
		// If we have payment info, we can answer
		if ctx.PaymentInfo != nil {
			isAnswerable = true
		}
	}

	// Override confidence if answerable
	if isAnswerable {
		// Boost to High confidence (at least 0.9)
		if intent.Confidence < 0.9 {
			intent.Confidence = 0.95
			intent.Source = "data-backed"
		}
	}

	return intent
}

// =============================================================================
// EMOTION DETECTION
// =============================================================================

func (e *intentExtractor) detectEmotion(lower string) EmotionState {
	// Frustration signals
	frustrationPatterns := []string{
		"not working", "doesn't work", "problem", "issue", "wrong",
		"angry", "annoyed", "frustrated", "terrible", "awful", "horrible",
		"still waiting", "no response", "ignored",
	}
	for _, p := range frustrationPatterns {
		if strings.Contains(lower, p) {
			return EmotionState{Type: EmotionFrustrated, Intensity: 0.8}
		}
	}

	// Eagerness signals
	eagernessPatterns := []string{
		"can't wait", "excited", "love it", "perfect", "ready to pay",
		"let's do it", "yes please", "definitely", "absolutely",
		"take my money", "shut up and take",
	}
	for _, p := range eagernessPatterns {
		if strings.Contains(lower, p) {
			return EmotionState{Type: EmotionEager, Intensity: 0.9}
		}
	}

	// Confusion signals
	confusionPatterns := []string{
		"confused", "don't understand", "what do you mean", "huh",
		"not sure", "unclear", "which one",
	}
	for _, p := range confusionPatterns {
		if strings.Contains(lower, p) {
			return EmotionState{Type: EmotionConfused, Intensity: 0.7}
		}
	}

	// Hesitation signals
	hesitationPatterns := []string{
		"maybe", "not sure", "thinking", "let me think", "hmm",
		"i'll think about it", "need to check", "hold on",
	}
	for _, p := range hesitationPatterns {
		if strings.Contains(lower, p) {
			return EmotionState{Type: EmotionHesitant, Intensity: 0.6}
		}
	}

	// Happy signals
	happyPatterns := []string{
		"thank you", "thanks", "awesome", "great", "wonderful",
		"happy", "pleased", "satisfied",
	}
	for _, p := range happyPatterns {
		if strings.Contains(lower, p) {
			return EmotionState{Type: EmotionHappy, Intensity: 0.7}
		}
	}

	return EmotionState{Type: EmotionNeutral, Intensity: 0.5}
}

// =============================================================================
// INTENT PATTERNS
// =============================================================================

func (e *intentExtractor) isCancel(lower string) bool {
	patterns := []string{
		"cancel", "nevermind", "never mind", "forget it", "stop",
		"don't want", "changed my mind", "no longer",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isHelp(lower string) bool {
	patterns := []string{
		"help", "support", "assist", "human", "talk to someone",
		"speak to", "agent", "representative",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isGreeting(lower string) bool {
	// Comprehensive greetings: English + Nigerian/Pidgin
	greetings := []string{
		// English
		"hi", "hello", "hey", "yo", "hiya", "howdy", "hola",
		"good morning", "good afternoon", "good evening", "good day",
		"morning", "afternoon", "evening",
		"what's up", "whats up", "sup", "wassup", "wazz up", "wazzup",
		"how are you", "how do you do", "how's it going", "hows it going",

		// Nigerian Pidgin English
		"omo", "bros", "bro", "sis", "sister", "broda", "sista",
		"how far", "how body", "how you dey", "wetin dey", "wetin dey happen",
		"e dey", "e don tey", "abeg", "na you", "oya", "sha",
		"wahala", "person", "guy", "babe", "babes",

		// Yoruba greetings
		"e kaaro", "e kaasan", "e kale", "bawo ni", "se daada ni",

		// Hausa greetings
		"sannu", "ina kwana", "ina wuni",

		// Igbo greetings
		"kedu", "ndewo", "nnọọ",
	}

	lower = strings.TrimSpace(lower)

	// Check for exact or prefix matches
	for _, g := range greetings {
		if lower == g ||
			strings.HasPrefix(lower, g+" ") ||
			strings.HasPrefix(lower, g+"!") ||
			strings.HasPrefix(lower, g+",") ||
			strings.HasPrefix(lower, g+".") {
			return true
		}
	}

	// Additional checks for very short messages (likely greetings)
	if len(lower) <= 15 && (strings.Contains(lower, "hi ") || lower == "hi" ||
		strings.Contains(lower, "hey ") || lower == "hey" ||
		strings.Contains(lower, "hello") || lower == "yo") {
		return true
	}

	return false
}

func (e *intentExtractor) isViewCart(lower string) bool {
	patterns := []string{
		"my cart", "my order", "what did i order", "what's in my",
		"view cart", "show cart", "cart summary", "order summary",
		"what am i getting", "what have i ordered",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isPaymentQuery(lower string) bool {
	patterns := []string{
		"how do i pay", "payment", "bank details", "account number",
		"how to pay", "where do i send", "bank account", "transfer",
		"send money", "pay you",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isStatusInquiry(lower string) bool {
	patterns := []string{
		"where is my order", "order status", "tracking", "when will",
		"how long", "delivery time", "shipped", "dispatched",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isClearCart(lower string) bool {
	patterns := []string{
		"clear cart", "empty cart", "start over", "remove all",
		"clear everything", "delete all",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isCheckout(lower string) bool {
	patterns := []string{
		"checkout", "check out", "ready to buy", "i'm done",
		"that's all", "that is all", "proceed", "finalize",
		"complete order", "place order", "ready to pay",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isPaymentConfirmation(lower string) bool {
	patterns := []string{
		"i've paid", "i paid", "payment sent", "transferred",
		"sent the money", "done paying", "just paid",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isPriceInquiry(lower string) bool {
	patterns := []string{
		"how much", "price", "cost", "rate",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isNegotiation(lower string) bool {
	// Negotiation patterns in English and Nigerian Pidgin
	patterns := []string{
		// English
		"too expensive", "too much", "too high", "very expensive",
		"can you reduce", "reduce price", "lower price", "discount",
		"best price", "last price", "final price", "good price",
		"what's your best", "give me discount", "any discount",

		// Nigerian Pidgin
		"abeg reduce", "e too cost", "e too much", "make you reduce",
		"last price na how much", "your last", "wetin be your last",
		"e dey expensive", "e cost die", "na wa o", "too plenty",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (e *intentExtractor) isProductInquiry(lower string) bool {
	// 1. Check for broad "what do you have" patterns with flexibility
	// Matches: "what do you have", "what do you guys have", "what do u sell", "wetin you get"
	if (strings.Contains(lower, "what") || strings.Contains(lower, "wetin")) &&
		(strings.Contains(lower, "have") || strings.Contains(lower, "sell") || strings.Contains(lower, "offer") || strings.Contains(lower, "get")) {
		// Exclude "what do you mean" or "what do i get" (context dependent)
		if !strings.Contains(lower, "mean") && !strings.Contains(lower, "get if") {
			return true
		}
	}

	patterns := []string{
		"what's available", "show me", "menu",
		"products", "catalog", "options",
		"i want to order", "want to buy",
		"looking for", "do you have", "do you sell", "don't you sell",
		"make a new order", "make an order", "place an order", "new order",
		"available items", "list items", "see items",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// =============================================================================
// ENTITY EXTRACTION
// =============================================================================

// Quantity patterns: "2 shawarmas", "two chicken", "a burger", "one pizza"
var quantityRegex = regexp.MustCompile(`(?i)^(\d+|one|two|three|four|five|six|seven|eight|nine|ten|a|an)\s+`)

func (e *intentExtractor) extractAddItem(lower string, products []Product) (*ProductEntity, int) {
	// Must have purchase intent signals
	purchaseIntents := []string{
		"i want", "i need", "give me", "get me", "add", "order",
		"i'll take", "i'd like", "i would like", "can i get",
		"please add", "put", "include",
	}

	hasPurchaseIntent := false
	for _, p := range purchaseIntents {
		if strings.Contains(lower, p) {
			hasPurchaseIntent = true
			break
		}
	}

	// Also check if message starts with a quantity (e.g., "2 shawarmas")
	if quantityRegex.MatchString(lower) {
		hasPurchaseIntent = true
	}

	if !hasPurchaseIntent {
		return nil, 0
	}

	// Try to match against catalog products
	for _, product := range products {
		productLower := strings.ToLower(product.Name)

		// Check if product name appears in message
		if strings.Contains(lower, productLower) {
			quantity := e.extractQuantity(lower)
			return &ProductEntity{
				ID:       product.ID,
				Name:     product.Name,
				Price:    product.Price,
				Currency: product.Currency,
				Matched:  product.Name,
			}, quantity
		}

		// Also check for partial matches (e.g., "shawarma" matching "Chicken Shawarma")
		words := strings.Fields(productLower)
		for _, word := range words {
			if len(word) > 3 && strings.Contains(lower, word) {
				quantity := e.extractQuantity(lower)
				return &ProductEntity{
					ID:       product.ID,
					Name:     product.Name,
					Price:    product.Price,
					Currency: product.Currency,
					Matched:  word,
				}, quantity
			}
		}
	}

	return nil, 0
}

func (e *intentExtractor) extractRemoveItem(lower string, products []Product) (*ProductEntity, int) {
	removeIntents := []string{
		"remove", "delete", "take out", "take off", "no more",
		"don't want", "cancel the",
	}

	hasRemoveIntent := false
	for _, p := range removeIntents {
		if strings.Contains(lower, p) {
			hasRemoveIntent = true
			break
		}
	}

	if !hasRemoveIntent {
		return nil, 0
	}

	// Try to match against catalog products
	for _, product := range products {
		productLower := strings.ToLower(product.Name)

		// Exact match
		if strings.Contains(lower, productLower) {
			return &ProductEntity{
				ID:       product.ID,
				Name:     product.Name,
				Price:    product.Price,
				Currency: product.Currency,
				Matched:  product.Name,
			}, 1
		}

		// Partial match (words)
		words := strings.Fields(productLower)
		for _, word := range words {
			if len(word) > 3 && strings.Contains(lower, word) {
				return &ProductEntity{
					ID:       product.ID,
					Name:     product.Name,
					Price:    product.Price,
					Currency: product.Currency,
					Matched:  word,
				}, 1
			}
		}
	}

	return nil, 0
}

// cleanJSON removes markdown code blocks from LLM output
func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "```json", "")
	raw = strings.ReplaceAll(raw, "```", "")
	return strings.TrimSpace(raw)
}

func (e *intentExtractor) extractQuantity(text string) int {
	// Number words to digits
	wordToNum := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		"a": 1, "an": 1,
	}

	// Try numeric first
	numRegex := regexp.MustCompile(`(\d+)`)
	if matches := numRegex.FindStringSubmatch(text); len(matches) > 1 {
		if num, err := strconv.Atoi(matches[1]); err == nil && num > 0 && num < 100 {
			return num
		}
	}

	// Try word numbers
	lower := strings.ToLower(text)
	for word, num := range wordToNum {
		if strings.Contains(lower, word+" ") || strings.HasPrefix(lower, word+" ") {
			return num
		}
	}

	// Default to 1
	return 1
}
