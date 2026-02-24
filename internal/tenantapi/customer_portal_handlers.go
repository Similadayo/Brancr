package tenantapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// =======================
// Customer Portal API (Token-based access)
// =======================

// getOrderByPortalToken allows customers to access their order using a portal token
func (h *Handler) getOrderByPortalToken(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_token",
			"message": "Portal token is required",
		})
		return
	}

	// Find payment by portal token
	var payment storage.Payment
	if err := h.DB.Where("portal_token = ?", token).First(&payment).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "invalid_token",
			"message": "Invalid or expired portal token",
		})
		return
	}

	// Check if token is expired
	if payment.TokenExpiresAt != nil && time.Now().After(*payment.TokenExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "token_expired",
			"message": "Portal token has expired. Please request a new link.",
		})
		return
	}

	// Get order details
	var order storage.Order
	if err := h.DB.Where("id = ?", payment.OrderID).First(&order).Error; err != nil {
		h.Logger.Error("order not found for payment", zap.Stringer("payment_id", payment.ID), zap.Stringer("order_id", payment.OrderID))
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "order_not_found",
			"message": "Order not found",
		})
		return
	}

	// Parse order items
	var items []storage.OrderItem
	if err := json.Unmarshal([]byte(order.Items), &items); err != nil {
		items = []storage.OrderItem{}
	}

	// Get tenant business profile
	var tenant storage.Tenant
	if err := h.DB.Preload("BusinessProfile").Where("id = ?", order.TenantID).First(&tenant).Error; err != nil {
		h.Logger.Error("tenant not found", zap.Stringer("tenant_id", order.TenantID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "server_error",
			"message": "Failed to load order details",
		})
		return
	}

	// Build response
	response := map[string]any{
		"order": map[string]any{
			"id":                order.ID,
			"order_number":      order.OrderNumber,
			"payment_reference": order.PaymentReference,
			"status":            order.Status,
			"total_amount":      order.TotalAmount,
			"currency":          order.Currency,
			"items":             items,
			"customer_name":     order.CustomerName,
			"customer_phone":    order.CustomerPhone,
			"customer_email":    order.CustomerEmail,
			"platform":          order.Platform,
			"created_at":        order.CreatedAt,
			"confirmed_at":      order.ConfirmedAt,
			"completed_at":      order.CompletedAt,
		},
		"payment": map[string]any{
			"id":                  payment.ID,
			"payment_reference":   payment.PaymentReference,
			"amount":              payment.Amount,
			"currency":            payment.Currency,
			"payment_method":      payment.PaymentMethod,
			"status":              payment.Status,
			"verification_status": payment.VerificationStatus,
			"receipt_id":          payment.ReceiptID,
			"receipt_url":         payment.ReceiptURL,
			"created_at":          payment.CreatedAt,
			"completed_at":        payment.CompletedAt,
		},
		"business": map[string]any{
			"name":     tenant.BusinessProfile.Name,
			"email":    tenant.Email,
			"phone":    tenant.OwnerPhone,
			"location": tenant.BusinessProfile.Location,
		},
	}

	writeJSON(w, http.StatusOK, response)
}

// getReceiptByPortalToken allows customers to download/view their receipt
func (h *Handler) getReceiptByPortalToken(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_token",
			"message": "Portal token is required",
		})
		return
	}

	// Find payment by portal token
	var payment storage.Payment
	if err := h.DB.Where("portal_token = ?", token).First(&payment).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "invalid_token",
			"message": "Invalid or expired portal token",
		})
		return
	}

	// Check if token is expired
	if payment.TokenExpiresAt != nil && time.Now().After(*payment.TokenExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "token_expired",
			"message": "Portal token has expired. Please request a new link.",
		})
		return
	}

	// Check if receipt exists
	if payment.ReceiptID == "" || payment.ReceiptURL == "" {
		// Generate receipt if not exists
		if payment.Status == "confirmed" || payment.Status == "verified" {
			cloudinaryURL := h.getCloudinaryURL()
			publicURL := h.Config.PublicURL
			if publicURL == "" {
				publicURL = "https://api.brancr.com" // Default fallback
			}
			receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
			_, err := receiptService.GenerateReceipt(r.Context(), payment.ID, payment.TenantID)
			if err != nil {
				h.Logger.Error("failed to generate receipt", zap.Error(err), zap.Stringer("payment_id", payment.ID))
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":   "receipt_generation_failed",
					"message": "Failed to generate receipt",
				})
				return
			}

			// Reload payment to get receipt URL
			if err := h.DB.Where("id = ?", payment.ID).First(&payment).Error; err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":   "server_error",
					"message": "Failed to load receipt",
				})
				return
			}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "receipt_not_available",
				"message": "Receipt is only available for confirmed payments",
			})
			return
		}
	}

	// If ReceiptURL is a Cloudinary/external URL, redirect to it
	if strings.HasPrefix(payment.ReceiptURL, "https://") || strings.HasPrefix(payment.ReceiptURL, "http://") {
		http.Redirect(w, r, payment.ReceiptURL, http.StatusFound)
		return
	}

	// ReceiptURL is HTML - need to generate PDF on-the-fly or serve properly
	// Get order details for PDF generation
	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", payment.OrderID, payment.TenantID).First(&order).Error; err != nil {
		h.Logger.Error("failed to fetch order for receipt", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "server_error",
			"message": "Failed to load receipt",
		})
		return
	}

	var tenant storage.Tenant
	if err := h.DB.Preload("BusinessProfile").Where("id = ?", payment.TenantID).First(&tenant).Error; err != nil {
		h.Logger.Error("failed to fetch tenant for receipt", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "server_error",
			"message": "Failed to load receipt",
		})
		return
	}

	// Generate receipt HTML
	cloudinaryURL := h.getCloudinaryURL()
	publicURL := h.Config.PublicURL
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}
	receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
	receiptHTML, err := receiptService.GenerateReceiptHTML(order, payment, tenant)
	if err != nil {
		h.Logger.Error("failed to generate receipt HTML", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "server_error",
			"message": "Failed to generate receipt",
		})
		return
	}

	// Default to PDF unless explicitly requesting HTML
	// Check if client explicitly wants HTML (via query parameter)
	wantsHTML := r.URL.Query().Get("format") == "html"
	wantsPDF := !wantsHTML // Default to PDF

	if wantsPDF {
		// Generate PDF on-the-fly
		pdfData, err := receipt.ConvertHTMLToPDF(r.Context(), receiptHTML, h.Logger)
		if err != nil {
			h.Logger.Warn("failed to generate PDF on-the-fly, serving HTML instead", zap.Error(err))
			// Fall through to serve HTML
		} else {
			// Serve PDF
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="receipt-%s.pdf"`, payment.PaymentReference))
			w.WriteHeader(http.StatusOK)
			w.Write(pdfData)
			return
		}
	}

	// Serve HTML with proper styling (print-friendly)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(receiptHTML))
}

// listCustomerOrders lists all orders for a customer (using portal token)
func (h *Handler) listCustomerOrders(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_token",
			"message": "Portal token is required",
		})
		return
	}

	// Find payment by portal token to get customer ID
	var payment storage.Payment
	if err := h.DB.Where("portal_token = ?", token).First(&payment).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "invalid_token",
			"message": "Invalid or expired portal token",
		})
		return
	}

	// Check if token is expired
	if payment.TokenExpiresAt != nil && time.Now().After(*payment.TokenExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "token_expired",
			"message": "Portal token has expired. Please request a new link.",
		})
		return
	}

	// Get all orders for this customer
	var orders []storage.Order
	if err := h.DB.Where("customer_id = ? AND tenant_id = ?", payment.CustomerID, payment.TenantID).
		Order("created_at DESC").
		Find(&orders).Error; err != nil {
		h.Logger.Error("failed to fetch customer orders", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "server_error",
			"message": "Failed to load orders",
		})
		return
	}

	// Build response
	orderList := make([]map[string]any, 0, len(orders))
	for _, order := range orders {
		var items []storage.OrderItem
		json.Unmarshal([]byte(order.Items), &items)

		orderList = append(orderList, map[string]any{
			"id":                order.ID,
			"order_number":      order.OrderNumber,
			"payment_reference": order.PaymentReference,
			"status":            order.Status,
			"total_amount":      order.TotalAmount,
			"currency":          order.Currency,
			"items":             items,
			"created_at":        order.CreatedAt,
			"confirmed_at":      order.ConfirmedAt,
			"completed_at":      order.CompletedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"orders": orderList,
		"count":  len(orderList),
	})
}

// generatePortalToken generates a portal token for an order (tenant-only endpoint)
func (h *Handler) generatePortalToken(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "unauthorized",
			"message": "Authentication required",
		})
		return
	}

	orderIDStr := chi.URLParam(r, "orderId")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_order_id",
			"message": "Invalid order ID",
		})
		return
	}

	// Verify order belongs to tenant
	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", orderID, claims.TenantID).First(&order).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "order_not_found",
			"message": "Order not found",
		})
		return
	}

	// Generate portal token and URL
	publicURL := h.Config.PublicURL
	if publicURL == "" {
		// Derive public URL from request (helps local/dev environments)
		scheme := "https"
		if hdr := r.Header.Get("X-Forwarded-Proto"); hdr != "" {
			scheme = hdr
		} else if r.TLS == nil {
			scheme = "http"
		}
		host := r.Host
		if host == "" {
			host = "api.brancr.com"
		}
		publicURL = fmt.Sprintf("%s://%s", scheme, host)
	}
	// ReceiptService for portal token generation doesn't need Cloudinary or EmailService
	receiptService := receipt.NewReceiptService(h.DB, h.Logger, "", publicURL, nil)

	token, url, err := receiptService.GeneratePortalTokenAndURL(r.Context(), orderID, claims.TenantID, publicURL)
	if err != nil {
		h.Logger.Error("failed to generate portal token", zap.Error(err), zap.Stringer("order_id", orderID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "token_generation_failed",
			"message": "Failed to generate portal token",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"portal_token": token,
		"portal_url":   url,
		"message":      "Portal token generated successfully",
	})
}
