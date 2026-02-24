package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type ValidationResponse struct {
	Valid   bool     `json:"valid"`
	Reasons []string `json:"reasons,omitempty"`
}

type EscalationSettingsResponse struct {
	Enabled            bool   `json:"enabled"`
	EscalationBehavior string `json:"escalation_behavior"` // always_on, configurable, advanced
	IsConfigurable     bool   `json:"is_configurable"`
}

type UpdateEscalationSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

// getEscalationSettings retrieves current escalation configuration
func (h *Handler) getEscalationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	tenantID := claims.TenantID

	// 1. Get Plan Settings (to know if configurable)
	var tenantPlan storage.TenantPlan
	err := h.DB.WithContext(r.Context()).
		Where("tenant_id = ? AND billing_status IN (?)", tenantID, []string{"trial", "active"}).
		Order("created_at DESC").
		First(&tenantPlan).Error

	behavior := "always_on"
	isConfigurable := false

	if err == nil && tenantPlan.EscalationBehavior != "" {
		behavior = tenantPlan.EscalationBehavior
	}

	if behavior == "configurable" {
		isConfigurable = true
	}

	// 2. Get User Preference from Metadata
	var tenant storage.Tenant
	if err := h.DB.WithContext(r.Context()).Select("metadata").First(&tenant, tenantID).Error; err != nil {
		h.Logger.Error("failed to get tenant", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve settings"})
		return
	}

	enabled := true // Default
	if tenant.Metadata != "" && tenant.Metadata != "{}" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(tenant.Metadata), &meta); err == nil {
			if val, ok := meta["escalation_enabled"].(bool); ok {
				enabled = val
			}
		}
	}

	writeJSON(w, http.StatusOK, EscalationSettingsResponse{
		Enabled:            enabled,
		EscalationBehavior: behavior,
		IsConfigurable:     isConfigurable,
	})
}

// updateEscalationSettings updates escalation configuration
func (h *Handler) updateEscalationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	tenantID := claims.TenantID

	var req UpdateEscalationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	// 1. Verify Plan allows configuration
	var tenantPlan storage.TenantPlan
	err := h.DB.WithContext(r.Context()).
		Where("tenant_id = ? AND billing_status IN (?)", tenantID, []string{"trial", "active"}).
		Order("created_at DESC").
		First(&tenantPlan).Error

	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "No active plan found"})
		return
	}

	if tenantPlan.EscalationBehavior != "configurable" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your current plan does not allow configuring escalation settings"})
		return
	}

	// 2. Update Metadata
	var tenant storage.Tenant
	if err := h.DB.WithContext(r.Context()).First(&tenant, tenantID).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve tenant"})
		return
	}

	var meta map[string]interface{}
	if tenant.Metadata == "" || tenant.Metadata == "{}" {
		meta = make(map[string]interface{})
	} else {
		if err := json.Unmarshal([]byte(tenant.Metadata), &meta); err != nil {
			meta = make(map[string]interface{}) // Reset if corrupt
		}
	}

	meta["escalation_enabled"] = req.Enabled

	newMeta, err := json.Marshal(meta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save settings"})
		return
	}

	if err := h.DB.WithContext(r.Context()).Model(&tenant).Update("metadata", string(newMeta)).Error; err != nil {
		h.Logger.Error("failed to update tenant metadata", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save settings"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
