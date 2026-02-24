package fulfillment

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/similadayo/brancr/internal/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Service struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

var capacityTokenRegex = regexp.MustCompile(`(?i)\b\d{2,4}\s*(gb|tb)\b`)

func NewService(db *gorm.DB, logger *zap.Logger) *Service {
	return &Service{
		DB:     db,
		Logger: logger,
	}
}

func normalizeItemName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeItemFamilyKey(value string) string {
	// Best-effort "family" key so variant-like names can inherit requirements.
	// Example: "iPhone 13 Pro Max (512GB)" -> "iphone13promax".
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" {
		return ""
	}
	raw = capacityTokenRegex.ReplaceAllString(raw, " ")

	// Remove anything in parentheses (often variants like capacities/colors).
	if idx := strings.Index(raw, "("); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}

	return normalizeItemName(raw)
}

func (s *Service) resolveFallbackItemIDByFamily(tenantID uuid.UUID, orderItemName string) string {
	if s == nil || s.DB == nil || tenantID == uuid.Nil {
		return ""
	}
	family := normalizeItemFamilyKey(orderItemName)
	if family == "" {
		return ""
	}

	// Collect candidate item IDs that actually have requirements assigned.
	type itemIDRow struct {
		ItemID string
	}
	var rows []itemIDRow
	if err := s.DB.Model(&storage.ItemRequirement{}).
		Select("DISTINCT item_id AS item_id").
		Where("tenant_id = ?", tenantID).
		Limit(400).
		Scan(&rows).Error; err != nil {
		return ""
	}
	if len(rows) == 0 {
		return ""
	}

	bestID := ""
	bestScore := 0
	for _, row := range rows {
		id := strings.TrimSpace(row.ItemID)
		if id == "" {
			continue
		}

		// Try to resolve ID to a name (if stored as UUID). If not, treat it as a name-like key.
		name := ""
		if parsed, err := uuid.Parse(id); err == nil {
			var prod storage.Product
			if err := s.DB.Select("name").Where("tenant_id = ? AND id = ?", tenantID, parsed).First(&prod).Error; err == nil {
				name = prod.Name
			} else {
				var menu storage.MenuItem
				if err := s.DB.Select("name").Where("tenant_id = ? AND id = ?", tenantID, parsed).First(&menu).Error; err == nil {
					name = menu.Name
				} else {
					var svc storage.Service
					if err := s.DB.Select("name").Where("tenant_id = ? AND id = ?", tenantID, parsed).First(&svc).Error; err == nil {
						name = svc.Name
					}
				}
			}
		} else {
			name = id
		}

		otherFamily := normalizeItemFamilyKey(name)
		if otherFamily == "" {
			continue
		}

		score := 0
		if otherFamily == family {
			score = 1000 + len(otherFamily)
		} else if strings.Contains(otherFamily, family) || strings.Contains(family, otherFamily) {
			// Partial match - prefer closer (longer overlap).
			if len(otherFamily) < len(family) {
				score = 700 + len(otherFamily)
			} else {
				score = 700 + len(family)
			}
		}

		if score > bestScore {
			bestScore = score
			bestID = id
		}
	}

	return bestID
}

func (s *Service) resolveItemIDForOrderName(tenantID uuid.UUID, orderItemName string) string {
	name := strings.TrimSpace(orderItemName)
	if name == "" {
		return ""
	}

	// Strict exact-name match first.
	var svc storage.Service
	if err := s.DB.Where("tenant_id = ? AND name = ?", tenantID, name).First(&svc).Error; err == nil {
		return svc.ID.String()
	}
	var prod storage.Product
	if err := s.DB.Where("tenant_id = ? AND name = ?", tenantID, name).First(&prod).Error; err == nil {
		return prod.ID.String()
	}
	var menu storage.MenuItem
	if err := s.DB.Where("tenant_id = ? AND name = ?", tenantID, name).First(&menu).Error; err == nil {
		return menu.ID.String()
	}

	// Fallback: normalized fuzzy matching for variant-like names.
	target := normalizeItemName(name)
	if target == "" {
		return ""
	}

	type candidate struct {
		id   string
		name string
	}
	candidates := []candidate{}

	var svcs []storage.Service
	if err := s.DB.Select("id", "name").Where("tenant_id = ?", tenantID).Find(&svcs).Error; err == nil {
		for _, v := range svcs {
			candidates = append(candidates, candidate{id: v.ID.String(), name: v.Name})
		}
	}
	var prods []storage.Product
	if err := s.DB.Select("id", "name").Where("tenant_id = ?", tenantID).Find(&prods).Error; err == nil {
		for _, v := range prods {
			candidates = append(candidates, candidate{id: v.ID.String(), name: v.Name})
		}
	}
	var menus []storage.MenuItem
	if err := s.DB.Select("id", "name").Where("tenant_id = ?", tenantID).Find(&menus).Error; err == nil {
		for _, v := range menus {
			candidates = append(candidates, candidate{id: v.ID.String(), name: v.Name})
		}
	}

	bestID := ""
	bestScore := 0
	for _, c := range candidates {
		cn := normalizeItemName(c.name)
		if cn == "" {
			continue
		}
		score := 0
		if cn == target {
			score = 1000 + len(cn)
		} else if strings.Contains(cn, target) || strings.Contains(target, cn) {
			if len(cn) < len(target) {
				score = 600 + len(cn)
			} else {
				score = 600 + len(target)
			}
		}
		if score > bestScore {
			bestScore = score
			bestID = c.id
		}
	}

	return bestID
}

func buildItemKeyCandidates(resolvedID, orderItemName string) []string {
	candidates := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(v string) {
		key := strings.TrimSpace(v)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, key)
	}

	add(resolvedID)
	add(orderItemName)
	add(normalizeItemName(orderItemName))
	return candidates
}

func normalizeAppliesTo(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "", "any", "all":
		return "any"
	case "delivery":
		return "delivery"
	case "pickup":
		return "pickup"
	default:
		return "any"
	}
}

func requirementAppliesToOrder(requirement storage.Requirement, order *storage.Order) bool {
	appliesTo := normalizeAppliesTo(requirement.AppliesTo)
	if appliesTo == "any" || order == nil {
		return true
	}

	isPickup := order.IsPickup
	isDelivery := !isPickup && strings.TrimSpace(order.DeliveryAddress) != ""

	switch appliesTo {
	case "pickup":
		return isPickup
	case "delivery":
		return isDelivery
	default:
		return true
	}
}

// EnsureFulfillmentState initializes tracking for an order if needed.
// Should be called when Order becomes PAID/CONFIRMED.
func (s *Service) EnsureFulfillmentState(order *storage.Order) error {
	if order == nil {
		return nil
	}

	var orderItems []storage.OrderItem
	if err := json.Unmarshal([]byte(order.Items), &orderItems); err != nil {
		return err
	}

	for _, item := range orderItems {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}

		itemID := s.resolveItemIDForOrderName(order.TenantID, name)
		candidates := buildItemKeyCandidates(itemID, name)
		if len(candidates) == 0 {
			continue
		}

		var matchedReq storage.ItemRequirement
		if err := s.DB.Model(&storage.ItemRequirement{}).
			Where("tenant_id = ? AND item_id IN ?", order.TenantID, candidates).
			Order("order_index asc").
			First(&matchedReq).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			// Variant inheritance fallback: try to find requirements assigned to the same family item.
			if fallbackID := s.resolveFallbackItemIDByFamily(order.TenantID, name); fallbackID != "" {
				if err := s.DB.Model(&storage.ItemRequirement{}).
					Where("tenant_id = ? AND item_id = ?", order.TenantID, fallbackID).
					Order("order_index asc").
					First(&matchedReq).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return err
				}
			} else {
				continue
			}
		}

		stateItemID := strings.TrimSpace(matchedReq.ItemID)
		if stateItemID == "" {
			continue
		}

		var existing storage.FulfillmentState
		err := s.DB.Where("tenant_id = ? AND order_id = ? AND order_item_id = ?", order.TenantID, order.ID, stateItemID).
			First(&existing).Error
		if err == nil {
			if existing.CustomerID == uuid.Nil {
				existing.CustomerID = order.CustomerID
				s.DB.Save(&existing)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		state := storage.FulfillmentState{
			TenantID:    order.TenantID,
			OrderID:     order.ID,
			OrderItemID: stateItemID,
			CustomerID:  order.CustomerID,
			Status:      "collecting",
		}
		if err := s.DB.Create(&state).Error; err != nil {
			return err
		}
	}

	return nil
}

// GetNextRequirement finds the next thing the customer needs to provide.
// Returns nil if nothing is needed.
func (s *Service) GetNextRequirement(orderID uuid.UUID) (*storage.Requirement, *storage.FulfillmentState, error) {
	var order storage.Order
	if err := s.DB.Select("id", "is_pickup", "delivery_address").Where("id = ?", orderID).First(&order).Error; err != nil {
		return nil, nil, err
	}

	var state storage.FulfillmentState
	// Find active state for this order that is NOT completed
	if err := s.DB.Where("order_id = ? AND status IN ('not_started', 'collecting')", orderID).
		Order("created_at asc").
		First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil // No pending tasks
		}
		return nil, nil, err
	}

	// Find requirements for this item
	var requirements []storage.ItemRequirement
	if err := s.DB.Preload("Requirement").
		Where("item_id = ? AND tenant_id = ?", state.OrderItemID, state.TenantID).
		Order("order_index asc").
		Find(&requirements).Error; err != nil {
		return nil, nil, err
	}

	// Find existing submissions
	var submissions []storage.RequirementSubmission
	if err := s.DB.Where("fulfillment_state_id = ?", state.ID).Find(&submissions).Error; err != nil {
		return nil, nil, err
	}

	// Map submissions for easy lookup
	submittedReqs := make(map[uuid.UUID]bool)
	for _, sub := range submissions {
		submittedReqs[sub.RequirementID] = true
	}

	// Return the first missing requirement
	for _, reqlink := range requirements {
		if !requirementAppliesToOrder(reqlink.Requirement, &order) {
			continue
		}
		if !submittedReqs[reqlink.RequirementID] {
			return &reqlink.Requirement, &state, nil
		}
	}

	// If all submitted, mark state as 'completed'
	state.Status = "completed"
	s.DB.Save(&state)

	return nil, nil, nil
}

// GetPendingRequirementForConversation determines if there is an active fulfillment task for this conversation
// It first finds the most recent valid Order for this conversation.
func (s *Service) GetPendingRequirementForConversation(tenantID uuid.UUID, conversationID uuid.UUID) (*storage.Requirement, *storage.FulfillmentState, error) {
	// 1. Find the most recent active order for this conversation.
	// Includes "pending" so requirement collection can happen before payment in v4.
	var order storage.Order
	if err := s.DB.Where("tenant_id = ? AND conversation_id = ? AND status IN ('pending', 'confirmed', 'paid', 'verified')", tenantID, conversationID).
		Order("created_at desc").
		First(&order).Error; err != nil {
		// No active orders found for this path
		return nil, nil, nil
	}

	// 2. Ensure/resolve pre-payment requirement state so pending orders can resume
	// requirement collection even if a previous turn drifted out of structured flow.
	return s.GetPrePaymentRequirementForOrder(&order)
}

// SubmitRequirement saves the answer
func (s *Service) SubmitRequirement(stateID, reqID uuid.UUID, value string) error {
	sub := storage.RequirementSubmission{
		FulfillmentStateID: stateID,
		RequirementID:      reqID,
		Value:              value,
		Verified:           false,
	}
	return s.DB.Create(&sub).Error
}

// HasPendingRequirementsForOrder checks if an order has requirements that need to be collected.
// Used for products/menu to check requirements BEFORE payment.
// Returns: hasPendingRequirements, firstRequirement, error
func (s *Service) HasPendingRequirementsForOrder(order *storage.Order) (bool, *storage.Requirement, error) {
	if order == nil {
		return false, nil, nil
	}

	var orderItems []storage.OrderItem
	if err := json.Unmarshal([]byte(order.Items), &orderItems); err != nil {
		return false, nil, err
	}

	for _, item := range orderItems {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}

		// Find the item ID (product/menu/service) with robust matching.
		itemID := s.resolveItemIDForOrderName(order.TenantID, name)
		if itemID == "" {
			// Variant inheritance fallback: try family-based item ID that has requirements.
			itemID = s.resolveFallbackItemIDByFamily(order.TenantID, name)
		}

		if itemID == "" {
			continue
		}

		// Check if this item has requirements
		var requirements []storage.ItemRequirement
		if err := s.DB.Preload("Requirement").
			Where("tenant_id = ? AND item_id = ?", order.TenantID, itemID).
			Order("order_index asc").
			Find(&requirements).Error; err != nil {
			continue
		}

		if len(requirements) > 0 {
			for _, reqLink := range requirements {
				if requirementAppliesToOrder(reqLink.Requirement, order) {
					return true, &reqLink.Requirement, nil
				}
			}
		}
	}

	return false, nil, nil
}

// GetPrePaymentRequirementForOrder gets the next unanswered requirement for an order that is being collected pre-payment.
// This works with orders in draft/confirmed/building status (before payment).
func (s *Service) GetPrePaymentRequirementForOrder(order *storage.Order) (*storage.Requirement, *storage.FulfillmentState, error) {
	if order == nil {
		return nil, nil, nil
	}

	// First ensure fulfillment state exists
	if err := s.EnsureFulfillmentState(order); err != nil {
		return nil, nil, err
	}

	// Then get next requirement
	return s.GetNextRequirement(order.ID)
}
