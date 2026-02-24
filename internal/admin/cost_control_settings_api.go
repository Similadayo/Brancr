package admin

import (
	"encoding/json"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

// CostControlSettings stores system-wide cost control settings
type CostControlSettings struct {
	MaxAICallsPerMin int `json:"max_ai_calls_per_min"`
}

var (
	costControlSettingsMu sync.RWMutex
	costControlSettings   = CostControlSettings{
		MaxAICallsPerMin: 3, // Default
	}
)

// GetCostControlSettingsHandler returns current cost control settings
// GET /admin/api/cost-control/settings
func GetCostControlSettingsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		costControlSettingsMu.RLock()
		settings := costControlSettings
		costControlSettingsMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(settings); err != nil {
			ctx.Logger.Error("failed to encode cost control settings", zap.Error(err))
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// UpdateCostControlSettingsHandler updates cost control settings
// POST /admin/api/cost-control/settings
func UpdateCostControlSettingsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload CostControlSettings
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			ctx.Logger.Warn("failed to decode cost control settings payload", zap.Error(err))
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate
		if payload.MaxAICallsPerMin < 1 {
			http.Error(w, "max_ai_calls_per_min must be at least 1", http.StatusBadRequest)
			return
		}
		if payload.MaxAICallsPerMin > 10 {
			http.Error(w, "max_ai_calls_per_min cannot exceed 10", http.StatusBadRequest)
			return
		}

		// Update settings
		costControlSettingsMu.Lock()
		costControlSettings = payload
		costControlSettingsMu.Unlock()

		// Update ProductionBuffer dynamically (if available)
		if ctx.ProductionBuffer != nil {
			ctx.ProductionBuffer.UpdateMaxAICallsPerMin(payload.MaxAICallsPerMin)
			ctx.Logger.Info("cost control settings updated in production buffer",
				zap.Int("max_ai_calls_per_min", payload.MaxAICallsPerMin))
		}

		ctx.Logger.Info("cost control settings updated",
			zap.Int("max_ai_calls_per_min", payload.MaxAICallsPerMin),
			zap.String("admin_email", AdminEmailFromRequest(r)))

		// Audit log
		ae := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "cost_control.settings.update",
			Metadata: map[string]interface{}{
				"max_ai_calls_per_min": payload.MaxAICallsPerMin,
			},
		}
		_ = auditWriter(r.Context(), ctx, ae)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			ctx.Logger.Error("failed to encode response", zap.Error(err))
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// GetCostControlSettings returns the current cost control settings (for internal use)
func GetCostControlSettings() CostControlSettings {
	costControlSettingsMu.RLock()
	defer costControlSettingsMu.RUnlock()
	return costControlSettings
}

// InitializeCostControlSettings initializes cost control settings from config
func InitializeCostControlSettings(maxAICallsPerMin int) {
	if maxAICallsPerMin > 0 {
		costControlSettingsMu.Lock()
		costControlSettings.MaxAICallsPerMin = maxAICallsPerMin
		costControlSettingsMu.Unlock()
	}
}

