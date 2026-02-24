package context

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Builder is responsible for assembling the ContextSnapshot.
// It performs read-only DB operations and memory aggregation.
type Builder struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewBuilder creates a new Context Builder.
func NewBuilder(db *gorm.DB, logger *zap.Logger) *Builder {
	return &Builder{
		db:     db,
		logger: logger,
	}
}

// BuildSnapshot gathers all data required for the AI to make a decision.
// It strictly follows a read-only, fail-fast (for critical data) approach.
func (b *Builder) BuildSnapshot(
	ctx context.Context,
	req *types.AIRequest,
	conv *memory.ConversationMemory,
	op *memory.OperationalMemory,
) (*ContextSnapshot, error) {

	// Step 1: Validate Inputs
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}
	if conv == nil || op == nil {
		return nil, errors.New("memory cannot be nil")
	}

	snapshot := &ContextSnapshot{
		Message:      req.MessageText,
		Platform:     req.Scope.Platform,
		Timestamp:    req.Timestamp,
		Conversation: conv,
		Operational:  op,
		ActiveOrder:  nil, // Default to nil
	}

	if b.db == nil {
		snapshot.Business = types.BusinessProfile{
			ID:   req.Scope.TenantID,
			Name: "",
		}
		return snapshot, nil
	}

	// Step 2: Load Business Profile
	var tenant storage.Tenant
	if err := b.db.WithContext(ctx).
		Preload("BusinessProfile").
		First(&tenant, "id = ?", req.Scope.TenantID).Error; err != nil {
		return nil, err
	}

	snapshot.Business = types.BusinessProfile{
		ID:          req.Scope.TenantID,
		Name:        tenant.BusinessProfile.Name,
		Industry:    tenant.BusinessProfile.Industry,
		Description: tenant.BusinessProfile.Description,
		Tone:        tenant.BusinessProfile.Tone,
		Timezone:    tenant.Timezone,
		CatalogKind: types.CatalogKindFromIndustry(tenant.BusinessProfile.Industry),
	}
	b.logger.Debug("business_profile_loaded", zap.String("tenant_id", req.Scope.TenantID.String()))

	// Step 3: Load Customer Info
	snapshot.Customer = types.CustomerInfo{}
	var customer storage.Customer
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND channel = ? AND platform_id = ?",
			req.Scope.TenantID, req.Scope.Platform, req.Scope.CustomerID).
		First(&customer).Error; err == nil {
		snapshot.Customer = types.CustomerInfo{
			ID:          customer.ID,
			Name:        customer.DisplayName,
			DisplayName: customer.DisplayName,
			Phone:       customer.PlatformID,
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		snapshot.Customer = types.CustomerInfo{}
	} else if err != nil {
		return nil, err
	}

	// Step 4: Load Offerings
	snapshot.Offerings = []types.Offering{}
	var menuItems []storage.MenuItem
	var products []storage.Product
	var services []storage.Service

	b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", req.Scope.TenantID, true).
		Find(&menuItems)
	b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", req.Scope.TenantID, true).
		Find(&products)
	b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", req.Scope.TenantID, true).
		Find(&services)

	for _, item := range menuItems {
		isAvailable := strings.ToLower(strings.TrimSpace(item.Availability)) != "unavailable"
		snapshot.Offerings = append(snapshot.Offerings, types.Offering{
			ID:                  item.ID,
			Type:                "menu_item",
			Name:                item.Name,
			Description:         item.Description,
			Price:               item.Price,
			Currency:            item.Currency,
			Category:            item.Category,
			NegotiationMinPrice: item.NegotiationMinPrice,
			NegotiationMaxPrice: item.NegotiationMaxPrice,
			IsAvailable:         isAvailable,
		})
	}

	for _, item := range products {
		isAvailable := strings.ToLower(strings.TrimSpace(item.Availability)) != "out_of_stock" && item.StockCount != 0
		snapshot.Offerings = append(snapshot.Offerings, types.Offering{
			ID:                  item.ID,
			Type:                "product",
			Name:                item.Name,
			Description:         item.Description,
			Price:               item.Price,
			Currency:            item.Currency,
			Category:            item.Category,
			NegotiationMinPrice: item.NegotiationMinPrice,
			NegotiationMaxPrice: item.NegotiationMaxPrice,
			IsAvailable:         isAvailable,
		})
	}

	for _, item := range services {
		snapshot.Offerings = append(snapshot.Offerings, types.Offering{
			ID:          item.ID,
			Type:        "service",
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
			IsAvailable: item.IsActive,
		})
	}

	// Step 5: Load Payment Config
	var paymentAccount storage.TenantPaymentAccount
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND is_default = ? AND is_active = ?", req.Scope.TenantID, true, true).
		First(&paymentAccount).Error; err == nil {
		accountNumber := ""
		if paymentAccount.AccountNumber != "" {
			if decrypted, err := utils.DecryptToken(paymentAccount.AccountNumber); err == nil {
				accountNumber = decrypted
			} else if len(paymentAccount.AccountNumber) > 4 {
				accountNumber = "****" + paymentAccount.AccountNumber[len(paymentAccount.AccountNumber)-4:]
			} else {
				accountNumber = "****"
			}
		}
		methods := []string{}
		switch strings.ToLower(strings.TrimSpace(paymentAccount.AccountType)) {
		case "bank", "bank_transfer":
			methods = append(methods, "bank_transfer")
		case "mobile_money":
			methods = append(methods, "mobile_money")
		case "cash":
			methods = append(methods, "cash")
		default:
			if paymentAccount.AccountType != "" {
				methods = append(methods, paymentAccount.AccountType)
			}
		}
		snapshot.Payment = types.PaymentConfig{
			BankName:      paymentAccount.BankName,
			AccountNumber: accountNumber,
			AccountName:   paymentAccount.AccountName,
			Methods:       methods,
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Step 6: Load Active Order Snapshot (Optional)
	if op.ActiveOrderID != nil {
		b.logger.Debug("active_order_snapshot_lookup", zap.String("order_id", op.ActiveOrderID.String()))
		var ord storage.Order
		if err := b.db.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", *op.ActiveOrderID, req.Scope.TenantID).
			First(&ord).Error; err == nil {
			var items []storage.OrderItem
			if strings.TrimSpace(ord.Items) != "" {
				_ = json.Unmarshal([]byte(ord.Items), &items)
			}

			outItems := make([]types.OrderItem, 0, len(items))
			subtotal := 0.0
			for _, it := range items {
				lineTotal := it.TotalPrice
				if lineTotal <= 0 {
					lineTotal = float64(it.Quantity) * it.UnitPrice
				}
				subtotal += lineTotal
				outItems = append(outItems, types.OrderItem{
					Name:      it.Name,
					Quantity:  it.Quantity,
					Price:     it.UnitPrice,
					LineTotal: lineTotal,
				})
			}
			snapshot.ActiveOrder = &types.OrderSnapshot{
				OrderID:  ord.ID,
				Items:    outItems,
				Subtotal: subtotal,
				Tax:      0,
				Total:    ord.TotalAmount,
				Currency: ord.Currency,
				Status:   ord.Status,
			}
		}
	}

	return snapshot, nil
}
