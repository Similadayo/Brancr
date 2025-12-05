package tenantapi

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/receipt"
	"go.uber.org/zap"
)

// =======================
// Receipt Generation
// =======================

// generateReceipt generates a receipt for a confirmed payment
func (h *Handler) generateReceipt(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "unauthorized",
			"message": "Authentication required",
		})
		return
	}

	paymentIDStr := chi.URLParam(r, "id")
	var paymentID uint
	if _, err := fmt.Sscanf(paymentIDStr, "%d", &paymentID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_payment_id",
			"message": "Invalid payment ID",
		})
		return
	}

	// Generate receipt
	cloudinaryURL := h.getCloudinaryURL()
	publicURL := h.Config.PublicURL
	if publicURL == "" {
		publicURL = "https://api.brancr.com" // Default fallback
	}
	receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
	payment, err := receiptService.GenerateReceipt(r.Context(), paymentID, claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to generate receipt", zap.Error(err), zap.Uint("payment_id", paymentID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "receipt_generation_failed",
			"message": "Failed to generate receipt",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"receipt_id":  payment.ReceiptID,
		"receipt_url": payment.ReceiptURL,
		"message":     "Receipt generated successfully",
	})
}

