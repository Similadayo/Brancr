package middleware

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OnboardingCheck middleware ensures the tenant has completed onboarding before accessing protected routes.
// If onboarding is not complete, it returns a 403 Forbidden with a specific error code that the frontend can handle.
func OnboardingCheck(db *gorm.DB, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get tenant from context (must be authenticated first)
			claims, ok := TenantFromContext(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if tenant has completed onboarding
			var onboardingComplete bool
			err := db.Table("tenants").
				Select("onboarding_complete").
				Where("id = ?", claims.TenantID).
				Scan(&onboardingComplete).Error

			if err != nil {
				logger.Error("failed to check onboarding status",
					zap.Uint("tenant_id", claims.TenantID),
					zap.Error(err))
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			// If onboarding is not complete, return 403 with specific error code
			if !onboardingComplete {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":             "onboarding_required",
					"message":           "Please complete onboarding before accessing this resource",
					"onboarding_complete": false,
					"redirect_to":        "/app/onboarding",
				})
				return
			}

			// Onboarding complete, proceed
			next.ServeHTTP(w, r)
		})
	}
}

