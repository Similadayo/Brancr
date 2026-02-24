package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt" // 🆕 Added import
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/similadayo/brancr/internal/ai_v4/catalog"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/decision"
	"github.com/similadayo/brancr/internal/ai_v4/emotion"
	"github.com/similadayo/brancr/internal/ai_v4/executor"
	"github.com/similadayo/brancr/internal/fulfillment"

	// "github.com/similadayo/brancr/internal/ai_v4/intent" // REMOVED: Intent is no longer a driver
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/loader"
	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/order"
	"github.com/similadayo/brancr/internal/ai_v4/payment"
	"github.com/similadayo/brancr/internal/ai_v4/python"
	"github.com/similadayo/brancr/internal/ai_v4/role"
	"github.com/similadayo/brancr/internal/ai_v4/semantic"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/social"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/observability"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// Package-level constants for consistent behavior
const (
	// BuyingConfidenceThreshold: Customer must have >= 70% confidence to trigger ORDER MODE
	buyingConfidenceThreshold = 0.7
)

var unavailableReplySubjectPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*(.+?)\s+is not in our catalog right now`),
	regexp.MustCompile(`(?i)^\s*(?:sorry,\s*)?(.+?)\s+is out of stock`),
	regexp.MustCompile(`(?i)^\s*(.+?)\s+is currently out of stock`),
	regexp.MustCompile(`(?i)^\s*(.+?)\s+is currently unavailable`),
}

// RunnerDeps bundles all dependencies for the FlowRunner.
type RunnerDeps struct {
	Memory  *memory.MemoryFlow
	Context *v4ctx.Builder
	Facts   *signals.FactExtractor
	// Intent   *intent.Engine // REMOVED
	Semantic    *semantic.LLMInterpreter
	Emotion     *emotion.Detector
	Role        *role.Loader
	Decision    *decision.Engine
	Order       *order.OrderFlow
	Payment     *payment.PaymentFlow
	Executor    *executor.ExecutorAdapter
	Social      *social.Renderer
	Logger      *zap.Logger
	DB          *gorm.DB
	Python      *python.Client
	Fulfillment *fulfillment.Service
}

// FlowRunner orchestrates a single turn of the AI.
type FlowRunner struct {
	deps RunnerDeps
}

// NewFlowRunner creates a new runner with injected dependencies.
func NewFlowRunner(deps RunnerDeps) *FlowRunner {
	return &FlowRunner{
		deps: deps,
	}
}

// pruneKnowledgeForContext creates a lighter copy of tenant knowledge
// AND STRICTLY ENFORCES CATALOG ISOLATION based on industry.
func pruneKnowledgeForContext(k *v4ctx.TenantKnowledgeSnapshot, lastDomain, stage string, industry string) *v4ctx.TenantKnowledgeSnapshot {
	cp := *k

	// 🆕 STRICT CATALOG ISOLATION (Step 1)
	// If industry implies a specific catalog type, WIPE OUT the others.
	// This prevents "leaky" context where an Agency sees Menu items.
	kind := types.CatalogKindFromIndustry(industry)

	// SAFETY: If tenant explicitly has only services, force Service even if industry string is ambiguous.
	if k.HasServices && !k.HasProducts && !k.HasMenu {
		kind = types.CatalogService
	}

	// SAFETY: Only override industry mapping when the mapped kind has NO offerings.
	// If a tenant has multiple offering types, we should stick to the business profile kind.
	switch kind {
	case types.CatalogService:
		if !k.HasServices {
			if k.HasProducts {
				kind = types.CatalogProduct
			} else if k.HasMenu {
				kind = types.CatalogMenu
			}
		}
	case types.CatalogProduct:
		if !k.HasProducts {
			if k.HasMenu {
				kind = types.CatalogMenu
			} else if k.HasServices {
				kind = types.CatalogService
			}
		}
	case types.CatalogMenu:
		if !k.HasMenu {
			if k.HasProducts {
				kind = types.CatalogProduct
			} else if k.HasServices {
				kind = types.CatalogService
			}
		}
	}

	switch kind {
	case types.CatalogMenu:
		cp.Products = []v4ctx.ProductSummary{}
		cp.Services = []v4ctx.ServiceSummary{}
	case types.CatalogProduct:
		cp.MenuItems = []v4ctx.MenuItemSummary{}
		cp.Services = []v4ctx.ServiceSummary{}
	case types.CatalogService:
		cp.MenuItems = []v4ctx.MenuItemSummary{}
		cp.Products = []v4ctx.ProductSummary{}
	}

	needsDetails := lastDomain == "offerings" || lastDomain == "ordering" ||
		stage == "offerings_exploration" || stage == "ordering" || stage == "confirming" || stage == "clarifying"

	if !needsDetails {
		if len(cp.FAQs) > 5 {
			cp.FAQs = cp.FAQs[:5]
		}
	} else {
		// Existing logic...
		const SAFETY_CAP = 100
		if len(cp.MenuItems) > SAFETY_CAP {
			cp.MenuItems = cp.MenuItems[:SAFETY_CAP]
		}
		if len(cp.Products) > SAFETY_CAP {
			cp.Products = cp.Products[:SAFETY_CAP]
		}
		if len(cp.Services) > SAFETY_CAP {
			cp.Services = cp.Services[:SAFETY_CAP]
		}
	}
	return &cp
}

// 🆕 CATALOG INTENT CHECK (Step 2)
func isCatalogIntent(intentStr string) bool {
	i := types.Intent(intentStr)
	switch i {
	case types.IntentMenuRequest,
		types.IntentProductRequest,
		types.IntentServiceRequest,
		types.IntentPriceRequest,
		types.IntentCatalogExplain,
		"recommendation",
		"negotiation_offer", "price_objection": // 🆕 Prevent fallthrough to Knowledge Base for negotiation
		return true
	default:
		return false
	}
}

func isRecommendationIntent(intentStr string) bool {
	intent := strings.ToLower(strings.TrimSpace(intentStr))
	return intent == "recommendation" || intent == "recommend"
}

func isRecommendationRequest(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	phrases := []string{
		"recommend", "recommendation", "suggest", "any recommendations", "any recommendation",
		"what do you recommend", "what would you recommend",
	}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func shouldForceCatalogRouting(message string, focusItem string, knowledge *v4ctx.TenantKnowledgeSnapshot) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" || knowledge == nil {
		return false
	}

	// Avoid forcing catalog routing for pure small-talk/greetings.
	// Example bug: customer says "Good morning" and we reply with a product availability card
	// because a previous focus item existed.
	normalizedChitChat := strings.Trim(strings.Join(strings.Fields(strings.Trim(msg, " \t\r\n!?.,")), " "), " \t\r\n")
	switch normalizedChitChat {
	case "hi", "hello", "hey", "good morning", "good afternoon", "good evening", "morning", "good day",
		"thanks", "thank you", "thankyou", "thx":
		return false
	}

	// Semantic routing: rely on catalog matching signal instead of hardcoded phrases.
	if isFullCatalogRequest(msg) {
		return true
	}

	// Follow-up variant/availability question with an active focus should stay in catalog routing,
	// but only when the user's message actually looks like a catalog follow-up.
	if strings.TrimSpace(focusItem) != "" {
		shortFollowUps := map[string]struct{}{
			"ok": {}, "okay": {}, "yes": {}, "yeah": {}, "yep": {}, "no": {}, "nah": {},
			"go ahead": {}, "proceed": {}, "continue": {},
			"delivery": {}, "pickup": {},
			"pay now": {}, "pay on delivery": {}, "pod": {}, "transfer": {},
		}
		if _, ok := shortFollowUps[normalizedChitChat]; ok {
			return true
		}

		followUpKeywords := []string{
			"price", "cost", "how much", "best price",
			"available", "still", "stock",
			"variant", "storage", "gb", "256", "512", "color", "size",
			"buy", "order", "confirm",
			"deliver", "delivery", "pickup",
			"pay", "payment", "transfer",
		}
		for _, k := range followUpKeywords {
			if strings.Contains(msg, k) {
				return true
			}
		}
	}

	// If catalog resolver can confidently resolve a focused item, route to catalog.
	unified := catalog.UnifyOfferings(knowledge)
	if catalog.ResolveFocusedItem(message, unified) != "" {
		return true
	}

	seriesToken := extractSeriesToken(message)
	candidates := buildCatalogCandidates(knowledge)
	if len(candidates) == 0 {
		return false
	}

	bestScore := 0
	for _, c := range candidates {
		s := scoreCatalogCandidate(message, seriesToken, c)
		if s > bestScore {
			bestScore = s
		}
	}

	if seriesToken != "" && bestScore > 0 {
		return true
	}
	return bestScore >= 50
}

// recalculateOrderTotal acts as the Final Authority on pricing.
// It matches items returned by Python (names) to the Source of Truth (Go Knowledge).
func recalculateOrderTotal(items []v4ctx.OrderItem, k *v4ctx.TenantKnowledgeSnapshot) (int64, string) {
	var total int64
	currency := "NGN"
	if len(items) > 0 {
		// Default to NGN or whatever found
	}

	for i, item := range items {
		var foundPrice int64
		var foundCurr string
		matched := false

		// 1. Check Menu Items
		for _, menu := range k.MenuItems {
			// Case-insensitive match or exact? Python does exact mapMapping.
			// Robust fallback: EqualFold
			if strings.EqualFold(menu.Name, item.Name) {
				foundPrice = int64(menu.Price) // Price is float64 in internal logic usually, ensure it's cast correctly
				foundCurr = menu.Currency
				matched = true
				break
			}
		}

		// 2. Check Products
		if !matched {
			for _, prod := range k.Products {
				if strings.EqualFold(prod.Name, item.Name) {
					foundPrice = int64(prod.Price)
					foundCurr = prod.Currency
					matched = true
					break
				}
			}
		}

		// 3. Check Services
		if !matched {
			for _, svc := range k.Services {
				if strings.EqualFold(svc.Name, item.Name) {
					foundPrice = int64(svc.Price)
					foundCurr = "NGN"
					matched = true
					break
				}
			}
		}

		// Update Strict Price
		if matched {
			items[i].UnitPrice = foundPrice // OVERRIDE Python's value
			if foundCurr != "" {
				currency = foundCurr
			}
		}

		total += items[i].UnitPrice * int64(items[i].Quantity)
	}
	return total, currency
}

// =============================================================================
// NIGERIAN PRICE FORMAT PARSING - Expanded for local currency expressions
// =============================================================================

// Primary price regex - handles: "120k", "2M", "₦150,000", "NGN 50000"
var availabilityNormalizeRegex = regexp.MustCompile(`[^a-z0-9]+`)
var capacityNormalizeRegex = regexp.MustCompile(`\b\d+\s*(gb|tb)\b`)
var capacityTokenExtractRegex = regexp.MustCompile(`(?i)\b(\d{2,4})\s*(gb|tb)\b`)
var digitRegex = regexp.MustCompile(`[0-9]`)
var variantPreferenceRegex = regexp.MustCompile(`\b(\d{2,4}\s*gb|\d+\s*tb|color|colour|variant|size)\b`)

func customerWantsToCancel(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}

	// Keep this conservative: only clear cancellation intent should trigger order cancellation.
	phrases := []string{
		"cancel", "cancel order", "cancel my order",
		"never mind", "nevermind", "forget it",
		"stop the order", "stop order",
		"don't proceed", "do not proceed",
	}
	for _, p := range phrases {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

type availabilityCandidate struct {
	name         string
	price        float64
	currency     string
	availability string
	stockCount   int
	kind         string
	description  string
}

func normalizeText(input string) string {
	normalized := strings.ToLower(input)
	normalized = availabilityNormalizeRegex.ReplaceAllString(normalized, " ")
	return strings.TrimSpace(normalized)
}

func tokenizeText(input string) []string {
	parts := strings.Fields(normalizeText(input))
	var tokens []string
	for _, p := range parts {
		if len(p) > 2 {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

func messageMentionsFocus(message string, focus string) bool {
	msgNorm := normalizeText(message)
	focusNorm := normalizeText(focus)
	if msgNorm == "" || focusNorm == "" {
		return false
	}
	if strings.Contains(msgNorm, focusNorm) {
		return true
	}
	for _, tok := range tokenizeText(focusNorm) {
		if len(tok) >= 4 && strings.Contains(msgNorm, tok) {
			return true
		}
	}
	return false
}

func isGenericFocusLabel(focus string) bool {
	f := normalizeText(focus)
	if f == "" {
		return true
	}
	if digitRegex.MatchString(f) {
		return false
	}

	generic := map[string]bool{
		"iphone": true, "iphones": true, "phone": true, "phones": true,
		"product": true, "products": true, "item": true, "items": true,
		"service": true, "services": true, "menu": true, "plan": true, "plans": true,
	}

	tokens := strings.Fields(f)
	if len(tokens) <= 2 {
		for _, tok := range tokens {
			if generic[tok] {
				return true
			}
		}
	}
	return false
}

func choosePersistedActiveItem(previousActive string, currentFocus string, semanticTarget string) string {
	prev := strings.TrimSpace(previousActive)
	curr := strings.TrimSpace(currentFocus)
	target := strings.TrimSpace(semanticTarget)

	// Prefer explicit variant selections (e.g. "(256GB)") over generic semantic targets like
	// "iPhone 13 Pro Max" to avoid pinning future negotiations to the wrong variant.
	currLooksVariant := strings.Contains(curr, "(") || extractCapacityToken(curr) != ""
	targetLooksVariant := strings.Contains(target, "(") || extractCapacityToken(target) != ""

	isSpecific := func(v string) bool {
		return v != "" && !isGenericFocusLabel(v)
	}

	// Prefer the most specific signal available.
	if currLooksVariant && !targetLooksVariant && isSpecific(curr) {
		return curr
	}
	if isSpecific(target) {
		return target
	}
	if isSpecific(curr) {
		return curr
	}
	if isSpecific(prev) {
		return prev
	}
	if curr != "" {
		return curr
	}
	return prev
}

func computeOrderTotalFromSnapshotItems(items []v4ctx.OrderItem) int64 {
	var total int64
	for _, it := range items {
		if it.Quantity <= 0 {
			continue
		}
		total += int64(it.Quantity) * it.UnitPrice
	}
	return total
}

func isCustomerFrustrated(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}
	phrases := []string{
		"you don't want to sell",
		"you dont want to sell",
		"this is too much",
		"you're not serious",
		"you are not serious",
		"frustrating",
		"annoying",
	}
	for _, p := range phrases {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

func resolveOrderItemByName(name string, knowledge *v4ctx.TenantKnowledgeSnapshot) (v4ctx.OrderItem, string, bool) {
	n := strings.TrimSpace(name)
	if n == "" || knowledge == nil {
		return v4ctx.OrderItem{}, "", false
	}
	for _, p := range knowledge.Products {
		if strings.EqualFold(p.Name, n) {
			return v4ctx.OrderItem{Name: p.Name, Quantity: 1, UnitPrice: int64(p.Price)}, p.Currency, true
		}
	}
	for _, m := range knowledge.MenuItems {
		if strings.EqualFold(m.Name, n) {
			return v4ctx.OrderItem{Name: m.Name, Quantity: 1, UnitPrice: int64(m.Price)}, m.Currency, true
		}
	}
	for _, s := range knowledge.Services {
		if strings.EqualFold(s.Name, n) {
			return v4ctx.OrderItem{Name: s.Name, Quantity: 1, UnitPrice: int64(s.Price)}, "NGN", true
		}
	}
	return v4ctx.OrderItem{}, "", false
}

func resolveCatalogTargetByName(name string, knowledge *v4ctx.TenantKnowledgeSnapshot) (*v4ctx.ProductSummary, *v4ctx.ServiceSummary, *v4ctx.MenuItemSummary) {
	n := strings.TrimSpace(name)
	if n == "" || knowledge == nil {
		return nil, nil, nil
	}
	for i := range knowledge.Products {
		p := &knowledge.Products[i]
		if strings.EqualFold(p.Name, n) {
			return p, nil, nil
		}
	}
	for i := range knowledge.Services {
		s := &knowledge.Services[i]
		if strings.EqualFold(s.Name, n) {
			return nil, s, nil
		}
	}
	for i := range knowledge.MenuItems {
		m := &knowledge.MenuItems[i]
		if strings.EqualFold(m.Name, n) {
			return nil, nil, m
		}
	}
	return nil, nil, nil
}

func isProgressedOrderStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "confirming",
		"awaiting_delivery_choice", "awaiting_delivery_address", "awaiting_pickup_time",
		"awaiting_recipient_name", "awaiting_recipient_phone", "awaiting_delivery_instructions",
		"collecting_requirements",
		"payment_ready", "awaiting_payment_method", "payment_sent", "fulfilled":
		return true
	default:
		return false
	}
}

func isActiveOrderFlowStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "confirming",
		"awaiting_delivery_choice", "awaiting_delivery_address", "awaiting_pickup_time",
		"awaiting_recipient_name", "awaiting_recipient_phone", "awaiting_delivery_instructions",
		"collecting_requirements",
		"payment_ready", "awaiting_payment_method":
		return true
	default:
		return false
	}
}

func resumePromptForStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case "awaiting_delivery_choice":
		return "When you're ready, choose delivery or pickup."
	case "awaiting_delivery_address":
		return "When you're ready, share your delivery address."
	case "awaiting_pickup_time":
		return "When you're ready, share your preferred pickup time."
	case "awaiting_recipient_name":
		return "When you're ready, share the recipient name."
	case "awaiting_recipient_phone":
		return "When you're ready, share the recipient phone number."
	case "awaiting_delivery_instructions":
		return "When you're ready, share delivery instructions (or say 'none')."
	default:
		return ""
	}
}

func orderStageRank(stage string) int {
	switch strings.TrimSpace(stage) {
	case "collecting":
		return 10
	case "clarifying":
		return 20
	case "confirming":
		return 30
	case "awaiting_delivery_choice":
		return 40
	case "awaiting_delivery_address":
		return 50
	case "awaiting_recipient_name":
		return 55
	case "awaiting_recipient_phone":
		return 57
	case "awaiting_delivery_instructions":
		return 59
	case "awaiting_pickup_time":
		return 60
	case "collecting_requirements":
		return 70
	case "payment_ready":
		return 80
	case "awaiting_payment_method":
		return 85
	case "payment_sent":
		return 90
	case "fulfilled":
		return 100
	default:
		return 0
	}
}

func isAvailabilityQuery(message string) bool {
	m := strings.ToLower(message)
	phrases := []string{
		"do you have",
		"do u have",
		"have you got",
		"got any",
		"available",
		"in stock",
		"instock",
		"stock left",
	}
	for _, p := range phrases {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

func isStockFollowUpQuery(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}

	hasStockLanguage := strings.Contains(msg, "in stock") ||
		strings.Contains(msg, "out of stock") ||
		strings.Contains(msg, "restock") ||
		strings.Contains(msg, "back in stock") ||
		strings.Contains(msg, "available")
	if !hasStockLanguage {
		return false
	}

	hasTimelineLanguage := strings.Contains(msg, "when") ||
		strings.Contains(msg, "date") ||
		strings.Contains(msg, "again")
	if hasTimelineLanguage {
		return true
	}

	pronounFollowUps := []string{
		"is it in stock",
		"isn't it in stock",
		"it isn't in stock",
		"it is not in stock",
		"so it's not in stock",
		"so it is not in stock",
	}
	for _, p := range pronounFollowUps {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func stockFollowUpAsksOutOfStockDate(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(msg, "when did") && strings.Contains(msg, "out of stock")
}

func messageLooksLikeNegotiation(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	cues := []string{
		"best price", "last price", "final price", "lowest price",
		"any discount", "discount", "reduce", "price drop", "price reduction",
		"can you do", "can u do", "can we do", "can you come down",
		"my budget", "budget is", "i have", "i can pay", "i will pay",
	}
	for _, c := range cues {
		if strings.Contains(msg, c) {
			return true
		}
	}
	return false
}

func resolveAvailabilityCandidate(message string, focusItem string, knowledge *v4ctx.TenantKnowledgeSnapshot) *availabilityCandidate {
	if knowledge == nil {
		return nil
	}

	if focusItem != "" {
		for _, m := range knowledge.MenuItems {
			if strings.EqualFold(m.Name, focusItem) {
				return &availabilityCandidate{
					name:         m.Name,
					price:        m.Price,
					currency:     m.Currency,
					availability: m.Availability,
					stockCount:   -1,
					kind:         "menu",
					description:  m.Description,
				}
			}
		}
		for _, p := range knowledge.Products {
			if strings.EqualFold(p.Name, focusItem) {
				return &availabilityCandidate{
					name:         p.Name,
					price:        p.Price,
					currency:     p.Currency,
					availability: p.Availability,
					stockCount:   p.StockCount,
					kind:         "product",
					description:  p.Description,
				}
			}
		}
		for _, s := range knowledge.Services {
			if strings.EqualFold(s.Name, focusItem) {
				return &availabilityCandidate{
					name:         s.Name,
					price:        s.Price,
					currency:     "NGN",
					availability: "available",
					stockCount:   -1,
					kind:         "service",
					description:  s.Description,
				}
			}
		}
	}

	messageTokens := tokenizeText(message)
	messageNormalized := normalizeText(message)

	bestScore := 0
	var best *availabilityCandidate

	checkCandidate := func(name string, price float64, currency string, availability string, stockCount int, kind string, description string) {
		nameNormalized := normalizeText(name)
		score := 0
		if strings.Contains(messageNormalized, nameNormalized) || strings.Contains(nameNormalized, messageNormalized) {
			score += len(nameNormalized)
		}
		nameTokens := tokenizeText(name)
		for _, t := range messageTokens {
			for _, nt := range nameTokens {
				if t == nt {
					score++
				}
			}
		}
		if score > bestScore {
			bestScore = score
			best = &availabilityCandidate{
				name:         name,
				price:        price,
				currency:     currency,
				availability: availability,
				stockCount:   stockCount,
				kind:         kind,
				description:  description,
			}
		}
	}

	for _, m := range knowledge.MenuItems {
		checkCandidate(m.Name, m.Price, m.Currency, m.Availability, -1, "menu", m.Description)
	}
	for _, p := range knowledge.Products {
		checkCandidate(p.Name, p.Price, p.Currency, p.Availability, p.StockCount, "product", p.Description)
	}
	for _, s := range knowledge.Services {
		checkCandidate(s.Name, s.Price, "NGN", "available", -1, "service", s.Description)
	}

	if bestScore == 0 {
		return nil
	}
	return best
}

func formatCurrency(amount float64, currency string) string {
	if currency == "" {
		currency = "NGN"
	}
	if amount == 0 {
		return ""
	}
	if amount >= 1000 {
		return fmt.Sprintf("%s%s", currency, formatNumberWithCommas(int64(amount)))
	}
	return fmt.Sprintf("%s%.0f", currency, amount)
}

func containsAmount(text string, amount float64) bool {
	if amount <= 0 {
		return false
	}
	val := int64(math.Round(amount))
	raw := fmt.Sprintf("%d", val)
	withCommas := formatNumberWithCommas(val)
	return strings.Contains(text, raw) || strings.Contains(text, withCommas)
}

func pickAvailabilityLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	idx := int(time.Now().UnixNano() % int64(len(lines)))
	if idx < 0 {
		idx = 0
	}
	return lines[idx]
}

func buildAvailabilityReply(message string, focusItem string, knowledge *v4ctx.TenantKnowledgeSnapshot) (string, *availabilityCandidate) {
	if !isAvailabilityQuery(message) {
		return "", nil
	}
	candidate := resolveAvailabilityCandidate(message, focusItem, knowledge)
	if candidate == nil || candidate.name == "" {
		return "", nil
	}
	// If multiple variants exist and the customer didn't specify one, prefer family listing.
	if !hasExplicitVariantPreference(message) && knowledge != nil {
		base := normalizeFamilyName(candidate.name)
		if base != "" {
			count := 0
			for _, c := range buildCatalogCandidates(knowledge) {
				if !isAvailableCandidate(c) {
					continue
				}
				if normalizeFamilyName(c.name) == base {
					count++
				}
			}
			if count >= 2 {
				return "", nil
			}
		}
	}

	availability := strings.ToLower(strings.TrimSpace(candidate.availability))
	isOutOfStock := availability == "out_of_stock" || availability == "unavailable" || candidate.stockCount == 0
	priceText := formatCurrency(candidate.price, candidate.currency)

	if isOutOfStock {
		if priceText != "" {
			lines := []string{
				"Sorry, %s is out of stock right now. The last price was %s. Reply \"switch to\" followed by the model name if you want alternatives.",
				"%s is currently out of stock. It was %s last. Reply \"switch to\" followed by the model name if you want alternatives.",
				"Unfortunately, %s is out of stock at the moment. Last price was %s. Reply \"switch to\" followed by the model name if you want alternatives.",
			}
			return fmt.Sprintf(pickAvailabilityLine(lines), candidate.name, priceText), candidate
		}
		lines := []string{
			"Sorry, %s is out of stock right now. Reply \"switch to\" followed by the model name if you want alternatives.",
			"%s is currently out of stock. Reply \"switch to\" followed by the model name if you want alternatives.",
			"Unfortunately, %s is out of stock at the moment. Reply \"switch to\" followed by the model name if you want alternatives.",
		}
		return fmt.Sprintf(pickAvailabilityLine(lines), candidate.name), candidate
	}

	if priceText != "" {
		lines := []string{
			"Yes, we have %s in stock at %s.",
			"%s is available for %s.",
			"We have %s in stock at %s.",
			"%s is available at %s.",
		}
		return fmt.Sprintf(pickAvailabilityLine(lines), candidate.name, priceText), candidate
	}
	lines := []string{
		"Yes, we have %s in stock.",
		"%s is available right now.",
		"We have %s in stock.",
		"%s is available.",
	}
	return fmt.Sprintf(pickAvailabilityLine(lines), candidate.name), candidate
}

func extractUnavailableSubjectFromReply(lastReply string) string {
	reply := strings.TrimSpace(lastReply)
	if reply == "" {
		return ""
	}
	for _, re := range unavailableReplySubjectPatterns {
		m := re.FindStringSubmatch(reply)
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		name = strings.Trim(name, " \t\r\n\"'.,!?")
		if name != "" {
			return name
		}
	}
	return ""
}

func buildAvailableAlternativesLine(requested string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return ""
	}
	candidates := buildCatalogCandidates(knowledge)
	if len(candidates) == 0 {
		return ""
	}

	requestNorm := normalizeText(requested)
	requestBrand := ""
	if fields := strings.Fields(requestNorm); len(fields) > 0 {
		requestBrand = fields[0]
	}

	alts := make([]availabilityCandidate, 0, 3)
	for _, c := range candidates {
		if !isAvailableCandidate(c) {
			continue
		}
		if requestBrand != "" {
			nameNorm := normalizeText(c.name)
			if !strings.HasPrefix(nameNorm, requestBrand+" ") {
				continue
			}
		}
		alts = append(alts, c)
	}
	if len(alts) == 0 {
		return ""
	}

	sort.SliceStable(alts, func(i, j int) bool {
		si := scoreCatalogCandidate(requested, "", alts[i])
		sj := scoreCatalogCandidate(requested, "", alts[j])
		if si == sj {
			return strings.ToLower(alts[i].name) < strings.ToLower(alts[j].name)
		}
		return si > sj
	})
	if len(alts) > 3 {
		alts = alts[:3]
	}

	lines := make([]string, 0, len(alts))
	for _, alt := range alts {
		priceText := formatCurrency(alt.price, alt.currency)
		if priceText == "" {
			lines = append(lines, alt.name)
		} else {
			lines = append(lines, fmt.Sprintf("%s (%s)", alt.name, priceText))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Available alternatives right now: " + strings.Join(lines, ", ") + "."
}

func buildStockFollowUpReply(message string, focusItem string, convSnapshot *v4ctx.ConversationSnapshot, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil || !isStockFollowUpQuery(message) {
		return ""
	}

	target := strings.TrimSpace(focusItem)
	if target == "" && convSnapshot != nil {
		target = strings.TrimSpace(convSnapshot.ActiveItem)
	}
	if target == "" && convSnapshot != nil {
		target = extractUnavailableSubjectFromReply(convSnapshot.LastAIReply)
	}

	candidate := resolveAvailabilityCandidate(message, target, knowledge)
	if candidate != nil && strings.TrimSpace(candidate.name) != "" {
		target = strings.TrimSpace(candidate.name)
	}

	if target == "" && candidate == nil {
		return ""
	}

	msgLower := strings.ToLower(strings.TrimSpace(message))
	asksTimeline := strings.Contains(msgLower, "when") ||
		strings.Contains(msgLower, "restock") ||
		strings.Contains(msgLower, "back in stock") ||
		strings.Contains(msgLower, "available again") ||
		strings.Contains(msgLower, "date")
	asksOutOfStockDate := stockFollowUpAsksOutOfStockDate(message)
	alternativesLine := buildAvailableAlternativesLine(target, knowledge)

	if candidate != nil {
		if isAvailableCandidate(*candidate) {
			priceText := formatCurrency(candidate.price, candidate.currency)
			if priceText != "" {
				return fmt.Sprintf("%s is currently in stock at %s.", candidate.name, priceText)
			}
			return fmt.Sprintf("%s is currently in stock.", candidate.name)
		}

		if asksOutOfStockDate {
			base := fmt.Sprintf("We don't have the exact date %s went out of stock yet. It is still out of stock right now.", candidate.name)
			if alternativesLine != "" {
				return base + " " + alternativesLine
			}
			return base
		}
		if asksTimeline {
			base := fmt.Sprintf("We don't have a confirmed restock date for %s yet. It is still out of stock right now.", candidate.name)
			if alternativesLine != "" {
				return base + " " + alternativesLine
			}
			return base
		}
		base := fmt.Sprintf("No, %s is still out of stock right now.", candidate.name)
		if alternativesLine != "" {
			return base + " " + alternativesLine
		}
		return base
	}

	if asksOutOfStockDate {
		base := fmt.Sprintf("We don't have an out-of-stock date for %s because it is not currently listed in our catalog.", target)
		if alternativesLine != "" {
			return base + " " + alternativesLine
		}
		return base
	}
	if asksTimeline {
		base := fmt.Sprintf("We don't have a restock date for %s because it is not currently listed in our catalog.", target)
		if alternativesLine != "" {
			return base + " " + alternativesLine
		}
		return base
	}

	base := fmt.Sprintf("%s is not currently listed in our catalog, so it is not in stock with us right now.", target)
	if alternativesLine != "" {
		return base + " " + alternativesLine
	}
	return base
}

func extractSeriesToken(message string) string {
	msg := strings.ToLower(message)
	seriesRegex := regexp.MustCompile(`\b(\d{1,2})\s*(series|line|generation)\b`)
	m := seriesRegex.FindStringSubmatch(msg)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func buildCatalogCandidates(knowledge *v4ctx.TenantKnowledgeSnapshot) []availabilityCandidate {
	if knowledge == nil {
		return nil
	}
	kind := types.CatalogKindFromIndustry(knowledge.Industry)
	// SAFETY: If tenant explicitly has only services, force Service even if industry string is ambiguous.
	if knowledge.HasServices && !knowledge.HasProducts && !knowledge.HasMenu {
		kind = types.CatalogService
	}
	// SAFETY: Only override industry mapping when the mapped kind has NO offerings.
	switch kind {
	case types.CatalogService:
		if !knowledge.HasServices {
			if knowledge.HasProducts {
				kind = types.CatalogProduct
			} else if knowledge.HasMenu {
				kind = types.CatalogMenu
			}
		}
	case types.CatalogProduct:
		if !knowledge.HasProducts {
			if knowledge.HasMenu {
				kind = types.CatalogMenu
			} else if knowledge.HasServices {
				kind = types.CatalogService
			}
		}
	case types.CatalogMenu:
		if !knowledge.HasMenu {
			if knowledge.HasProducts {
				kind = types.CatalogProduct
			} else if knowledge.HasServices {
				kind = types.CatalogService
			}
		}
	}

	out := []availabilityCandidate{}
	switch kind {
	case types.CatalogMenu:
		for _, m := range knowledge.MenuItems {
			out = append(out, availabilityCandidate{
				name:         m.Name,
				price:        m.Price,
				currency:     m.Currency,
				availability: m.Availability,
				stockCount:   -1,
				kind:         "menu",
				description:  m.Description,
			})
		}
	case types.CatalogProduct:
		for _, p := range knowledge.Products {
			out = append(out, availabilityCandidate{
				name:         p.Name,
				price:        p.Price,
				currency:     p.Currency,
				availability: p.Availability,
				stockCount:   p.StockCount,
				kind:         "product",
				description:  p.Description,
			})
		}
	default:
		for _, s := range knowledge.Services {
			out = append(out, availabilityCandidate{
				name:         s.Name,
				price:        s.Price,
				currency:     "NGN",
				availability: "available",
				stockCount:   -1,
				kind:         "service",
				description:  s.Description,
			})
		}
	}
	return out
}

func buildCatalogCandidatesByKind(knowledge *v4ctx.TenantKnowledgeSnapshot, kind types.CatalogKind) []availabilityCandidate {
	if knowledge == nil {
		return nil
	}
	out := []availabilityCandidate{}
	switch kind {
	case types.CatalogMenu:
		for _, m := range knowledge.MenuItems {
			out = append(out, availabilityCandidate{
				name:         m.Name,
				price:        m.Price,
				currency:     m.Currency,
				availability: m.Availability,
				stockCount:   -1,
				kind:         "menu",
				description:  m.Description,
			})
		}
	case types.CatalogProduct:
		for _, p := range knowledge.Products {
			out = append(out, availabilityCandidate{
				name:         p.Name,
				price:        p.Price,
				currency:     p.Currency,
				availability: p.Availability,
				stockCount:   p.StockCount,
				kind:         "product",
				description:  p.Description,
			})
		}
	default:
		for _, s := range knowledge.Services {
			out = append(out, availabilityCandidate{
				name:         s.Name,
				price:        s.Price,
				currency:     "NGN",
				availability: "available",
				stockCount:   -1,
				kind:         "service",
				description:  s.Description,
			})
		}
	}
	return out
}

func fullCatalogKindHint(message string) (types.CatalogKind, bool) {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return "", false
	}
	// Service/agency phrasing.
	if strings.Contains(msg, "plan") || strings.Contains(msg, "plans") ||
		strings.Contains(msg, "package") || strings.Contains(msg, "packages") ||
		strings.Contains(msg, "service") || strings.Contains(msg, "services") ||
		strings.Contains(msg, "rate card") || strings.Contains(msg, "rates") {
		return types.CatalogService, true
	}
	// Menu phrasing.
	if strings.Contains(msg, "menu") {
		return types.CatalogMenu, true
	}
	// Product phrasing.
	if strings.Contains(msg, "products") || strings.Contains(msg, "product") {
		return types.CatalogProduct, true
	}
	return "", false
}

func buildFullCatalogReplyForRequest(message string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return ""
	}
	if hint, ok := fullCatalogKindHint(message); ok {
		switch hint {
		case types.CatalogService:
			if len(knowledge.Services) == 0 {
				return "We don’t have service plans listed yet. Tell me what you need and I’ll share options."
			}
		case types.CatalogMenu:
			if len(knowledge.MenuItems) == 0 {
				return "We don’t have a menu listed yet. Tell me what you’re looking for and I’ll help."
			}
		case types.CatalogProduct:
			if len(knowledge.Products) == 0 {
				return "We don’t have products listed yet. Tell me what you want and I’ll help you right away."
			}
		}
		return buildFullCatalogReply(buildCatalogCandidatesByKind(knowledge, hint))
	}

	// If exactly one offering type exists, prefer it.
	kinds := []struct {
		kind  types.CatalogKind
		count int
	}{
		{kind: types.CatalogProduct, count: len(knowledge.Products)},
		{kind: types.CatalogMenu, count: len(knowledge.MenuItems)},
		{kind: types.CatalogService, count: len(knowledge.Services)},
	}
	nonZero := []types.CatalogKind{}
	for _, k := range kinds {
		if k.count > 0 {
			nonZero = append(nonZero, k.kind)
		}
	}
	if len(nonZero) == 1 {
		return buildFullCatalogReply(buildCatalogCandidatesByKind(knowledge, nonZero[0]))
	}

	return buildFullCatalogReply(buildCatalogCandidates(knowledge))
}

func isFullCatalogRequest(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	phrases := []string{
		"full catalog", "full catalogue", "full menu", "full list", "price list",
		"all items", "everything you have", "see your catalog", "see your menu",
		"show me the catalog", "show me your catalog", "show me the menu", "show me your menu",
		// Service/agency phrasing (Brancr plans/packages).
		"see your plans", "show your plans", "show me your plans", "view your plans",
		"see your services", "show your services", "view your services",
		"see your packages", "show your packages", "view your packages",
		"pricing", "price list", "rate card", "rates",
	}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	if strings.Contains(msg, "catalog") || strings.Contains(msg, "menu") {
		if strings.Contains(msg, "show") || strings.Contains(msg, "see") || strings.Contains(msg, "view") || strings.Contains(msg, "list") {
			return true
		}
		if msg == "catalog" || msg == "menu" {
			return true
		}
	}
	// Treat generic "plans/services/packages" requests as full catalog when asked to show/list them.
	if strings.Contains(msg, "plan") || strings.Contains(msg, "plans") || strings.Contains(msg, "service") || strings.Contains(msg, "services") || strings.Contains(msg, "package") || strings.Contains(msg, "packages") {
		if strings.Contains(msg, "show") || strings.Contains(msg, "see") || strings.Contains(msg, "view") || strings.Contains(msg, "list") {
			return true
		}
		if msg == "plans" || msg == "services" || msg == "packages" {
			return true
		}
	}
	return false
}

func buildFullCatalogReply(candidates []availabilityCandidate) string {
	if len(candidates) == 0 {
		return "We don’t have a catalog listed yet. Tell me what you need and I’ll help you right away."
	}
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].name) < strings.ToLower(candidates[j].name)
	})

	lines := make([]string, 0, len(candidates))
	for _, c := range candidates {
		priceText := formatCurrency(c.price, c.currency)
		line := c.name
		if priceText != "" {
			line = fmt.Sprintf("- %s - %s", c.name, priceText)
		} else {
			line = fmt.Sprintf("- %s", c.name)
		}
		if !isAvailableCandidate(c) {
			line += " (currently unavailable)"
		}
		lines = append(lines, line)
	}
	return "Here is our full catalog:\n" + strings.Join(lines, "\n")
}

func buildRecommendationReply(message string, candidates []availabilityCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var available []availabilityCandidate
	for _, c := range candidates {
		if isAvailableCandidate(c) {
			available = append(available, c)
		}
	}
	if len(available) == 0 {
		return ""
	}

	sort.Slice(available, func(i, j int) bool {
		if available[i].price == available[j].price {
			return available[i].name < available[j].name
		}
		if available[i].price == 0 {
			return false
		}
		if available[j].price == 0 {
			return true
		}
		return available[i].price < available[j].price
	})

	maxItems := 3
	lines := make([]string, 0, maxItems)
	for _, c := range available {
		if len(lines) >= maxItems {
			break
		}
		priceText := formatCurrency(c.price, c.currency)
		if priceText != "" {
			lines = append(lines, fmt.Sprintf("• %s — %s", c.name, priceText))
		} else {
			lines = append(lines, fmt.Sprintf("• %s", c.name))
		}
	}
	if len(lines) == 0 {
		return ""
	}

	intro := "Here are a few options I recommend:"
	msg := strings.ToLower(message)
	if strings.Contains(msg, "cheap") || strings.Contains(msg, "not expensive") || strings.Contains(msg, "budget") {
		intro = "Here are a few budget-friendly options:"
	}
	return intro + "\n" + strings.Join(lines, "\n") + "\nLet us know if you want to move to one of these options."
}

func buildStructuredTargetsReply(targets []string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if len(targets) == 0 || knowledge == nil {
		return ""
	}
	candidates := buildCatalogCandidates(knowledge)
	if len(candidates) == 0 {
		return ""
	}

	// Match targets to catalog candidates in order.
	type matched struct {
		name     string
		price    float64
		currency string
	}
	var matches []matched
	seen := map[string]bool{}

	addMatch := func(c availabilityCandidate) {
		key := strings.ToLower(c.name)
		if seen[key] {
			return
		}
		seen[key] = true
		matches = append(matches, matched{name: c.name, price: c.price, currency: c.currency})
	}

	// Precompute family buckets.
	families := map[string][]availabilityCandidate{}
	for _, c := range candidates {
		if !isAvailableCandidate(c) {
			continue
		}
		base := normalizeFamilyName(c.name)
		if base == "" {
			continue
		}
		families[base] = append(families[base], c)
	}

	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// Exact match first.
		var best *availabilityCandidate
		for _, c := range candidates {
			if strings.EqualFold(c.name, t) {
				tmp := c
				best = &tmp
				break
			}
		}
		if best != nil && best.name != "" && isAvailableCandidate(*best) {
			addMatch(*best)
			if len(matches) >= 3 {
				break
			}
			continue
		}

		// Family expansion: if target is a base name, include all variants.
		targetBase := normalizeFamilyName(t)
		if targetBase != "" {
			if fam, ok := families[targetBase]; ok && len(fam) >= 2 {
				sort.Slice(fam, func(i, j int) bool {
					if fam[i].price == fam[j].price {
						return fam[i].name < fam[j].name
					}
					return fam[i].price < fam[j].price
				})
				for _, c := range fam {
					addMatch(c)
					if len(matches) >= 5 {
						break
					}
				}
				break
			}
		}

		// Fuzzy match fallback.
		bestScore := 0
		var bestCand *availabilityCandidate
		for _, c := range candidates {
			s := scoreCatalogCandidate(t, "", c)
			if s > bestScore {
				bestScore = s
				tmp := c
				bestCand = &tmp
			}
		}
		if bestScore >= 20 && bestCand != nil && bestCand.name != "" && isAvailableCandidate(*bestCand) {
			addMatch(*bestCand)
		}
		if len(matches) >= 3 {
			break
		}
	}
	if len(matches) == 0 {
		return ""
	}

	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		priceText := formatCurrency(m.price, m.currency)
		if priceText != "" {
			lines = append(lines, fmt.Sprintf("• %s — %s", m.name, priceText))
		} else {
			lines = append(lines, fmt.Sprintf("• %s", m.name))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Here are the options:" + "\n" + strings.Join(lines, "\n")
}

func normalizeForMatch(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	s := strings.ToLower(input)
	s = strings.ReplaceAll(s, "(", " ")
	s = strings.ReplaceAll(s, ")", " ")
	s = availabilityNormalizeRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func extractTargetsFromReplyText(reply string, knowledge *v4ctx.TenantKnowledgeSnapshot) []string {
	if strings.TrimSpace(reply) == "" || knowledge == nil {
		return nil
	}
	text := normalizeForMatch(reply)
	type match struct {
		name string
		pos  int
	}
	var matches []match

	addIfFound := func(name string) {
		if name == "" {
			return
		}
		n := normalizeForMatch(name)
		if idx := strings.Index(text, n); idx >= 0 {
			matches = append(matches, match{name: name, pos: idx})
		}
	}

	// Prefer longer names to avoid partial overlaps.
	allNames := []string{}
	for _, p := range knowledge.Products {
		allNames = append(allNames, p.Name)
	}
	for _, m := range knowledge.MenuItems {
		allNames = append(allNames, m.Name)
	}
	for _, s := range knowledge.Services {
		allNames = append(allNames, s.Name)
	}
	sort.Slice(allNames, func(i, j int) bool { return len(allNames[i]) > len(allNames[j]) })
	for _, n := range allNames {
		addIfFound(n)
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pos < matches[j].pos })
	out := []string{}
	seen := map[string]bool{}
	for _, m := range matches {
		if seen[strings.ToLower(m.name)] {
			continue
		}
		seen[strings.ToLower(m.name)] = true
		out = append(out, m.name)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// ProductMatch holds the validation state for a single catalog item found in the text
type ProductMatch struct {
	Name       string
	IsSubMatch bool   // true if this name is a substring of another match
	PriceValid bool   // true if the price was found in the text
	Raw        string // original name for debugging
}

func itemPricingMatchesReply(reply string, knowledge *v4ctx.TenantKnowledgeSnapshot) bool {
	// Let Python handle pricing and validation logic.
	// We no longer strictly validate exact price string matches in Go,
	// as it causes false positive fallbacks when the LLM summarizes or
	// formats prices differently.
	return true
}

func replyIncludesAllCatalogItems(reply string, knowledge *v4ctx.TenantKnowledgeSnapshot) bool {
	if strings.TrimSpace(reply) == "" || knowledge == nil {
		return false
	}
	text := normalizeForMatch(reply)
	if text == "" {
		return false
	}

	var names []string
	for _, p := range knowledge.Products {
		if strings.TrimSpace(p.Name) != "" {
			names = append(names, p.Name)
		}
	}
	for _, m := range knowledge.MenuItems {
		if strings.TrimSpace(m.Name) != "" {
			names = append(names, m.Name)
		}
	}
	for _, s := range knowledge.Services {
		if strings.TrimSpace(s.Name) != "" {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		return false
	}

	for _, name := range names {
		if !strings.Contains(text, normalizeForMatch(name)) {
			return false
		}
	}
	return true
}

func isCatalogReplyValid(reply string, message string, knowledge *v4ctx.TenantKnowledgeSnapshot) bool {
	if strings.TrimSpace(reply) == "" || knowledge == nil {
		return false
	}
	if !itemPricingMatchesReply(reply, knowledge) {
		return false
	}
	if isFullCatalogRequest(message) {
		return replyIncludesAllCatalogItems(reply, knowledge)
	}
	return true
}

func normalizeFamilyName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if idx := strings.Index(lower, "("); idx >= 0 {
		lower = strings.TrimSpace(lower[:idx])
	}
	lower = capacityNormalizeRegex.ReplaceAllString(lower, " ")
	lower = availabilityNormalizeRegex.ReplaceAllString(lower, " ")
	return strings.TrimSpace(lower)
}

func extractCapacityToken(message string) string {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return ""
	}
	m := capacityTokenExtractRegex.FindStringSubmatch(msg)
	if len(m) < 3 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m[1]) + strings.TrimSpace(m[2]))
}

func normalizeCompact(s string) string {
	// Lowercase and remove whitespace for robust token matching like "256gb" vs "256 GB".
	lower := strings.ToLower(s)
	return strings.Join(strings.Fields(lower), "")
}

func extractCapacityTokens(message string) []string {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return nil
	}
	matches := capacityTokenExtractRegex.FindAllStringSubmatch(msg, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		tok := strings.ToLower(strings.TrimSpace(m[1]) + strings.TrimSpace(m[2]))
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func capacityNumberFromName(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	m := capacityTokenExtractRegex.FindStringSubmatch(n)
	if len(m) < 2 {
		return 0
	}
	val, err := strconv.Atoi(strings.TrimSpace(m[1]))
	if err != nil {
		return 0
	}
	return val
}

func messageAsksBestPricesForBoth(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	if !strings.Contains(msg, "price") {
		return false
	}
	asksBest := strings.Contains(msg, "best price") || strings.Contains(msg, "best prices") || strings.Contains(msg, "best")
	if !asksBest {
		return false
	}
	if strings.Contains(msg, "both") {
		return true
	}
	// e.g. "256GB and 512GB"
	return len(extractCapacityTokens(msg)) >= 2
}

func buildMultiVariantBestPricesReply(message string, activeItem string, lastAIReply string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil || !messageAsksBestPricesForBoth(message) {
		return ""
	}

	base := normalizeFamilyName(activeItem)
	if base == "" {
		base = normalizeFamilyName(bestCatalogNameMentionedInText(lastAIReply, knowledge))
	}
	if base == "" {
		return ""
	}

	all := buildCatalogCandidates(knowledge)
	if len(all) == 0 {
		return ""
	}

	var family []availabilityCandidate
	for _, c := range all {
		if !isAvailableCandidate(c) {
			continue
		}
		if normalizeFamilyName(c.name) != base {
			continue
		}
		if c.price <= 0 {
			continue
		}
		family = append(family, c)
	}
	if len(family) < 2 {
		return ""
	}

	// If the customer specified capacities, honor them.
	tokens := extractCapacityTokens(message)
	selected := []availabilityCandidate{}
	selectedNames := map[string]bool{}

	lastLower := strings.ToLower(lastAIReply)
	pickForToken := func(tok string) {
		type scored struct {
			c availabilityCandidate
			s int
		}
		var candidates []scored
		for _, c := range family {
			if !strings.Contains(normalizeCompact(c.name), tok) {
				continue
			}
			s := 0
			if strings.Contains(lastLower, strings.ToLower(c.name)) {
				s += 10
			}
			// Prefer closer capacity number matches when available.
			if n := capacityNumberFromName(c.name); n > 0 {
				s += n / 10
			}
			candidates = append(candidates, scored{c: c, s: s})
		}
		if len(candidates) == 0 {
			return
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].s == candidates[j].s {
				return strings.ToLower(candidates[i].c.name) < strings.ToLower(candidates[j].c.name)
			}
			return candidates[i].s > candidates[j].s
		})
		chosen := candidates[0].c
		if !selectedNames[chosen.name] {
			selectedNames[chosen.name] = true
			selected = append(selected, chosen)
		}
	}

	if len(tokens) >= 2 {
		for _, tok := range tokens {
			pickForToken(tok)
		}
	} else {
		// "both" without explicit capacities: prefer variants mentioned in the last reply; else list the top 2.
		for _, c := range family {
			if strings.Contains(lastLower, strings.ToLower(c.name)) {
				if !selectedNames[c.name] {
					selectedNames[c.name] = true
					selected = append(selected, c)
				}
			}
		}
		if len(selected) < 2 {
			// Deterministically pick two variants by capacity then name.
			sort.SliceStable(family, func(i, j int) bool {
				ci := capacityNumberFromName(family[i].name)
				cj := capacityNumberFromName(family[j].name)
				if ci != 0 && cj != 0 && ci != cj {
					return ci < cj
				}
				return strings.ToLower(family[i].name) < strings.ToLower(family[j].name)
			})
			for _, c := range family {
				if len(selected) >= 2 {
					break
				}
				if !selectedNames[c.name] {
					selectedNames[c.name] = true
					selected = append(selected, c)
				}
			}
		}
	}

	if len(selected) < 2 {
		return ""
	}

	sort.SliceStable(selected, func(i, j int) bool {
		ci := capacityNumberFromName(selected[i].name)
		cj := capacityNumberFromName(selected[j].name)
		if ci != 0 && cj != 0 && ci != cj {
			return ci < cj
		}
		return strings.ToLower(selected[i].name) < strings.ToLower(selected[j].name)
	})

	lines := make([]string, 0, len(selected))
	for _, c := range selected {
		lines = append(lines, fmt.Sprintf("• %s — %s", c.name, formatCurrency(c.price, c.currency)))
	}
	return "Best prices:\n" + strings.Join(lines, "\n")
}

func bestCatalogNameMentionedInText(text string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if strings.TrimSpace(text) == "" || knowledge == nil {
		return ""
	}
	lower := strings.ToLower(text)
	best := ""
	bestLen := 0
	for _, m := range knowledge.MenuItems {
		if m.Name != "" && strings.Contains(lower, strings.ToLower(m.Name)) && len(m.Name) > bestLen {
			best = m.Name
			bestLen = len(m.Name)
		}
	}
	for _, p := range knowledge.Products {
		if p.Name != "" && strings.Contains(lower, strings.ToLower(p.Name)) && len(p.Name) > bestLen {
			best = p.Name
			bestLen = len(p.Name)
		}
	}
	for _, s := range knowledge.Services {
		if s.Name != "" && strings.Contains(lower, strings.ToLower(s.Name)) && len(s.Name) > bestLen {
			best = s.Name
			bestLen = len(s.Name)
		}
	}
	return best
}

// resolveVariantFocusFromMessage handles follow-ups like "the 256GB" after the AI has shown variants.
// It prevents stale ActiveItem from "leaking" into variant-specific price/availability questions.
func resolveVariantFocusFromMessage(message string, activeItem string, lastAIReply string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return ""
	}
	token := extractCapacityToken(message)
	if token == "" {
		return ""
	}

	base := normalizeFamilyName(activeItem)
	if base == "" {
		base = normalizeFamilyName(bestCatalogNameMentionedInText(lastAIReply, knowledge))
	}

	type cand struct {
		name  string
		score int
	}
	var matches []cand
	lastLower := strings.ToLower(lastAIReply)

	addIfMatch := func(name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		if base != "" && normalizeFamilyName(name) != base {
			return
		}
		if !strings.Contains(normalizeCompact(name), token) {
			return
		}
		score := 0
		if strings.TrimSpace(activeItem) != "" && strings.EqualFold(strings.TrimSpace(activeItem), strings.TrimSpace(name)) {
			score += 5
		}
		if strings.TrimSpace(lastAIReply) != "" && strings.Contains(lastLower, strings.ToLower(name)) {
			score += 10
		}
		matches = append(matches, cand{name: name, score: score})
	}

	for _, p := range knowledge.Products {
		addIfMatch(p.Name)
	}
	for _, m := range knowledge.MenuItems {
		addIfMatch(m.Name)
	}
	for _, s := range knowledge.Services {
		addIfMatch(s.Name)
	}

	if len(matches) == 0 && base != "" {
		// If family filtering was too strict, retry global capacity match only if it yields a single unambiguous hit.
		base = ""
		for _, p := range knowledge.Products {
			addIfMatch(p.Name)
		}
		for _, m := range knowledge.MenuItems {
			addIfMatch(m.Name)
		}
		for _, s := range knowledge.Services {
			addIfMatch(s.Name)
		}
	}

	if len(matches) == 0 {
		return ""
	}
	if len(matches) == 1 {
		return matches[0].name
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return strings.ToLower(matches[i].name) < strings.ToLower(matches[j].name)
		}
		return matches[i].score > matches[j].score
	})
	if matches[0].score > matches[1].score {
		return matches[0].name
	}
	return ""
}

func hasExplicitVariantPreference(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	return variantPreferenceRegex.MatchString(msg)
}

func buildFamilyVariantReply(message string, focusItem string, candidates []availabilityCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	if hasExplicitVariantPreference(message) {
		return ""
	}

	base := ""
	if strings.TrimSpace(focusItem) != "" {
		base = normalizeFamilyName(focusItem)
	} else {
		// Derive a family base from the best matching candidate in the message.
		bestScore := 0
		bestName := ""
		for _, c := range candidates {
			s := scoreCatalogCandidate(message, "", c)
			if s > bestScore {
				bestScore = s
				bestName = c.name
			}
		}
		if bestScore >= 20 {
			base = normalizeFamilyName(bestName)
		}
	}

	msgNorm := normalizeText(message)
	if base == "" || !strings.Contains(msgNorm, base) {
		return ""
	}

	var family []availabilityCandidate
	for _, c := range candidates {
		if !isAvailableCandidate(c) {
			continue
		}
		if normalizeFamilyName(c.name) == base {
			family = append(family, c)
		}
	}
	if len(family) < 2 {
		return ""
	}

	sort.Slice(family, func(i, j int) bool {
		if family[i].price == family[j].price {
			return family[i].name < family[j].name
		}
		return family[i].price < family[j].price
	})

	lines := make([]string, 0, len(family))
	for _, c := range family {
		priceText := formatCurrency(c.price, c.currency)
		if priceText != "" {
			lines = append(lines, fmt.Sprintf("• %s — %s", c.name, priceText))
		} else {
			lines = append(lines, fmt.Sprintf("• %s", c.name))
		}
	}

	intro := fmt.Sprintf("Yes, we have these %s options:", base)
	return intro + "\n" + strings.Join(lines, "\n")
}

func isAvailableCandidate(c availabilityCandidate) bool {
	availability := strings.ToLower(strings.TrimSpace(c.availability))
	if availability == "out_of_stock" || availability == "unavailable" {
		return false
	}
	if c.kind == "product" && c.stockCount == 0 {
		return false
	}
	return true
}

func scoreCatalogCandidate(message string, seriesToken string, candidate availabilityCandidate) int {
	msgNorm := normalizeText(message)
	nameNorm := normalizeText(candidate.name)
	descNorm := normalizeText(candidate.description)
	if msgNorm == "" || nameNorm == "" {
		return 0
	}

	score := 0
	if strings.Contains(msgNorm, nameNorm) {
		score += 120
	}
	if strings.Contains(nameNorm, msgNorm) {
		score += 90
	}

	queryTokens := tokenizeText(msgNorm)
	nameTokens := tokenizeText(nameNorm)
	descTokens := tokenizeText(descNorm)
	for _, qt := range queryTokens {
		for _, nt := range nameTokens {
			if qt == nt {
				score += 20
			}
		}
		for _, dt := range descTokens {
			if qt == dt {
				score += 5
			}
		}
	}

	// Prefix-friendly typo tolerance.
	for _, qt := range queryTokens {
		if len(qt) < 4 {
			continue
		}
		for _, nt := range nameTokens {
			if strings.HasPrefix(nt, qt) || strings.HasPrefix(qt, nt) {
				score += 8
			}
		}
	}

	if seriesToken != "" && strings.Contains(nameNorm, seriesToken) {
		score += 30
	}
	return score
}

func buildDeterministicCatalogReply(message string, focusItem string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if isFullCatalogRequest(message) {
		return buildFullCatalogReplyForRequest(message, knowledge)
	}

	candidates := buildCatalogCandidates(knowledge)
	if len(candidates) == 0 {
		return ""
	}

	if familyReply := buildFamilyVariantReply(message, focusItem, candidates); strings.TrimSpace(familyReply) != "" {
		return familyReply
	}

	// Exact focus item first when explicit.
	explicit := catalog.ResolveLongestMatch(message, knowledge)
	if strings.TrimSpace(focusItem) != "" && (explicit == nil || strings.EqualFold(strings.TrimSpace(explicit.Name), strings.TrimSpace(focusItem))) {
		for _, c := range candidates {
			if strings.EqualFold(strings.TrimSpace(c.name), strings.TrimSpace(focusItem)) {
				if isAvailableCandidate(c) {
					priceText := formatCurrency(c.price, c.currency)
					if priceText != "" {
						return fmt.Sprintf("%s is available at %s.", c.name, priceText)
					}
					return fmt.Sprintf("%s is available.", c.name)
				}
				return fmt.Sprintf("%s is currently unavailable. Reply \"switch to\" followed by the model name if you want alternatives.", c.name)
			}
		}
	}

	seriesToken := extractSeriesToken(message)
	type scored struct {
		c availabilityCandidate
		s int
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		s := scoreCatalogCandidate(message, seriesToken, c)
		if s > 0 {
			scoredList = append(scoredList, scored{c: c, s: s})
		}
	}
	if len(scoredList) == 0 {
		return ""
	}

	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].s == scoredList[j].s {
			return scoredList[i].c.name < scoredList[j].c.name
		}
		return scoredList[i].s > scoredList[j].s
	})

	maxItems := 5
	lines := []string{}
	for _, it := range scoredList {
		if len(lines) >= maxItems {
			break
		}
		if !isAvailableCandidate(it.c) {
			continue
		}
		priceText := formatCurrency(it.c.price, it.c.currency)
		if priceText != "" {
			lines = append(lines, fmt.Sprintf("• %s — %s", it.c.name, priceText))
		} else {
			lines = append(lines, fmt.Sprintf("• %s", it.c.name))
		}
	}

	if len(lines) == 0 {
		return "I found matching options, but they are currently unavailable. Reply \"switch to\" followed by the model name if you want alternatives."
	}

	intro := "Here are the matching options we currently have:"
	if seriesToken != "" {
		intro = fmt.Sprintf("Here are the %s-series options we currently have:", seriesToken)
	}
	return intro + "\n" + strings.Join(lines, "\n")
}

// extractSafePreface returns a short, non-factual lead-in from the LLM reply.
// It rejects anything with digits, prices/currency markers, or catalog item mentions.
func extractSafePreface(llmReply string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	reply := strings.TrimSpace(llmReply)
	if reply == "" || knowledge == nil {
		return ""
	}

	// Reject anything that looks like facts (numbers, prices, currency).
	if containsDigits(reply) || containsCurrencyHint(reply) {
		return ""
	}

	lower := strings.ToLower(reply)
	for _, p := range knowledge.Products {
		if p.Name != "" && strings.Contains(lower, strings.ToLower(p.Name)) {
			return ""
		}
	}
	for _, m := range knowledge.MenuItems {
		if m.Name != "" && strings.Contains(lower, strings.ToLower(m.Name)) {
			return ""
		}
	}
	for _, s := range knowledge.Services {
		if s.Name != "" && strings.Contains(lower, strings.ToLower(s.Name)) {
			return ""
		}
	}

	// Keep only the first sentence and cap length to avoid rambling.
	s := firstSentence(reply)
	if len(s) > 160 {
		s = s[:160]
	}
	return strings.TrimSpace(s)
}

func extractSafeFollowUp(llmReply string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	return extractSafePreface(llmReply, knowledge)
}

func shouldBlockFollowUp(followUp string, inOrderingMode bool, hasNewItems bool) bool {
	if inOrderingMode || hasNewItems {
		return false
	}
	lower := strings.ToLower(followUp)
	if strings.Contains(lower, "delivery") || strings.Contains(lower, "pickup") || strings.Contains(lower, "pick up") {
		return true
	}
	return false
}

func containsDigits(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func containsCurrencyHint(s string) bool {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "ngn") ||
		strings.Contains(lower, "usd") ||
		strings.Contains(lower, "eur") ||
		strings.Contains(lower, "gbp") ||
		strings.Contains(lower, "₦") ||
		strings.Contains(lower, "$") ||
		strings.Contains(lower, "€") ||
		strings.Contains(lower, "£") {
		return true
	}
	return false
}

func firstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			return strings.TrimSpace(s[:i+1])
		}
	}
	return strings.TrimSpace(s)
}

func isWeakStallReply(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	stallPhrases := []string{
		"please bear with me",
		"working on your request",
		"one moment",
		"hold on",
	}
	for _, p := range stallPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func inferFocusFromLastReply(message string, lastAIReply string, knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	if knowledge == nil {
		return ""
	}
	msg := strings.ToLower(message)
	vagueFollowUpPhrases := []string{
		// Pronouns
		"that one", "this one", "that", "this", "it",
		// Price follow-ups
		"best price", "how much", "any discount", "what about the price",
		"the price", "any deal", "how about", "can you do", "the best",
		"what's the price", "whats the price",
		// Condition / spec follow-ups
		"the condition", "conditions", "what condition", "is it new",
		"is it available", "still available", "any left", "in stock",
		// Vague continuation
		"tell me more", "more details", "send specs",
	}
	isVagueFollowUp := false
	for _, p := range vagueFollowUpPhrases {
		if strings.Contains(msg, p) {
			isVagueFollowUp = true
			break
		}
	}
	if !isVagueFollowUp {
		return ""
	}
	best := ""
	bestLen := 0
	lowerReply := strings.ToLower(lastAIReply)
	for _, m := range knowledge.MenuItems {
		if strings.Contains(lowerReply, strings.ToLower(m.Name)) && len(m.Name) > bestLen {
			best = m.Name
			bestLen = len(m.Name)
		}
	}
	for _, p := range knowledge.Products {
		if strings.Contains(lowerReply, strings.ToLower(p.Name)) && len(p.Name) > bestLen {
			best = p.Name
			bestLen = len(p.Name)
		}
	}
	for _, s := range knowledge.Services {
		if strings.Contains(lowerReply, strings.ToLower(s.Name)) && len(s.Name) > bestLen {
			best = s.Name
			bestLen = len(s.Name)
		}
	}
	return best
}

func isVariantOnlyTarget(target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	if t == "" {
		return false
	}
	fields := strings.Fields(t)
	if len(fields) == 0 || len(fields) > 2 {
		return false
	}
	if strings.Contains(t, "iphone") || strings.Contains(t, "samsung") || strings.Contains(t, "galaxy") ||
		strings.Contains(t, "pixel") || strings.Contains(t, "infinix") || strings.Contains(t, "tecno") {
		return false
	}
	for _, f := range fields {
		ff := strings.Trim(strings.ToLower(f), ".,!?()[]{}")
		if strings.HasSuffix(ff, "gb") || ff == "128" || ff == "256" || ff == "512" || ff == "1tb" {
			return true
		}
	}
	return false
}

func isVariantOnlyMessage(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "iphone") || strings.Contains(msg, "samsung") || strings.Contains(msg, "galaxy") ||
		strings.Contains(msg, "pixel") || strings.Contains(msg, "infinix") || strings.Contains(msg, "tecno") {
		return false
	}
	variantRegex := regexp.MustCompile(`\b(64|128|256|512)\s*gb\b|\b1tb\b`)
	return variantRegex.MatchString(msg)
}

func isCapacityToken(token string) bool {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return false
	}
	if strings.HasSuffix(t, "gb") || t == "1tb" {
		return true
	}
	if t == "64" || t == "128" || t == "256" || t == "512" {
		return true
	}
	return false
}

func messageMentionsTargetModel(message string, target string) bool {
	msgNorm := normalizeText(message)
	if msgNorm == "" {
		return false
	}
	for _, tok := range tokenizeText(target) {
		if len(tok) < 4 || isCapacityToken(tok) {
			continue
		}
		if strings.Contains(msgNorm, tok) {
			return true
		}
	}
	return false
}

func shouldSkipFocusOverrideForAmbiguousVariantQuery(message string, currentFocus string, newTarget string) bool {
	if strings.TrimSpace(currentFocus) == "" || strings.TrimSpace(newTarget) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(currentFocus), strings.TrimSpace(newTarget)) {
		return false
	}
	if !isVariantOnlyMessage(message) {
		return false
	}
	return !messageMentionsTargetModel(message, newTarget)
}

func buildEmptyReplyFallback(normalizedIntent string, isCatalog bool, focusItem string, activeItem string, messageText string) string {
	if normalizedIntent == "negotiation_offer" || normalizedIntent == "price_objection" {
		if strings.TrimSpace(focusItem) != "" {
			return fmt.Sprintf("Noted your budget. Is this for %s? I can check the best possible price for you.", focusItem)
		}
		if strings.TrimSpace(activeItem) != "" {
			return fmt.Sprintf("Noted your budget. Is this for %s? I can check the best possible price for you.", activeItem)
		}
		return "Noted your budget. Which specific item is this for so I can check the best possible price?"
	}
	if isCatalog {
		return "I see you're asking about our prices/menu. Could you specify which item you're interested in?"
	}
	if strings.Contains(messageText, "?") {
		return "Could you please clarify your question? I want to make sure I understand correctly."
	}
	return "I didn't quite catch that. How can we help you today?"
}

func (r *FlowRunner) renderSwitchRequiredReply(ctx context.Context, businessName string, current string, requested string) string {
	fallback := buildSwitchRequiredReply(current, requested)
	if r.deps.Python == nil {
		return fallback
	}

	reply, err := r.deps.Python.GenerateGuardrailReply(ctx, python.GuardrailReplyRequest{
		Reason:        "switch_required",
		BusinessName:  strings.TrimSpace(businessName),
		CurrentItem:   strings.TrimSpace(current),
		RequestedItem: strings.TrimSpace(requested),
	})
	if err != nil {
		if r.deps.Logger != nil {
			r.deps.Logger.Warn("render_switch_required_reply_failed", zap.Error(err))
		}
		return fallback
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		return fallback
	}
	return reply
}

// RunTurn executes one complete AI turn in a fixed, deterministic order.
func (r *FlowRunner) RunTurn(
	ctx context.Context,
	req *types.AIRequest,
) (response *types.AIResponse, runErr error) {
	// 🔧 PANIC RECOVERY: Prevent crashes from nil pointer panics
	defer func() {
		if rec := recover(); rec != nil {
			var convID string
			if req != nil && req.Scope.ConversationID != (uuid.UUID{}) {
				convID = req.Scope.ConversationID.String()
			}
			r.deps.Logger.Error("panic_recovered_in_run_turn",
				zap.Any("panic", rec),
				zap.String("conversation_id", convID))
			runErr = fmt.Errorf("internal error: recovered from panic: %v", rec)
			response = nil
		}
	}()

	if req == nil {
		return nil, errors.New("request cannot be nil")
	}
	log := r.deps.Logger.With(zap.String("conversation_id", req.Scope.ConversationID.String()))
	log.Debug("run_turn_started")

	// 1. Memory Load
	convMem, opMem, err := r.deps.Memory.LoadAll(ctx, req.Scope)
	if err != nil {
		return nil, err
	}

	// 2. Context Snapshot
	snapshot, err := r.deps.Context.BuildSnapshot(ctx, req, convMem, opMem)
	if err != nil {
		return nil, err
	}

	// 3. Knowledge Load (Required)
	knowledge, err := loader.LoadTenantKnowledge(ctx, r.deps.DB, req.Scope.TenantID)
	if err != nil {
		return nil, err
	}
	snapshot.TenantKnowledge = knowledge

	// 4. Conversation Snapshot
	convSnapshot, err := loader.LoadConversationSnapshot(ctx, r.deps.DB, req.Scope.ConversationID.String())
	if err != nil {
		log.Error("conversation_snapshot_load_failed", zap.Error(err))
		convSnapshot = v4ctx.DefaultConversationSnapshot()
	}

	// 5. Order Snapshot
	orderSnapshot, err := loader.LoadOrderSnapshot(ctx, r.deps.DB, req.Scope.ConversationID.String())
	if err != nil {
		log.Error("order_snapshot_load_failed", zap.Error(err))
		orderSnapshot = &v4ctx.OrderSnapshot{Stage: "collecting", Items: []v4ctx.OrderItem{}}
	}

	log.Info("context_loaded",
		zap.Int("items", len(knowledge.MenuItems)),
		zap.String("current_order_stage", orderSnapshot.Stage))

	if strings.TrimSpace(convSnapshot.CloseoutState) == "" {
		convSnapshot.CloseoutState = "open"
	}
	if closeoutSuppressionEnabled() && convSnapshot.CloseoutState == "closed" && isReopenBuyingIntent(req.MessageText) {
		convSnapshot.CloseoutState = "open"
	}

	// =================================================================================
	// PYTHON BRAIN (Authority for Understanding & Flow)
	// =================================================================================

	prunedKnowledge := pruneKnowledgeForContext(knowledge, convSnapshot.LastDomain, convSnapshot.ConversationStage, knowledge.Industry)

	// 🆕 CATALOG QUERY ANALYSIS (Step 4)
	// We determine if user is browsing or checking details in Go (Authority).
	unifiedOfferings := catalog.UnifyOfferings(knowledge)
	catalogMode := catalog.DetectCatalogQueryMode(req.MessageText, unifiedOfferings)

	// Resolve Kind from Industry string (TenantKnowledge flattened structure)
	catalogKind := types.CatalogKindFromIndustry(knowledge.Industry)

	// Prefer explicit item mentions from the current message, even in browse mode.
	// This prevents stale ActiveItem from leaking into a new catalog question.
	focusItem := catalog.ResolveFocusedItem(req.MessageText, unifiedOfferings)
	focusSetBy := "go"
	guardSet := map[string]struct{}{}
	addGuard := func(name string) {
		n := strings.TrimSpace(name)
		if n == "" {
			return
		}
		guardSet[n] = struct{}{}
	}
	formatGuards := func() string {
		if len(guardSet) == 0 {
			return "none"
		}
		names := make([]string, 0, len(guardSet))
		for k := range guardSet {
			names = append(names, k)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}

	// 🆕 VARIANT FOLLOW-UP: Handle follow-ups like "the 256GB" after we showed variants.
	// This must run BEFORE sticky ActiveItem fallback to avoid leaking the previous variant.
	if focusItem == "" {
		if v := resolveVariantFocusFromMessage(req.MessageText, convSnapshot.ActiveItem, convSnapshot.LastAIReply, knowledge); strings.TrimSpace(v) != "" {
			focusItem = v
			log.Info("variant_focus_resolved", zap.String("focus", focusItem))
			focusSetBy = "go"
		}
	}

	// 🆕 STICKY FOCUS: If no new item mentioned, keep talking about the previous one
	if focusItem == "" {
		focusItem = convSnapshot.ActiveItem
	}
	if focusItem == "" {
		focusItem = inferFocusFromLastReply(req.MessageText, convSnapshot.LastAIReply, knowledge)
	}
	if strings.TrimSpace(convSnapshot.ActiveItem) != "" &&
		strings.TrimSpace(focusItem) != "" &&
		isGenericFocusLabel(focusItem) &&
		!messageMentionsFocus(req.MessageText, focusItem) {
		log.Info("preserving_active_item_over_generic_focus",
			zap.String("active_item", convSnapshot.ActiveItem),
			zap.String("generic_focus", focusItem))
		focusItem = convSnapshot.ActiveItem
		focusSetBy = "go"
	}

	log.Info("Catalog resolved",
		zap.String("kind", string(catalogKind)),
		zap.String("mode", string(catalogMode)),
		zap.String("focus", focusItem),
	)

	// 🆕 FULFILLMENT CHECK (Step 5)
	var fulfillmentTask *python.FulfillmentTask
	var activeReq *storage.Requirement
	var activeState *storage.FulfillmentState
	var fulfillmentFollowUp string
	var fulfillmentPrimaryReply string
	var fulfillmentCompletedThisTurn bool

	// User-First: Check for questions using expanded Nigerian patterns
	looksLikeQuestion := func(s string) bool {
		return looksLikeQuestionStandalone(s)
	}

	looksLikeNonAnswer := func(s string) bool {
		t := strings.TrimSpace(s)
		if t == "" {
			return true
		}
		lower := strings.ToLower(t)
		lower = strings.TrimSpace(lower)
		// Very short acknowledgements / greetings that should not advance a form-like flow.
		if len(lower) <= 20 {
			simple := map[string]bool{
				"hi": true, "hello": true, "hey": true,
				"ok": true, "okay": true, "k": true,
				"alright": true, "alrighty": true,
				"sure": true, "cool": true,
				"thanks": true, "thank you": true, "thankyou": true,
				"noted":       true,
				"i have paid": true, "paid": true, "payment made": true, "transfer done": true,
			}
			if simple[lower] {
				return true
			}
			if strings.HasPrefix(lower, "hi ") || strings.HasPrefix(lower, "hello ") || strings.HasPrefix(lower, "hey ") {
				return true
			}
			if strings.HasPrefix(lower, "good morning") || strings.HasPrefix(lower, "good afternoon") || strings.HasPrefix(lower, "good evening") {
				return true
			}
		}
		return false
	}

	lastAIWasFulfillmentPrompt := func() bool {
		last := strings.ToLower(strings.TrimSpace(convSnapshot.LastAIReply))
		if last == "" || activeReq == nil {
			return false
		}
		reqLabel := strings.ToLower(strings.TrimSpace(activeReq.Label))
		if reqLabel != "" && strings.Contains(last, reqLabel) {
			return true
		}
		// Fallback to common phrasing used by our fulfillment prompts.
		if strings.Contains(last, "to complete your") && strings.Contains(last, "please send") {
			return true
		}
		if strings.Contains(last, "next, to complete your") && strings.Contains(last, "please send") {
			return true
		}
		return false
	}

	getFulfillmentLabel := func() string {
		label := "service request"
		var tenant storage.Tenant
		if err := r.deps.DB.First(&tenant, req.Scope.TenantID).Error; err == nil {
			var industry storage.Industry
			if err := r.deps.DB.First(&industry, tenant.IndustryID).Error; err == nil {
				if !industry.HasServices {
					label = "order"
				}
			}
		}
		return label
	}

	resolveFulfillmentItemName := func(orderItemID string) string {
		id := strings.TrimSpace(orderItemID)
		if id == "" {
			return ""
		}
		if parsed, err := uuid.Parse(id); err == nil {
			var prod storage.Product
			if err := r.deps.DB.Select("name").Where("id = ? AND tenant_id = ?", parsed, req.Scope.TenantID).First(&prod).Error; err == nil && strings.TrimSpace(prod.Name) != "" {
				return strings.TrimSpace(prod.Name)
			}
			var menu storage.MenuItem
			if err := r.deps.DB.Select("name").Where("id = ? AND tenant_id = ?", parsed, req.Scope.TenantID).First(&menu).Error; err == nil && strings.TrimSpace(menu.Name) != "" {
				return strings.TrimSpace(menu.Name)
			}
			var svc storage.Service
			if err := r.deps.DB.Select("name").Where("id = ? AND tenant_id = ?", parsed, req.Scope.TenantID).First(&svc).Error; err == nil && strings.TrimSpace(svc.Name) != "" {
				return strings.TrimSpace(svc.Name)
			}
		}
		return ""
	}

	fileMediaURL := ""
	fileMediaStoredURL := ""
	fileMediaType := ""
	fileFilename := ""
	fileCaptionFromMeta := ""
	hasFileMedia := false
	fulfillmentItemName := ""
	if r.deps.Fulfillment != nil {
		// populated after activeReq is known
	}

	if r.deps.Fulfillment != nil {
		// 🔧 FIX: Renamed from 'req' to 'pendingReq' to avoid shadowing function parameter
		pendingReq, state, err := r.deps.Fulfillment.GetPendingRequirementForConversation(req.Scope.TenantID, req.Scope.ConversationID)
		if err != nil {
			log.Warn("fulfillment_check_failed", zap.Error(err))
		} else if pendingReq != nil && state != nil {
			log.Info("fulfillment_task_found", zap.String("requirement", pendingReq.Label))
			activeReq = pendingReq
			activeState = state
			if orderSnapshot != nil {
				// Lock stage during requirement collection so Python updates cannot regress flow.
				orderSnapshot.Stage = "collecting_requirements"
			}
			fulfillmentItemName = resolveFulfillmentItemName(state.OrderItemID)
			fulfillmentTask = &python.FulfillmentTask{
				TargetItem:       strings.TrimSpace(fulfillmentItemName),
				RequirementLabel: pendingReq.Label,
				FieldType:        pendingReq.DataType,
				Instruction:      fmt.Sprintf("Please ask the customer for: %s. %s", pendingReq.Label, pendingReq.Description),
			}
			if strings.TrimSpace(fulfillmentTask.TargetItem) == "" {
				fulfillmentTask.TargetItem = state.OrderItemID
			}
		}
	}

	// Deterministic "last order" reply (no LLM needed). This must take precedence over catalog routing.
	// Only enable when we are not in active order/fulfillment flows.
	precomputedLastOrderReply := ""
	if activeReq == nil && activeState == nil &&
		(orderSnapshot == nil || !orderSnapshot.HasActiveOrder || len(orderSnapshot.Items) == 0) &&
		isLastOrderRequest(req.MessageText) {
		if msg, _ := r.buildLastOrderReply(ctx, req.Scope.TenantID, req.Scope.ConversationID); strings.TrimSpace(msg) != "" {
			precomputedLastOrderReply = strings.TrimSpace(msg)
		}
	}

	// ---------------------------------------------------------------------------------
	// Optional token optimization: allow bypassing Python AnalyzeTurn in deterministic stages.
	// Default is OFF because many tenants expect Python's reply to be what gets sent.
	// ---------------------------------------------------------------------------------
	pythonDecisionAuthority := strings.ToLower(strings.TrimSpace(os.Getenv("AI_V4_PYTHON_AUTHORITY"))) != "false"
	allowBypassPythonAnalyze := strings.ToLower(strings.TrimSpace(os.Getenv("AI_V4_BYPASS_PYTHON_ANALYZE"))) == "true"
	if pythonDecisionAuthority {
		allowBypassPythonAnalyze = false
	}
	shouldBypassPythonAnalyze := false
	if strings.TrimSpace(precomputedLastOrderReply) != "" {
		shouldBypassPythonAnalyze = true
	} else if allowBypassPythonAnalyze && strings.TrimSpace(req.MessageText) != "" && !looksLikeQuestion(req.MessageText) {
		orderFlowActive := orderSnapshot != nil &&
			orderSnapshot.HasActiveOrder &&
			len(orderSnapshot.Items) > 0 &&
			isActiveOrderFlowStage(orderSnapshot.Stage)
		fulfillmentFlowActive := activeReq != nil && activeState != nil
		shouldBypassPythonAnalyze = orderFlowActive || fulfillmentFlowActive
	}

	var analysis *python.AnalyzeTurnResponse
	var negotiationCtx *python.NegotiationContext
	if r.deps.DB != nil {
		var latestSalesConv storage.SalesConversation
		err := r.deps.DB.
			Where("conversation_id = ? AND tenant_id = ?", req.Scope.ConversationID, req.Scope.TenantID).
			Order("updated_at desc").
			First(&latestSalesConv).Error
		if err == nil {
			negotiationCtx = &python.NegotiationContext{
				IsActive:          strings.EqualFold(strings.TrimSpace(latestSalesConv.Stage), "negotiation") || latestSalesConv.EscalationPending,
				NegotiationCount:  latestSalesConv.NegotiationCount,
				LastAIOffer:       latestSalesConv.LastAIOffer,
				LastCustomerOffer: latestSalesConv.LastOfferAmount,
				EscalationPending: latestSalesConv.EscalationPending,
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warn("negotiation_context_load_failed", zap.Error(err))
		}
	}

	if shouldBypassPythonAnalyze {
		analysis = r.deps.Python.GenerateFallbackResponse(req.MessageText, knowledge.BusinessName)
		analysis.Decision.Domain = "commerce"
		analysis.Decision.Stage = strings.TrimSpace(orderSnapshot.Stage)
		analysis.Semantic.CancelIntent = customerWantsToCancel(req.MessageText)
		log.Info("python_analyze_turn_bypassed",
			zap.String("order_stage", strings.TrimSpace(orderSnapshot.Stage)),
			zap.Bool("fulfillment_active", activeReq != nil && activeState != nil),
		)
	} else {
		analysis, err = r.deps.Python.AnalyzeTurn(ctx, &python.AnalyzeTurnRequest{
			TenantID:           req.Scope.TenantID,
			Message:            req.MessageText,
			TenantKnowledge:    prunedKnowledge,
			Conversation:       convSnapshot,
			Order:              orderSnapshot,
			FulfillmentTask:    fulfillmentTask, // Pass to Python
			CatalogKind:        string(catalogKind),
			CatalogMode:        string(catalogMode),
			FocusItem:          focusItem,
			NegotiationContext: negotiationCtx,
		})
		if err != nil {
			return nil, err
		}
	}
	analysisReplyNegotiation := strings.EqualFold(strings.TrimSpace(analysis.Reply.Mode), "python_negotiation")
	if analysisReplyNegotiation {
		log.Info("python_negotiation_mode_detected", zap.String("stage", analysis.Decision.Stage))
	}
	if pythonDecisionAuthority {
		if stageRaw, ok := analysis.StateUpdates["conversation_state"]; ok {
			if stage, ok := stageRaw.(string); ok && strings.TrimSpace(stage) != "" {
				analysis.Decision.Stage = strings.TrimSpace(stage)
			}
		}
		if roundRaw, ok := analysis.StateUpdates["negotiation_round"]; ok {
			switch v := roundRaw.(type) {
			case float64:
				r := int(v)
				analysis.ConversationUpdate.NegotiationRoundUpdate = &r
			case int:
				r := v
				analysis.ConversationUpdate.NegotiationRoundUpdate = &r
			}
		}
	}

	requestItemID, requestVariantID, requestName := extractRequestedModelVariant(req.MessageText, knowledge)
	requestedSKU := skuRequest{
		ItemID:    strings.TrimSpace(requestItemID),
		VariantID: strings.TrimSpace(requestVariantID),
		Name:      strings.TrimSpace(requestName),
	}
	businessName := ""
	if knowledge != nil {
		businessName = knowledge.BusinessName
	}
	skuMutationIntent := analysis.Semantic.DetectedIntent == "order" ||
		analysis.Semantic.DetectedIntent == "purchase" ||
		analysis.Semantic.DetectedIntent == "confirm_order" ||
		analysis.Semantic.CustomerOffer > 0
	skuLockBlockedThisTurn := false

	if skuLockEnabled() &&
		orderSnapshot != nil &&
		isExplicitSwitchConfirmation(req.MessageText) &&
		strings.TrimSpace(requestedSKU.Name) != "" &&
		!isLockedSKUCompatible(orderSnapshot.ActiveItemID, orderSnapshot.ActiveVariantID, requestedSKU.ItemID, requestedSKU.VariantID) {
		confirmSwitchToRequested(orderSnapshot, requestedSKU)
		convSnapshot.ActiveItem = requestedSKU.Name
		focusItem = requestedSKU.Name
		focusSetBy = "go"
		log.Info("sku_switch_confirmed",
			zap.String("to", requestedSKU.Name))
		observability.IncSKUSwitchConfirmed()
	}

	if unavailableGuardEnabled() {
		if requested, alternatives, hit := detectUnavailableModelRequest(req.MessageText, knowledge); hit {
			log.Info("unavailable_model_guard_hit", zap.String("requested", requested))
			observability.IncUnavailableModelGuardHit()
			addGuard("unavailable_model_guard")
			finalReply := buildUnavailableWithAlternativesReply(requested, alternatives)

			convSnapshot.LastUserMessage = req.MessageText
			convSnapshot.LastAIReply = finalReply
			convSnapshot.LastDomain = "offerings"
			convSnapshot.ActiveItem = requested
			convSnapshot.TurnCount++

			stateBytes, _ := json.Marshal(convSnapshot)
			r.deps.DB.Model(&storage.Conversation{}).
				Where("id = ?", req.Scope.ConversationID).
				Update("ai_state", string(stateBytes))

			orderBytes, _ := json.Marshal(orderSnapshot)
			r.deps.DB.Model(&storage.Conversation{}).
				Where("id = ?", req.Scope.ConversationID).
				Update("ai_order_state", string(orderBytes))

			log.Info("authority_trace",
				zap.String("who_set_reply", "go"),
				zap.String("who_set_focus", focusSetBy),
				zap.String("who_blocked_action", "go_guard"),
				zap.String("guard_triggered", formatGuards()))
			return &types.AIResponse{
				ReplyText:       finalReply,
				ReplySegments:   []string{finalReply},
				ProposedActions: analysis.Actions,
			}, nil
		}
	}

	if activeReq != nil && activeState != nil && strings.EqualFold(strings.TrimSpace(activeReq.DataType), "file") {
		var latest storage.Interaction
		err := r.deps.DB.Where("tenant_id = ? AND conversation_id = ? AND platform = ? AND direction = ?",
			req.Scope.TenantID, req.Scope.ConversationID, req.Scope.Platform, "incoming").
			Order("created_at desc").First(&latest).Error
		if err == nil {
			var meta map[string]any
			if strings.TrimSpace(latest.Metadata) != "" {
				_ = json.Unmarshal([]byte(latest.Metadata), &meta)
			}

			fileMediaURL, _ = meta["media_url"].(string)
			fileMediaStoredURL, _ = meta["media_stored_url"].(string)
			fileMediaType, _ = meta["media_type"].(string)
			if m, ok := meta["media"].(map[string]any); ok {
				if fn, ok := m["filename"].(string); ok {
					fileFilename = fn
				}
				if cap, ok := m["caption"].(string); ok {
					fileCaptionFromMeta = cap
				}
			}
			if fileMediaURL == "" {
				fileMediaURL = fileMediaStoredURL
			}
			if strings.TrimSpace(fileMediaURL) != "" {
				hasFileMedia = true
			}
		}
	}

	// 🆕 FULFILLMENT SUBMISSION (Step 6)
	// Save when a fulfillment task is active and the message is likely the answer.
	shouldTreatAsSubmission := false
	if activeReq != nil && activeState != nil {
		if strings.EqualFold(strings.TrimSpace(activeReq.DataType), "file") {
			shouldTreatAsSubmission = hasFileMedia
		} else {
			// Use Python's fulfillment intent as a signal, but guard against obvious non-answers.
			pythonSaysSubmission := strings.EqualFold(strings.TrimSpace(analysis.Semantic.DetectedIntent), "fulfillment_submission")
			if looksLikeNonAnswer(req.MessageText) {
				shouldTreatAsSubmission = false
			} else {
				// Save deterministically while requirement flow is active:
				// any non-question, non-ack now counts as a requirement answer.
				shouldTreatAsSubmission = pythonSaysSubmission || !looksLikeQuestion(req.MessageText) || lastAIWasFulfillmentPrompt()
			}
		}
	}

	if activeReq != nil && activeState != nil && !shouldTreatAsSubmission {
		label := getFulfillmentLabel()
		itemCtx := strings.TrimSpace(fulfillmentItemName)
		msg := ""
		if itemCtx != "" {
			msg = fmt.Sprintf("To complete your %s for %s, please send: %s.", label, itemCtx, activeReq.Label)
		} else {
			msg = fmt.Sprintf("To complete your %s, please send: %s.", label, activeReq.Label)
		}
		if strings.TrimSpace(activeReq.Description) != "" {
			msg += " " + strings.TrimSpace(activeReq.Description)
		}
		if looksLikeNonAnswer(req.MessageText) {
			// They came back later / sent an acknowledgement; resume directly from where they stopped.
			fulfillmentPrimaryReply = msg
			fulfillmentFollowUp = ""
		} else if strings.EqualFold(strings.TrimSpace(activeReq.DataType), "file") && !hasFileMedia {
			// If it's a file requirement and the customer didn't send media, override the reply
			// so we don't derail into unrelated conversation.
			fulfillmentPrimaryReply = msg
			fulfillmentFollowUp = ""
		} else if strings.TrimSpace(fulfillmentFollowUp) == "" {
			// Otherwise, append the reminder after answering their clarification.
			fulfillmentFollowUp = msg
		}
	}

	if activeReq != nil && activeState != nil && shouldTreatAsSubmission {
		log.Info("fulfillment_submission_detected", zap.String("value_length", fmt.Sprintf("%d", len(req.MessageText))))

		valueToSave := strings.TrimSpace(req.MessageText)
		saved := false

		// For file requirements, save media URL/id + caption from latest incoming interaction metadata.
		if strings.EqualFold(strings.TrimSpace(activeReq.DataType), "file") {
			if !hasFileMedia {
				log.Info("fulfillment_file_missing_media_url")
				valueToSave = ""
			} else {
				sub := map[string]any{
					"media_type":       strings.TrimSpace(fileMediaType),
					"media_url":        strings.TrimSpace(fileMediaURL),
					"media_stored_url": strings.TrimSpace(fileMediaStoredURL),
					"caption":          valueToSave,
				}
				if strings.TrimSpace(fileFilename) != "" {
					sub["filename"] = strings.TrimSpace(fileFilename)
				}
				if strings.TrimSpace(fileCaptionFromMeta) != "" {
					sub["caption_meta"] = strings.TrimSpace(fileCaptionFromMeta)
				}
				if b, mErr := json.Marshal(sub); mErr == nil {
					valueToSave = string(b)
				}
			}
		}

		if strings.TrimSpace(valueToSave) != "" {
			if err := r.deps.Fulfillment.SubmitRequirement(activeState.ID, activeReq.ID, valueToSave); err != nil {
				log.Error("failed_to_save_fulfillment_submission", zap.Error(err))
				// Don't fail the turn, just log. AI might ask again next time since it wasn't saved.
			} else {
				saved = true
				log.Info("✅ Fulfillment Requirement Saved", zap.String("label", activeReq.Label))
			}
		}

		if saved {
			nextReq, _, nErr := r.deps.Fulfillment.GetPendingRequirementForConversation(req.Scope.TenantID, req.Scope.ConversationID)
			if nErr != nil {
				log.Warn("failed_to_get_next_requirement", zap.Error(nErr))
			} else if nextReq != nil {
				label := getFulfillmentLabel()
				nextItem := strings.TrimSpace(fulfillmentItemName)
				msg := ""
				if nextItem != "" {
					msg = fmt.Sprintf("Next, to complete your %s for %s, please send: %s.", label, nextItem, nextReq.Label)
				} else {
					msg = fmt.Sprintf("Next, to complete your %s, please send: %s.", label, nextReq.Label)
				}
				if strings.TrimSpace(nextReq.Description) != "" {
					msg += " " + strings.TrimSpace(nextReq.Description)
				}
				fulfillmentPrimaryReply = "Thanks, got it."
				fulfillmentFollowUp = msg
			} else {
				fulfillmentPrimaryReply = "Thanks, got it."
				// Requirements are complete now; continue normal order flow in this same turn.
				fulfillmentCompletedThisTurn = true
				activeReq = nil
				if orderSnapshot != nil {
					orderSnapshot.Stage = "payment_ready"
					orderSnapshot.LastPromptKey = ""
				}
				fulfillmentFollowUp = ""
			}
		}
	}

	// (Note) File missing-media prompting handled above in the sticky reminder block.

	log.Info("python_semantic_extraction",
		zap.Int("items_extracted", len(analysis.Semantic.Items)),
		zap.Int("ambiguities", len(analysis.Semantic.Ambiguities)))

	// 🚫 DEPRECATED: hasExplicitOrderSignal - replaced by LLM buying_confidence
	// Kept for reference only. The LLM now semantically determines buying readiness.
	// hasExplicitOrderSignal := func(s string) bool {
	// 	t := strings.ToLower(strings.TrimSpace(s))
	// 	if t == "" {
	// 		return false
	// 	}
	// 	orderPhrases := []string{
	// 		"i want",
	// 		"i'd like",
	// 		"i would like",
	// 		"i need",
	// 		"i'll take",
	// 		"i will take",
	// 		"order ",
	// 		"place an order",
	// 		"buy ",
	// 		"purchase ",
	// 		"sign me up",
	// 		"subscribe",
	// 	}
	// 	for _, p := range orderPhrases {
	// 		if strings.Contains(t, p) {
	// 			return true
	// 		}
	// 	}
	// 	return false
	// }

	// 🆕 CONTEXT INFERENCE BRIDGE: If Go failed to resolve focus, but Python found it (via context), use it.
	// If Python extracted a target item from the user's message, let it override stale sticky focus.
	// This prevents deterministic fallbacks from answering for the previous ActiveItem.
	if strings.TrimSpace(analysis.Semantic.TargetItem) != "" {
		if strings.TrimSpace(focusItem) != "" && isVariantOnlyTarget(analysis.Semantic.TargetItem) {
			log.Info("focus_item_override_skipped_variant_only_target",
				zap.String("target", analysis.Semantic.TargetItem),
				zap.String("focus", focusItem))
			addGuard("variant_target_focus_lock")
		} else if strings.TrimSpace(focusItem) != "" &&
			!strings.EqualFold(strings.TrimSpace(analysis.Semantic.TargetItem), strings.TrimSpace(focusItem)) &&
			shouldSkipFocusOverrideForAmbiguousVariantQuery(req.MessageText, focusItem, analysis.Semantic.TargetItem) {
			log.Info("focus_item_override_skipped_ambiguous_variant_query",
				zap.String("target", analysis.Semantic.TargetItem),
				zap.String("focus", focusItem))
			addGuard("ambiguous_variant_focus_lock")
		} else if len(analysis.Semantic.Items) > 0 || messageMentionsFocus(req.MessageText, analysis.Semantic.TargetItem) {
			if m := catalog.ResolveLongestMatch(analysis.Semantic.TargetItem, knowledge); m != nil && strings.TrimSpace(m.Name) != "" {
				if !strings.EqualFold(strings.TrimSpace(focusItem), strings.TrimSpace(m.Name)) {
					log.Info("focus_item_overridden_from_python_target",
						zap.String("previous_focus", focusItem),
						zap.String("focus", m.Name))
				}
				focusItem = m.Name
				focusSetBy = "python"
			} else if focusItem == "" {
				focusItem = analysis.Semantic.TargetItem
				log.Info("focus_item_updated_from_python_context", zap.String("focus", focusItem))
				focusSetBy = "python"
			}
		}
	}

	// =================================================================================
	// GO AUTHORITY: Minimal Catalog Resolution (No Go-side negotiation/offer logic)
	// =================================================================================
	orderingFlowLocked := orderSnapshot != nil && isActiveOrderFlowStage(orderSnapshot.Stage)
	resolvedItems := []v4ctx.OrderItem{}

	if len(analysis.Semantic.Items) > 0 && !orderingFlowLocked {
		for _, semanticItem := range analysis.Semantic.Items {
			catalogItem := catalog.ResolveLongestMatch(semanticItem.Raw, knowledge)
			if catalogItem == nil {
				log.Warn("semantic_item_not_in_catalog", zap.String("raw", semanticItem.Raw))
				continue
			}

			resolvedItems = append(resolvedItems, v4ctx.OrderItem{
				Name:      catalogItem.Name,
				Quantity:  semanticItem.Quantity,
				UnitPrice: int64(catalogItem.Price),
			})

			log.Info("catalog_match",
				zap.String("raw", semanticItem.Raw),
				zap.String("resolved", catalogItem.Name),
				zap.Int("quantity_from_python", semanticItem.Quantity),
				zap.Float64("price_from_db", catalogItem.Price))
		}

		if len(resolvedItems) > 0 {
			if pythonDecisionAuthority || len(analysis.Actions) > 0 {
				log.Info("python_authority_skipping_go_order_inference",
					zap.Int("resolved_items", len(resolvedItems)),
					zap.Int("actions", len(analysis.Actions)))
				analysis.Semantic.Items = nil
			} else {
				isBrowsing := isCatalogIntent(analysis.Semantic.DetectedIntent)
				isExplicitOrderIntent := analysis.Semantic.DetectedIntent == "order" ||
					analysis.Semantic.DetectedIntent == "purchase" ||
					analysis.Semantic.DetectedIntent == "confirm_order"
				hasHighBuyingConfidence := analysis.Semantic.BuyingConfidence >= buyingConfidenceThreshold
				shouldStartOrder := (convSnapshot != nil && convSnapshot.ConversationMode == "ordering") || isExplicitOrderIntent || hasHighBuyingConfidence

				if !isBrowsing && shouldStartOrder && !looksLikeQuestion(req.MessageText) {
					if orderSnapshot == nil {
						orderSnapshot = &v4ctx.OrderSnapshot{
							Stage: "collecting",
							Items: []v4ctx.OrderItem{},
						}
					}
					orderSnapshot.Items = resolvedItems
					orderSnapshot.HasActiveOrder = true
					orderSnapshot.Stage = "collecting"
					convSnapshot.ConversationMode = "ordering"

					total := computeOrderTotalFromSnapshotItems(resolvedItems)
					currency := "NGN"
					if len(resolvedItems) > 0 {
						for _, p := range knowledge.Products {
							if strings.EqualFold(p.Name, resolvedItems[0].Name) && strings.TrimSpace(p.Currency) != "" {
								currency = p.Currency
								break
							}
						}
						for _, m := range knowledge.MenuItems {
							if strings.EqualFold(m.Name, resolvedItems[0].Name) && strings.TrimSpace(m.Currency) != "" {
								currency = m.Currency
								break
							}
						}
					}
					orderSnapshot.TotalPrice = total
					orderSnapshot.Currency = currency
					if len(orderSnapshot.Items) > 0 {
						applyActiveSKULock(orderSnapshot, orderSnapshot.Items[0].Name, orderSnapshot.Items[0].UnitPrice)
					}

					log.Info("order_resolved_by_go_legacy_mode",
						zap.Int("items", len(resolvedItems)),
						zap.Int64("total", total))
				} else {
					analysis.Semantic.Items = nil
					log.Info("skipping_go_order_inference",
						zap.String("intent", analysis.Semantic.DetectedIntent),
						zap.Bool("is_browsing", isBrowsing),
						zap.Bool("should_start_order", shouldStartOrder),
						zap.Bool("looks_like_question", looksLikeQuestion(req.MessageText)),
						zap.Int("resolved_items", len(resolvedItems)))
				}
			}
		}
	}

	if skuLockEnabled() {
		currentLockedName := strings.TrimSpace(convSnapshot.ActiveItem)
		if currentLockedName == "" && orderSnapshot != nil && len(orderSnapshot.Items) > 0 {
			currentLockedName = strings.TrimSpace(orderSnapshot.Items[0].Name)
		}
		hasLock := orderSnapshot != nil &&
			strings.TrimSpace(orderSnapshot.ActiveItemID) != "" &&
			strings.TrimSpace(orderSnapshot.ActiveVariantID) != ""

		if hasLock && !isLockedSKUCompatible(orderSnapshot.ActiveItemID, orderSnapshot.ActiveVariantID, requestedSKU.ItemID, requestedSKU.VariantID) {
			if isExplicitSwitchConfirmation(req.MessageText) && strings.TrimSpace(requestedSKU.Name) != "" {
				confirmSwitchToRequested(orderSnapshot, requestedSKU)
				convSnapshot.ActiveItem = requestedSKU.Name
				focusItem = requestedSKU.Name
				focusSetBy = "go"
				log.Info("sku_switch_confirmed",
					zap.String("from", currentLockedName),
					zap.String("to", requestedSKU.Name))
				observability.IncSKUSwitchConfirmed()
			} else if skuMutationIntent {
				analysis.Reply.Text = r.renderSwitchRequiredReply(ctx, businessName, currentLockedName, requestedSKU.Name)
				orderSnapshot.PendingSwitchItemID = requestedSKU.ItemID
				orderSnapshot.PendingSwitchVariantID = requestedSKU.VariantID
				skuLockBlockedThisTurn = true
				addGuard("sku_lock")
				log.Info("sku_lock_blocked",
					zap.String("locked_item_id", orderSnapshot.ActiveItemID),
					zap.String("locked_variant_id", orderSnapshot.ActiveVariantID),
					zap.String("requested_item_id", requestedSKU.ItemID),
					zap.String("requested_variant_id", requestedSKU.VariantID))
				observability.IncSKULockBlocked()
			}
		}
	}

	// =================================================================================
	// GO AUTHORITY (State Guards & Calculations)
	// =================================================================================

	// 1. Order Logic Authorities
	if analysis.OrderUpdate != nil {
		// When fulfillment requirements are active, keep the order state pinned to requirement collection.
		if activeReq != nil && activeState != nil {
			analysis.OrderUpdate.Stage = "collecting_requirements"
			if orderSnapshot != nil && strings.TrimSpace(orderSnapshot.DeliveryMethod) != "" {
				analysis.OrderUpdate.DeliveryMethod = orderSnapshot.DeliveryMethod
			}
		}

		// A. CALCULATE TOTALS (Go is the Bank)
		// We trust Python's "Items" list (structure), but we verify the PRICES.
		newTotal, newCurrency := recalculateOrderTotal(analysis.OrderUpdate.Items, knowledge)
		analysis.OrderUpdate.TotalPrice = newTotal
		analysis.OrderUpdate.Currency = newCurrency

		// B. STATE REGRESSION GUARDRAIL
		// Never allow a proposed order state to move backwards once the flow has progressed.
		currentRank := orderStageRank(orderSnapshot.Stage)
		proposedRank := orderStageRank(analysis.OrderUpdate.Stage)
		isRegression := currentRank > 0 && proposedRank > 0 && proposedRank < currentRank

		if isRegression {
			log.Warn("order_state_regression_blocked",
				zap.String("current", orderSnapshot.Stage),
				zap.String("proposed", analysis.OrderUpdate.Stage))
			observability.IncStageRegressionBlocked()
			addGuard("order_state_regression")

			analysis.OrderUpdate.Stage = orderSnapshot.Stage
		}

		// Preserve known delivery method when Python omits it.
		if orderSnapshot != nil && strings.TrimSpace(orderSnapshot.DeliveryMethod) != "" && strings.TrimSpace(analysis.OrderUpdate.DeliveryMethod) == "" {
			analysis.OrderUpdate.DeliveryMethod = orderSnapshot.DeliveryMethod
		}

		// C. PERSIST ORDER STATE
		orderBytes, _ := json.Marshal(analysis.OrderUpdate)
		if dbErr := r.deps.DB.Model(&storage.Conversation{}).
			Where("id = ?", req.Scope.ConversationID).
			Update("ai_order_state", string(orderBytes)).Error; dbErr != nil {
			log.Error("persist_order_failed", zap.Error(dbErr))
		}
	}

	// =================================================================================
	// GO REPLY GENERATION (Authority for Customer-Facing Text)
	// =================================================================================
	if len(analysis.Actions) > 0 {
		log.Info("executing_proposed_actions",
			zap.Int("count", len(analysis.Actions)),
			zap.Bool("python_authority", pythonDecisionAuthority))
		err := r.executeProposedActions(ctx, analysis.Actions, req.Scope, snapshot, knowledge, orderSnapshot, convSnapshot)
		if err != nil {
			log.Error("action_execution_failed", zap.Error(err))
		}
	}

	var finalReply string
	fulfillmentActive := activeReq != nil && activeState != nil

	// If fulfillment logic produced a primary reply (e.g. submission confirmation or file-needed prompt),
	// use it as the authoritative reply and let fulfillmentFollowUp append the next question.
	if strings.TrimSpace(fulfillmentPrimaryReply) != "" {
		finalReply = strings.TrimSpace(fulfillmentPrimaryReply)
	}

	// If we computed a deterministic last-order reply, use it as authoritative for this turn.
	if strings.TrimSpace(finalReply) == "" && !fulfillmentActive && strings.TrimSpace(precomputedLastOrderReply) != "" {
		finalReply = strings.TrimSpace(precomputedLastOrderReply)
		convSnapshot.ConversationMode = "general"
	}

	// USE ORDER FLOW if:
	// 1. ConversationMode is "ordering" (customer already in order flow), OR
	// 2. New order items were just detected
	inOrderingMode := convSnapshot.ConversationMode == "ordering" ||
		(orderSnapshot != nil && orderSnapshot.HasActiveOrder && isActiveOrderFlowStage(orderSnapshot.Stage))
	if inOrderingMode && convSnapshot.ConversationMode != "ordering" {
		// Ensure v4 stays in structured order flow when snapshot stage is already in-flight.
		convSnapshot.ConversationMode = "ordering"
	}
	hasNewItems := orderSnapshot != nil && orderSnapshot.HasActiveOrder && len(orderSnapshot.Items) > 0

	// If we somehow ended up in ordering mode without an actual order, bail out deterministically.
	if convSnapshot.ConversationMode == "ordering" && (orderSnapshot == nil || !orderSnapshot.HasActiveOrder || len(orderSnapshot.Items) == 0) {
		convSnapshot.ConversationMode = "general"
		inOrderingMode = false
		hasNewItems = false
		if orderSnapshot != nil && len(orderSnapshot.Items) == 0 {
			orderSnapshot.HasActiveOrder = false
			if isActiveOrderFlowStage(orderSnapshot.Stage) {
				orderSnapshot.Stage = ""
			}
		}
		log.Info("exiting_order_mode_due_to_empty_order_snapshot")
	}

	// 🆕 Skip order flow if intent is payment_confirmation (use Python's natural confirmation instead)
	isPaymentConfirmation := analysis.Semantic.DetectedIntent == "payment_confirmation"

	// 🆕 EXIT ORDER MODE for LOW BUYING CONFIDENCE:
	// If customer is uncertain ("I want X but don't know which"), exit order mode
	// and let Python's conversational reply help them browse/decide (uses package-level constant)
	isUncertainCustomer := analysis.Semantic.BuyingConfidence > 0 && analysis.Semantic.BuyingConfidence < buyingConfidenceThreshold
	isProgressedOrder := orderSnapshot != nil && (orderSnapshot.HasActiveOrder || isProgressedOrderStage(orderSnapshot.Stage))
	if isUncertainCustomer && inOrderingMode && !isProgressedOrder {
		log.Info("🔓 Exiting ORDER MODE - customer is uncertain, switching to browse mode",
			zap.Float64("buying_confidence", analysis.Semantic.BuyingConfidence),
			zap.Float64("threshold", buyingConfidenceThreshold))
		convSnapshot.ConversationMode = "general"
		inOrderingMode = false
		// Also clear the active order since customer is browsing, not ordering
		if orderSnapshot != nil {
			orderSnapshot.HasActiveOrder = false
			orderSnapshot.Items = nil
		}
		// CRITICAL: Recalculate hasNewItems after clearing order
		hasNewItems = false
	}

	if strings.TrimSpace(finalReply) == "" && !fulfillmentActive && !inOrderingMode && !hasNewItems {
		if skuLockEnabled() && orderSnapshot != nil &&
			!skuLockBlockedThisTurn &&
			strings.TrimSpace(orderSnapshot.ActiveItemID) != "" &&
			strings.TrimSpace(orderSnapshot.ActiveVariantID) != "" &&
			!isLockedSKUCompatible(orderSnapshot.ActiveItemID, orderSnapshot.ActiveVariantID, requestedSKU.ItemID, requestedSKU.VariantID) &&
			!isExplicitSwitchConfirmation(req.MessageText) &&
			skuMutationIntent &&
			strings.TrimSpace(requestedSKU.Name) != "" {
			current := strings.TrimSpace(convSnapshot.ActiveItem)
			if current == "" && len(orderSnapshot.Items) > 0 {
				current = strings.TrimSpace(orderSnapshot.Items[0].Name)
			}
			finalReply = r.renderSwitchRequiredReply(ctx, businessName, current, requestedSKU.Name)
			orderSnapshot.PendingSwitchItemID = requestedSKU.ItemID
			orderSnapshot.PendingSwitchVariantID = requestedSKU.VariantID
			addGuard("sku_lock")
			log.Info("sku_lock_blocked_availability", zap.String("requested", requestedSKU.Name))
			observability.IncSKULockBlocked()
		}
		if strings.TrimSpace(finalReply) == "" &&
			isRecommendationRequest(req.MessageText) &&
			strings.TrimSpace(analysis.Reply.Text) == "" {
			if recommendationReply := buildRecommendationReply(req.MessageText, buildCatalogCandidates(knowledge)); strings.TrimSpace(recommendationReply) != "" {
				finalReply = recommendationReply
				addGuard("recommendation_fallback")
				log.Info("recommendation_fallback_reply_used")
			}
		}
		if strings.TrimSpace(finalReply) == "" {
			if stockFollowUpReply := buildStockFollowUpReply(req.MessageText, focusItem, convSnapshot, knowledge); strings.TrimSpace(stockFollowUpReply) != "" {
				finalReply = stockFollowUpReply
				addGuard("stock_followup_guard")
				log.Info("stock_followup_reply_used", zap.String("focus", strings.TrimSpace(focusItem)))
			}
		}
		if strings.TrimSpace(finalReply) == "" {
			availabilityReply, availabilityCandidate := buildAvailabilityReply(req.MessageText, focusItem, knowledge)
			if availabilityReply != "" {
				finalReply = availabilityReply
				if availabilityCandidate != nil && strings.TrimSpace(availabilityCandidate.description) != "" {
					desc, err := r.deps.Python.GenerateAvailabilityDescription(ctx, python.AvailabilityDescriptionRequest{
						BusinessName:    knowledge.BusinessName,
						ItemName:        availabilityCandidate.name,
						ItemDescription: availabilityCandidate.description,
					})
					if err == nil {
						desc = strings.TrimSpace(desc)
						if desc != "" {
							finalReply = strings.TrimSpace(finalReply) + "\n" + desc
						}
					}
				}
			}
		}
	}

	if strings.TrimSpace(finalReply) == "" &&
		specPriorityEnabled() &&
		orderSnapshot != nil &&
		isActiveOrderFlowStage(orderSnapshot.Stage) &&
		isDirectSpecQuestion(req.MessageText) {
		specReply := buildSpecPriorityReply(req.MessageText, focusItem, knowledge, orderSnapshot)
		if strings.TrimSpace(specReply) != "" {
			resume := resumePromptForStage(orderSnapshot.Stage)
			if strings.TrimSpace(resume) != "" {
				finalReply = strings.TrimSpace(specReply) + "\n\n" + resume
			} else {
				finalReply = strings.TrimSpace(specReply)
			}
			log.Info("spec_priority_interruption", zap.String("stage", orderSnapshot.Stage))
			observability.IncSpecPriorityInterruption()
		}
	}

	if strings.TrimSpace(finalReply) == "" &&
		skuLockEnabled() &&
		orderSnapshot != nil &&
		!skuLockBlockedThisTurn &&
		inOrderingMode &&
		strings.TrimSpace(orderSnapshot.ActiveItemID) != "" &&
		strings.TrimSpace(orderSnapshot.ActiveVariantID) != "" &&
		!isLockedSKUCompatible(orderSnapshot.ActiveItemID, orderSnapshot.ActiveVariantID, requestedSKU.ItemID, requestedSKU.VariantID) &&
		!isExplicitSwitchConfirmation(req.MessageText) &&
		skuMutationIntent &&
		strings.TrimSpace(requestedSKU.Name) != "" {
		current := strings.TrimSpace(convSnapshot.ActiveItem)
		if current == "" && len(orderSnapshot.Items) > 0 {
			current = strings.TrimSpace(orderSnapshot.Items[0].Name)
		}
		finalReply = r.renderSwitchRequiredReply(ctx, businessName, current, requestedSKU.Name)
		orderSnapshot.PendingSwitchItemID = requestedSKU.ItemID
		orderSnapshot.PendingSwitchVariantID = requestedSKU.VariantID
		addGuard("sku_lock")
		log.Info("sku_lock_blocked_ordering_stage",
			zap.String("stage", orderSnapshot.Stage),
			zap.String("requested", requestedSKU.Name))
		observability.IncSKULockBlocked()
	}

	if (strings.TrimSpace(finalReply) == "" || fulfillmentCompletedThisTurn) &&
		(!fulfillmentActive || fulfillmentCompletedThisTurn) &&
		(inOrderingMode || hasNewItems) &&
		!isPaymentConfirmation {
		// ORDER FLOW: Use comprehensive order processor
		log.Info("📦 Using order flow",
			zap.Bool("in_ordering_mode", inOrderingMode),
			zap.Bool("has_new_items", hasNewItems))

		reply, err := r.ProcessOrderFlow(
			ctx,
			log,
			orderSnapshot,
			&analysis.Semantic,
			analysis.Reply.Text,
			req.MessageText,
			req.Scope.TenantID,
			req.Scope.CustomerID,
			req.Scope.ConversationID,
			req.Scope.Platform,
			convSnapshot, // Pass convSnapshot to allow mode changes
		)

		if err != nil {
			log.Error("order_flow_failed", zap.Error(err))
			if strings.TrimSpace(finalReply) == "" {
				finalReply = "We're processing your order. Please try again."
			}
		} else {
			if strings.TrimSpace(finalReply) == "" {
				finalReply = reply
			} else if fulfillmentCompletedThisTurn {
				finalReply = strings.TrimSpace(finalReply) + "\n\n" + strings.TrimSpace(reply)
			}
		}
	} else if strings.TrimSpace(finalReply) == "" {
		// NON-ORDER FLOW: Use Python's conversational reply
		// This handles greetings, menu requests, general chat, etc.
		if pythonDecisionAuthority {
			finalReply = strings.TrimSpace(analysis.Reply.Text)
			if finalReply == "" {
				finalReply = "I can help with that. Please tell me the exact item or variant you want, and I'll confirm availability."
			}
		} else {

			// 🆕 STEP 3: Normalize Intent (Fix for Service/Menu mismatch)
			// 1. Populate CatalogKind if missing (safe calc)
			if snapshot.Business.CatalogKind == "" {
				snapshot.Business.CatalogKind = types.CatalogKindFromIndustry(snapshot.Business.Industry)
			}

			// 2. Normalize the intent from Python
			rawIntent := analysis.Semantic.DetectedIntent
			normalizedIntent := types.NormalizeCatalogIntent(rawIntent, snapshot.Business.CatalogKind)

			// 3. Update the analysis for downstream consistency
			analysis.Semantic.DetectedIntent = normalizedIntent

			// 🆕 Feature: AI Conversational Freedom (Knowledge Fallback)
			// If no items extracted and no active order, try to answer using proper Knowledge Base
			// instead of generic fallback.
			// FIX: Strictly gate catalog intents, with deterministic lexical fallback when intent misses.
			intentCatalog := isCatalogIntent(normalizedIntent)
			retrievalEnabled := retrievalEngineEnabledForScope(req.Scope.TenantID, req.Scope.ConversationID)
			forcedCatalog := retrievalEnabled && shouldForceCatalogRouting(req.MessageText, focusItem, knowledge)
			isCatalog := intentCatalog || forcedCatalog
			isRecommendation := isRecommendationIntent(normalizedIntent) || isRecommendationRequest(req.MessageText)

			// LOGGING OBSERVABILITY (Step 8)
			log.Info("Routing decision (Normalized)",
				zap.String("raw_intent", rawIntent),
				zap.String("normalized_intent", normalizedIntent),
				zap.String("catalog_kind", string(snapshot.Business.CatalogKind)),
				zap.Bool("retrieval_engine_enabled", retrievalEnabled),
				zap.Bool("is_catalog_intent", isCatalog),
				zap.Bool("is_recommendation", isRecommendation),
				zap.Bool("intent_catalog", intentCatalog),
				zap.Bool("forced_catalog", forcedCatalog),
				zap.String("domain", analysis.Decision.Domain),
			)

			usedKnowledge := false
			usedCatalogReply := false

			// If the customer asks for best prices for multiple variants (e.g. "for both"),
			// answer deterministically from the catalog instead of entering negotiation for a single focus item.
			if strings.TrimSpace(finalReply) == "" && !fulfillmentActive {
				if reply := buildMultiVariantBestPricesReply(req.MessageText, convSnapshot.ActiveItem, convSnapshot.LastAIReply, knowledge); strings.TrimSpace(reply) != "" {
					finalReply = reply
					usedCatalogReply = true
					observability.IncCatalogMatchSuccess("multi_variant_best_prices")
					log.Info("used_multi_variant_best_prices_reply",
						zap.String("preview", finalReply[:min(80, len(finalReply))]))
				}
			}

			isNegotiationTurn := (analysisReplyNegotiation ||
				analysis.Semantic.DetectedIntent == "negotiation_offer" ||
				analysis.Semantic.DetectedIntent == "price_objection" ||
				analysis.Semantic.CustomerOffer > 0 ||
				messageLooksLikeNegotiation(req.MessageText)) &&
				!customerAcceptsLastOffer(req.MessageText) &&
				!analysis.Semantic.IsQuestion
			if !usedCatalogReply && isNegotiationTurn && strings.TrimSpace(analysis.Reply.Text) != "" {
				finalReply = analysis.Reply.Text
				usedCatalogReply = true
				log.Info("used_negotiation_reply_skip_catalog",
					zap.String("preview", finalReply[:min(80, len(finalReply))]))
			}
			if !usedCatalogReply && (isCatalog || isRecommendation) && retrievalEnabled {
				// Trust Python for RECOMMENDATIONS (Phase 5 Smart Recommender is already grounded).
				// Strict validation often fails on "Itel A70 (64GB)" vs "Itel A70" substring matches.
				isValid := isCatalogReplyValid(analysis.Reply.Text, req.MessageText, knowledge)
				if !isValid && isRecommendation && strings.TrimSpace(analysis.Reply.Text) != "" {
					isValid = true
					log.Info("allowing_recommendation_reply_bypass_validation")
				}

				if isValid {
					finalReply = analysis.Reply.Text
					usedCatalogReply = true
					observability.IncCatalogMatchSuccess("llm_validated")
					log.Info("used_llm_validated_reply",
						zap.String("preview", finalReply[:min(80, len(finalReply))]))
				} else {
					fallback := ""
					if isRecommendation {
						fallback = buildRecommendationReply(req.MessageText, buildCatalogCandidates(knowledge))
					}
					if strings.TrimSpace(fallback) == "" && isFullCatalogRequest(req.MessageText) {
						fallback = buildFullCatalogReplyForRequest(req.MessageText, knowledge)
					} else if strings.TrimSpace(fallback) == "" {
						fallback = buildDeterministicCatalogReply(req.MessageText, focusItem, knowledge)
					}
					if strings.TrimSpace(fallback) != "" {
						finalReply = fallback
						usedCatalogReply = true
						observability.IncCatalogMatchSuccess("deterministic_fallback")
						log.Info("used_catalog_fallback_reply",
							zap.String("preview", finalReply[:min(80, len(finalReply))]))
					}
				}
			}
			// Only invoke the Knowledge endpoint when we actually need it.
			// If Python already produced a conversational reply and the user isn't asking a question,
			// prefer the AnalyzeTurn reply to avoid "reply drift" where Go overwrites Python's message.
			shouldAttemptKnowledge := strings.TrimSpace(analysis.Reply.Text) == "" || looksLikeQuestion(req.MessageText)
			if shouldAttemptKnowledge &&
				!usedCatalogReply &&
				!fulfillmentActive &&
				len(analysis.Semantic.Items) == 0 &&
				!orderSnapshot.HasActiveOrder &&
				!isCatalog &&
				analysis.Decision.Domain != "commerce" {
				// Prepare context (last 3 turns)
				convCtx := []map[string]string{}
				start := 0
				if len(convMem.Messages) > 3 {
					start = len(convMem.Messages) - 3
				}
				for _, m := range convMem.Messages[start:] {
					convCtx = append(convCtx, map[string]string{
						"role":    string(m.Role),
						"content": m.Content,
					})
				}

				// Prepare FAQs
				faqs := []map[string]interface{}{}
				for _, f := range knowledge.FAQs {
					faqs = append(faqs, map[string]interface{}{
						"question": f.Question,
						"answer":   f.Answer,
					})
				}

				// Call Knowledge Endpoint
				kReq := python.KnowledgeRequest{
					BusinessName:        knowledge.BusinessName,
					KnowledgeBase:       knowledge.KnowledgeBase,
					FAQs:                faqs,
					ConversationContext: convCtx,
					Message:             req.MessageText,
					MessageIntent:       normalizedIntent, // PASSED NORMALIZED
				}

				kReply, err := r.deps.Python.GenerateKnowledgeReply(ctx, kReq)
				// Expanded filter: don't override Python's good reply with knowledge gaps
				kReplyLower := strings.ToLower(kReply)
				isKnowledgeGap := strings.Contains(kReplyLower, "i don't have that information") ||
					strings.Contains(kReplyLower, "i don't have enough") ||
					strings.Contains(kReplyLower, "ask a manager") ||
					strings.Contains(kReplyLower, "escalat")
				if err == nil && kReply != "" && !isKnowledgeGap {
					finalReply = kReply
					usedKnowledge = true
					log.Info("🧠 used_knowledge_reply", zap.String("preview", finalReply[:min(50, len(finalReply))]))
				}
			}

			if !usedCatalogReply && !usedKnowledge {
				finalReply = analysis.Reply.Text

				// 🆕 FINAL FALLBACK (Fix for "Empty Text" Error)
				// If Python kept silent (e.g. negotiation intent) but Go didn't generate a reply
				// (e.g. item match failed), we MUST NOT send empty text.
				if finalReply == "" {
					log.Warn("empty_reply_fallback_triggered", zap.String("intent", normalizedIntent))
					finalReply = buildEmptyReplyFallback(normalizedIntent, isCatalog, focusItem, convSnapshot.ActiveItem, req.MessageText)
					observability.IncCatalogFallback("empty_reply")
				}

				log.Info("using_conversational_reply",
					zap.String("preview", finalReply[:min(50, len(finalReply))]))
			}
		}
	}

	if strings.TrimSpace(fulfillmentFollowUp) != "" {
		if strings.TrimSpace(finalReply) == "" {
			finalReply = fulfillmentFollowUp
		} else {
			finalReply = strings.TrimSpace(finalReply) + "\n\n" + strings.TrimSpace(fulfillmentFollowUp)
		}
	}

	if closeoutSuppressionEnabled() {
		if isCloseoutMessage(req.MessageText) {
			if strings.EqualFold(strings.TrimSpace(convSnapshot.CloseoutState), "closed") {
				lower := strings.ToLower(strings.TrimSpace(finalReply))
				if strings.Contains(lower, "anything else") || strings.Contains(lower, "would you like details on") {
					finalReply = ""
				}
				if strings.TrimSpace(finalReply) == "" {
					finalReply = "No problem. If you need anything later, just let us know."
				}
				observability.IncCloseoutSuppressed()
				log.Info("closeout_suppressed")
			} else {
				finalReply = "No problem. If you need anything later, just let us know."
				markCloseout(convSnapshot)
			}
		}
	}

	// Final safety guard: never send weak stall placeholders.
	// In Python-authority mode, do not replace healthy Python replies with deterministic catalog text.
	if !pythonDecisionAuthority && isWeakStallReply(finalReply) {
		// Log but do not override. We trust Python entirely now.
		log.Warn("weak_stall_reply_from_python_logged_not_overridden", zap.String("reply", finalReply))
	}

	if strings.TrimSpace(convSnapshot.LastAIReply) != "" &&
		strings.EqualFold(strings.TrimSpace(convSnapshot.LastAIReply), strings.TrimSpace(finalReply)) {
		observability.IncRepeatPrompt()
	}
	whoSetReply := "go"
	analysisReply := strings.TrimSpace(analysis.Reply.Text)
	if analysisReply != "" && strings.EqualFold(strings.TrimSpace(finalReply), analysisReply) {
		whoSetReply = "python"
	}
	whoBlockedAction := "none"
	if skuLockBlockedThisTurn {
		whoBlockedAction = "go_guard"
	}
	log.Info("authority_trace",
		zap.String("who_set_reply", whoSetReply),
		zap.String("who_set_focus", focusSetBy),
		zap.String("who_blocked_action", whoBlockedAction),
		zap.String("guard_triggered", formatGuards()))

	// 2. Persist Conversation State
	// 🆕 Calculate new negotiation round
	newRound := convSnapshot.NegotiationRound
	if analysis.ConversationUpdate.NegotiationRoundUpdate != nil {
		newRound = *analysis.ConversationUpdate.NegotiationRoundUpdate
	}

	startActiveItem := strings.TrimSpace(convSnapshot.ActiveItem)
	persistedActiveItem := choosePersistedActiveItem(convSnapshot.ActiveItem, focusItem, analysis.Semantic.TargetItem)
	lastOffered := convSnapshot.LastOfferedPrice
	if strings.TrimSpace(persistedActiveItem) != "" && startActiveItem != "" && !strings.EqualFold(strings.TrimSpace(persistedActiveItem), startActiveItem) {
		// Clear last offer when customer switches to a different product/variant.
		lastOffered = 0
	}

	newState := v4ctx.ConversationSnapshot{
		LastUserMessage:      req.MessageText,
		LastAIReply:          finalReply, // Go-generated reply (authoritative)
		LastDomain:           analysis.Decision.Domain,
		ConversationStage:    analysis.Decision.Stage,
		ConversationMode:     convSnapshot.ConversationMode, // Preserve mode
		ActiveItem:           persistedActiveItem,           // Preserve previous focus when current turn has none
		NegotiationRound:     newRound,                      // Persist negotiation round
		LastOfferedPrice:     lastOffered,
		TurnCount:            convSnapshot.TurnCount + analysis.ConversationUpdate.TurnCountIncrement,
		CloseoutState:        convSnapshot.CloseoutState,
		CloseoutMessageCount: convSnapshot.CloseoutMessageCount,
		LastCloseoutAt:       convSnapshot.LastCloseoutAt,
	}
	stateBytes, _ := json.Marshal(newState)
	r.deps.DB.Model(&storage.Conversation{}).
		Where("id = ?", req.Scope.ConversationID).
		Update("ai_state", string(stateBytes))

	// 3. Persist Order Snapshot (CRITICAL FIX)
	// After ProcessOrderFlow updates orderSnapshot, we must persist it
	if orderSnapshot != nil {
		orderBytes, _ := json.Marshal(orderSnapshot)
		if dbErr := r.deps.DB.Model(&storage.Conversation{}).
			Where("id = ?", req.Scope.ConversationID).
			Update("ai_order_state", string(orderBytes)).Error; dbErr != nil {
			log.Error("persist_order_snapshot_failed", zap.Error(dbErr))
		} else {
			log.Info("✅ order_snapshot_persisted",
				zap.String("stage", orderSnapshot.Stage),
				zap.Int("items", len(orderSnapshot.Items)))
		}
	}

	// 4. Response Construction
	// Safety Net: reject meta
	if strings.Contains(strings.ToLower(finalReply), "here's a reply") {
		return nil, errors.New("safety_rejected_meta_reply")
	}

	response = &types.AIResponse{
		ReplyText:       finalReply, // Go-generated reply (authoritative)
		ReplySegments:   []string{finalReply},
		ProposedActions: analysis.Actions,
	}
	if analysis.LLMMeta != nil {
		response.Metadata = map[string]any{
			"llm_model":             analysis.LLMMeta.Model,
			"llm_prompt_tokens":     analysis.LLMMeta.TokenUsage.PromptTokens,
			"llm_completion_tokens": analysis.LLMMeta.TokenUsage.CompletionTokens,
			"llm_total_tokens":      analysis.LLMMeta.TokenUsage.TotalTokens,
		}
	}

	// 4. Memory Append
	if convMem.Metadata == nil {
		convMem.Metadata = make(map[string]any)
	}
	if orderSnapshot != nil {
		if dm := strings.TrimSpace(orderSnapshot.DeliveryMethod); dm != "" {
			convMem.Metadata["preferred_delivery_method"] = dm
		}
		if addr := strings.TrimSpace(orderSnapshot.DeliveryAddress); addr != "" {
			convMem.Metadata["last_delivery_address"] = addr
		}
	}
	if strings.TrimSpace(focusItem) != "" {
		convMem.Metadata["last_focus_item"] = strings.TrimSpace(focusItem)
	}
	msgLower := strings.ToLower(strings.TrimSpace(req.MessageText))
	if strings.Contains(msgLower, "pay on delivery") || strings.Contains(msgLower, "cash on delivery") || strings.Contains(msgLower, "pod") {
		convMem.Metadata["payment_preference"] = "pod"
	} else if strings.Contains(msgLower, "pay now") || strings.Contains(msgLower, "bank transfer") || strings.Contains(msgLower, "transfer") {
		convMem.Metadata["payment_preference"] = "bank_transfer"
	}
	convMem.Metadata["memory_updated_at"] = time.Now().Format(time.RFC3339)

	convMem.Messages = append(convMem.Messages, types.Message{Role: "user", Content: req.MessageText, Timestamp: req.Timestamp})
	convMem.Messages = append(convMem.Messages, types.Message{Role: "assistant", Content: response.ReplyText, Timestamp: time.Now()})
	r.deps.Memory.SaveAll(ctx, req.Scope, convMem, opMem)

	return response, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatNumberWithCommas(n int64) string {
	str := fmt.Sprintf("%d", n)
	var result []rune
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, c)
	}
	return string(result)
}
