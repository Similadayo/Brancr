package loader

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

func LoadTenantKnowledge(
	ctx context.Context,
	db *gorm.DB,
	tenantID uuid.UUID,
) (*v4ctx.TenantKnowledgeSnapshot, error) {

	// 1. Load Tenant & Business Profile
	var tenant storage.Tenant
	if err := db.Preload("BusinessProfile").
		First(&tenant, "id = ?", tenantID).Error; err != nil {
		return nil, err // Return error if tenant not found or DB error
	}

	// 2. Load All Business Data strictly
	var menu []storage.MenuItem
	var products []storage.Product
	var services []storage.Service
	var faqs []storage.BusinessFAQ
	// Removed Policies/Offers as they are not in the snapshot struct

	// Execute queries
	db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&menu)
	db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&products)
	db.Where("tenant_id = ? AND is_active = ?", tenantID, true).Find(&services)
	db.Where("tenant_id = ?", tenantID).Find(&faqs)

	// Initialize Snapshot
	k := &v4ctx.TenantKnowledgeSnapshot{
		TenantID:       tenantID,
		BusinessName:   tenant.BusinessProfile.Name,
		Description:    tenant.BusinessProfile.Description, // Explicit match
		Industry:       tenant.BusinessProfile.Industry,    // Fixed mapping
		Location:       tenant.BusinessProfile.Location,
		OperatingHours: tenant.BusinessProfile.OperatingHours,
		Website:        tenant.BusinessProfile.Website,
		Tone:           tenant.BusinessProfile.Tone,
		Keywords:       tenant.BusinessProfile.Keywords,
		KnowledgeBase:  tenant.BusinessProfile.KnowledgeBase,
		LastUpdated:    time.Now(),

		// Initialize slices to avoid nil
		MenuItems: []v4ctx.MenuItemSummary{},
		Products:  []v4ctx.ProductSummary{},
		Services:  []v4ctx.ServiceSummary{},
		FAQs:      []v4ctx.FAQEntry{},
	}

	// ✅ Explicit Mapping (Part 1)

	// Map MenuItems
	for _, m := range menu {
		k.MenuItems = append(k.MenuItems, v4ctx.MenuItemSummary{
			Name:            m.Name,
			Description:     m.Description,
			Price:           m.Price,
			Currency:        m.Currency,
			Category:        m.Category,
			NegotiationMode: m.NegotiationMode,
			MinPrice:        m.NegotiationMinPrice,
			Availability:    m.Availability,
		})
	}

	// Map Products
	for _, p := range products {
		k.Products = append(k.Products, v4ctx.ProductSummary{
			Name:            p.Name,
			Description:     p.Description,
			Price:           p.Price,
			Currency:        p.Currency,
			Category:        p.Category,
			NegotiationMode: p.NegotiationMode,
			MinPrice:        p.NegotiationMinPrice,
			Availability:    p.Availability,
			StockCount:      p.StockCount,
			Variants:        p.Variants,
		})
	}

	// Map Services
	for _, s := range services {
		// Parse Pricing JSON
		var price float64
		parsePrice := func(v interface{}) (float64, bool) {
			switch t := v.(type) {
			case float64:
				return t, true
			case int:
				return float64(t), true
			case int64:
				return float64(t), true
			case string:
				clean := strings.TrimSpace(t)
				clean = strings.ReplaceAll(clean, ",", "")
				clean = strings.TrimPrefix(clean, "₦")
				clean = strings.TrimPrefix(clean, "ngn")
				clean = strings.TrimPrefix(clean, "NGN")
				clean = strings.TrimSpace(clean)
				if clean == "" {
					return 0, false
				}
				f, err := strconv.ParseFloat(clean, 64)
				if err != nil {
					return 0, false
				}
				return f, true
			default:
				return 0, false
			}
		}
		if s.Pricing != "" && s.Pricing != "{}" {
			var pricingMap map[string]interface{}
			if err := json.Unmarshal([]byte(s.Pricing), &pricingMap); err == nil {
				// Handle "amount" or "rate" or "price"
				if raw, ok := pricingMap["amount"]; ok {
					if p, ok := parsePrice(raw); ok {
						price = p
					}
				} else if raw, ok := pricingMap["rate"]; ok {
					if p, ok := parsePrice(raw); ok {
						price = p
					}
				} else if raw, ok := pricingMap["price"]; ok {
					if p, ok := parsePrice(raw); ok {
						price = p
					}
				}
			}
		}

		k.Services = append(k.Services, v4ctx.ServiceSummary{
			Name:            s.Name,
			Description:     s.Description,
			Pricing:         string(s.Pricing), // Convert JSONB to string
			Deliverables:    s.Deliverables,
			Category:        s.Category,
			Price:           price,
			NegotiationMode: s.NegotiationMode,
			MinPrice:        s.NegotiationMinPrice,
			MaxPrice:        s.NegotiationMaxPrice,
		})
	}

	// Map FAQs
	for _, f := range faqs {
		k.FAQs = append(k.FAQs, v4ctx.FAQEntry{
			Question: f.Question,
			Answer:   f.Answer,
		})
	}

	// Set Capability Flags (Part 1)
	if len(k.Products) > 0 {
		k.HasProducts = true
	}
	if len(k.MenuItems) > 0 {
		k.HasMenu = true
	}
	if len(k.Services) > 0 {
		k.HasServices = true
	}

	// NOTE: HasAnyOfferings is not in the struct definition in Step 18, utilizing the specific flags above.
	// If the snapshot struct needs HasAnyOfferings, I would add it, but strictly following Step 18 struct.

	return k, nil
}
