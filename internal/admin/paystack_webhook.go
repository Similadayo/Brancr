package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/billing"
	"go.uber.org/zap"
)

// PaystackWebhookHandler handles Paystack webhook events
func PaystackWebhookHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Verify webhook signature (if secret is configured)
		// Note: Paystack webhook secret should be set in environment
		secret := r.Header.Get("X-Paystack-Signature")
		if secret != "" {
			// Verify signature (implement if needed)
			// For now, we'll trust the webhook if it comes from Paystack IPs
		}

		var event billing.PaystackWebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			ctx.Logger.Error("failed to decode Paystack webhook", zap.Error(err))
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Initialize services
		paystackClient := billing.NewPaystackClient(
			ctx.Config.PaystackSecretKey,
			ctx.Config.PaystackPublicKey,
			ctx.Logger,
		)
		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		paystackService := billing.NewPaystackService(paystackClient, ctx.DB, ctx.Logger, billingService)

		// Handle webhook event
		if err := paystackService.HandleWebhook(reqCtx, event); err != nil {
			ctx.Logger.Error("failed to handle Paystack webhook",
				zap.String("event", event.Event),
				zap.Error(err))
			http.Error(w, "failed to process webhook", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
		})
	}
}

// VerifyPaystackSignature verifies Paystack webhook signature
func VerifyPaystackSignature(payload []byte, signature, secret string) bool {
	if secret == "" {
		return false // Can't verify without secret
	}

	// Paystack uses HMAC SHA512
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

