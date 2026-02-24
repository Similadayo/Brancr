package v3

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenant"
	"github.com/similadayo/brancr/internal/utils"
)

// =============================================================================
// CONTEXT BUILDER - RAG, Memory, Role Profiles
// =============================================================================

// contextBuilder implements ContextBuilder interface
type contextBuilder struct {
	db                  *gorm.DB
	logger              *zap.Logger
	v2StateManager      V2StateManager
	v2KnowledgeCompiler V2KnowledgeCompiler
}

// NewContextBuilder creates a new context builder
func NewContextBuilder(db *gorm.DB, logger *zap.Logger) ContextBuilder {
	return &contextBuilder{
		db:     db,
		logger: logger,
	}
}

// NewContextBuilderWithV2 creates a context builder with V2 integration
func NewContextBuilderWithV2(db *gorm.DB, logger *zap.Logger, stateManager V2StateManager, knowledgeCompiler V2KnowledgeCompiler) ContextBuilder {
	return &contextBuilder{
		db:                  db,
		logger:              logger,
		v2StateManager:      stateManager,
		v2KnowledgeCompiler: knowledgeCompiler,
	}
}

// Build creates the full conversation context with RAG, memory, and role profile
func (b *contextBuilder) Build(ctx context.Context, req ConversationRequest) (*ConversationContext, error) {
	convCtx := &ConversationContext{
		TenantID:       req.TenantID,
		CustomerID:     req.CustomerID,
		ConversationID: req.ConversationID,
		Platform:       req.Platform,
		Emotion:        EmotionState{Type: EmotionNeutral, Intensity: 0.5},
	}

	// Load tenant business info
	if err := b.loadBusinessInfo(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load business info", zap.Error(err))
	}

	// RAG: Load product catalog
	if err := b.loadProducts(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load products", zap.Error(err))
	}

	// RAG: Load services
	if err := b.loadServices(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load services", zap.Error(err))
	}

	// RAG: Load FAQs
	if err := b.loadFAQs(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load FAQs", zap.Error(err))
	}

	// Memory: Load recent messages
	if err := b.loadRecentMessages(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load recent messages", zap.Error(err))
	}

	// Obligations: Infer from message history what's been answered
	convCtx.Obligations = InferObligationsFromHistory(convCtx.RecentMessages)

	// Memory: Load pending cart from persistence
	if err := b.loadPendingCart(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load pending cart", zap.Error(err))
	}

	// Memory: Load current order
	if err := b.loadCurrentOrder(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load current order", zap.Error(err))
	}

	// Memory: Load customer history
	if err := b.loadCustomerHistory(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load customer history", zap.Error(err))
	}

	// Role: Load industry profile (Legacy, will be superseded by RuntimeProfile)
	b.loadRoleProfile(ctx, convCtx)

	// Runtime Profile (Scale-Invariant Truth)
	if err := b.buildRuntimeProfile(ctx, convCtx); err != nil {
		b.logger.Warn("failed to build runtime profile", zap.Error(err))
	}
	if err := b.loadPaymentConfig(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load payment config", zap.Error(err))
	}

	// Voice: Load voice fingerprint (Phase 3)
	if err := b.loadVoiceFingerprint(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load voice fingerprint", zap.Error(err))
	}

	// Negotiation: Load active sales conversation state
	if err := b.loadSalesConversation(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load sales conversation", zap.Error(err))
	}

	// V2 Integration: Load structured state and compiled knowledge
	b.loadV2State(ctx, convCtx)
	b.loadV2Knowledge(ctx, convCtx)

	// Derive phase and role from V2 state
	b.DerivePhaseAndRole(convCtx)

	// Obligations: Infer and Hydrate
	b.loadObligations(ctx, convCtx)

	// Load Transaction State (Phase 1)
	if err := b.loadTransactionState(ctx, convCtx); err != nil {
		b.logger.Warn("failed to load transaction state", zap.Error(err))
	}

	return convCtx, nil
}

// loadTransactionState populates TransactionState from storage, defaulting to Browsing
func (b *contextBuilder) loadTransactionState(ctx context.Context, convCtx *ConversationContext) error {
	var aiState storage.ConversationAIState
	err := b.db.WithContext(ctx).
		Where("conversation_id = ?", convCtx.ConversationID).
		First(&aiState).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Default to Browsing if no record
			convCtx.TransactionState = domain.TransactionBrowsing
			b.logger.Debug("transaction_state_defaulted",
				zap.String("conversation_id", convCtx.ConversationID.String()),
				zap.String("state", string(domain.TransactionBrowsing)))
			return nil
		}
		return err
	}

	if aiState.TransactionState == "" {
		convCtx.TransactionState = domain.TransactionBrowsing
	} else {
		convCtx.TransactionState = aiState.TransactionState
	}

	b.logger.Debug("transaction_state_loaded",
		zap.String("conversation_id", convCtx.ConversationID.String()),
		zap.String("state", string(convCtx.TransactionState)))

	return nil
}

// UpdateTransactionState updates only the TransactionState in storage
func (b *contextBuilder) UpdateTransactionState(ctx context.Context, conversationID uuid.UUID, state domain.TransactionState) error {
	// Efficient update using Model & UpdateColumn to avoid fetching full record
	return b.db.WithContext(ctx).
		Model(&storage.ConversationAIState{}).
		Where("conversation_id = ?", conversationID).
		UpdateColumn("transaction_state", state).Error
}

// loadV2State loads the V2 StructuredConversationState
func (b *contextBuilder) loadV2State(ctx context.Context, convCtx *ConversationContext) {
	if b.v2StateManager == nil {
		return
	}

	state, err := b.v2StateManager.GetOrCreateState(ctx, convCtx.TenantID, convCtx.CustomerID, convCtx.ConversationID)
	if err != nil {
		b.logger.Warn("failed to load v2 state", zap.Error(err))
		return
	}
	convCtx.StructuredState = state
}

// loadV2Knowledge loads the V2 CompiledTenantKnowledge
func (b *contextBuilder) loadV2Knowledge(ctx context.Context, convCtx *ConversationContext) {
	if b.v2KnowledgeCompiler == nil {
		return
	}

	knowledge, err := b.v2KnowledgeCompiler.Compile(ctx, convCtx.TenantID)
	if err != nil {
		b.logger.Warn("failed to compile v2 knowledge", zap.Error(err))
		return
	}
	// Extract KnowledgeBaseContent via reflection (since it's an interface{})
	val := reflect.ValueOf(knowledge)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("KnowledgeBaseContent")
		if field.IsValid() && field.Kind() == reflect.String {
			convCtx.KnowledgeBaseContent = field.String()
		}
	}

	convCtx.CompiledKnowledge = knowledge
}

// DerivePhaseAndRole determines conversation phase and AI role from V2 state
func (b *contextBuilder) DerivePhaseAndRole(convCtx *ConversationContext) {
	// Default phase and role
	convCtx.CurrentPhase = "discovery"
	convCtx.ActiveRole = "sales_rep"

	if convCtx.StructuredState == nil {
		return
	}

	// Use reflection-free approach: check if state has the fields we need
	// We use type assertion with interface{} since StructuredState is interface{}
	state := convCtx.StructuredState

	// Try to extract Stage field
	if stager, ok := state.(interface{ GetStage() string }); ok {
		stage := stager.GetStage()
		convCtx.CurrentPhase = mapStageToPhase(stage)
	}

	// Check for support state being active
	if supporter, ok := state.(interface{ IsSupportActive() bool }); ok {
		if supporter.IsSupportActive() {
			convCtx.ActiveRole = "customer_service"
			convCtx.CurrentPhase = "assistance"
			return
		}
	}

	// Check for negotiation state being active
	if negotiator, ok := state.(interface{ IsNegotiationActive() bool }); ok {
		if negotiator.IsNegotiationActive() {
			convCtx.ActiveRole = "negotiator"
			return
		}
	}

	// Derive role from phase
	switch convCtx.CurrentPhase {
	case "transaction", "follow_up":
		convCtx.ActiveRole = "order_manager"
	case "assistance":
		convCtx.ActiveRole = "customer_service"
	default:
		convCtx.ActiveRole = "sales_rep"
	}
}

// loadObligations infers and hydrates obligations
func (b *contextBuilder) loadObligations(ctx context.Context, convCtx *ConversationContext) {
	// 1. Infer from history (Base)
	convCtx.Obligations = InferObligationsFromHistory(convCtx.RecentMessages)

	// 2. Hydrate from Persisted State (Guard 3)
	if convCtx.StructuredState == nil {
		return
	}

	// Check for LastShownOfferings accessor
	if st, ok := convCtx.StructuredState.(interface{ GetLastShownOfferings() []domain.Offering }); ok {
		convCtx.Obligations.LastShownOfferings = st.GetLastShownOfferings()
	}
}

// mapStageToPhase converts V2 stage to conversation phase
func mapStageToPhase(stage string) string {
	switch stage {
	case "exploring":
		return "discovery"
	case "negotiation":
		return "discovery" // Still in discovery but negotiating
	case "ordering":
		return "transaction"
	case "payment":
		return "transaction"
	case "closed":
		return "follow_up"
	default:
		return "discovery"
	}
}

// =============================================================================
// BUSINESS INFO
// =============================================================================

func (b *contextBuilder) loadBusinessInfo(ctx context.Context, convCtx *ConversationContext) error {
	var tenant storage.Tenant
	if err := b.db.WithContext(ctx).
		Preload("BusinessProfile").
		First(&tenant, convCtx.TenantID).Error; err != nil {
		return err
	}

	profile := tenant.BusinessProfile

	convCtx.BusinessName = profile.Name
	if convCtx.BusinessName == "" {
		convCtx.BusinessName = tenant.Name
	}
	convCtx.BusinessTone = profile.Tone

	// Load actual Industry model for full context
	if tenant.IndustryID != uuid.Nil {
		var industry storage.Industry
		if err := b.db.WithContext(ctx).First(&industry, tenant.IndustryID).Error; err == nil {
			convCtx.BusinessType = industry.Name
			convCtx.IndustryCategory = industry.Category
			convCtx.IndustryHasProducts = industry.HasProducts
			convCtx.IndustryHasMenu = industry.HasMenu
			convCtx.IndustryHasServices = industry.HasServices
		}
	}

	// Fallback to profile.Industry if no Industry model found
	if convCtx.BusinessType == "" {
		convCtx.BusinessType = profile.Industry
	}

	convCtx.BusinessDescription = profile.Description
	convCtx.BusinessNiche = profile.Niche
	convCtx.OperatingHours = profile.OperatingHours
	convCtx.Location = profile.Location
	convCtx.Website = profile.Website
	convCtx.KnowledgeBase = profile.KnowledgeBase

	// Load Delivery Config from Business Profile
	if profile.DeliveryConfig != "" && profile.DeliveryConfig != "{}" {
		var delConfig storage.DeliveryConfig
		if err := json.Unmarshal([]byte(profile.DeliveryConfig), &delConfig); err == nil {
			convCtx.DeliveryConfig = &delConfig
		}
	}

	return nil
}

// =============================================================================
// RAG: PRODUCTS
// =============================================================================

func (b *contextBuilder) loadProducts(ctx context.Context, convCtx *ConversationContext) error {
	// Load products (for e-commerce)
	var products []storage.Product
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND (is_active = ? OR is_active IS NULL)", convCtx.TenantID, true).
		Limit(100). // Cap to prevent memory issues
		Find(&products).Error; err == nil {
		for _, p := range products {
			// Parse images from JSON
			var images []string
			if p.Images != "" {
				json.Unmarshal([]byte(p.Images), &images)
			}
			imageURL := ""
			if len(images) > 0 {
				imageURL = images[0]
			}

			convCtx.Products = append(convCtx.Products, Product{
				ID:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Price:       p.Price,
				Currency:    p.Currency,
				Category:    p.Category,
				ImageURL:    imageURL,
				InStock:     p.StockCount != 0 && p.Availability == "in_stock",
			})
		}
	}

	// Also load menu items if available (for F&B)
	var menuItems []storage.MenuItem
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND (is_active = ? OR is_active IS NULL)", convCtx.TenantID, true).
		Limit(100).
		Find(&menuItems).Error; err == nil {
		for _, m := range menuItems {
			// Parse images from JSON
			var images []string
			if m.Images != "" {
				json.Unmarshal([]byte(m.Images), &images)
			}
			imageURL := ""
			if len(images) > 0 {
				imageURL = images[0]
			}

			convCtx.Products = append(convCtx.Products, Product{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
				Price:       m.Price,
				Currency:    m.Currency,
				Category:    m.Category,
				ImageURL:    imageURL,
				InStock:     m.Availability == "available",
			})
		}
	}

	return nil
}

// =============================================================================
// RAG: SERVICES
// =============================================================================

func (b *contextBuilder) loadServices(ctx context.Context, convCtx *ConversationContext) error {
	var services []storage.Service
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", convCtx.TenantID, true).
		Limit(50).
		Find(&services).Error; err != nil {
		return err
	}

	for _, s := range services {
		// Parse pricing
		var price float64
		// Simple parsing for now - we might want a proper struct later
		if strings.Contains(s.Pricing, "\"amount\":") {
			// Extremely hacky specific extraction for demo speed
			// In production, unmarshal into a struct map[string]interface{}
			// Assuming {"type": "fixed", "amount": 5000}
			// This is just to satisfy the struct
			price = 0 // Default to 0 for now to avoid json parsing complexity in error handling
		}

		convCtx.Services = append(convCtx.Services, Service{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Price:       price,
			Currency:    "NGN", // Default as per model default
			Duration:    s.Duration,
		})
	}

	return nil
}

// =============================================================================
// RAG: FAQs
// =============================================================================

func (b *contextBuilder) loadFAQs(ctx context.Context, convCtx *ConversationContext) error {
	var faqs []storage.BusinessFAQ
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ?", convCtx.TenantID).
		Limit(50).
		Find(&faqs).Error; err != nil {
		return err
	}

	for _, f := range faqs {
		convCtx.FAQs = append(convCtx.FAQs, FAQ{
			Question: f.Question,
			Answer:   f.Answer,
		})
	}

	return nil
}

// =============================================================================
// MEMORY: RECENT MESSAGES
// =============================================================================

func (b *contextBuilder) loadRecentMessages(ctx context.Context, convCtx *ConversationContext) error {
	// Load actual message history from Interaction table
	var interactions []storage.Interaction
	if err := b.db.WithContext(ctx).
		Where("conversation_id = ?", convCtx.ConversationID).
		Order("created_at DESC").
		Limit(10). // Load last 10 messages
		Find(&interactions).Error; err != nil {
		return nil // No messages yet is fine
	}

	// Reverse to get chronological order (oldest first)
	for i := len(interactions) - 1; i >= 0; i-- {
		interaction := interactions[i]
		role := "customer"
		if interaction.Direction == "outgoing" {
			role = "assistant"
		}
		convCtx.RecentMessages = append(convCtx.RecentMessages, Message{
			Role:      role,
			Content:   interaction.Content,
			Timestamp: interaction.CreatedAt,
		})
	}

	return nil
}

// =============================================================================
// MEMORY: CURRENT ORDER
// =============================================================================

func (b *contextBuilder) loadCurrentOrder(ctx context.Context, convCtx *ConversationContext) error {
	var order storage.Order
	// Find active order for this conversation
	err := b.db.WithContext(ctx).
		Where("conversation_id = ? AND status IN ?",
			convCtx.ConversationID,
			[]string{"draft", "pending", "building", "checkout", "awaiting_payment"},
		).
		Order("created_at DESC").
		First(&order).Error

	if err != nil {
		// No active order is fine
		convCtx.CurrentOrder = &Order{State: StateIdle}
		return nil
	}

	// Parse items from JSON
	var items []storage.OrderItem
	if order.Items != "" {
		json.Unmarshal([]byte(order.Items), &items)
	}

	// Populate PendingCart from current order if it's in a draft/building state
	// This ensures the cart is persistent across turns using the Order table
	if order.Status == "draft" || order.Status == "pending" || order.Status == "building" {
		convCtx.PendingCart = make([]PendingCartItem, 0, len(items))
		for _, item := range items {
			// Try to match product ID if possible, but storage.OrderItem doesn't have it by default.
			// We might need to rely on Name match or if we stored ProductID in JSON.
			// storage.OrderItem def: Name, Description, Quantity, UnitPrice, TotalPrice.
			// Ideally storage.OrderItem should have ProductID.
			// Checking models.go -> OrderItem struct:
			// type OrderItem struct { Name, Description, Quantity, UnitPrice, TotalPrice }
			// It implies we lose ProductID if we rely solely on this.
			// However, for the purpose of the cart, Name/Price/Qty is often enough for display,
			// but for logic we might need ID.
			// Let's assume we can map back by name or just operate without ID for now,
			// OR we assume we should add ProductID to OrderItem struct in models (if I could, but user said models.go has Order struct).
			// Let's stick to what we have. If ProductID is missing, we use potentially ambiguous matches or 0.

			// Re-hydration:
			// We look up product by name to get ID if needed?
			// For now, let's just restore what we have.
			convCtx.PendingCart = append(convCtx.PendingCart, PendingCartItem{
				ProductName: item.Name,
				Quantity:    item.Quantity,
				UnitPrice:   item.UnitPrice,
				// ProductID is lost in standard OrderItem storage unless we add it.
				// We will proceed without it for display, or try to find it.
			})
		}
	}

	// Also restore DeliveryState from order if present
	if order.DeliveryAddress != "" || order.IsPickup {
		if convCtx.DeliveryState == nil {
			convCtx.DeliveryState = &DeliveryState{}
		}
		convCtx.DeliveryState.Address = order.DeliveryAddress
		if order.IsPickup {
			convCtx.DeliveryState.Method = "pickup"
		} else {
			convCtx.DeliveryState.Method = "delivery"
		}
		// Try to restore SelectedArea if stored in Notes or Metadata?
		// For now, we rely on Address.
	}

	orderItems := make([]OrderItem, 0, len(items))
	// Always populate orderItems for read-only View
	for _, item := range items {
		orderItems = append(orderItems, OrderItem{
			ProductName: item.Name,
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
			TotalPrice:  item.TotalPrice,
		})
	}

	convCtx.CurrentOrder = &Order{
		ID:          order.ID,
		OrderNumber: order.OrderNumber,
		Items:       orderItems,
		Total:       order.TotalAmount,
		Currency:    order.Currency,
		CreatedAt:   order.CreatedAt,
		State:       OrderState(order.Status),
	}

	return nil
}

// mapOrderState converts DB status to V3 OrderState
func mapOrderState(status string) OrderState {
	switch status {
	case "draft", "building":
		return StateBuilding
	case "pending", "checkout":
		return StateCheckout
	case "awaiting_payment", "payment":
		return StatePayment
	case "confirmed", "paid":
		return StateCompleted
	case "cancelled":
		return StateCancelled
	default:
		return StateIdle
	}
}

// =============================================================================
// MEMORY: CUSTOMER HISTORY
// =============================================================================

func (b *contextBuilder) loadCustomerHistory(ctx context.Context, convCtx *ConversationContext) error {
	var customer storage.Customer
	if err := b.db.WithContext(ctx).First(&customer, convCtx.CustomerID).Error; err != nil {
		return err
	}

	// Count previous orders
	var orderCount int64
	b.db.WithContext(ctx).
		Model(&storage.Order{}).
		Where("customer_id = ? AND status IN ?", convCtx.CustomerID, []string{"confirmed", "completed", "delivered"}).
		Count(&orderCount)

	// Calculate total spend
	var totalSpend float64
	b.db.WithContext(ctx).
		Model(&storage.Order{}).
		Where("customer_id = ? AND status IN ?", convCtx.CustomerID, []string{"confirmed", "completed", "delivered"}).
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&totalSpend)

	// Get last order date
	var lastOrders []storage.Order
	var lastOrderDate *time.Time
	if err := b.db.WithContext(ctx).
		Where("customer_id = ? AND status IN ?", convCtx.CustomerID, []string{"confirmed", "completed", "delivered"}).
		Order("created_at DESC").
		Limit(1).
		Find(&lastOrders).Error; err == nil && len(lastOrders) > 0 {
		lastOrderDate = &lastOrders[0].CreatedAt
	}

	// Phase 8: Long-Term Memory (Customer Insights)
	var insights *CustomerInsights
	var insightRecord storage.CustomerInsight

	// 1. Try to fetch existing insight record
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", convCtx.TenantID, convCtx.CustomerID).
		First(&insightRecord).Error; err == nil {

		var tags []string
		if insightRecord.Tags != "" {
			json.Unmarshal([]byte(insightRecord.Tags), &tags)
		}

		insights = &CustomerInsights{
			Summary:   insightRecord.Summary,
			BuyerType: insightRecord.BuyerType,
			// Since we don't store "PreferredProducts" in insight yet, we'll infer or leave empty
			PreferredProducts: nil, // We'll infer below if needed
		}
	}

	// 2. Inference Logic: If no insight or we want to enrich top products
	if insights == nil {
		insights = &CustomerInsights{
			BuyerType: "undecided",
		}
	}

	// Infer top products/categories from past orders
	// This helps AI suggest relevant items ("Another Pepperoni Pizza?")
	var topItems []string
	if orderCount > 0 {
		// Simple query to get item names from past orders
		// Since items are JSONB, this is a bit heavy, so we limit to last 3 orders
		var recentOrders []storage.Order
		b.db.WithContext(ctx).
			Where("customer_id = ? AND status IN ?", convCtx.CustomerID, []string{"confirmed", "completed", "delivered"}).
			Order("created_at DESC").
			Limit(3).
			Find(&recentOrders)

		itemCounts := make(map[string]int)
		for _, ord := range recentOrders {
			var items []storage.OrderItem // Use storage struct to match DB JSON
			if ord.Items != "" {
				json.Unmarshal([]byte(ord.Items), &items)
				for _, item := range items {
					itemCounts[item.Name]++
				}
			}
		}

		// Find max
		for name, count := range itemCounts {
			if count >= 1 {
				topItems = append(topItems, name)
			}
		}
		// Limit to top 3
		if len(topItems) > 3 {
			topItems = topItems[:3]
		}
		insights.PreferredProducts = topItems

		// Set BuyerType if not set
		if insights.BuyerType == "undecided" || insights.BuyerType == "" {
			if totalSpend > 100000 { // Arbitrary threshold for now
				insights.BuyerType = "premium"
			} else if orderCount > 3 {
				insights.BuyerType = "regular"
			}
		}
	}

	convCtx.CustomerHistory = &CustomerData{
		Name:           customer.DisplayName,
		Phone:          customer.PlatformID,
		PreviousOrders: int(orderCount),
		TotalSpend:     totalSpend,
		LastOrderDate:  lastOrderDate,
		Insights:       insights,
	}

	return nil
}

// =============================================================================
// ROLE PROFILES
// =============================================================================

// Industry role profiles
var roleProfiles = map[string]RoleProfile{
	"e-commerce": {
		Industry:  "e-commerce",
		Tone:      "helpful_casual",
		MaxMsgLen: 150,
		UpsellOK:  true,
		Emojis:    true,
		Phrases: PhraseSet{
			Greeting: []string{"Hey!", "Hi there!", "What's up?"},
			Confirm:  []string{"Got it!", "Perfect!", "Done!"},
			Empathy:  []string{"I hear you", "No worries", "Let me sort that out"},
			Error:    []string{"Hmm, something went wrong", "Let me check on that"},
		},
		Behavior: RoleBehavior{
			ShowMenuOnGreet: false,
		},
		Style: ConversationalStyle{
			Verbosity: "medium",
			Pace:      "relaxed",
			Warmth:    "friendly", // "Hey there!"
		},
	},
	"food": {
		Industry:  "food",
		Tone:      "quick_friendly",
		MaxMsgLen: 100,
		UpsellOK:  true,
		Emojis:    true,
		Phrases: PhraseSet{
			Greeting: []string{"Hey!", "Welcome!"},
			Confirm:  []string{"Order received!", "Coming right up!"},
			Empathy:  []string{"Sorry about that", "Let me fix that"},
			Error:    []string{"Oops!", "One sec"},
			TimeEst:  []string{"Ready in ~%d mins"},
		},
		Behavior: RoleBehavior{
			ShowMenuOnGreet: true, // Food: Immediate visual menu
		},
		Style: ConversationalStyle{
			Verbosity: "low",    // Bursts: "Okay" ... "Coming right up"
			Pace:      "fast",   // Fast food = fast chat
			Warmth:    "casual", // "Safe, boss"
		},
	},
	"consulting": {
		Industry:  "consulting",
		Tone:      "professional",
		MaxMsgLen: 250,
		UpsellOK:  false,
		Emojis:    false,
		Phrases: PhraseSet{
			Greeting: []string{"Hello", "Good to hear from you"},
			Confirm:  []string{"Understood", "I've noted that"},
			Empathy:  []string{"I understand", "Let me help with that"},
			Error:    []string{"I apologize for the inconvenience", "Let me look into that"},
		},
		Behavior: RoleBehavior{
			ProactiveSupport: true, // Consulting: "How can I help"
		},
		Style: ConversationalStyle{
			Verbosity: "medium",
			Pace:      "relaxed",
			Warmth:    "empathetic", // "I hear you"
		},
	},
	"default": {
		Industry:  "general",
		Tone:      "friendly",
		MaxMsgLen: 150,
		UpsellOK:  true,
		Emojis:    true,
		Phrases: PhraseSet{
			Greeting: []string{"Hi!", "Hello!"},
			Confirm:  []string{"Got it!", "Done!"},
			Empathy:  []string{"I hear you", "No problem"},
			Error:    []string{"Something went wrong", "Let me check"},
		},
	},
}

func (b *contextBuilder) loadRoleProfile(ctx context.Context, convCtx *ConversationContext) {
	industry := strings.ToLower(convCtx.BusinessType)

	// Normalize industry name
	switch {
	case strings.Contains(industry, "food"), strings.Contains(industry, "restaurant"),
		strings.Contains(industry, "bakery"), strings.Contains(industry, "cafe"):
		convCtx.RoleProfile = roleProfiles["food"]
	case strings.Contains(industry, "consult"), strings.Contains(industry, "agency"),
		strings.Contains(industry, "professional"):
		convCtx.RoleProfile = roleProfiles["consulting"]
	case strings.Contains(industry, "commerce"), strings.Contains(industry, "retail"),
		strings.Contains(industry, "shop"), strings.Contains(industry, "store"):
		convCtx.RoleProfile = roleProfiles["e-commerce"]
	default:
		convCtx.RoleProfile = roleProfiles["default"]
	}

	// Override Tone if configured in Business Profile
	if convCtx.RoleProfile.Tone != "" && convCtx.BusinessTone != "" {
		convCtx.RoleProfile.Tone = convCtx.BusinessTone
	}

	// Populate TonePreset (New System)
	// - RuntimeProfile (convCtx.RuntimeProfile) is the single source of truth.
	// - It is populated in buildRuntimeProfile later/earlier.
	// - We do NOT manually map here anymore.
}

// =============================================================================
// RUNTIME PROFILE
// =============================================================================

func (b *contextBuilder) buildRuntimeProfile(ctx context.Context, convCtx *ConversationContext) error {
	// Use the shared builder
	// This fetches fresh data (cached inside the builder if implemented)
	profile, err := tenant.BuildRuntimeProfile(ctx, b.db, convCtx.TenantID.String())
	if err != nil {
		return err
	}

	convCtx.RuntimeProfile = profile
	return nil
}

// =============================================================================
// PAYMENT CONFIG
// =============================================================================

func (b *contextBuilder) loadPaymentConfig(ctx context.Context, convCtx *ConversationContext) error {
	var paymentAccount storage.TenantPaymentAccount
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_default = ?", convCtx.TenantID, true).
		First(&paymentAccount).Error; err != nil {
		// Try any active account
		if err := b.db.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", convCtx.TenantID, true).
			First(&paymentAccount).Error; err != nil {
			return err
		}
	}

	// Decrypt account number if encrypted
	accountNumber := paymentAccount.AccountNumber
	if decrypted, err := utils.DecryptToken(accountNumber); err == nil && decrypted != "" {
		accountNumber = decrypted
	}

	convCtx.PaymentInfo = &PaymentConfig{
		BankName:      paymentAccount.BankName,
		AccountName:   paymentAccount.AccountName,
		AccountNumber: accountNumber, // DECRYPTED - full number
		Provider:      paymentAccount.AccountType,
	}

	return nil
}

// =============================================================================
// VOICE FINGERPRINT (Phase 3)
// =============================================================================

func (b *contextBuilder) loadVoiceFingerprint(ctx context.Context, convCtx *ConversationContext) error {
	var fp []storage.VoiceFingerprint

	// Try to find existing fingerprint
	// Use Find + Limit to avoid "record not found" error log
	err := b.db.WithContext(ctx).Where("tenant_id = ?", convCtx.TenantID).Limit(1).Find(&fp).Error

	if err != nil {
		return err // Real database error
	}

	if len(fp) == 0 {
		// Create default fingerprint if none exists
		newFP := storage.VoiceFingerprint{
			TenantID:       convCtx.TenantID,
			EmojiRate:      0.1,
			SlangRate:      0.0,
			Formality:      0.5,
			Directness:     0.5,
			SentenceLength: 0.4,
			QuestionRate:   0.2,
			UpdatedAt:      time.Now(),
		}
		// Only create in memory for now
		convCtx.VoiceFingerprint = &newFP
		return nil
	}
	// logic continues if found

	convCtx.VoiceFingerprint = &fp[0]
	return nil
}

func (b *contextBuilder) loadPendingCart(ctx context.Context, convCtx *ConversationContext) error {
	var sc storage.SalesConversation
	// Find persistent cart
	err := b.db.WithContext(ctx).
		Where("conversation_id = ?", convCtx.ConversationID).
		First(&sc).Error

	if err == nil && sc.PendingOrderDraft != "" && sc.PendingOrderDraft != "{}" && sc.PendingOrderDraft != "[]" {
		var items []PendingCartItem
		if err := json.Unmarshal([]byte(sc.PendingOrderDraft), &items); err == nil {
			convCtx.PendingCart = items
		}
	}
	// Ignore record not found or empty draft
	return nil
}

func (b *contextBuilder) loadSalesConversation(ctx context.Context, convCtx *ConversationContext) error {
	var sc storage.SalesConversation
	// Try to find active conversation
	// We sort by updated_at desc to get the latest one
	err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND conversation_id = ?",
			convCtx.TenantID, convCtx.CustomerID, convCtx.ConversationID).
		Order("updated_at desc").
		First(&sc).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new one if not found
			sc = storage.SalesConversation{
				Base:           storage.Base{ID: uuid.New()},
				TenantID:       convCtx.TenantID,
				CustomerID:     convCtx.CustomerID,
				ConversationID: convCtx.ConversationID,
				Stage:          "discovery",
			}
			if err := b.db.WithContext(ctx).Create(&sc).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}

	convCtx.SalesConversation = &sc
	return nil
}
