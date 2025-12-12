package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type negotiationDefaultPayload struct {
	Mode     string  `json:"mode"`
	MinPrice float64 `json:"min_price"`
	MaxPrice float64 `json:"max_price"`
}

// getNegotiationSettings returns tenant-wide negotiation defaults.
func (h *Handler) getNegotiationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.WithContext(r.Context()).First(&tenant, claims.TenantID).Error; err != nil {
		h.Logger.Error("failed to load tenant", zap.Error(err))
		http.Error(w, "failed to load tenant", http.StatusInternalServerError)
		return
	}

	payload := negotiationDefaultPayload{Mode: negotiationModeFixed, MinPrice: 0, MaxPrice: 0}
	if tenant.IndustrySettings != "" {
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(tenant.IndustrySettings), &settings); err == nil {
			if v, ok := settings["negotiation_default"].(map[string]interface{}); ok {
				if mode, ok := v["mode"].(string); ok {
					payload.Mode = mode
				}
				if min, ok := v["min_price"].(float64); ok {
					payload.MinPrice = min
				}
				if max, ok := v["max_price"].(float64); ok {
					payload.MaxPrice = max
				}
			}
		}
	}

	mode, ok := normalizeNegotiationMode(payload.Mode)
	if !ok || mode == negotiationModeDefault {
		payload.Mode = negotiationModeFixed
		payload.MinPrice = 0
		payload.MaxPrice = 0
	} else {
		payload.Mode = mode
		if err := validateNegotiation(payload.Mode, payload.MinPrice, payload.MaxPrice); err != nil {
			payload.Mode = negotiationModeFixed
			payload.MinPrice = 0
			payload.MaxPrice = 0
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"negotiation_default": payload,
	})
}

// updateNegotiationSettings sets tenant-wide negotiation defaults.
func (h *Handler) updateNegotiationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		NegotiationDefault *negotiationDefaultPayload `json:"negotiation_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NegotiationDefault == nil {
		http.Error(w, "negotiation_default is required", http.StatusBadRequest)
		return
	}

	mode, ok := normalizeNegotiationMode(req.NegotiationDefault.Mode)
	if !ok || mode == negotiationModeDefault {
		http.Error(w, "invalid negotiation_default.mode", http.StatusBadRequest)
		return
	}
	if err := validateNegotiation(mode, req.NegotiationDefault.MinPrice, req.NegotiationDefault.MaxPrice); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.WithContext(r.Context()).First(&tenant, claims.TenantID).Error; err != nil {
		h.Logger.Error("failed to load tenant", zap.Error(err))
		http.Error(w, "failed to load tenant", http.StatusInternalServerError)
		return
	}

	settings := make(map[string]interface{})
	if tenant.IndustrySettings != "" {
		_ = json.Unmarshal([]byte(tenant.IndustrySettings), &settings)
	}
	settings["negotiation_default"] = map[string]interface{}{
		"mode":      mode,
		"min_price": req.NegotiationDefault.MinPrice,
		"max_price": req.NegotiationDefault.MaxPrice,
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		h.Logger.Error("failed to serialize settings", zap.Error(err))
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}

	if err := h.DB.WithContext(r.Context()).Model(&storage.Tenant{}).Where("id = ?", claims.TenantID).Update("industry_settings", string(settingsJSON)).Error; err != nil {
		h.Logger.Error("failed to update tenant industry settings", zap.Error(err))
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"negotiation_default": negotiationDefaultPayload{Mode: mode, MinPrice: req.NegotiationDefault.MinPrice, MaxPrice: req.NegotiationDefault.MaxPrice},
	})
}
