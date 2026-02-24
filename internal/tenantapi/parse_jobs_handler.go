package tenantapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
)

// getParseJobStatus handles GET /tenant/products/parse/jobs/{job_id}
func (h *Handler) getParseJobStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.TenantFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	jobID := chi.URLParam(r, "job_id")
	if jobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}
	if h.JobQueue == nil || h.JobQueue.GetRedisClient() == nil {
		http.Error(w, "job queue not available", http.StatusServiceUnavailable)
		return
	}
	key := "parse:job:" + jobID
	rv, err := h.JobQueue.GetRedisClient().Get(r.Context(), key).Result()
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"job_id": jobID, "status": rv})
}
