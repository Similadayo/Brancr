package tenantapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

// EscalationContextResponse for right panel display
type EscalationContextResponse struct {
	ID           uuid.UUID `json:"id"`
	Type         string    `json:"type"`   // payment, complaint, booking, general
	Status       string    `json:"status"` // pending, approved, resolved, confirmed
	CustomerName string    `json:"customer_name"`
	Platform     string    `json:"platform"`
	Message      string    `json:"message"`
	CreatedAt    time.Time `json:"created_at"`

	// Context
	Summary    string   `json:"summary"`
	KeyDetails []string `json:"key_details"`

	// Actions available
	PrimaryAction    string   `json:"primary_action"` // "Approve", "Resolve", "Confirm"
	SecondaryActions []string `json:"secondary_actions"`

	// Payment-specific
	PaymentAmount *float64   `json:"payment_amount,omitempty"`
	OrderID       *uuid.UUID `json:"order_id,omitempty"`
	HasReceipt    bool       `json:"has_receipt,omitempty"`

	// Booking-specific
	ProposedTime *time.Time `json:"proposed_time,omitempty"`
}

// getEscalationContext returns context for right panel
func (h *Handler) getEscalationContext(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid escalation ID", nil)
		return
	}

	var escalation storage.PendingEscalation
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&escalation).Error; err != nil {
		h.writeNotFoundError(w, r, "escalation not found")
		return
	}

	// Get customer name
	var customer storage.Customer
	h.DB.First(&customer, escalation.CustomerID)

	// Extract context
	escType := bot.EscalationType(escalation.EscalationType)
	ctx := bot.ExtractContext(escalation.Message, escType)

	response := EscalationContextResponse{
		ID:               escalation.ID,
		Type:             escalation.EscalationType,
		Status:           escalation.ResolutionStatus,
		CustomerName:     customer.DisplayName,
		Platform:         escalation.Platform,
		Message:          escalation.Message,
		CreatedAt:        escalation.CreatedAt,
		Summary:          ctx.Summary,
		KeyDetails:       ctx.KeyDetails,
		PrimaryAction:    ctx.SuggestedAction,
		SecondaryActions: ctx.SecondaryActions,
		PaymentAmount:    &escalation.PaymentAmount,
		OrderID:          escalation.OrderID,
	}

	writeJSON(w, http.StatusOK, response)
}

// ApprovePaymentRequest for approving payment
type ApprovePaymentRequest struct {
	SendReceipt bool   `json:"send_receipt"`
	Message     string `json:"message"` // Optional custom message
}

// approvePaymentEscalation confirms payment and sends receipt
func (h *Handler) approvePaymentEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid escalation ID", nil)
		return
	}

	var req ApprovePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.SendReceipt = true // Default: send receipt
	}

	// Update escalation
	now := time.Now()
	updates := map[string]interface{}{
		"resolution_status": storage.ResolutionApproved,
		"resolved_at":       &now,
		"resolved_by":       claims.Email,
		"receipt_sent":      req.SendReceipt,
	}
	if req.Message != "" {
		updates["resolution_message"] = req.Message
	}

	// Fetch escalation to get OrderID
	var escalation storage.PendingEscalation
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).First(&escalation).Error; err != nil {
		h.writeNotFoundError(w, r, "escalation")
		return
	}

	// Update escalation status
	if err := h.DB.Model(&escalation).Updates(updates).Error; err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	// Trigger receipt link generation and customer notification (best-effort)
	if req.SendReceipt && h.MessageHandler != nil {
		// Load interaction for thread ID
		var interaction storage.Interaction
		if err := h.DB.First(&interaction, escalation.InteractionID).Error; err == nil {
			// Find associated order/payment
			var order storage.Order
			if escalation.OrderID != nil {
				_ = h.DB.First(&order, *escalation.OrderID).Error
			}

			var payment storage.Payment
			paymentFound := false
			if order.ID != uuid.Nil {
				if err := h.DB.Where("tenant_id = ? AND order_id = ?", claims.TenantID, order.ID).First(&payment).Error; err == nil {
					paymentFound = true
				}
			}
			if !paymentFound {
				if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND status IN ?", claims.TenantID, escalation.CustomerID, []string{"verified", "confirmed"}).Order("created_at DESC").First(&payment).Error; err == nil {
					paymentFound = true
				}
			}

			if paymentFound {
				// Ensure payment is verified
				now := time.Now()
				if payment.Status != "verified" {
					payment.Status = "verified"
					payment.VerificationStatus = "tenant_verified"
					payment.VerifiedAt = &now
					_ = h.DB.Save(&payment).Error
				}

				// Ensure portal token exists so we always have a downloadable receipt link
				publicURL := "https://api.brancr.com"
				if h.Config != nil && strings.TrimSpace(h.Config.PublicURL) != "" {
					publicURL = strings.TrimRight(strings.TrimSpace(h.Config.PublicURL), "/")
				}
				if order.ID != uuid.Nil {
					cloudinaryURL := h.getCloudinaryURL()
					receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
					_, _, _ = receiptService.GeneratePortalTokenAndURL(r.Context(), order.ID, claims.TenantID, publicURL)
					_ = h.DB.Where("id = ?", payment.ID).First(&payment).Error
				}
				if payment.PortalToken == "" {
					payment.PortalToken = utils.GeneratePortalToken()
					_ = h.DB.Save(&payment).Error
				}

				// Best-effort: generate receipt URL (PDF) when possible
				if payment.ReceiptURL == "" || payment.ReceiptID == "" {
					cloudinaryURL := h.getCloudinaryURL()
					receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
					if generated, err := receiptService.GenerateReceipt(r.Context(), payment.ID, claims.TenantID); err == nil {
						payment = *generated
					}
				}

				receiptLink := ""
				if strings.HasPrefix(payment.ReceiptURL, "https://") || strings.HasPrefix(payment.ReceiptURL, "http://") {
					receiptLink = payment.ReceiptURL
				} else if payment.PortalToken != "" {
					receiptLink = fmt.Sprintf("%s/portal/receipt?token=%s", publicURL, payment.PortalToken)
				} else if payment.PortalURL != "" {
					receiptLink = payment.PortalURL
				}

				if receiptLink != "" {
					receiptMsg := "✅ Payment confirmed.\n\nDownload your receipt: " + receiptLink
					if req.Message != "" {
						receiptMsg = strings.TrimSpace(req.Message) + "\n\n" + "Download your receipt: " + receiptLink
					}

					// Load customer + thread
					var customer storage.Customer
					if err := h.DB.First(&customer, escalation.CustomerID).Error; err == nil {
						threadID := customer.PlatformID
						if interaction.ConversationID != uuid.Nil {
							var conversation storage.Conversation
							if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
								if conversation.PlatformThreadID != "" {
									threadID = conversation.PlatformThreadID
								}
							}
						}
						if threadID == "" {
							threadID = customer.PlatformID
						}

						_ = h.MessageHandler.SendReply(r.Context(), claims.TenantID, escalation.Platform, threadID, customer.PlatformID, receiptMsg)
					}
				}
			}
		}
	}

	// AUTOMATICALLY UPDATE ORDER STATUS
	if escalation.OrderID != nil {
		var order storage.Order
		if err := h.DB.First(&order, *escalation.OrderID).Error; err == nil {
			// Tenant-approved payment means the order is CONFIRMED (dashboard should reflect this).
			now := time.Now()
			order.Status = "confirmed"
			if order.ConfirmedAt == nil {
				order.ConfirmedAt = &now
			}
			h.DB.Save(&order)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Payment approved and receipt sent",
	})
}

// resolveComplaintEscalation marks complaint as resolved
func (h *Handler) resolveComplaintEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid escalation ID", nil)
		return
	}

	var req struct {
		Message string `json:"message"` // Resolution message to customer
	}
	json.NewDecoder(r.Body).Decode(&req)

	now := time.Now()
	if err := h.DB.Model(&storage.PendingEscalation{}).
		Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		Updates(map[string]interface{}{
			"resolution_status":  storage.ResolutionResolved,
			"resolution_message": req.Message,
			"resolved_at":        &now,
			"resolved_by":        claims.Email,
		}).Error; err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	// TODO: Send resolution message to customer

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Complaint marked as resolved",
	})
}

// confirmBookingEscalation confirms a booking request
func (h *Handler) confirmBookingEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid escalation ID", nil)
		return
	}

	var req struct {
		ConfirmedTime time.Time `json:"confirmed_time"`
		Message       string    `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request", nil)
		return
	}

	now := time.Now()
	if err := h.DB.Model(&storage.PendingEscalation{}).
		Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		Updates(map[string]interface{}{
			"resolution_status":  storage.ResolutionConfirmed,
			"confirmed_time":     req.ConfirmedTime,
			"resolution_message": req.Message,
			"resolved_at":        &now,
			"resolved_by":        claims.Email,
		}).Error; err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	// TODO: Create booking record and notify customer

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Booking confirmed",
	})
}
