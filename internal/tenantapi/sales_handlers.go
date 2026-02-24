package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"go.uber.org/zap"
)

// getSalesNegotiations handles GET /tenant/sales-conversations/{id}/negotiations
func (h *Handler) getSalesNegotiations(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// ensure sales conversation belongs to tenant
	var sc storage.SalesConversation
	if err := h.DB.First(&sc, id).Error; err != nil {
		h.Logger.Error("sales conversation not found", zap.Error(err), zap.Stringer("sales_conversation_id", id))
		http.Error(w, "sales conversation not found", http.StatusNotFound)
		return
	}
	if sc.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	repo := repositories.NewNegotiationRepo(h.DB)
	negos, err := repo.ListBySalesConversation(r.Context(), id)
	if err != nil {
		h.Logger.Error("failed to list negotiations", zap.Error(err), zap.Stringer("sales_conversation_id", id))
		http.Error(w, "failed to list negotiations", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"negotiations": negos})
}

// approveNegotiation handles POST /tenant/negotiations/{id}/approve
func (h *Handler) approveNegotiation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid negotiation id", http.StatusBadRequest)
		return
	}
	var req struct {
		FinalPrice float64 `json:"final_price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Load negotiation
	var neg storage.NegotiationHistory
	if err := h.DB.First(&neg, id).Error; err != nil {
		h.Logger.Error("negotiation not found", zap.Error(err), zap.Stringer("negotiation_id", id))
		http.Error(w, "negotiation not found", http.StatusNotFound)
		return
	}
	// verify tenant owns the sales conversation
	var sc storage.SalesConversation
	if err := h.DB.First(&sc, neg.SalesConversationID).Error; err != nil {
		h.Logger.Error("sales conversation not found", zap.Error(err), zap.Stringer("sales_conversation_id", neg.SalesConversationID))
		http.Error(w, "sales conversation not found", http.StatusNotFound)
		return
	}
	if sc.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Update negotiation
	update := map[string]interface{}{"status": "agreed", "final_agreement": req.FinalPrice}
	if err := h.DB.Model(&neg).Updates(update).Error; err != nil {
		h.Logger.Error("failed to update negotiation", zap.Error(err), zap.Stringer("negotiation_id", neg.ID))
		http.Error(w, "failed to update negotiation", http.StatusInternalServerError)
		return
	}
	// Update sales conversation price and stage
	if req.FinalPrice > 0 {
		h.DB.Model(&sc).Updates(map[string]interface{}{"price_discussed": req.FinalPrice, "pending_order_status": "approved"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
