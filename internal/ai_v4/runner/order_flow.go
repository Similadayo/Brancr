package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/delivery"
	"github.com/similadayo/brancr/internal/ai_v4/order"
	"github.com/similadayo/brancr/internal/ai_v4/payment"
	"github.com/similadayo/brancr/internal/ai_v4/python"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func isLikelyPhoneNumber(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "+") {
		v = v[1:]
	}
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "-", "")
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return len(v) >= 8 && len(v) <= 15
}

func isLikelyPersonName(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	if strings.Contains(lower, "?") {
		return false
	}
	// Reject numbers-only and very short fragments.
	digitsOnly := true
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			digitsOnly = false
			break
		}
	}
	if digitsOnly {
		return false
	}
	words := strings.Fields(v)
	if len(words) >= 2 {
		return true
	}
	return len(v) >= 3
}

func nextStageAfterLogistics(orderSnapshot *v4ctx.OrderSnapshot) string {
	if orderSnapshot == nil {
		return "collecting"
	}
	method := strings.TrimSpace(orderSnapshot.DeliveryMethod)
	if method == "delivery" {
		if !isValidDeliveryAddress(orderSnapshot.DeliveryAddress) {
			return "awaiting_delivery_address"
		}
		if strings.TrimSpace(orderSnapshot.RecipientName) == "" {
			return "awaiting_recipient_name"
		}
		if strings.TrimSpace(orderSnapshot.RecipientPhone) == "" {
			return "awaiting_recipient_phone"
		}
		if strings.TrimSpace(orderSnapshot.DeliveryNotes) == "" {
			return "awaiting_delivery_instructions"
		}
		return "payment_ready"
	}

	if method == "pickup" {
		if strings.TrimSpace(orderSnapshot.PickupTime) == "" {
			return "awaiting_pickup_time"
		}
		if strings.TrimSpace(orderSnapshot.RecipientName) == "" {
			return "awaiting_recipient_name"
		}
		if strings.TrimSpace(orderSnapshot.RecipientPhone) == "" {
			return "awaiting_recipient_phone"
		}
		return "payment_ready"
	}

	return "awaiting_delivery_choice"
}

func buildRequirementsPrompt(req *storage.Requirement) string {
	if req == nil {
		return "Before payment, please share the required details so we can proceed."
	}
	msg := fmt.Sprintf("Before payment, please send: %s.", strings.TrimSpace(req.Label))
	if strings.TrimSpace(req.Description) != "" {
		msg += " " + strings.TrimSpace(req.Description)
	}
	return msg
}

func normalizePaymentMethod(method string) string {
	m := strings.ToLower(strings.TrimSpace(method))
	m = strings.ReplaceAll(m, "-", "_")
	m = strings.ReplaceAll(m, " ", "_")

	switch m {
	case "pod", "pay_on_delivery", "cash_on_delivery", "cod", "on_delivery":
		return "pod"
	case "bank", "bank_transfer", "transfer", "pay_now", "wire_transfer":
		return "bank_transfer"
	default:
		return m
	}
}

func paymentCapabilities(methods []string) (supportsPOD bool, supportsBankTransfer bool) {
	if len(methods) == 0 {
		return false, true
	}

	for _, method := range methods {
		switch normalizePaymentMethod(method) {
		case "pod":
			supportsPOD = true
		case "bank_transfer":
			supportsBankTransfer = true
		}
	}

	// Safe fallback: if tenant config is unclear, keep existing pay-now behavior.
	if !supportsPOD && !supportsBankTransfer {
		supportsBankTransfer = true
	}

	return supportsPOD, supportsBankTransfer
}

func detectPaymentMethodChoice(rawMessage string, supportsPOD bool, supportsBankTransfer bool) string {
	msg := strings.ToLower(strings.TrimSpace(rawMessage))
	if msg == "" {
		return ""
	}

	podHints := []string{
		"pay on delivery",
		"cash on delivery",
		"payment on delivery",
		"pod",
		"on delivery",
	}
	bankHints := []string{
		"pay now",
		"bank transfer",
		"transfer",
		"send account",
		"account details",
		"account number",
	}

	podMatch := false
	bankMatch := false

	for _, hint := range podHints {
		if strings.Contains(msg, hint) {
			podMatch = true
			break
		}
	}
	for _, hint := range bankHints {
		if strings.Contains(msg, hint) {
			bankMatch = true
			break
		}
	}

	if supportsPOD && !supportsBankTransfer && podMatch {
		return "pod"
	}
	if supportsBankTransfer && !supportsPOD && bankMatch {
		return "bank_transfer"
	}

	if supportsPOD && supportsBankTransfer {
		if podMatch && !bankMatch {
			return "pod"
		}
		if bankMatch && !podMatch {
			return "bank_transfer"
		}
		if podMatch && bankMatch {
			return "ambiguous"
		}
	}

	return ""
}

func buildPaymentMethodPrompt(supportsPOD bool, supportsBankTransfer bool) string {
	switch {
	case supportsPOD && supportsBankTransfer:
		return "Before we proceed, which payment option works for you: Pay on Delivery (POD) or Pay Now via bank transfer?"
	case supportsPOD:
		return "Payment for this order is Pay on Delivery (POD). Please confirm if you'd like to proceed."
	default:
		return "We accept Pay Now via bank transfer. Reply 'pay now' and I'll share the account details."
	}
}

func formatIntWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func buildDeliveryCostBreakdown(orderSnapshot *v4ctx.OrderSnapshot) string {
	if orderSnapshot == nil {
		return ""
	}
	currency := strings.TrimSpace(orderSnapshot.Currency)
	if currency == "" {
		currency = "NGN"
	}

	itemsTotal := orderSnapshot.TotalPrice
	deliveryFee := orderSnapshot.DeliveryFee
	if deliveryFee <= 0 {
		return ""
	}

	total := itemsTotal + deliveryFee
	area := strings.TrimSpace(orderSnapshot.DeliveryAreaName)
	areaSuffix := ""
	if area != "" {
		areaSuffix = fmt.Sprintf(" (%s)", area)
	}

	return fmt.Sprintf(
		"Cost breakdown:\n- Items total: %s%s\n- Delivery fee%s: %s%s\n- Total: %s%s",
		currency, formatIntWithCommas(itemsTotal),
		areaSuffix,
		currency, formatIntWithCommas(deliveryFee),
		currency, formatIntWithCommas(total),
	)
}

func detectDeliveryChoiceFromMessage(rawMessage string) string {
	msg := strings.ToLower(strings.TrimSpace(rawMessage))
	if msg == "" {
		return ""
	}

	deliveryHints := []string{"delivery", "deliver", "send to me", "ship"}
	pickupHints := []string{"pickup", "pick up", "i will pick", "collect", "walk in"}

	deliveryMatch := false
	pickupMatch := false
	for _, h := range deliveryHints {
		if strings.Contains(msg, h) {
			deliveryMatch = true
			break
		}
	}
	for _, h := range pickupHints {
		if strings.Contains(msg, h) {
			pickupMatch = true
			break
		}
	}

	switch {
	case deliveryMatch && !pickupMatch:
		return "delivery"
	case pickupMatch && !deliveryMatch:
		return "pickup"
	default:
		return ""
	}
}

func inferPickupTimeFallback(rawMessage string) string {
	msg := strings.TrimSpace(rawMessage)
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "?") {
		return ""
	}
	if lower == "ok" || lower == "okay" || lower == "alright" || lower == "thanks" {
		return ""
	}

	timeHints := []string{
		"today", "tomorrow", "am", "pm", "morning", "afternoon", "evening",
		"asap", "now", "by ", "at ", "around", ":",
	}
	for _, h := range timeHints {
		if strings.Contains(lower, h) {
			return msg
		}
	}
	return ""
}

func buildPickupGuidance(config storage.DeliveryConfig) string {
	address := strings.TrimSpace(config.PickupAddress)
	instructions := strings.TrimSpace(config.PickupInstructions)
	template := strings.TrimSpace(config.PickupTemplate)

	if template != "" {
		out := strings.ReplaceAll(template, "{{pickup_address}}", address)
		out = strings.ReplaceAll(out, "{{pickup_instructions}}", instructions)
		out = strings.TrimSpace(out)
		if out != "" {
			return out
		}
	}

	msg := "Great, pickup selected."
	if address != "" {
		msg += " Pickup address: " + address + "."
	} else {
		msg += " We'll share the pickup address shortly."
	}
	if instructions != "" {
		msg += " " + instructions
	}
	return msg
}

func promptWithDedup(orderSnapshot *v4ctx.OrderSnapshot, key string, message string, fallback string) string {
	if orderSnapshot == nil {
		return message
	}
	if strings.TrimSpace(key) == "" {
		return message
	}
	if orderSnapshot.LastPromptKey == key {
		return fallback
	}
	orderSnapshot.LastPromptKey = key
	return message
}

func setOrderStage(log *zap.Logger, orderSnapshot *v4ctx.OrderSnapshot, nextStage string, reason string) {
	if orderSnapshot == nil {
		return
	}
	current := strings.TrimSpace(orderSnapshot.Stage)
	next := strings.TrimSpace(nextStage)
	if next == "" || current == next {
		return
	}
	log.Info("order_stage_transition",
		zap.String("from", current),
		zap.String("to", next),
		zap.String("reason", strings.TrimSpace(reason)))
	orderSnapshot.Stage = next
	orderSnapshot.LastPromptKey = ""
}

// advanceStageFromSnapshotFacts ensures stage cannot stay behind already-collected facts.
func advanceStageFromSnapshotFacts(orderSnapshot *v4ctx.OrderSnapshot, deliveryConfig storage.DeliveryConfig) {
	if orderSnapshot == nil {
		return
	}
	stage := strings.TrimSpace(orderSnapshot.Stage)
	deliveryMethod := strings.TrimSpace(orderSnapshot.DeliveryMethod)
	hasAddress := isValidDeliveryAddress(orderSnapshot.DeliveryAddress)
	hasPickupTime := strings.TrimSpace(orderSnapshot.PickupTime) != ""

	if stage == "" {
		orderSnapshot.Stage = "collecting"
		stage = orderSnapshot.Stage
	}

	if stage == "awaiting_delivery_choice" {
		switch {
		case deliveryMethod == "delivery":
			orderSnapshot.Stage = "awaiting_delivery_address"
		case deliveryMethod == "pickup":
			orderSnapshot.Stage = "awaiting_pickup_time"
		case deliveryConfig.DeliveryEnabled && !deliveryConfig.PickupEnabled:
			orderSnapshot.DeliveryMethod = "delivery"
			orderSnapshot.Stage = "awaiting_delivery_address"
		case !deliveryConfig.DeliveryEnabled && deliveryConfig.PickupEnabled:
			orderSnapshot.DeliveryMethod = "pickup"
			orderSnapshot.Stage = "awaiting_pickup_time"
		}
		stage = orderSnapshot.Stage
	}

	if stage == "awaiting_delivery_address" && hasAddress {
		orderSnapshot.Stage = nextStageAfterLogistics(orderSnapshot)
	}
	if stage == "awaiting_pickup_time" && hasPickupTime {
		orderSnapshot.Stage = nextStageAfterLogistics(orderSnapshot)
	}

	// If stage somehow regressed while facts are complete, immediately restore payment readiness.
	stage = orderSnapshot.Stage
	if stage == "awaiting_delivery_choice" || stage == "awaiting_delivery_address" || stage == "awaiting_pickup_time" {
		if deliveryMethod == "delivery" && hasAddress {
			orderSnapshot.Stage = nextStageAfterLogistics(orderSnapshot)
		}
		if deliveryMethod == "pickup" && hasPickupTime {
			orderSnapshot.Stage = nextStageAfterLogistics(orderSnapshot)
		}
	}
}

func isValidDeliveryAddress(address string) bool {
	raw := strings.TrimSpace(address)
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "?") {
		return false
	}

	// Reject short area-only inputs like "Surulere, Lagos".
	words := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ','
	})
	hasDigits := false
	for _, ch := range raw {
		if ch >= '0' && ch <= '9' {
			hasDigits = true
			break
		}
	}
	if len(words) <= 3 && !hasDigits {
		streetHints := []string{"street", "st", "st.", "road", "rd", "rd.", "avenue", "ave", "close", "lane", "drive", "way", "boulevard", "blvd"}
		for _, h := range streetHints {
			if strings.Contains(lower, h) {
				return true
			}
		}
		return false
	}

	// If it has common street/location cues, accept.
	hints := []string{
		"street", "st.", "road", "rd", "avenue", "ave", "close", "lane", "drive",
		"apartment", "apt", "unit", "estate", "district", "phase", "block",
		"junction", "bus stop", "landmark", "near", "behind", "opposite",
	}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}

	// Otherwise require something more than just area + city.
	return len(strings.Fields(raw)) >= 4 && len(raw) >= 18
}

// Helper function to convert OrderItems to Python OrderItemSummary
func convertToOrderItemSummary(items []v4ctx.OrderItem) []python.OrderItemSummary {
	result := make([]python.OrderItemSummary, len(items))
	for i, item := range items {
		result[i] = python.OrderItemSummary{
			Name:     item.Name,
			Quantity: item.Quantity,
			Price:    int(item.UnitPrice),
		}
	}
	return result
}

// ProcessOrderFlow handles the complete order flow with stage progression
func (r *FlowRunner) ProcessOrderFlow(
	ctx context.Context,
	log *zap.Logger,
	orderSnapshot *v4ctx.OrderSnapshot,
	semantic *python.SemanticResult,
	candidateReply string, // Python AnalyzeTurn reply draft for this turn
	rawMessage string,
	tenantID uuid.UUID,
	customerID string, // Platform-specific ID (string)
	conversationID uuid.UUID,
	platform string,
	convSnapshot *v4ctx.ConversationSnapshot, // For mode changes
) (finalReply string, err error) {

	// ========================================
	// TERMINAL STAGE GUARD: Prevent re-entry after order is complete.
	// Without this, terminal stages fall through to the unknown-stage
	// recovery at the bottom which resets to "confirming", causing
	// order duplication on every subsequent customer message.
	// ========================================
	switch strings.TrimSpace(orderSnapshot.Stage) {
	case "payment_sent", "fulfilled", "cancelled":
		log.Info("order_flow_terminal_stage_exit",
			zap.String("stage", orderSnapshot.Stage))
		// Exit ordering mode — order is done.
		if convSnapshot != nil {
			convSnapshot.ConversationMode = "general"
		}
		orderSnapshot.HasActiveOrder = false
		return "", nil
	}

	preview := func(s string) string {
		t := strings.TrimSpace(s)
		if t == "" {
			return ""
		}
		if len(t) > 100 {
			return t[:100]
		}
		return t
	}

	// chooseCandidate returns candidateReply if it satisfies all groups.
	// Each group is "any-of": at least one term in the group must appear in candidateReply.
	chooseCandidate := func(groups ...[]string) (string, bool) {
		clean := strings.TrimSpace(candidateReply)
		c := strings.ToLower(clean)
		if c == "" {
			return "", false
		}

		for _, group := range groups {
			if len(group) == 0 {
				continue
			}
			matched := false
			for _, term := range group {
				tt := strings.ToLower(strings.TrimSpace(term))
				if tt == "" {
					continue
				}
				if strings.Contains(c, tt) {
					matched = true
					break
				}
			}
			if !matched {
				return "", false
			}
		}

		log.Info("order_flow_reply_source",
			zap.String("stage", strings.TrimSpace(orderSnapshot.Stage)),
			zap.String("source", "python_draft"),
			zap.String("preview", preview(clean)),
		)

		return clean, true
	}

	// ========================================
	// CANCEL INTENT CHECK (Gap 4)
	// ========================================
	if semantic.CancelIntent {
		log.Info("🚫 Cancel intent detected - cancelling order")

		// Update order status in DB if exists
		if orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
				"status": "cancelled",
			})
		}

		// Reset order snapshot
		orderSnapshot.HasActiveOrder = false
		orderSnapshot.Items = nil
		setOrderStage(log, orderSnapshot, "cancelled", "customer_cancel_intent")

		// Exit ordering mode
		if convSnapshot != nil {
			convSnapshot.ConversationMode = "general"
			log.Info("🔓 Exiting ORDER MODE - order cancelled")
		}

		return "No problem. Your order has been cancelled. Let me know if you'd like to order something else.", nil
	}

	// Fetch business profile and delivery config
	var businessProfile storage.BusinessProfile
	if err := r.deps.DB.Where("tenant_id = ?", tenantID).First(&businessProfile).Error; err != nil {
		log.Error("failed_to_fetch_business_profile", zap.Error(err))
		return "", err
	}

	var deliveryConfig storage.DeliveryConfig
	configValid := false

	if businessProfile.DeliveryConfig != "" && businessProfile.DeliveryConfig != "{}" {
		if err := json.Unmarshal([]byte(businessProfile.DeliveryConfig), &deliveryConfig); err == nil {
			configValid = true
		} else {
			log.Warn("failed_to_parse_delivery_config", zap.Error(err))
		}
	}

	// If config is missing or invalid, use INDUSTRY-AWARE DEFAULTS
	if !configValid {
		log.Info("using_industry_aware_delivery_defaults",
			zap.String("industry", businessProfile.Industry))

		industry := strings.ToLower(businessProfile.Industry)

		switch {
		case strings.Contains(industry, "food") ||
			strings.Contains(industry, "restaurant") ||
			strings.Contains(industry, "bakery") ||
			strings.Contains(industry, "catering") ||
			strings.Contains(industry, "cafe") ||
			strings.Contains(industry, "beverage"):
			// Food businesses: delivery + pickup
			deliveryConfig = storage.DeliveryConfig{
				DeliveryEnabled: true,
				PickupEnabled:   true,
				PaymentMethods:  []string{"bank_transfer"},
			}

		case strings.Contains(industry, "ecommerce") ||
			strings.Contains(industry, "e-commerce") ||
			strings.Contains(industry, "retail") ||
			strings.Contains(industry, "fashion") ||
			strings.Contains(industry, "store"):
			// E-commerce: delivery only
			deliveryConfig = storage.DeliveryConfig{
				DeliveryEnabled: true,
				PickupEnabled:   false,
				PaymentMethods:  []string{"bank_transfer"},
			}

		case strings.Contains(industry, "consult") ||
			strings.Contains(industry, "service") ||
			strings.Contains(industry, "professional") ||
			strings.Contains(industry, "education") ||
			strings.Contains(industry, "legal") ||
			strings.Contains(industry, "health") ||
			strings.Contains(industry, "marketing") ||
			strings.Contains(industry, "advertising") ||
			strings.Contains(industry, "agency") ||
			strings.Contains(industry, "media") ||
			strings.Contains(industry, "tech") ||
			strings.Contains(industry, "software") ||
			strings.Contains(industry, "design") ||
			strings.Contains(industry, "creative"):
			// Services: no delivery needed, skip to payment
			deliveryConfig = storage.DeliveryConfig{
				DeliveryEnabled: false,
				PickupEnabled:   false,
				PaymentMethods:  []string{"bank_transfer"},
			}

		default:
			// Unknown industry: default to delivery + pickup
			deliveryConfig = storage.DeliveryConfig{
				DeliveryEnabled: true,
				PickupEnabled:   true,
				PaymentMethods:  []string{"bank_transfer"},
			}
		}

		log.Info("delivery_config_set",
			zap.Bool("delivery", deliveryConfig.DeliveryEnabled),
			zap.Bool("pickup", deliveryConfig.PickupEnabled))
	}

	// ========================================
	// STAGE 0: PROMOTION (Collecting -> Confirming)
	// ========================================
	if orderSnapshot.Stage == "collecting" {
		if len(orderSnapshot.Items) == 0 {
			// Guardrail: never promote an empty cart into confirmation.
			// This prevents "NGN0 / no items selected" orders from being created on short follow-ups like "hey".
			orderSnapshot.HasActiveOrder = false
			return "I didn't catch the exact item you want. Please tell me the item (and variant) plus quantity.", nil
		}
		setOrderStage(log, orderSnapshot, "confirming", "items_collected")
	}

	// Prevent loop regressions when data already exists from previous turns.
	prevStage := orderSnapshot.Stage
	advanceStageFromSnapshotFacts(orderSnapshot, deliveryConfig)
	if strings.TrimSpace(prevStage) != strings.TrimSpace(orderSnapshot.Stage) {
		log.Info("order_stage_fact_advance",
			zap.String("from", strings.TrimSpace(prevStage)),
			zap.String("to", strings.TrimSpace(orderSnapshot.Stage)),
			zap.String("delivery_method", strings.TrimSpace(orderSnapshot.DeliveryMethod)),
			zap.Bool("has_delivery_address", strings.TrimSpace(orderSnapshot.DeliveryAddress) != ""),
			zap.Bool("has_pickup_time", strings.TrimSpace(orderSnapshot.PickupTime) != ""))
	}

	// ========================================
	// STAGE 1: ORDER CONFIRMATION
	// ========================================
	if orderSnapshot.Stage == "confirming" {
		if len(orderSnapshot.Items) == 0 {
			setOrderStage(log, orderSnapshot, "collecting", "empty_items_guard")
			orderSnapshot.HasActiveOrder = false
			return "I didn't catch the exact item you want. Please tell me the item (and variant) plus quantity.", nil
		}

		// Save order to database (first time)
		if orderSnapshot.OrderID == "" {
			existingOrder, err := order.GetOrderByConversation(r.deps.DB, conversationID)
			if err == gorm.ErrRecordNotFound {
				// Parse customerID string to UUID (it's platform-specific, but we need UUID for DB)
				// For now, use conversationID as customerID if parsing fails
				customerUUID, parseErr := uuid.Parse(customerID)
				if parseErr != nil {
					// If customerID is not a UUID, generate one from the string
					customerUUID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(customerID))
				}

				savedOrder, err := order.SaveOrder(ctx, r.deps.DB, tenantID, customerUUID, conversationID, platform, orderSnapshot)
				if err != nil {
					log.Error("failed_to_save_order", zap.Error(err))
				} else {
					orderSnapshot.OrderID = savedOrder.ID.String()
					log.Info("✅ order_saved_to_db",
						zap.String("order_number", savedOrder.OrderNumber),
						zap.String("payment_ref", savedOrder.PaymentReference))
				}
			} else if err == nil {
				// Order already exists
				orderSnapshot.OrderID = existingOrder.ID.String()
			}
		}

		// Deterministic confirmation is built strictly from persisted snapshot state.
		finalReply = buildDeterministicOrderConfirmation(orderSnapshot, deliveryConfig)

		// Determine next stage based on delivery config
		if deliveryConfig.DeliveryEnabled && deliveryConfig.PickupEnabled {
			setOrderStage(log, orderSnapshot, "awaiting_delivery_choice", "confirmation_complete")
		} else if deliveryConfig.DeliveryEnabled {
			setOrderStage(log, orderSnapshot, "awaiting_delivery_address", "delivery_only_mode")
		} else if deliveryConfig.PickupEnabled {
			setOrderStage(log, orderSnapshot, "awaiting_pickup_time", "pickup_only_mode")
		} else {
			setOrderStage(log, orderSnapshot, "payment_ready", "no_delivery_or_pickup_required")
		}

		// If we are ready for payment immediately (skipped delivery/pickup), fall through to Payment Stage
		if orderSnapshot.Stage != "payment_ready" {
			return finalReply, nil
		}
		// If payment_ready, we continue to generate payment instructions immediately.
		// The previous 'finalReply' (confirmation text) will be replaced or appended by payment instructions.
		log.Info("🔄 Falling through to payment stage immediately")
	}

	// ========================================
	// STAGE 2: DELIVERY/PICKUP CHOICE
	// ========================================
	if orderSnapshot.Stage == "awaiting_delivery_choice" {
		deliveryChoice := semantic.DeliveryIntent
		if deliveryChoice == "" {
			deliveryChoice = detectDeliveryChoiceFromMessage(rawMessage)
		}

		if deliveryChoice == "delivery" {
			orderSnapshot.DeliveryMethod = "delivery"
			setOrderStage(log, orderSnapshot, "awaiting_delivery_address", "customer_selected_delivery")
			if orderSnapshot.OrderID != "" {
				orderID, _ := uuid.Parse(orderSnapshot.OrderID)
				order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
					"is_pickup": false,
					"notes":     "delivery_method=delivery",
				})
			}
			if c, ok := chooseCandidate([]string{"delivery", "deliver"}, []string{"address", "location"}); ok {
				return c, nil
			}
			return "Got it. What's your delivery address?", nil
		} else if deliveryChoice == "pickup" {
			orderSnapshot.DeliveryMethod = "pickup"
			setOrderStage(log, orderSnapshot, "awaiting_pickup_time", "customer_selected_pickup")
			if orderSnapshot.OrderID != "" {
				orderID, _ := uuid.Parse(orderSnapshot.OrderID)
				order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
					"is_pickup": true,
					"notes":     "delivery_method=pickup",
				})
			}
			msg := buildPickupGuidance(deliveryConfig) + " When would you like to pick it up?"
			if c, ok := chooseCandidate([]string{"pickup", "pick up", "pick-up", "collect"}, []string{"when", "time", "today", "tomorrow"}); ok {
				// Keep tenant pickup guidance deterministic, but let Python phrase the question.
				msg = buildPickupGuidance(deliveryConfig) + " " + c
			}
			return msg, nil
		}
		// Still waiting for choice
		if c, ok := chooseCandidate([]string{"delivery", "deliver"}, []string{"pickup", "pick up", "pick-up", "collect"}); ok {
			return promptWithDedup(
				orderSnapshot,
				"awaiting_delivery_choice",
				c,
				"I can continue once you choose one option: delivery or pickup.",
			), nil
		}
		return promptWithDedup(
			orderSnapshot,
			"awaiting_delivery_choice",
			"Would you like delivery or pickup?",
			"I can continue once you choose one option: delivery or pickup.",
		), nil
	}

	// ========================================
	// STAGE 3: DELIVERY ADDRESS
	// ========================================
	if orderSnapshot.Stage == "awaiting_delivery_address" {
		address := strings.TrimSpace(semantic.Address)
		if address == "" {
			address = inferDeliveryAddressFallback(rawMessage)
		}
		if address != "" && !isValidDeliveryAddress(address) {
			address = ""
		}
		if address != "" {
			orderSnapshot.DeliveryAddress = address

			// Calculate delivery fee
			areaName, fee, outsideZone, err := delivery.CalculateDeliveryFee(ctx, address, &deliveryConfig)
			if err != nil {
				log.Warn("delivery_fee_calculation_failed", zap.Error(err))
			} else {
				orderSnapshot.DeliveryFee = int64(fee)
				orderSnapshot.DeliveryAreaName = areaName
				log.Info("✅ delivery_fee_calculated",
					zap.String("area", areaName),
					zap.Float64("fee", fee),
					zap.Bool("outside_zone", outsideZone))

				// Track outside zone for analytics
				if outsideZone {
					log.Info("📍 delivery_outside_configured_zones",
						zap.String("address", address),
						zap.Float64("default_fee_applied", fee))
				}
			}

			// Update order in database
			if orderSnapshot.OrderID != "" {
				orderID, _ := uuid.Parse(orderSnapshot.OrderID)
				order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
					"delivery_address": orderSnapshot.DeliveryAddress,
					"is_pickup":        false,
					"notes":            "delivery_method=delivery",
					"total_amount":     float64(orderSnapshot.TotalPrice + orderSnapshot.DeliveryFee),
				})
			}

			setOrderStage(log, orderSnapshot, nextStageAfterLogistics(orderSnapshot), "delivery_address_captured")

			// If we still need customer delivery details (recipient/phone/instructions), ask now.
			breakdown := buildDeliveryCostBreakdown(orderSnapshot)
			switch strings.TrimSpace(orderSnapshot.Stage) {
			case "awaiting_recipient_name":
				msg := "Great. What name should we put for the delivery recipient?"
				if strings.TrimSpace(breakdown) != "" {
					msg = strings.TrimSpace(breakdown) + "\n\n" + msg
				}
				return msg, nil
			case "awaiting_recipient_phone":
				msg := "Thanks. Please share the recipient phone number for the delivery driver."
				if strings.TrimSpace(breakdown) != "" {
					msg = strings.TrimSpace(breakdown) + "\n\n" + msg
				}
				return msg, nil
			case "awaiting_delivery_instructions":
				msg := "Any special delivery instructions? (e.g., landmark, gate code, call on arrival)"
				if strings.TrimSpace(breakdown) != "" {
					msg = strings.TrimSpace(breakdown) + "\n\n" + msg
				}
				return msg, nil
			}
			// Otherwise fall through to payment stage.
		} else {
			if c, ok := chooseCandidate([]string{"delivery", "deliver"}, []string{"address", "location"}); ok {
				return promptWithDedup(
					orderSnapshot,
					"awaiting_delivery_address",
					c,
					"I still need your delivery address to continue.",
				), nil
			}
			return promptWithDedup(
				orderSnapshot,
				"awaiting_delivery_address",
				"What's your delivery address?",
				"I still need your delivery address to continue.",
			), nil
		}
	}

	// ========================================
	// STAGE 4: PICKUP TIME
	// ========================================
	if orderSnapshot.Stage == "awaiting_pickup_time" {
		pickupTime := strings.TrimSpace(orderSnapshot.PickupTime)
		if pickupTime == "" {
			pickupTime = strings.TrimSpace(semantic.Address)
		}
		if pickupTime == "" {
			pickupTime = inferPickupTimeFallback(rawMessage)
		}

		// BUG FIX: When the customer repeats "pick up" / "pickup" while we are
		// waiting for a pickup TIME, treat it as "I'll come pick up" (i.e. no
		// specific time preference) rather than leaving pickupTime empty.
		// Without this, the empty pickupTime causes re-prompting, and on the
		// next turn the delivery-choice detector re-fires, which re-enters
		// the confirming stage and duplicates the order.
		if pickupTime == "" {
			lowerMsg := strings.ToLower(strings.TrimSpace(rawMessage))
			pickupPhrases := []string{"pickup", "pick up", "pick-up", "collect", "i will pick", "i'll pick"}
			for _, p := range pickupPhrases {
				if strings.Contains(lowerMsg, p) {
					pickupTime = "Customer will pick up (no specific time)"
					log.Info("pickup_time_inferred_from_pickup_phrase",
						zap.String("raw_message", rawMessage))
					break
				}
			}
		}

		if pickupTime != "" {
			orderSnapshot.PickupTime = pickupTime
			if orderSnapshot.OrderID != "" {
				orderID, _ := uuid.Parse(orderSnapshot.OrderID)
				order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
					"is_pickup": true,
					"notes":     "delivery_method=pickup",
				})
			}
			setOrderStage(log, orderSnapshot, nextStageAfterLogistics(orderSnapshot), "pickup_time_captured")

			switch strings.TrimSpace(orderSnapshot.Stage) {
			case "awaiting_recipient_name":
				return "Great. What name should we put for the pickup recipient?", nil
			case "awaiting_recipient_phone":
				return "Thanks. Please share the recipient phone number we can reach if needed.", nil
			}
		} else {
			if c, ok := chooseCandidate([]string{"pickup", "pick up", "pick-up", "collect"}, []string{"when", "time", "today", "tomorrow"}); ok {
				return promptWithDedup(
					orderSnapshot,
					"awaiting_pickup_time",
					c,
					"I still need your preferred pickup time before we continue.",
				), nil
			}
			return promptWithDedup(
				orderSnapshot,
				"awaiting_pickup_time",
				"When would you like to pick it up?",
				"I still need your preferred pickup time before we continue.",
			), nil
		}
	}

	// ========================================
	// STAGE 4.1: RECIPIENT NAME
	// ========================================
	if orderSnapshot.Stage == "awaiting_recipient_name" {
		name := strings.TrimSpace(rawMessage)
		if !isLikelyPersonName(name) {
			name = ""
		}
		if name == "" {
			fallback := "I still need the recipient name (first and last name) to continue."
			prompt := "What name should we put for the recipient?"
			if strings.TrimSpace(orderSnapshot.DeliveryMethod) == "pickup" {
				prompt = "What name should we put for the pickup recipient?"
				fallback = "I still need the pickup recipient name to continue."
			}
			return promptWithDedup(orderSnapshot, "awaiting_recipient_name", prompt, fallback), nil
		}

		orderSnapshot.RecipientName = name
		if orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
				"customer_name": orderSnapshot.RecipientName,
			})
		}

		setOrderStage(log, orderSnapshot, nextStageAfterLogistics(orderSnapshot), "recipient_name_captured")
		if strings.TrimSpace(orderSnapshot.Stage) == "awaiting_recipient_phone" {
			return "Thanks. Please share the recipient phone number.", nil
		}
		// Should not happen, but keep flow safe.
		return "Thanks. Please share the recipient phone number.", nil
	}

	// ========================================
	// STAGE 4.2: RECIPIENT PHONE
	// ========================================
	if orderSnapshot.Stage == "awaiting_recipient_phone" {
		phone := strings.TrimSpace(rawMessage)
		if !isLikelyPhoneNumber(phone) {
			phone = ""
		}
		if phone == "" {
			return promptWithDedup(
				orderSnapshot,
				"awaiting_recipient_phone",
				"Please share the recipient phone number (e.g., 080..., +234...).",
				"I still need a valid phone number to continue.",
			), nil
		}

		orderSnapshot.RecipientPhone = phone
		if orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
				"customer_phone": orderSnapshot.RecipientPhone,
			})
		}

		setOrderStage(log, orderSnapshot, nextStageAfterLogistics(orderSnapshot), "recipient_phone_captured")
		if strings.TrimSpace(orderSnapshot.Stage) == "awaiting_delivery_instructions" {
			return "Any special delivery instructions? (e.g., landmark, gate code, call on arrival)", nil
		}
		// Fall through to payment stage.
	}

	// ========================================
	// STAGE 4.3: DELIVERY INSTRUCTIONS
	// ========================================
	if orderSnapshot.Stage == "awaiting_delivery_instructions" {
		notes := strings.TrimSpace(rawMessage)
		lower := strings.ToLower(notes)
		if notes == "" || lower == "ok" || lower == "okay" || lower == "thanks" || lower == "thank you" {
			notes = ""
		}
		if notes == "" {
			return promptWithDedup(
				orderSnapshot,
				"awaiting_delivery_instructions",
				"Please share any special delivery instructions (landmark, gate code, call on arrival). If none, reply 'none'.",
				"I still need delivery instructions to continue (reply 'none' if there are none).",
			), nil
		}
		if strings.EqualFold(strings.TrimSpace(notes), "none") {
			notes = "none"
		}

		orderSnapshot.DeliveryNotes = notes
		if orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			order.UpdateOrder(r.deps.DB, orderID, map[string]interface{}{
				"delivery_notes": orderSnapshot.DeliveryNotes,
			})
		}

		setOrderStage(log, orderSnapshot, nextStageAfterLogistics(orderSnapshot), "delivery_notes_captured")
		// Fall through to payment stage.
	}

	// ========================================
	// STAGE 4.5: REQUIREMENTS COLLECTION (PRE-PAYMENT)
	// ========================================
	if orderSnapshot.Stage == "collecting_requirements" {
		// If requirements are still pending, keep prompting.
		if r.deps.Fulfillment != nil && orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			var ord storage.Order
			if err := r.deps.DB.First(&ord, orderID).Error; err == nil {
				nextReq, _, reqErr := r.deps.Fulfillment.GetPrePaymentRequirementForOrder(&ord)
				if reqErr != nil {
					log.Warn("prepayment_requirement_check_failed", zap.Error(reqErr))
				} else if nextReq != nil {
					orderSnapshot.LastPromptKey = ""
					return buildRequirementsPrompt(nextReq), nil
				}
			}
		}
		// Requirements complete: continue to payment stage.
		setOrderStage(log, orderSnapshot, "payment_ready", "requirements_completed")
	}

	// ========================================
	// STAGE 5: PAYMENT MODE + INSTRUCTIONS
	// ========================================
	if orderSnapshot.Stage == "payment_ready" || orderSnapshot.Stage == "awaiting_payment_method" {
		// Get saved order
		var savedOrder *storage.Order
		if orderSnapshot.OrderID != "" {
			orderID, _ := uuid.Parse(orderSnapshot.OrderID)
			var ord storage.Order
			if err := r.deps.DB.First(&ord, orderID).Error; err == nil {
				savedOrder = &ord
			}
		}

		if savedOrder == nil {
			log.Error("order_not_found_for_payment")
			return "There was an issue with your order. Please try again.", nil
		}

		// Requirements must be collected before payment instructions are sent.
		if r.deps.Fulfillment != nil {
			nextReq, _, reqErr := r.deps.Fulfillment.GetPrePaymentRequirementForOrder(savedOrder)
			if reqErr != nil {
				log.Warn("prepayment_requirement_check_failed", zap.Error(reqErr))
			} else if nextReq != nil {
				setOrderStage(log, orderSnapshot, "collecting_requirements", "pending_prepayment_requirement")
				return buildRequirementsPrompt(nextReq), nil
			}
		}

		supportsPOD, supportsBankTransfer := paymentCapabilities(deliveryConfig.PaymentMethods)
		selectedMethod := detectPaymentMethodChoice(rawMessage, supportsPOD, supportsBankTransfer)
		breakdown := buildDeliveryCostBreakdown(orderSnapshot)

		// If both payment modes are enabled, collect customer preference before sending payment details.
		if supportsPOD && supportsBankTransfer {
			if selectedMethod == "" {
				setOrderStage(log, orderSnapshot, "awaiting_payment_method", "awaiting_customer_payment_choice")
				if c, ok := chooseCandidate(
					[]string{"pay on delivery", "cash on delivery", "pod"},
					[]string{"bank transfer", "pay now", "transfer"},
				); ok {
					prompt := promptWithDedup(
						orderSnapshot,
						"awaiting_payment_method",
						c,
						"Please choose one option so I can proceed: Pay on Delivery (POD) or Pay Now via bank transfer.",
					)
					if strings.TrimSpace(breakdown) != "" {
						return strings.TrimSpace(breakdown) + "\n\n" + strings.TrimSpace(prompt), nil
					}
					return prompt, nil
				}
				prompt := promptWithDedup(
					orderSnapshot,
					"awaiting_payment_method",
					buildPaymentMethodPrompt(supportsPOD, supportsBankTransfer),
					"Please choose one option so I can proceed: Pay on Delivery (POD) or Pay Now via bank transfer.",
				)
				if strings.TrimSpace(breakdown) != "" {
					return strings.TrimSpace(breakdown) + "\n\n" + strings.TrimSpace(prompt), nil
				}
				return prompt, nil
			}
			if selectedMethod == "ambiguous" {
				setOrderStage(log, orderSnapshot, "awaiting_payment_method", "ambiguous_payment_choice")
				msg := "Please choose one payment option so I can continue: Pay on Delivery (POD) or Pay Now (bank transfer)."
				if strings.TrimSpace(breakdown) != "" {
					msg = strings.TrimSpace(breakdown) + "\n\n" + msg
				}
				return msg, nil
			}
		}

		if selectedMethod == "pod" {
			finalReply = "Perfect. We'll proceed with Pay on Delivery (POD). Payment will be collected when your order is delivered."
			if strings.TrimSpace(breakdown) != "" {
				finalReply = strings.TrimSpace(breakdown) + "\n\n" + finalReply
			}
			r.deps.DB.Model(&savedOrder).Updates(map[string]interface{}{
				"payment_instructions": finalReply,
				"status":               "pending",
			})

			setOrderStage(log, orderSnapshot, "payment_sent", "pod_confirmed")
			if convSnapshot != nil {
				convSnapshot.ConversationMode = "general"
				log.Info("exiting_order_mode_after_pod_selection")
			}
			return finalReply, nil
		}

		// Fetch payment account
		paymentAccount, err := payment.GetDefaultPaymentAccount(r.deps.DB, tenantID)
		if err != nil {
			log.Error("no_payment_account", zap.Error(err))
			return "We're getting your payment details ready. One moment.", nil
		}

		// Calculate final total
		finalTotal := float64(orderSnapshot.TotalPrice + orderSnapshot.DeliveryFee)

		// Decrypt account number for display (stored encrypted)
		decryptedAccountNumber := payment.GetDecryptedAccountNumber(paymentAccount)

		log.Info("🔍 DEBUG: Account number for payment",
			zap.String("decrypted_account", decryptedAccountNumber),
			zap.Int("length", len(decryptedAccountNumber)))

		// Determine terminology based on industry
		termLabel := "order"
		var tenant storage.Tenant
		if err := r.deps.DB.Preload("BusinessProfile").First(&tenant, tenantID).Error; err == nil {
			industry := strings.ToLower(tenant.BusinessProfile.Industry)
			if strings.Contains(industry, "service") ||
				strings.Contains(industry, "consulting") ||
				strings.Contains(industry, "salon") ||
				strings.Contains(industry, "clinic") ||
				strings.Contains(industry, "repair") ||
				strings.Contains(industry, "logistics") {
				termLabel = "service"
			}
		}

		// Generate LLM payment instructions
		paymentData := python.PaymentInstructionData{
			OrderNumber:      savedOrder.OrderNumber,
			Total:            finalTotal,
			Currency:         orderSnapshot.Currency,
			BankName:         paymentAccount.BankName,
			AccountNumber:    decryptedAccountNumber, // Use decrypted account number
			AccountName:      paymentAccount.AccountName,
			PaymentReference: savedOrder.PaymentReference,
			PaymentMethods:   deliveryConfig.PaymentMethods,
			TermLabel:        termLabel,
		}

		llmReply, err := r.deps.Python.GeneratePaymentInstructions(ctx, paymentData)
		if err != nil {
			log.Warn("llm_payment_reply_failed", zap.Error(err))
			// Fallback to structured format
			finalReply = payment.FormatPaymentInstructions(paymentAccount, savedOrder.PaymentReference, finalTotal, orderSnapshot.Currency)
		} else {
			finalReply = llmReply
		}
		if strings.TrimSpace(breakdown) != "" {
			finalReply = strings.TrimSpace(breakdown) + "\n\n" + strings.TrimSpace(finalReply)
		}

		// Update order with payment instructions
		r.deps.DB.Model(&savedOrder).Updates(map[string]interface{}{
			"payment_instructions": finalReply,
			"status":               "pending",
		})

		log.Info("✅ payment_instructions_sent",
			zap.String("order_number", savedOrder.OrderNumber),
			zap.Float64("total", finalTotal))

		// EXIT ORDERING MODE: Order complete, return to general conversation
		setOrderStage(log, orderSnapshot, "payment_sent", "payment_instructions_sent")
		if convSnapshot != nil {
			convSnapshot.ConversationMode = "general"
			log.Info("🔓 Exiting ORDER MODE - conversation unlocked")
		}

		return finalReply, nil
	}

	log.Warn("unknown_order_stage_recovered",
		zap.String("stage", strings.TrimSpace(orderSnapshot.Stage)),
		zap.Bool("has_active_order", orderSnapshot.HasActiveOrder),
		zap.Int("items_count", len(orderSnapshot.Items)))

	// Recover deterministically instead of failing the turn.
	if orderSnapshot.HasActiveOrder && len(orderSnapshot.Items) > 0 {
		setOrderStage(log, orderSnapshot, "confirming", "recover_from_unknown_stage")
		return buildFallbackOrderReply(orderSnapshot), nil
	}

	setOrderStage(log, orderSnapshot, "collecting", "recover_from_unknown_stage")
	return "Let's continue your order. Tell me the exact item you want and quantity.", nil
}

func inferDeliveryAddressFallback(message string) string {
	text := strings.TrimSpace(message)
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	if strings.Contains(lower, "?") {
		return ""
	}

	switch lower {
	case "hi", "hello", "hey", "ok", "okay", "thanks", "thank you", "yes", "no", "sure":
		return ""
	}

	if isValidDeliveryAddress(text) {
		return strings.Trim(text, " .")
	}

	return ""
}

// buildFallbackOrderReply creates a structured order confirmation when LLM fails
func buildFallbackOrderReply(orderSnapshot *v4ctx.OrderSnapshot) string {
	return buildDeterministicOrderConfirmation(orderSnapshot, storage.DeliveryConfig{})
}

func buildDeterministicOrderConfirmation(orderSnapshot *v4ctx.OrderSnapshot, deliveryConfig storage.DeliveryConfig) string {
	if orderSnapshot == nil || len(orderSnapshot.Items) == 0 {
		return "I didn't catch the exact item you want. Please tell me the item (and variant) plus quantity."
	}
	currency := strings.TrimSpace(orderSnapshot.Currency)
	if currency == "" {
		currency = "NGN"
	}

	// If a negotiated price was agreed, use it as the effective unit price for display and total.
	agreedUnit := orderSnapshot.AgreedUnitPrice

	var reply strings.Builder
	reply.WriteString("Thanks, your order is confirmed:\n")

	var computedTotal int64
	for _, item := range orderSnapshot.Items {
		if item.Quantity <= 0 {
			continue
		}
		unitPrice := item.UnitPrice
		if agreedUnit > 0 && len(orderSnapshot.Items) == 1 {
			unitPrice = agreedUnit
		}
		computedTotal += unitPrice * int64(item.Quantity)
		reply.WriteString(fmt.Sprintf("- %d x %s, %s%s\n",
			item.Quantity,
			item.Name,
			currency,
			formatIntWithCommas(unitPrice)))
	}

	// Use the computed total (which respects the negotiated price) instead of the stale snapshot total.
	total := computedTotal
	if total <= 0 {
		total = orderSnapshot.TotalPrice
	}
	reply.WriteString(fmt.Sprintf("TOTAL %s%s.", currency, formatIntWithCommas(total)))
	if deliveryConfig.DeliveryEnabled && deliveryConfig.PickupEnabled {
		reply.WriteString(" Would you like delivery or pickup?")
	}

	return strings.TrimSpace(reply.String())
}
