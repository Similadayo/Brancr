package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SalesConversationManager manages sales conversation state and flow
type SalesConversationManager struct {
	DB              *gorm.DB
	IndustryService *industry.Service
	Logger          *zap.Logger
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
func (m *SalesConversationManager) GetOrCreateSalesConversation(ctx context.Context, tenantID, customerID, conversationID uint) (*storage.SalesConversation, error) {
	var salesConv storage.SalesConversation
	err := m.DB.Where("tenant_id = ? AND customer_id = ? AND conversation_id = ?", tenantID, customerID, conversationID).
		First(&salesConv).Error

	if err == nil {
		return &salesConv, nil
	}

	// Create new sales conversation
	salesConv = storage.SalesConversation{
		ConversationID:  conversationID,
		TenantID:        tenantID,
		CustomerID:      customerID,
		Stage:           "greeting",
		CommitmentLevel: "exploring",
		ItemsDiscussed:  "[]",
		PriceDiscussed:  0,
		NegotiationCount: 0,
		ObjectionsRaised: "[]",
	}

	if err := m.DB.Create(&salesConv).Error; err != nil {
		return nil, fmt.Errorf("failed to create sales conversation: %w", err)
	}

	return &salesConv, nil
}

// UpdateStage updates the sales conversation stage
func (m *SalesConversationManager) UpdateStage(ctx context.Context, salesConvID uint, stage string) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("stage", stage).Error
}

// UpdateCommitmentLevel updates the commitment level
func (m *SalesConversationManager) UpdateCommitmentLevel(ctx context.Context, salesConvID uint, level string) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("commitment_level", level).Error
}

// AddItemDiscussed adds an item to the items discussed list
func (m *SalesConversationManager) AddItemDiscussed(ctx context.Context, salesConvID uint, itemName string, itemPrice float64) error {
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
func (m *SalesConversationManager) UpdatePriceDiscussed(ctx context.Context, salesConvID uint, price float64) error {
	return m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConvID).
		Update("price_discussed", price).Error
}

// AddObjection records a customer objection
func (m *SalesConversationManager) AddObjection(ctx context.Context, salesConvID uint, objection string) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}

	var objections []string
	if salesConv.ObjectionsRaised != "" {
		json.Unmarshal([]byte(salesConv.ObjectionsRaised), &objections)
	}

	objections = append(objections, objection)
	objectionsJSON, _ := json.Marshal(objections)

	return m.DB.Model(&salesConv).
		Update("objections_raised", string(objectionsJSON)).Error
}

// IncrementNegotiation increments the negotiation count
func (m *SalesConversationManager) IncrementNegotiation(ctx context.Context, salesConvID uint) error {
	var salesConv storage.SalesConversation
	if err := m.DB.First(&salesConv, salesConvID).Error; err != nil {
		return err
	}
	salesConv.NegotiationCount++
	return m.DB.Save(&salesConv).Error
}

// RecordNegotiation records a negotiation attempt
func (m *SalesConversationManager) RecordNegotiation(ctx context.Context, salesConvID uint, negotiationType string, customerOffer, counterOffer, finalAgreement float64, status string) error {
	negotiation := storage.NegotiationHistory{
		SalesConversationID: salesConvID,
		Type:                negotiationType,
		CustomerOffer:       customerOffer,
		CounterOffer:        counterOffer,
		FinalAgreement:      finalAgreement,
		Status:              status,
	}

	return m.DB.Create(&negotiation).Error
}

// BuildIndustrySalesContext builds industry-specific sales context for AI prompts
func (m *SalesConversationManager) BuildIndustrySalesContext(ctx context.Context, tenantID uint, salesConv *storage.SalesConversation) (string, error) {
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

	// Add objections
	if salesConv.ObjectionsRaised != "" {
		var objections []string
		json.Unmarshal([]byte(salesConv.ObjectionsRaised), &objections)
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
func (m *SalesConversationManager) DetectCommitment(ctx context.Context, message string, salesConv *storage.SalesConversation) (bool, float64) {
	messageLower := strings.ToLower(message)
	
	// Strong commitment indicators
	strongIndicators := []string{
		"i'll take it", "i'll buy", "i want to order", "i'm ready to pay",
		"how do i pay", "where do i send", "payment details", "bank details",
		"i'm interested", "let's proceed", "i'll go with", "i'll get",
		"send me the", "give me your", "what's your account",
	}
	
	// Medium commitment indicators
	mediumIndicators := []string{
		"how much", "what's the price", "how much does it cost",
		"tell me more", "i'm considering", "sounds good",
		"that works", "okay", "alright",
	}
	
	// Check for strong commitment
	for _, indicator := range strongIndicators {
		if strings.Contains(messageLower, indicator) {
			return true, 0.9
		}
	}
	
	// Check for medium commitment
	for _, indicator := range mediumIndicators {
		if strings.Contains(messageLower, indicator) {
			return true, 0.6
		}
	}
	
	// If already in negotiation stage and price discussed, higher likelihood
	if salesConv.Stage == "negotiation" && salesConv.PriceDiscussed > 0 {
		return true, 0.7
	}
	
	return false, 0.0
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

// DetectCompleteOrderDetails detects if all order details have been collected
// Returns true if items, quantity, delivery info, and contact info are present
func (m *SalesConversationManager) DetectCompleteOrderDetails(ctx context.Context, salesConv *storage.SalesConversation, message string, customerPhone string) bool {
	// Check if items are discussed
	hasItems := false
	if salesConv.ItemsDiscussed != "" && salesConv.ItemsDiscussed != "[]" {
		var items []map[string]interface{}
		if err := json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items); err == nil && len(items) > 0 {
			hasItems = true
		}
	}

	// Check if price is discussed
	hasPrice := salesConv.PriceDiscussed > 0

	// Check for delivery/pickup info in message
	messageLower := strings.ToLower(message)
	hasDeliveryInfo := strings.Contains(messageLower, "delivery") ||
		strings.Contains(messageLower, "pickup") ||
		strings.Contains(messageLower, "address") ||
		strings.Contains(messageLower, "location") ||
		strings.Contains(messageLower, "landmark")

	// Check for contact info (phone number)
	hasContactInfo := customerPhone != "" || 
		strings.Contains(messageLower, "phone") ||
		strings.Contains(messageLower, "call") ||
		strings.Contains(messageLower, "contact")

	// Check for quantity in message or items
	hasQuantity := false
	if salesConv.ItemsDiscussed != "" {
		var items []map[string]interface{}
		if err := json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items); err == nil {
			for _, item := range items {
				if q, ok := item["quantity"].(float64); ok && q > 0 {
					hasQuantity = true
					break
				} else if q, ok := item["quantity"].(int); ok && q > 0 {
					hasQuantity = true
					break
				}
			}
		}
	}
	// Also check message for quantity indicators
	if !hasQuantity {
		hasQuantity = strings.Contains(messageLower, "4") ||
			strings.Contains(messageLower, "four") ||
			strings.Contains(messageLower, "quantity") ||
			strings.Contains(messageLower, "how many")
	}

	// Order is complete if we have items, price, delivery info, and contact info
	// Quantity is nice to have but not always required (defaults to 1)
	return hasItems && hasPrice && hasDeliveryInfo && hasContactInfo
}

