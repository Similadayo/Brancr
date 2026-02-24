package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/renderer"
	"github.com/similadayo/brancr/internal/ai_v4/router"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// v4PipelineAdapter adapts ai_v4.Router to the MessageHandler's Responder interface
type v4PipelineAdapter struct {
	router   *router.AIRouter
	logger   *zap.Logger
	db       *gorm.DB
	usageSvc interface {
		RecordAIReply(ctx context.Context, tenantID uuid.UUID, tokensUsed int, estimatedCost float64, modelName string) error
	}
	sendReplyFn func(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error
}

// SetSendReplyFunc allows wiring the send reply function after creation
func (a *v4PipelineAdapter) SetSendReplyFunc(fn func(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error) {
	a.sendReplyFn = fn
}

func (a *v4PipelineAdapter) SetUsageService(svc interface {
	RecordAIReply(ctx context.Context, tenantID uuid.UUID, tokensUsed int, estimatedCost float64, modelName string) error
}) {
	a.usageSvc = svc
}

// formatOfferings creates an industry-agnostic formatted list of offerings
func formatOfferings(knowledge *v4ctx.TenantKnowledgeSnapshot) string {
	var sections []string

	if len(knowledge.MenuItems) > 0 {
		section := formatMenuItemsSection(knowledge.MenuItems, knowledge.Industry)
		sections = append(sections, section)
	}

	if len(knowledge.Products) > 0 {
		section := formatProductsSection(knowledge.Products, knowledge.Industry)
		sections = append(sections, section)
	}

	if len(knowledge.Services) > 0 {
		section := formatServicesSection(knowledge.Services, knowledge.Industry)
		sections = append(sections, section)
	}

	if len(sections) == 0 {
		return "We don't have our offerings catalog set up yet. Please ask us directly what you need!"
	}

	header := fmt.Sprintf("📋 *%s*\n\n", knowledge.BusinessName)
	return header + strings.Join(sections, "\n\n")
}

func formatMenuItemsSection(items []v4ctx.MenuItemSummary, industry string) string {
	header := "🍽️ *Menu*"
	if industry == "coffee_shop" || industry == "cafe" {
		header = "☕ *Our Drinks & Treats*"
	} else if industry == "bakery" {
		header = "🍰 *Baked Goods*"
	}

	var lines []string
	lines = append(lines, header)

	for i, item := range items {
		if i >= 20 {
			lines = append(lines, fmt.Sprintf("\n_...and %d more items_", len(items)-20))
			break
		}

		price := formatPrice(item.Price, item.Currency)
		line := fmt.Sprintf("• %s — %s", item.Name, price)
		if item.Description != "" && len(item.Description) < 50 {
			line += fmt.Sprintf("\n  _%s_", item.Description)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func formatProductsSection(items []v4ctx.ProductSummary, industry string) string {
	header := "🛍️ *Products*"
	if industry == "fashion" || industry == "clothing" {
		header = "👕 *Our Collection*"
	} else if industry == "electronics" {
		header = "📱 *Devices & Gadgets*"
	}

	var lines []string
	lines = append(lines, header)

	for i, item := range items {
		if i >= 20 {
			lines = append(lines, fmt.Sprintf("\n_...and %d more products_", len(items)-20))
			break
		}

		price := formatPrice(item.Price, item.Currency)
		line := fmt.Sprintf("• %s — %s", item.Name, price)
		if item.Description != "" && len(item.Description) < 50 {
			line += fmt.Sprintf("\n  _%s_", item.Description)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func formatServicesSection(services []v4ctx.ServiceSummary, industry string) string {
	header := "💼 *Services*"
	if industry == "healthcare" || industry == "medical" {
		header = "🏥 *Medical Services*"
	} else if industry == "legal" {
		header = "⚖️ *Legal Services*"
	} else if industry == "consulting" {
		header = "📊 *Consulting Packages*"
	}

	var lines []string
	lines = append(lines, header)

	for i, svc := range services {
		if i >= 15 {
			lines = append(lines, fmt.Sprintf("\n_...and %d more services_", len(services)-15))
			break
		}

		line := fmt.Sprintf("• %s", svc.Name)
		// Note: ServiceSummary uses Pricing (JSON string), not direct Price field

		if svc.Description != "" && len(svc.Description) < 60 {
			line += fmt.Sprintf("\n  _%s_", svc.Description)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func formatPrice(price float64, currency string) string {
	if currency == "" {
		currency = "NGN"
	}

	if price >= 1000 {
		return fmt.Sprintf("%s%s", currency, formatNumberWithCommas(int64(price)))
	}
	return fmt.Sprintf("%s%.0f", currency, price)
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

// executeActions processes ProposedActions from the AI response
func (a *v4PipelineAdapter) executeActions(
	ctx context.Context,
	resp *types.AIResponse,
	tenantID uuid.UUID,
	platform string,
	conversationID string,
	threadID string,
	customerID string,
) error {
	a.logger.Info("🔍 executeActions_called",
		zap.Int("action_count", len(resp.ProposedActions)),
		zap.String("tenant_id", tenantID.String()))

	if len(resp.ProposedActions) == 0 {
		a.logger.Info("no_actions_to_execute")
		return nil
	}

	for _, action := range resp.ProposedActions {
		a.logger.Info("🔄 processing_action", zap.String("action_type", string(action.Type)))
		switch action.Type {
		case "send_offerings":
			a.logger.Info("🍽️  EXECUTING_SEND_OFFERINGS", zap.String("tenant_id", tenantID.String()))

			knowledge, err := a.loadTenantKnowledge(ctx, tenantID)
			if err != nil {
				a.logger.Error("❌ failed_to_load_knowledge",
					zap.Error(err),
					zap.String("tenant_id", tenantID.String()))
				continue
			}

			a.logger.Info("📊 knowledge_loaded",
				zap.Int("menu_items_count", len(knowledge.MenuItems)),
				zap.Int("products_count", len(knowledge.Products)),
				zap.Int("services_count", len(knowledge.Services)))

			offeringsText := formatOfferings(knowledge)
			a.logger.Info("📝 offerings_formatted",
				zap.Int("text_length", len(offeringsText)),
				zap.String("preview", offeringsText[:min(100, len(offeringsText))]))

			if a.sendReplyFn != nil {
				err = a.sendReplyFn(ctx, tenantID, platform, threadID, customerID, offeringsText)
				if err != nil {
					a.logger.Error("failed_to_send_offerings", zap.Error(err))
				} else {
					a.logger.Info("✅ offerings_sent",
						zap.String("tenant_id", tenantID.String()),
						zap.Int("items_count", len(knowledge.MenuItems)+len(knowledge.Products)+len(knowledge.Services)))
				}
			} else {
				a.logger.Warn("send_reply_function_not_wired")
			}

		case "request_payment":
			a.logger.Info("action_request_payment_triggered",
				zap.String("tenant_id", tenantID.String()))

		default:
			a.logger.Warn("unknown_action_type_in_adapter",
				zap.String("type", string(action.Type)))
		}
	}

	return nil
}

// loadTenantKnowledge loads the full knowledge snapshot from DB
func (a *v4PipelineAdapter) loadTenantKnowledge(ctx context.Context, tenantID uuid.UUID) (*v4ctx.TenantKnowledgeSnapshot, error) {
	var tenant storage.Tenant
	if err := a.db.Preload("BusinessProfile").Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		return nil, err
	}

	snapshot := &v4ctx.TenantKnowledgeSnapshot{
		TenantID:     tenantID,
		BusinessName: tenant.BusinessProfile.Name,
		Description:  tenant.BusinessProfile.Description,
		Industry:     tenant.BusinessProfile.Industry,
	}

	// Load menu items
	var menuItems []storage.MenuItem
	a.db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&menuItems)
	for _, m := range menuItems {
		snapshot.MenuItems = append(snapshot.MenuItems, v4ctx.MenuItemSummary{
			Name:         m.Name,
			Description:  m.Description,
			Price:        m.Price,
			Currency:     m.Currency,
			Availability: m.Availability,
		})
	}

	// Load products
	var products []storage.Product
	a.db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&products)
	for _, p := range products {
		snapshot.Products = append(snapshot.Products, v4ctx.ProductSummary{
			Name:         p.Name,
			Description:  p.Description,
			Price:        p.Price,
			Currency:     p.Currency,
			Availability: p.Availability,
			StockCount:   p.StockCount,
			Variants:     p.Variants,
		})
	}

	// Load services
	var services []storage.Service
	a.db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&services)
	for _, s := range services {
		snapshot.Services = append(snapshot.Services, v4ctx.ServiceSummary{
			Name:        s.Name,
			Description: s.Description,
			// Note: Service.Pricing is JSON, would need parsing to display price
		})
	}

	return snapshot, nil
}

// GenerateCustomerReply implements the Responder interface
func (a *v4PipelineAdapter) GenerateCustomerReply(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	message string,
	intent string,
	platform string,
	conversationID ...uuid.UUID,
) (string, error) {
	var convID uuid.UUID
	if len(conversationID) > 0 {
		convID = conversationID[0]
	} else {
		convID = uuid.New()
	}

	req := &types.AIRequest{
		Scope: types.MemoryScope{
			TenantID:       tenantID,
			CustomerID:     customerID.String(),
			ConversationID: convID,
			Platform:       platform,
		},
		MessageText: message,
		Timestamp:   time.Now(),
	}

	// Enforce tenant AI quota before running V4
	qs := quota.NewQuotaService(a.db, a.logger, nil)
	if err := qs.CheckAICallQuota(ctx, tenantID); err != nil {
		a.logger.Warn("v4_ai_quota_exceeded_blocking_reply", zap.Error(err), zap.Stringer("tenant_id", tenantID))
		return "We're currently unable to respond right now. Please try again shortly.", nil
	}

	aiResp, err := a.router.HandleMessage(ctx, req)
	if err != nil {
		return "", err
	}

	// 🆕 FIX: Extract threadID and platformCustomerID from context
	// This is critical for Instagram where the recipient ID (IGSID) differs from customerID
	threadID, hasThreadID := types.GetThreadID(ctx)
	platformCustomerID, hasPlatformCustomerID := types.GetPlatformCustomerID(ctx)

	// Fallback to convID.String() if context doesn't have threadID (shouldn't happen in production)
	if !hasThreadID || threadID == "" {
		a.logger.Warn("⚠️ threadID not found in context, using conversationID as fallback (may cause Instagram errors)",
			zap.String("tenant_id", tenantID.String()),
			zap.String("platform", platform))
		threadID = convID.String()
	}

	// Fallback platformCustomerID to customerID.String()
	if !hasPlatformCustomerID || platformCustomerID == "" {
		platformCustomerID = customerID.String()
	}

	a.logger.Info("🔧 executeActions context",
		zap.String("threadID", threadID),
		zap.String("platformCustomerID", platformCustomerID),
		zap.String("platform", platform))

	// Execute actions before returning text
	err = a.executeActions(ctx, aiResp, tenantID, platform, convID.String(), threadID, platformCustomerID)
	if err != nil {
		a.logger.Error("action_execution_failed", zap.Error(err))
	}

	// Meter AI v4 usage for quota + admin cost dashboards (best-effort)
	if aiResp != nil {
		meta := aiResp.Metadata
		var model string
		var promptTokens, completionTokens, totalTokens int
		if meta != nil {
			if v, ok := meta["llm_model"].(string); ok {
				model = strings.TrimSpace(v)
			}
			if v, ok := meta["llm_prompt_tokens"].(int); ok {
				promptTokens = v
			} else if v, ok := meta["llm_prompt_tokens"].(float64); ok {
				promptTokens = int(v)
			}
			if v, ok := meta["llm_completion_tokens"].(int); ok {
				completionTokens = v
			} else if v, ok := meta["llm_completion_tokens"].(float64); ok {
				completionTokens = int(v)
			}
			if v, ok := meta["llm_total_tokens"].(int); ok {
				totalTokens = v
			} else if v, ok := meta["llm_total_tokens"].(float64); ok {
				totalTokens = int(v)
			}
		}
		if model == "" {
			model = "default"
		}

		qs := quota.NewQuotaService(a.db, a.logger, nil)
		quotaTokenUsage := quota.TokenUsageFromAI(promptTokens, completionTokens, totalTokens)
		if quotaTokenUsage != nil {
			if incErr := qs.IncrementAICallCost(ctx, tenantID, model, quotaTokenUsage); incErr != nil {
				a.logger.Warn("failed_to_meter_v4_ai_cost", zap.Error(incErr), zap.Stringer("tenant_id", tenantID))
			} else {
				if a.usageSvc != nil {
					calc := quota.NewCostCalculator()
					costUSD, _ := calc.CalculateCost(model, quotaTokenUsage)
					if uErr := a.usageSvc.RecordAIReply(ctx, tenantID, totalTokens, costUSD, model); uErr != nil {
						a.logger.Warn("failed_to_record_v4_usage_event", zap.Error(uErr), zap.Stringer("tenant_id", tenantID))
					}
				}
			}
		} else {
			if incErr := qs.IncrementAICallUsage(ctx, tenantID); incErr != nil {
				a.logger.Warn("failed_to_meter_v4_ai_usage", zap.Error(incErr), zap.Stringer("tenant_id", tenantID))
			}
		}
	}

	text := renderer.RenderForChat(aiResp)
	return text, nil
}

// NewV4PipelineAdapter creates adapter with DB access for action execution
func NewV4PipelineAdapter(r *router.AIRouter, logger *zap.Logger, db *gorm.DB) *v4PipelineAdapter {
	return &v4PipelineAdapter{
		router: r,
		logger: logger,
		db:     db,
	}
}

// GetFullResponse provides access to the full V4 response
func (a *v4PipelineAdapter) GetFullResponse(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	message string,
	platform string,
	conversationID uuid.UUID,
) (*types.AIResponse, error) {
	req := &types.AIRequest{
		Scope: types.MemoryScope{
			TenantID:       tenantID,
			CustomerID:     customerID.String(),
			ConversationID: conversationID,
			Platform:       platform,
		},
		MessageText: message,
		Timestamp:   time.Now(),
	}

	return a.router.HandleMessage(ctx, req)
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
