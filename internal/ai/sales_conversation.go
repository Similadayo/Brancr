package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SalesConversationManager manages sales conversation state and flow
type SalesConversationManager struct {
	DB              *gorm.DB
	IndustryService *industry.Service
	Logger          *zap.Logger
}

// OrderDraft captures a pending order summary before tenant approval
type OrderDraft struct {
	Items       []storage.OrderItem `json:"items"`
	TotalAmount float64             `json:"total_amount"`
	Currency    string              `json:"currency"`
	PaymentMode string              `json:"payment_mode"` // pay_on_delivery or paid
}

// NewSalesConversationManager creates a new sales conversation manager
func NewSalesConversationManager(db *gorm.DB, industryService *industry.Service, logger *zap.Logger) *SalesConversationManager {
	return &SalesConversationManager{
		DB:              db,
		IndustryService: industryService,
		Logger:          logger,
	}
}

// GetOrCreateSalesConversation gets or creates a sales conversation for a customer
func (m *SalesConversationManager) GetOrCreateSalesConversation(ctx context.Context, tenantID, customerID, conversationID uuid.UUID) (*storage.SalesConversation, error) {
	var salesConv storage.SalesConversation
	err := m.DB.Where("tenant_id = ? AND customer_id = ? AND conversation_id = ?", tenantID, customerID, conversationID).
		First(&salesConv).Error

	if err == nil {
		return &salesConv, nil
	}

	// Create new sales conversation
	salesConv = storage.SalesConversation{
		ConversationID:    conversationID,
		TenantID:          tenantID,
		CustomerID:        customerID,
		Stage:             "greeting",
		CommitmentLevel:   "exploring",
		ItemsDiscussed:    "[]",
		PriceDiscussed:    0,
		NegotiationCount:  0,
		ObjectionsRaised:  "[]",
		PendingOrderDraft: "{}", // Valid empty JSON for JSONB column
	}

	if err := m.DB.Create(&salesConv).Error; err != nil {
		return nil, fmt.Errorf("failed to create sales conversation: %w", err)
	}

	return &salesConv, nil
}

// UpdateStage updates the sales conversation stage
func (m *SalesConversationManager) UpdateStage(ctx context.Context, salesConvID uuid.UUID, stage string) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("stage", stage).Error
}

// UpdateCommitmentLevel updates the commitment level
func (m *SalesConversationManager) UpdateCommitmentLevel(ctx context.Context, salesConvID uuid.UUID, level string) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("commitment_level", level).Error
}

// AddItemDiscussed adds an item to the items discussed list
func (m *SalesConversationManager) AddItemDiscussed(ctx context.Context, salesConvID uuid.UUID, itemName string, itemPrice float64) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}

	var items []map[string]interface{}
	if salesConv.ItemsDiscussed != "" {
		json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items)
	}

	items = append(items, map[string]interface{}{
		"name":  itemName,
		"price": itemPrice,
	})

	itemsJSON, _ := json.Marshal(items)
	return m.DB.Model(&salesConv).
		Update("items_discussed", string(itemsJSON)).Error
}

// UpdatePriceDiscussed updates the price being discussed
func (m *SalesConversationManager) UpdatePriceDiscussed(ctx context.Context, salesConvID uuid.UUID, price float64) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("price_discussed", price).Error
}

// AddObjection records a customer objection
// Handles both formats: plain array ["obj1"] and metadata object {"objections": [...], "domain": "..."}
func (m *SalesConversationManager) AddObjection(ctx context.Context, salesConvID uuid.UUID, objection string) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}

	var objections []string
	var metadata map[string]interface{}
	isMetadataFormat := false

	if salesConv.ObjectionsRaised != "" && salesConv.ObjectionsRaised != "[]" {
		// Try to parse as metadata object first
		if err := json.Unmarshal([]byte(salesConv.ObjectionsRaised), &metadata); err == nil {
			// Check if it's a metadata object (has domain/emotion/objections keys)
			if _, hasMetadata := metadata["domain"]; hasMetadata {
				isMetadataFormat = true
				// Extract objections from metadata object
				if objectionsRaw, ok := metadata["objections"].([]interface{}); ok {
					objections = make([]string, 0, len(objectionsRaw))
					for _, obj := range objectionsRaw {
						if str, ok := obj.(string); ok {
							objections = append(objections, str)
						}
					}
				}
			}
		}

		// If not metadata format, try plain array format
		if !isMetadataFormat {
			json.Unmarshal([]byte(salesConv.ObjectionsRaised), &objections)
		}
	}

	// Add new objection
	objections = append(objections, objection)

	// Preserve metadata format if it was in that format, otherwise use plain array
	if isMetadataFormat && metadata != nil {
		// Convert string array to interface array for metadata
		objectionsInterface := make([]interface{}, len(objections))
		for i, obj := range objections {
			objectionsInterface[i] = obj
		}
		metadata["objections"] = objectionsInterface
		objectionsJSON, _ := json.Marshal(metadata)
		return m.DB.Model(&salesConv).
			Update("objections_raised", string(objectionsJSON)).Error
	} else {
		// Plain array format
		objectionsJSON, _ := json.Marshal(objections)
		return m.DB.Model(&salesConv).
			Update("objections_raised", string(objectionsJSON)).Error
	}
}

// IncrementNegotiation increments the negotiation count
func (m *SalesConversationManager) IncrementNegotiation(ctx context.Context, salesConvID uuid.UUID) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}
	salesConv.NegotiationCount++
	return m.DB.Save(&salesConv).Error
}

// RecordNegotiation records a negotiation attempt and updates sales conversation counters
func (m *SalesConversationManager) RecordNegotiation(ctx context.Context, salesConvID uuid.UUID, negotiationType string, customerOffer, counterOffer, finalAgreement float64, status string) error {
	negotiation := storage.NegotiationHistory{
		SalesConversationID: salesConvID,
		Type:                negotiationType,
		CustomerOffer:       customerOffer,
		CounterOffer:        counterOffer,
		FinalAgreement:      finalAgreement,
		Status:              status,
	}

	if err := m.DB.Create(&negotiation).Error; err != nil {
		return err
	}

	// Update conversation counters
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err == nil {
		// Update same offer count: consider offers equal if difference < 1% or absolute < 100
		if salesConv.LastOfferAmount > 0 {
			delta := math.Abs(salesConv.LastOfferAmount - customerOffer)
			if delta <= 1.0 || (salesConv.LastOfferAmount > 0 && (delta/salesConv.LastOfferAmount) <= 0.01) {
				salesConv.SameOfferCount++
			} else {
				salesConv.SameOfferCount = 1
			}
		} else {
			salesConv.SameOfferCount = 1
		}

		salesConv.LastOfferAmount = customerOffer

		// Increment AI concession count when a counter offer exists
		if counterOffer > 0 {
			salesConv.AIConcessionCount++
			// CRITICAL: Track LastAIOffer to prevent backtracking
			// Once AI offers a price, it can never offer a higher price
			if salesConv.LastAIOffer == 0 || counterOffer < salesConv.LastAIOffer {
				salesConv.LastAIOffer = counterOffer
			}
		}

		if err := m.DB.Save(&salesConv).Error; err != nil {
			m.Logger.Warn("failed to update sales conversation counters", zap.Error(err), zap.Stringer("sales_conversation_id", salesConvID))
		}
	}

	return nil
}

// RecordAIPersuasion increments the AIPersuasionCount when AI tries to persuade the customer
func (m *SalesConversationManager) RecordAIPersuasion(ctx context.Context, salesConvID uuid.UUID) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}
	salesConv.AIPersuasionCount++
	return m.DB.Save(&salesConv).Error
}

// MarkFlexibilityAttempted sets the flag that a non-price leverage was attempted
func (m *SalesConversationManager) MarkFlexibilityAttempted(ctx context.Context, salesConvID uuid.UUID) error {
	return m.DB.Model(&storage.SalesConversation{}).Where("id = ?", salesConvID).Update("flexibility_attempted", true).Error
}

// MarkCustomerRequestedNumber sets the flag that the customer explicitly asked for a number
func (m *SalesConversationManager) MarkCustomerRequestedNumber(ctx context.Context, salesConvID uuid.UUID) error {
	return m.DB.Model(&storage.SalesConversation{}).Where("id = ?", salesConvID).Update("customer_requested_number", true).Error
}

// ResetForNewOrder clears order-specific state after an order is created/completed.
// This prevents items from previous orders from mixing with new orders.
// CRITICAL: Call this after CreateOrderFromSalesConversation succeeds.
func (m *SalesConversationManager) ResetForNewOrder(ctx context.Context, salesConvID uuid.UUID) error {
	m.Logger.Info("resetting sales conversation for new order",
		zap.Stringer("sales_conversation_id", salesConvID))

	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Updates(map[string]interface{}{
			"items_discussed":           "[]",
			"price_discussed":           0,
			"stage":                     "greeting",
			"commitment_level":          "exploring",
			"pending_order_draft":       "{}",
			"pending_order_status":      "",
			"negotiation_count":         0,
			"objections_raised":         "[]",
			"same_offer_count":          0,
			"last_offer_amount":         0,
			"last_ai_offer":             0,
			"ai_concession_count":       0,
			"ai_persuasion_count":       0,
			"flexibility_attempted":     false,
			"customer_requested_number": false,
		}).Error
}

// UpdateDeliveryAddress updates the collected delivery address
func (m *SalesConversationManager) UpdateDeliveryAddress(ctx context.Context, salesConvID uuid.UUID, address string) error {
	m.Logger.Info("updating delivery address",
		zap.Stringer("sales_conversation_id", salesConvID),
		zap.String("address", address))

	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("delivery_address", address).Error
}

// UpdateCustomerPhone updates the collected customer phone number
func (m *SalesConversationManager) UpdateCustomerPhone(ctx context.Context, salesConvID uuid.UUID, phone string) error {
	m.Logger.Info("updating customer phone",
		zap.Stringer("sales_conversation_id", salesConvID),
		zap.String("phone", phone))

	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("customer_phone", phone).Error
}

// UpdatePickupDetails updates pickup request status and time
func (m *SalesConversationManager) UpdatePickupDetails(ctx context.Context, salesConvID uuid.UUID, isPickup bool, pickupTime string) error {
	m.Logger.Info("updating pickup details",
		zap.Stringer("sales_conversation_id", salesConvID),
		zap.Bool("is_pickup", isPickup),
		zap.String("pickup_time", pickupTime))

	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Updates(map[string]interface{}{
			"pickup_requested": isPickup,
			"pickup_time":      pickupTime,
		}).Error
}

// BuildIndustrySalesContext builds industry-specific sales context for AI prompts
func (m *SalesConversationManager) BuildIndustrySalesContext(ctx context.Context, tenantID uuid.UUID, salesConv *storage.SalesConversation) (string, error) {
	industry, err := m.IndustryService.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return "", err
	}

	settings, err := m.IndustryService.GetIndustrySettings(ctx, tenantID)
	if err != nil {
		settings = make(map[string]interface{})
	}

	var context strings.Builder
	context.WriteString(fmt.Sprintf("INDUSTRY: %s\n", industry.Name))
	context.WriteString(fmt.Sprintf("SALES STAGE: %s\n", salesConv.Stage))
	context.WriteString(fmt.Sprintf("COMMITMENT LEVEL: %s\n", salesConv.CommitmentLevel))
	context.WriteString(fmt.Sprintf("NEGOTIATION COUNT: %d\n", salesConv.NegotiationCount))

	// Industry-specific sales guidance
	if salesStyle, ok := settings["sales_style"].(string); ok {
		context.WriteString(fmt.Sprintf("SALES STYLE: %s\n", salesStyle))
	}

	// Add items discussed
	if salesConv.ItemsDiscussed != "" {
		var items []map[string]interface{}
		json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items)
		if len(items) > 0 {
			context.WriteString("ITEMS DISCUSSED:\n")
			for _, item := range items {
				context.WriteString(fmt.Sprintf("- %s (Price: %.2f)\n", item["name"], item["price"]))
			}
		}
	}

	// Add price discussed
	if salesConv.PriceDiscussed > 0 {
		context.WriteString(fmt.Sprintf("PRICE DISCUSSED: %.2f\n", salesConv.PriceDiscussed))
	}

	// Add objections - handle both plain array and metadata object formats
	if salesConv.ObjectionsRaised != "" && salesConv.ObjectionsRaised != "[]" {
		var objections []string
		var metadata map[string]interface{}

		// Try metadata object format first
		if err := json.Unmarshal([]byte(salesConv.ObjectionsRaised), &metadata); err == nil {
			if _, hasMetadata := metadata["domain"]; hasMetadata {
				// Metadata format - extract objections
				if objectionsRaw, ok := metadata["objections"].([]interface{}); ok {
					objections = make([]string, 0, len(objectionsRaw))
					for _, obj := range objectionsRaw {
						if str, ok := obj.(string); ok {
							objections = append(objections, str)
						}
					}
				}
			}
		}

		// If no objections extracted, try plain array format
		if len(objections) == 0 {
			json.Unmarshal([]byte(salesConv.ObjectionsRaised), &objections)
		}

		if len(objections) > 0 {
			context.WriteString("OBJECTIONS RAISED:\n")
			for _, obj := range objections {
				context.WriteString(fmt.Sprintf("- %s\n", obj))
			}
		}
	}

	// Industry-specific guidance using dedicated prompt builder
	industryGuidance := BuildIndustrySalesPrompt(
		industry.Name,
		salesConv.Stage,
		salesConv.CommitmentLevel,
		salesConv.NegotiationCount > 0,
	)
	context.WriteString("\n")
	context.WriteString(industryGuidance)

	return context.String(), nil
}

// DetectCommitment detects if customer is ready to commit to purchase
// Returns true for explicit buying signals OR implicit commitment (providing delivery address)
func (m *SalesConversationManager) DetectCommitment(ctx context.Context, message string, salesConv *storage.SalesConversation) (bool, float64) {
	messageLower := strings.ToLower(message)

	// EXPLICIT: Direct buying/ordering signals
	explicitBuyingSignals := []string{
		"i'll take it", "i'll buy", "i want to order", "i'm ready to pay",
		"how do i pay", "where do i send", "payment details", "bank details",
		"let's proceed", "i'll go with", "i'll get", "i'll take",
		"send me the", "give me your", "what's your account", "account number",
		"i'm ready", "proceed with", "go ahead", "place order",
		"i want to buy", "i need to order", "make the order",
	}

	// Check for explicit buying signals first
	for _, signal := range explicitBuyingSignals {
		if strings.Contains(messageLower, signal) {
			if strings.HasSuffix(messageLower, "?") && (strings.Contains(messageLower, "how") || strings.Contains(messageLower, "what") || strings.Contains(messageLower, "where")) {
				return true, 0.7
			}
			return true, 0.9
		}
	}

	// IMPLICIT COMMITMENT: Customer providing delivery address = commitment
	// Use PATTERN-BASED detection instead of hardcoding city names

	// Pattern 1: Street address patterns (works globally)
	// Examples: "12 Allen Avenue", "No. 5 Church Street", "Plot 23", "Block B Flat 4"
	streetPatterns := []string{
		"street", "road", "avenue", "close", "crescent", "lane", "drive", "way", "court",
		"estate", "plaza", "mall", "park", "junction", "bus stop", "bustop",
	}

	// Pattern 2: House/building number patterns
	// Matches: "No. 5", "No 12", "number 3", "plot 45", "block a", "flat 2", "house 7"
	hasHouseNumber := false
	houseNumberPatterns := []string{"no.", "no ", "number ", "plot ", "block ", "flat ", "house "}
	for _, pattern := range houseNumberPatterns {
		if strings.Contains(messageLower, pattern) {
			hasHouseNumber = true
			break
		}
	}

	// Pattern 3: Numeric prefix (like "12 Allen Street") - number followed by text
	hasNumericPrefix := len(message) > 3 && (message[0] >= '0' && message[0] <= '9')

	// Pattern 4: Phone number alongside text (common in Nigerian addresses)
	hasPhonePattern := strings.Contains(message, "08") || strings.Contains(message, "09") ||
		strings.Contains(message, "07") || strings.Contains(message, "+234")

	// Pattern 5: Comma-separated parts (addresses often have commas)
	hasCommas := strings.Count(message, ",") >= 1

	// Count street pattern matches
	streetMatchCount := 0
	for _, pattern := range streetPatterns {
		if strings.Contains(messageLower, pattern) {
			streetMatchCount++
		}
	}

	// Calculate address confidence score
	addressScore := 0
	if streetMatchCount >= 1 {
		addressScore += 2 // Strong indicator
	}
	if hasHouseNumber {
		addressScore += 2 // Strong indicator
	}
	if hasNumericPrefix {
		addressScore += 1 // Moderate indicator
	}
	if hasPhonePattern {
		addressScore += 2 // Strong indicator (address + phone = commitment)
	}
	if hasCommas && len(message) > 15 {
		addressScore += 1 // Moderate indicator (multi-part address)
	}

	// If address score >= 3, treat as implicit commitment
	if addressScore >= 3 {
		m.Logger.Info("📍 Detected address pattern in message - treating as implicit commitment",
			zap.Int("address_score", addressScore),
			zap.Int("street_matches", streetMatchCount),
			zap.Bool("has_house_number", hasHouseNumber),
			zap.Bool("has_numeric_prefix", hasNumericPrefix),
			zap.Bool("has_phone", hasPhonePattern),
			zap.Bool("has_commas", hasCommas))
		return true, 0.8
	}

	// Check if already committed and in payment stage
	if salesConv.CommitmentLevel == "committed" && salesConv.Stage == "payment_instructions" {
		paymentIntent := strings.Contains(messageLower, "pay") ||
			strings.Contains(messageLower, "payment") ||
			strings.Contains(messageLower, "account") ||
			strings.Contains(messageLower, "bank") ||
			strings.Contains(messageLower, "transfer") ||
			strings.Contains(messageLower, "send money")
		if paymentIntent {
			return true, 0.8
		}
	}

	return false, 0.0
}

// BuildOrderDraft builds a draft order from the sales conversation (no DB writes)
func (m *SalesConversationManager) BuildOrderDraft(salesConv *storage.SalesConversation) (*OrderDraft, error) {
	items, total, err := m.extractOrderItemsFromConversation(salesConv)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no items discussed; cannot build order draft")
	}
	return &OrderDraft{
		Items:       items,
		TotalAmount: total,
		Currency:    "NGN",
		PaymentMode: "pay_on_delivery",
	}, nil
}

// SavePendingOrderDraft persists a pending order draft on the sales conversation
func (m *SalesConversationManager) SavePendingOrderDraft(ctx context.Context, salesConvID uuid.UUID, draft *OrderDraft) error {
	if draft == nil {
		return fmt.Errorf("draft is nil")
	}
	bytes, err := json.Marshal(draft)
	if err != nil {
		return fmt.Errorf("failed to marshal order draft: %w", err)
	}
	now := time.Now()
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Updates(map[string]interface{}{
			"pending_order_draft":  string(bytes),
			"pending_order_status": "pending_confirmation",
			"pending_order_set_at": &now,
		}).Error
}

// ApprovePendingOrder marks the draft as approved (order creation handled elsewhere)
func (m *SalesConversationManager) ApprovePendingOrder(ctx context.Context, salesConvID uuid.UUID) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Updates(map[string]interface{}{
			"pending_order_status": "approved",
		}).Error
}

// DeclinePendingOrder marks the draft as declined
func (m *SalesConversationManager) DeclinePendingOrder(ctx context.Context, salesConvID uuid.UUID) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Updates(map[string]interface{}{
			"pending_order_status": "declined",
		}).Error
}

func (m *SalesConversationManager) extractOrderItemsFromConversation(salesConv *storage.SalesConversation) ([]storage.OrderItem, float64, error) {
	var items []storage.OrderItem
	var totalAmount float64

	rawItems := strings.TrimSpace(salesConv.ItemsDiscussed)
	if rawItems != "" {
		var discussedItems []map[string]interface{}
		if err := json.Unmarshal([]byte(rawItems), &discussedItems); err != nil {
			m.Logger.Warn("resetting invalid items_discussed json", zap.Error(err), zap.Stringer("sales_conversation_id", salesConv.ID))
			discussedItems = []map[string]interface{}{}
			salesConv.ItemsDiscussed = "[]"
			m.DB.Model(salesConv).Update("items_discussed", "[]")
		}

		for _, item := range discussedItems {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}

			var price float64
			if p, ok := item["price"].(float64); ok {
				price = p
			} else if p, ok := item["price"].(int); ok {
				price = float64(p)
			} else {
				if salesConv.PriceDiscussed > 0 && len(discussedItems) == 1 {
					price = salesConv.PriceDiscussed
				} else {
					continue
				}
			}

			quantity := 1
			if q, ok := item["quantity"].(float64); ok {
				quantity = int(q)
			} else if q, ok := item["quantity"].(int); ok {
				quantity = q
			}

			description, _ := item["description"].(string)
			totalPrice := price * float64(quantity)

			items = append(items, storage.OrderItem{
				Name:        name,
				Description: description,
				Quantity:    quantity,
				UnitPrice:   price,
				TotalPrice:  totalPrice,
			})
			totalAmount += totalPrice
		}
	}

	// REMOVED: Fallback order creation from stale PriceDiscussed
	// This was causing the "7K order bug" where orders with no real items
	// were created using stale price values from previous conversations.
	// Orders should only be created when actual items are extracted.

	if len(items) == 0 {
		return nil, 0, fmt.Errorf("no items found in sales conversation - please specify what you want to order")
	}

	return items, totalAmount, nil
}

// DetectNegotiation detects if customer is negotiating
func (m *SalesConversationManager) DetectNegotiation(ctx context.Context, message string) (bool, string, float64) {
	messageLower := strings.ToLower(message)

	// Price negotiation
	priceIndicators := []string{
		"can you reduce", "can you lower", "is there discount",
		"too expensive", "cheaper", "reduce the price", "lower price",
		"what's your best price", "final price", "best offer",
	}

	// Quantity negotiation
	quantityIndicators := []string{
		"if i buy more", "bulk discount", "buy in bulk",
		"what if i take", "multiple items", "several",
	}

	// Terms negotiation
	termsIndicators := []string{
		"payment plan", "installment", "pay later",
		"deposit", "down payment", "terms",
	}

	for _, indicator := range priceIndicators {
		if strings.Contains(messageLower, indicator) {
			return true, "price", 0.8
		}
	}

	for _, indicator := range quantityIndicators {
		if strings.Contains(messageLower, indicator) {
			return true, "quantity", 0.7
		}
	}

	for _, indicator := range termsIndicators {
		if strings.Contains(messageLower, indicator) {
			return true, "terms", 0.7
		}
	}

	return false, "", 0.0
}

// ExtractOrderDetailsFromMessage extracts order details (items, quantity, price) from customer message using AI
func (m *SalesConversationManager) ExtractOrderDetailsFromMessage(ctx context.Context, tenantID uuid.UUID, message string, salesConv *storage.SalesConversation) error {
	// Only extract if intent is order-related or if message contains order indicators
	messageLower := strings.ToLower(message)
	orderIndicators := []string{
		"i want", "i need", "i'll take", "i'll get", "i'll buy",
		"cupcake", "order", "delivery", "pickup", "quantity", "how many",
		"4", "five", "six", "one", "two", "three",
	}

	hasOrderIndicators := false
	for _, indicator := range orderIndicators {
		if strings.Contains(messageLower, indicator) {
			hasOrderIndicators = true
			break
		}
	}

	if !hasOrderIndicators {
		return nil // Skip extraction if no order indicators
	}

	// Use AI to extract structured order details
	prompt := fmt.Sprintf(`Extract ALL order items from this customer message. Return ONLY valid JSON (no additional text):

{
  "items": [
    {"name": "item 1 name", "quantity": 1, "price": 0.0},
    {"name": "item 2 name", "quantity": 2, "price": 0.0}
  ],
  "total_price": 0.0
}

CRITICAL RULES:
- Extract EVERY item mentioned (could be 2, 3, or more items in one message)
- Example: "2 banana bread and 2 chocolate slices" should return TWO items:
  [{"name": "banana bread", "quantity": 2}, {"name": "chocolate slices", "quantity": 2}]
- Include toppings/variations in the item name (e.g., "banana bread with chocolate toppings")
- If quantity not mentioned, default to 1
- If price not mentioned, use 0 (will be filled from catalog)

Customer message: "%s"`, message)

	// Use a simple AI client to extract
	// We'll use the industry service's context if available
	aiClient := NewAIClient()
	messages := []Message{
		{Role: "system", Content: "You are an order detail extractor. Extract items, quantities, and prices from customer messages. Return ONLY valid JSON."},
		{Role: "user", Content: prompt},
	}

	rawReply, err := aiClient.Generate(ctx, messages)
	if err != nil {
		m.Logger.Warn("failed to extract order details with AI, using fallback", zap.Error(err))
		return m.extractOrderDetailsFallback(ctx, message, salesConv)
	}

	var extracted struct {
		Items      []map[string]interface{} `json:"items"`
		TotalPrice float64                  `json:"total_price"`
	}

	if err := json.Unmarshal([]byte(rawReply), &extracted); err != nil {
		m.Logger.Warn("failed to parse extracted order details, using fallback", zap.Error(err))
		return m.extractOrderDetailsFallback(ctx, message, salesConv)
	}

	// If the extractor returns items but no prices, fill prices from the tenant's catalog.
	// This is critical because ensureOrderAndPayment relies on a deterministic amount.
	filledTotal, filledCurrency := m.fillMissingItemPricesFromCatalog(ctx, tenantID, extracted.Items)
	if extracted.TotalPrice <= 0 && filledTotal > 0 {
		extracted.TotalPrice = filledTotal
	}
	_ = filledCurrency

	// Update sales conversation with extracted items
	if len(extracted.Items) > 0 {
		// Merge with existing items
		var existingItems []map[string]interface{}
		if salesConv.ItemsDiscussed != "" && salesConv.ItemsDiscussed != "[]" {
			json.Unmarshal([]byte(salesConv.ItemsDiscussed), &existingItems)
		}

		// Add new items (avoid duplicates by name)
		for _, newItem := range extracted.Items {
			itemName, _ := newItem["name"].(string)
			if itemName == "" {
				continue
			}

			// ==========================================================================
			// CRITICAL: Blacklist for concept words that should NEVER be product names
			// These are conversation concepts, not actual products/services
			// ==========================================================================
			lowerName := strings.ToLower(strings.TrimSpace(itemName))
			conceptBlacklist := []string{
				"order", "new order", "existing order", "my order", "the order",
				"cancel", "cancellation", "cancelled",
				"payment", "pay", "transfer", "bank",
				"delivery", "pickup", "address",
				"help", "hello", "hi", "hey", "thanks", "thank you",
				"yes", "no", "okay", "ok",
			}
			isBlacklisted := false
			for _, blacklisted := range conceptBlacklist {
				if lowerName == blacklisted {
					isBlacklisted = true
					m.Logger.Warn("🚫 Blocked concept word from being added as order item",
						zap.String("blocked_name", itemName),
						zap.Stringer("sales_conversation_id", salesConv.ID))
					break
				}
			}
			if isBlacklisted {
				continue
			}

			// Check if item already exists
			exists := false
			for i, existing := range existingItems {
				if existingName, ok := existing["name"].(string); ok && existingName == itemName {
					// Update existing item with new quantity/price if provided
					if q, ok := newItem["quantity"].(float64); ok && q > 0 {
						existingItems[i]["quantity"] = q
					}
					if p, ok := newItem["price"].(float64); ok && p > 0 {
						existingItems[i]["price"] = p
					}
					exists = true
					break
				}
			}

			if !exists {
				// Ensure quantity defaults to 1
				if q, ok := newItem["quantity"].(float64); !ok || q == 0 {
					newItem["quantity"] = 1.0
				}
				existingItems = append(existingItems, newItem)
			}
		}

		itemsJSON, _ := json.Marshal(existingItems)
		salesConv.ItemsDiscussed = string(itemsJSON)
	}

	// Update price discussed if total price is available
	if extracted.TotalPrice > 0 {
		salesConv.PriceDiscussed = extracted.TotalPrice
	} else if len(extracted.Items) > 0 {
		// Calculate total from items if total_price not provided
		total := 0.0
		for _, item := range extracted.Items {
			if p, ok := item["price"].(float64); ok {
				q := 1.0
				if qty, ok := item["quantity"].(float64); ok {
					q = qty
				}
				total += p * q
			}
		}
		if total > 0 {
			salesConv.PriceDiscussed = total
		}
	}

	// Save updated sales conversation
	return m.DB.Save(salesConv).Error
}

func (m *SalesConversationManager) fillMissingItemPricesFromCatalog(ctx context.Context, tenantID uuid.UUID, items []map[string]interface{}) (float64, string) {
	if len(items) == 0 {
		return 0, ""
	}

	total := 0.0
	currency := "NGN"

	for _, item := range items {
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		qty := 1.0
		if q, ok := item["quantity"].(float64); ok && q > 0 {
			qty = q
		} else if q, ok := item["quantity"].(int); ok && q > 0 {
			qty = float64(q)
		}

		price := 0.0
		switch v := item["price"].(type) {
		case float64:
			price = v
		case int:
			price = float64(v)
		case string:
			price = utils.ExtractLikelyAmountNGN(v)
		}

		if price <= 0 {
			catalogPrice, catalogCurrency, ok := m.lookupCatalogPriceForName(ctx, tenantID, name)
			if ok && catalogPrice > 0 {
				price = catalogPrice
				item["price"] = catalogPrice
				if catalogCurrency != "" {
					currency = catalogCurrency
				}
			}
		}

		if price > 0 {
			total += price * qty
		}
	}

	return total, currency
}

func (m *SalesConversationManager) lookupCatalogPriceForName(ctx context.Context, tenantID uuid.UUID, rawName string) (float64, string, bool) {
	needle := strings.ToLower(strings.TrimSpace(rawName))
	if needle == "" {
		return 0, "", false
	}

	// Get tenant's industry capabilities to filter by current industry
	// CRITICAL: Only look up prices for items that match current industry
	capabilities, err := m.IndustryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		m.Logger.Warn("failed to get industry capabilities for price lookup", zap.Error(err))
		// Continue with lookup but log warning
	}

	// Exact matches first (fast + deterministic) - only check items for current industry
	if capabilities == nil || capabilities.HasProducts {
		var p storage.Product
		err := m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) = ?", needle).
			First(&p).Error
		if err == nil && p.Price > 0 {
			return p.Price, p.Currency, true
		}
	}
	if capabilities == nil || capabilities.HasMenu {
		var mi storage.MenuItem
		err := m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) = ?", needle).
			First(&mi).Error
		if err == nil && mi.Price > 0 {
			return mi.Price, mi.Currency, true
		}
	}
	if capabilities == nil || capabilities.HasServices {
		var svc storage.Service
		err := m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) = ?", needle).
			First(&svc).Error
		if err == nil {
			if amt, ok := parseServiceFixedAmount(svc.Pricing); ok {
				return amt, "NGN", true
			}
		}
	}

	// Fuzzy/contains fallback (still deterministic: only use clear matches)
	// CRITICAL: Filter by industry capabilities to avoid returning prices from wrong industry
	bestScore := 0
	bestAmount := 0.0
	bestCurrency := ""

	// Products - only if current industry supports products
	if capabilities == nil || capabilities.HasProducts {
		var candidates []storage.Product
		_ = m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) LIKE ?", "%"+needle+"%").
			Limit(20).
			Find(&candidates).Error
		for _, c := range candidates {
			score := nameMatchScore(rawName, c.Name)
			if score > bestScore && c.Price > 0 {
				bestScore = score
				bestAmount = c.Price
				bestCurrency = c.Currency
			}
		}
	}

	// Menu items - only if current industry supports menu
	if capabilities == nil || capabilities.HasMenu {
		var candidates []storage.MenuItem
		_ = m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) LIKE ?", "%"+needle+"%").
			Limit(20).
			Find(&candidates).Error
		for _, c := range candidates {
			score := nameMatchScore(rawName, c.Name)
			if score > bestScore && c.Price > 0 {
				bestScore = score
				bestAmount = c.Price
				bestCurrency = c.Currency
			}
		}
	}

	// Services - only if current industry supports services
	if capabilities == nil || capabilities.HasServices {
		var candidates []storage.Service
		_ = m.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Where("LOWER(name) LIKE ?", "%"+needle+"%").
			Limit(20).
			Find(&candidates).Error
		for _, c := range candidates {
			score := nameMatchScore(rawName, c.Name)
			amt, ok := parseServiceFixedAmount(c.Pricing)
			if score > bestScore && ok && amt > 0 {
				bestScore = score
				bestAmount = amt
				bestCurrency = "NGN"
			}
		}
	}

	if bestScore >= 60 && bestAmount > 0 {
		return bestAmount, bestCurrency, true
	}

	return 0, "", false
}

func parseServiceFixedAmount(pricingJSON string) (float64, bool) {
	pricingJSON = strings.TrimSpace(pricingJSON)
	if pricingJSON == "" {
		return 0, false
	}

	var parsed struct {
		Type   string  `json:"type"`
		Amount float64 `json:"amount"`
		Rate   float64 `json:"rate"`
	}

	if err := json.Unmarshal([]byte(pricingJSON), &parsed); err != nil {
		return 0, false
	}

	// Only use fixed pricing deterministically
	if parsed.Amount > 0 && (parsed.Type == "fixed" || parsed.Type == "" /* older data */) {
		return parsed.Amount, true
	}

	return 0, false
}

func parseBusinessMenuPriceNGN(price string) float64 {
	price = strings.TrimSpace(price)
	if price == "" {
		return 0
	}
	if amt := utils.ExtractLikelyAmountNGN(price); amt > 0 {
		return amt
	}
	if f, err := strconv.ParseFloat(price, 64); err == nil {
		return f
	}
	return 0
}

func normalizeNameForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	// remove punctuation except spaces and alphanumerics
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			return r
		}
		return -1
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func nameMatchScore(query, candidate string) int {
	q := normalizeNameForMatch(query)
	c := normalizeNameForMatch(candidate)
	if q == "" || c == "" {
		return 0
	}
	if q == c {
		return 100
	}
	if strings.Contains(c, q) || strings.Contains(q, c) {
		return 80
	}
	qt := strings.Fields(q)
	ct := strings.Fields(c)
	overlap := 0
	for _, qtok := range qt {
		for _, ctok := range ct {
			if qtok == ctok {
				overlap++
				break
			}
		}
	}
	return overlap * 10
}

// extractOrderDetailsFallback uses simple pattern matching as fallback
func (m *SalesConversationManager) extractOrderDetailsFallback(ctx context.Context, message string, salesConv *storage.SalesConversation) error {
	messageLower := strings.ToLower(message)

	// Simple extraction: look for numbers followed by item names
	// This is a basic fallback - AI extraction is preferred

	// Extract quantity (look for numbers at start or after "i want", "i need", etc.)
	quantity := 1
	words := strings.Fields(messageLower)
	for i, word := range words {
		if word == "i" && i+1 < len(words) && (words[i+1] == "want" || words[i+1] == "need" || words[i+1] == "take") {
			// Look for number after "i want/need/take"
			if i+2 < len(words) {
				if q, err := strconv.Atoi(words[i+2]); err == nil && q > 0 && q < 100 {
					quantity = q
					break
				}
			}
		}
		// Direct number at start
		if i == 0 {
			if q, err := strconv.Atoi(word); err == nil && q > 0 && q < 100 {
				quantity = q
			}
		}
	}

	// Try to extract item name (look for common product words)
	itemName := ""
	productKeywords := []string{"cupcake", "cake", "item", "product", "order"}
	for _, keyword := range productKeywords {
		if strings.Contains(messageLower, keyword) {
			// Extract surrounding context as item name
			idx := strings.Index(messageLower, keyword)
			start := maxInt(0, idx-20)
			end := minInt(len(messageLower), idx+len(keyword)+20)
			itemName = strings.TrimSpace(messageLower[start:end])
			// Clean up
			itemName = strings.Trim(itemName, ".,!?;:")
			if len(itemName) > 50 {
				itemName = itemName[:50]
			}
			break
		}
	}

	if itemName == "" {
		itemName = "Order Item"
	}

	// Add to items discussed
	var existingItems []map[string]interface{}
	if salesConv.ItemsDiscussed != "" && salesConv.ItemsDiscussed != "[]" {
		json.Unmarshal([]byte(salesConv.ItemsDiscussed), &existingItems)
	}

	// Check if item already exists
	exists := false
	for i, existing := range existingItems {
		if existingName, ok := existing["name"].(string); ok && strings.Contains(strings.ToLower(existingName), strings.ToLower(itemName)) {
			existingItems[i]["quantity"] = float64(quantity)
			exists = true
			break
		}
	}

	if !exists {
		existingItems = append(existingItems, map[string]interface{}{
			"name":     itemName,
			"quantity": float64(quantity),
			"price":    0.0, // Will be filled from product catalog
		})
	}

	itemsJSON, _ := json.Marshal(existingItems)
	salesConv.ItemsDiscussed = string(itemsJSON)

	return m.DB.Save(salesConv).Error
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DetectCompleteOrderDetails detects if minimum order details have been collected
// Returns true if items and price are present (delivery/contact can be gathered later)
// RELAXED: Orders can be created with pending payment when customer confirms
func (m *SalesConversationManager) DetectCompleteOrderDetails(ctx context.Context, salesConv *storage.SalesConversation, message string, customerPhone string) bool {
	var parsedItems []map[string]interface{}
	rawItems := strings.TrimSpace(salesConv.ItemsDiscussed)
	if rawItems != "" && rawItems != "[]" {
		if err := json.Unmarshal([]byte(rawItems), &parsedItems); err != nil {
			m.Logger.Warn("resetting invalid items_discussed json", zap.Error(err), zap.Stringer("sales_conversation_id", salesConv.ID))
			parsedItems = nil
			salesConv.ItemsDiscussed = "[]"
			m.DB.Model(salesConv).Update("items_discussed", "[]")
		}
	}

	// Check if items are discussed - REQUIRED
	hasItems := len(parsedItems) > 0

	// Check if price is discussed - REQUIRED
	// Also check if items have prices attached
	hasPrice := salesConv.PriceDiscussed > 0
	if !hasPrice && len(parsedItems) > 0 {
		// Check if any item has a price
		for _, item := range parsedItems {
			if p, ok := item["price"].(float64); ok && p > 0 {
				hasPrice = true
				break
			}
		}
	}

	// MINIMUM REQUIREMENT: Just need items and price to create order with pending payment
	// Delivery/contact info can be gathered later - order is created in "pending" status
	return hasItems && hasPrice
}
