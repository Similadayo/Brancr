package tenantapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/middleware"
	"go.uber.org/zap"
)

// getPendingCheckpoints retrieves any active checkpoint for the tenant
func (h *Handler) getPendingCheckpoints(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	if h.CheckpointService == nil {
		h.writeInternalError(w, r, nil)
		return
	}

	checkpoints, err := h.CheckpointService.GetPendingCheckpoints(r.Context(), claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to get pending checkpoints", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	// Enrich with question text
	type enrichedCheckpoint struct {
		ID        uuid.UUID `json:"id"`
		EventType string    `json:"event_type"`
		Question  string    `json:"question"`
		Options   []string  `json:"options"`
		CreatedAt time.Time `json:"created_at"`
	}

	enriched := make([]enrichedCheckpoint, 0, len(checkpoints))
	for _, cp := range checkpoints {
		q := h.CheckpointService.GetQuestionsForEventType(cp.EventType)
		enriched = append(enriched, enrichedCheckpoint{
			ID:        cp.ID,
			EventType: cp.EventType,
			Question:  q.Question,
			Options:   q.Options,
			CreatedAt: cp.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"checkpoints": enriched})
}

// submitCheckpointResponse processes a user's response to a checkpoint
func (h *Handler) submitCheckpointResponse(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	checkpointIDStr := chi.URLParam(r, "id")
	checkpointID, err := uuid.Parse(checkpointIDStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid checkpoint id", nil)
		return
	}

	var input checkpoint.CheckpointResponseInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		h.writeValidationError(w, r, "invalid body", nil)
		return
	}

	if h.CheckpointService == nil {
		h.writeInternalError(w, r, nil)
		return
	}

	err = h.CheckpointService.SubmitResponse(r.Context(), claims.TenantID, checkpointID, &input)
	if err != nil {
		h.Logger.Error("failed to submit checkpoint response", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// dismissCheckpoint removes a checkpoint without answering
func (h *Handler) dismissCheckpoint(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	checkpointIDStr := chi.URLParam(r, "id")
	checkpointID, err := uuid.Parse(checkpointIDStr)
	if err != nil {
		h.writeValidationError(w, r, "invalid checkpoint id", nil)
		return
	}

	if h.CheckpointService == nil {
		h.writeInternalError(w, r, nil)
		return
	}

	err = h.CheckpointService.DismissCheckpoint(r.Context(), claims.TenantID, checkpointID)
	if err != nil {
		h.Logger.Error("failed to dismiss checkpoint", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
