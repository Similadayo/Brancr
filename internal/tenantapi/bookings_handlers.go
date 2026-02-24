package tenantapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"go.uber.org/zap"
)

// createBooking handles POST /tenant/bookings
func (h *Handler) createBooking(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ServiceID  uuid.UUID `json:"service_id"`
		CustomerID uuid.UUID `json:"customer_id,omitempty"`
		StartAt    string    `json:"start_at"` // ISO8601 string
		EndAt      string    `json:"end_at,omitempty"`
		Duration   string    `json:"duration,omitempty"`
		Notes      string    `json:"notes,omitempty"`
		Platform   string    `json:"platform,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.Logger.Warn("create-booking-invalid-body", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ServiceID == uuid.Nil || req.StartAt == "" {
		http.Error(w, "service_id and start_at are required", http.StatusBadRequest)
		return
	}
	startAt, err := time.Parse(time.RFC3339, req.StartAt)
	if err != nil {
		h.Logger.Warn("create-booking-invalid-start", zap.Error(err))
		http.Error(w, "invalid start_at", http.StatusBadRequest)
		return
	}
	var endAt time.Time
	if req.EndAt != "" {
		endAt, err = time.Parse(time.RFC3339, req.EndAt)
		if err != nil {
			h.Logger.Warn("create-booking-invalid-end", zap.Error(err))
			http.Error(w, "invalid end_at", http.StatusBadRequest)
			return
		}
	}

	b := storage.Booking{
		TenantID:   claims.TenantID,
		ServiceID:  req.ServiceID,
		CustomerID: req.CustomerID,
		StartAt:    startAt,
		EndAt:      endAt,
		Duration:   req.Duration,
		Status:     "requested",
		Platform:   req.Platform,
		Notes:      req.Notes,
	}
	repo := repositories.NewBookingRepo(h.DB)
	if err := repo.Create(r.Context(), &b); err != nil {
		h.Logger.Error("create-booking-failed", zap.Error(err))
		http.Error(w, "failed to create booking", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "booking_id": b.ID})
}

// listBookings handles GET /tenant/bookings
func (h *Handler) listBookings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo := repositories.NewBookingRepo(h.DB)
	bookings, err := repo.ListByTenant(r.Context(), claims.TenantID)
	if err != nil {
		h.Logger.Error("list-bookings-failed", zap.Error(err))
		http.Error(w, "failed to list bookings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": bookings})
}

// confirmBooking handles POST /tenant/bookings/{id}/confirm
func (h *Handler) confirmBooking(w http.ResponseWriter, r *http.Request) {
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
	repo := repositories.NewBookingRepo(h.DB)
	b, err := repo.GetByID(r.Context(), id)
	if err != nil {
		h.Logger.Error("booking-not-found", zap.Error(err), zap.Stringer("booking_id", id))
		http.Error(w, "booking not found", http.StatusNotFound)
		return
	}
	if b.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := repo.UpdateStatus(r.Context(), b.ID, "confirmed"); err != nil {
		h.Logger.Error("confirm-booking-failed", zap.Error(err))
		http.Error(w, "failed to confirm booking", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// cancelBooking handles POST /tenant/bookings/{id}/cancel
func (h *Handler) cancelBooking(w http.ResponseWriter, r *http.Request) {
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
	repo := repositories.NewBookingRepo(h.DB)
	b, err := repo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "booking not found", http.StatusNotFound)
		return
	}
	if b.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := repo.UpdateStatus(r.Context(), b.ID, "cancelled"); err != nil {
		http.Error(w, "failed to cancel booking", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
