package tenantapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/fulfillment"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
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

func looksLikeFullAddress(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	if strings.Contains(lower, "?") {
		return false
	}
	hasDigits := false
	for _, ch := range v {
		if ch >= '0' && ch <= '9' {
			hasDigits = true
			break
		}
	}
	if hasDigits {
		return true
	}
	hints := []string{"street", "st.", "road", "rd", "avenue", "close", "lane", "drive", "junction", "bus stop", "landmark", "near", "behind", "opposite"}
	for _, h := range hints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return len(strings.Fields(v)) >= 4 && len(v) >= 18
}

// listRequirements returns all ACTIVE requirements for a tenant
func (h *Handler) listRequirements(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	var reqs []storage.Requirement
	// Filter by tenant. GORM handles soft delete (DeletedAt) automatically.
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).Order("created_at desc").Find(&reqs).Error; err != nil {
		h.Logger.Error("fetch requirements failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requirements": reqs,
	})
}

// createRequirement creates a new reusable requirement
func (h *Handler) createRequirement(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	var body struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		DataType    string `json:"data_type"`
		AppliesTo   string `json:"applies_to"` // any, delivery, pickup
		// IsActive    bool   `json:"is_active"` // Removed, using soft delete
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, r, fmt.Errorf("invalid request body"), http.StatusBadRequest)
		return
	}

	if body.Label == "" || body.DataType == "" {
		h.writeError(w, r, fmt.Errorf("label and data_type are required"), http.StatusBadRequest)
		return
	}
	appliesTo := strings.ToLower(strings.TrimSpace(body.AppliesTo))
	if appliesTo == "" {
		appliesTo = "any"
	}
	if appliesTo != "any" && appliesTo != "delivery" && appliesTo != "pickup" {
		h.writeError(w, r, fmt.Errorf("applies_to must be one of: any, delivery, pickup"), http.StatusBadRequest)
		return
	}

	req := storage.Requirement{
		TenantID:    claims.TenantID,
		Label:       body.Label,
		Description: body.Description,
		DataType:    body.DataType,
		AppliesTo:   appliesTo,
		IsRequired:  true, // Default to true
	}

	if err := h.DB.Create(&req).Error; err != nil {
		h.Logger.Error("create requirement failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, req)
}

// updateRequirement updates an existing requirement
func (h *Handler) updateRequirement(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idParam := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idParam)
	if err != nil {
		h.writeError(w, r, fmt.Errorf("invalid requirement id"), http.StatusBadRequest)
		return
	}

	var body struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		DataType    string `json:"data_type"`
		AppliesTo   string `json:"applies_to"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, r, fmt.Errorf("invalid request body"), http.StatusBadRequest)
		return
	}

	var req storage.Requirement
	if err := h.DB.Where("id = ? AND tenant_id = ?", reqID, claims.TenantID).First(&req).Error; err != nil {
		h.writeNotFoundError(w, r, "requirement")
		return
	}

	if body.Label != "" {
		req.Label = body.Label
	}
	if body.Description != "" {
		req.Description = body.Description
	}
	if body.DataType != "" {
		req.DataType = body.DataType
	}
	if strings.TrimSpace(body.AppliesTo) != "" {
		appliesTo := strings.ToLower(strings.TrimSpace(body.AppliesTo))
		if appliesTo != "any" && appliesTo != "delivery" && appliesTo != "pickup" {
			h.writeError(w, r, fmt.Errorf("applies_to must be one of: any, delivery, pickup"), http.StatusBadRequest)
			return
		}
		req.AppliesTo = appliesTo
	}

	if err := h.DB.Save(&req).Error; err != nil {
		h.Logger.Error("update requirement failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// deleteRequirement soft deletes a requirement
func (h *Handler) deleteRequirement(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idParam := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idParam)
	if err != nil {
		h.writeError(w, r, fmt.Errorf("invalid requirement id"), http.StatusBadRequest)
		return
	}

	if err := h.DB.Where("id = ? AND tenant_id = ?", reqID, claims.TenantID).Delete(&storage.Requirement{}).Error; err != nil {
		h.Logger.Error("delete requirement failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// getItemRequirements returns requirements assigned to a specific item
func (h *Handler) getItemRequirements(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	itemID := chi.URLParam(r, "itemId")

	var itemReqs []storage.ItemRequirement
	if err := h.DB.Preload("Requirement").Where("tenant_id = ? AND item_id = ?", claims.TenantID, itemID).
		Order("order_index asc").
		Find(&itemReqs).Error; err != nil {
		h.Logger.Error("fetch item requirements failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item_requirements": itemReqs,
	})
}

// assignRequirementsToItem REPLACE all requirements for an item
func (h *Handler) assignRequirementsToItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	itemID := chi.URLParam(r, "itemId")

	var body struct {
		RequirementIDs []uuid.UUID `json:"requirement_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, r, fmt.Errorf("invalid request body"), http.StatusBadRequest)
		return
	}

	tx := h.DB.Begin()

	// 1. Delete existing assignments
	if err := tx.Where("tenant_id = ? AND item_id = ?", claims.TenantID, itemID).Delete(&storage.ItemRequirement{}).Error; err != nil {
		tx.Rollback()
		h.Logger.Error("failed to clear old item requirements", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	// 2. Add new assignments
	if len(body.RequirementIDs) > 0 {
		var newAssignments []storage.ItemRequirement
		for i, reqID := range body.RequirementIDs {
			newAssignments = append(newAssignments, storage.ItemRequirement{
				TenantID:      claims.TenantID,
				ItemID:        itemID,
				RequirementID: reqID,
				OrderIndex:    i + 1,
			})
		}
		if err := tx.Create(&newAssignments).Error; err != nil {
			tx.Rollback()
			h.Logger.Error("failed to create new item requirements", zap.Error(err))
			h.writeInternalError(w, r, err)
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		h.Logger.Error("transaction commit failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// getOrderRequirements returns the fulfillment status for an order
func (h *Handler) getOrderRequirements(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	orderIDParam := chi.URLParam(r, "orderId")
	orderID, err := uuid.Parse(orderIDParam)
	if err != nil {
		h.writeError(w, r, fmt.Errorf("invalid order id"), http.StatusBadRequest)
		return
	}

	// Ensure fulfillment states exist before reading status.
	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", orderID, claims.TenantID).First(&order).Error; err != nil {
		h.writeNotFoundError(w, r, "order")
		return
	}
	if err := fulfillment.NewService(h.DB, h.Logger).EnsureFulfillmentState(&order); err != nil {
		h.Logger.Error("ensure fulfillment states failed", zap.Error(err), zap.String("order_id", orderID.String()))
		h.writeInternalError(w, r, err)
		return
	}

	// 1. Find Fulfillment States for this order
	var states []storage.FulfillmentState
	if err := h.DB.Where("order_id = ? AND tenant_id = ?", orderID, claims.TenantID).Find(&states).Error; err != nil {
		h.Logger.Error("fetch fulfillment states failed", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	type ReqStatus struct {
		RequirementID uuid.UUID `json:"requirement_id"`
		Label         string    `json:"label"`
		DataType      string    `json:"data_type"`
		AppliesTo     string    `json:"applies_to"`
		Description   string    `json:"description"`
		IsRequired    bool      `json:"is_required"`
		Value         string    `json:"value,omitempty"`
		MediaURL      string    `json:"media_url,omitempty"`
		StoredURL     string    `json:"stored_url,omitempty"`
		Filename      string    `json:"filename,omitempty"`
		Caption       string    `json:"caption,omitempty"`
		SubmittedAt   string    `json:"submitted_at,omitempty"`
		Status        string    `json:"status"` // pending, submitted
	}

	// Initialize result as empty slice (not nil) for JSON
	result := make([]ReqStatus, 0)

	// System-level fulfillment requirements (delivery/pickup) shown on the Orders page even
	// when the tenant has not configured item-level requirements.
	// This preserves the old UX where "Delivery Address / Recipient / Phone / Instructions"
	// always appear as pending until filled.
	seen := make(map[string]bool)
	addSystemReq := func(label, appliesTo, description, value string, required bool) {
		if strings.EqualFold(strings.TrimSpace(label), "Recipient Phone") && !isLikelyPhoneNumber(value) {
			value = ""
		}
		if strings.EqualFold(strings.TrimSpace(label), "Delivery Address") && strings.TrimSpace(value) != "" && !looksLikeFullAddress(value) {
			value = ""
		}

		key := strings.ToLower(strings.TrimSpace(label)) + "|" + strings.ToLower(strings.TrimSpace(appliesTo))
		if seen[key] {
			return
		}
		seen[key] = true
		status := "pending"
		submittedAt := ""
		if strings.TrimSpace(value) != "" {
			status = "submitted"
		}
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("system_requirement:"+key))
		result = append(result, ReqStatus{
			RequirementID: id,
			Label:         label,
			DataType:      "text",
			AppliesTo:     appliesTo,
			Description:   description,
			IsRequired:    required,
			Value:         strings.TrimSpace(value),
			SubmittedAt:   submittedAt,
			Status:        status,
		})
	}

	if order.IsPickup {
		pickupTime := ""
		if order.AppointmentTime != nil {
			pickupTime = order.AppointmentTime.Format(time.RFC3339)
		}
		addSystemReq("Pickup Time", "pickup", "When will you pick it up?", pickupTime, true)
		addSystemReq("Recipient Name", "pickup", "Name of the person picking up the item.", order.CustomerName, true)
		addSystemReq("Recipient Phone", "pickup", "Active phone number we can reach if needed.", order.CustomerPhone, true)
	} else {
		addSystemReq("Delivery Address", "delivery", "Full street address including landmark.", order.DeliveryAddress, true)
		addSystemReq("Recipient Name", "delivery", "Name of the person receiving the item.", order.CustomerName, true)
		addSystemReq("Recipient Phone", "delivery", "Active phone number for delivery driver.", order.CustomerPhone, true)
		addSystemReq("Special Delivery Instructions", "delivery", "Gate code, leave at door, etc.", order.DeliveryNotes, true)
	}

	// 2. For each state (item), get requirements and submissions
	for _, state := range states {
		var itemReqs []storage.ItemRequirement
		if err := h.DB.Preload("Requirement").
			Where("tenant_id = ? AND item_id = ?", claims.TenantID, state.OrderItemID).
			Order("order_index asc").
			Find(&itemReqs).Error; err != nil {
			continue
		}

		var submissions []storage.RequirementSubmission
		if err := h.DB.Where("fulfillment_state_id = ?", state.ID).Find(&submissions).Error; err != nil {
			continue
		}

		subMap := make(map[uuid.UUID]storage.RequirementSubmission)
		for _, s := range submissions {
			subMap[s.RequirementID] = s
		}

		for _, ir := range itemReqs {
			// Deduplicate against system requirements. A requirement with applies_to="any"
			// overlaps with system requirements tagged "delivery" or "pickup", so we must
			// check all three variants of the key.
			labelKey := strings.ToLower(strings.TrimSpace(ir.Requirement.Label))
			appliesKey := strings.ToLower(strings.TrimSpace(ir.Requirement.AppliesTo))
			if seen[labelKey+"|"+appliesKey] || seen[labelKey+"|delivery"] || seen[labelKey+"|pickup"] || seen[labelKey+"|any"] {
				continue
			}

			status := "pending"
			val := ""
			submittedAt := ""

			if sub, exists := subMap[ir.RequirementID]; exists {
				status = "submitted"
				val = sub.Value
				submittedAt = sub.CreatedAt.Format(time.RFC3339)

				if strings.EqualFold(ir.Requirement.DataType, "file") {
					var meta map[string]any
					if err := json.Unmarshal([]byte(sub.Value), &meta); err == nil {
						mediaURL, _ := meta["media_url"].(string)
						storedURL, _ := meta["media_stored_url"].(string)
						filename, _ := meta["filename"].(string)
						caption, _ := meta["caption"].(string)

						// Prefer stable stored URL when available
						if storedURL != "" {
							val = storedURL
						} else if mediaURL != "" {
							val = mediaURL
						}

						// Populate optional fields for UI
						result = append(result, ReqStatus{
							RequirementID: ir.RequirementID,
							Label:         ir.Requirement.Label,
							DataType:      ir.Requirement.DataType,
							AppliesTo:     ir.Requirement.AppliesTo,
							Description:   ir.Requirement.Description,
							IsRequired:    ir.Requirement.IsRequired,
							Value:         val,
							MediaURL:      mediaURL,
							StoredURL:     storedURL,
							Filename:      filename,
							Caption:       caption,
							SubmittedAt:   submittedAt,
							Status:        status,
						})
						continue
					}
				}
			}

			result = append(result, ReqStatus{
				RequirementID: ir.RequirementID,
				Label:         ir.Requirement.Label,
				DataType:      ir.Requirement.DataType,
				AppliesTo:     ir.Requirement.AppliesTo,
				Description:   ir.Requirement.Description,
				IsRequired:    ir.Requirement.IsRequired,
				Value:         val,
				SubmittedAt:   submittedAt,
				Status:        status,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requirements_status": result,
	})
}
