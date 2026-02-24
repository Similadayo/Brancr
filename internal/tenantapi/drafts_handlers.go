package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// Draft payloads

// @Summary      Create draft
// @Description  Create a draft scoped to the tenant
// @Tags         drafts
// @Accept       json
// @Produce      json
// @Param        payload  body     object  true  "Draft payload"
// @Success      201 {object} object
// @Security     BearerAuth
// @Router       /tenant/drafts [post]
func (h *Handler) createDraft(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Guard: ensure migrations have created the drafts table
	if h.DB != nil && !h.DB.Migrator().HasTable(&storage.Draft{}) {
		h.Logger.Warn("Drafts table missing - refusing request", zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "server misconfiguration: drafts table missing; run migrations", http.StatusInternalServerError)
		return
	}

	var req struct {
		Key      string          `json:"key"`
		Content  json.RawMessage `json:"content"`
		Metadata json.RawMessage `json:"metadata"`
		OwnerID  *uuid.UUID      `json:"owner_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	// Upsert behavior: if a draft exists for this tenant/key (and owner if provided), update it.
	var existing storage.Draft
	q := h.DB.Where("tenant_id = ? AND key = ?", claims.TenantID, req.Key)
	if req.OwnerID != nil {
		q = q.Where("owner_id = ?", req.OwnerID)
	}
	err := q.First(&existing).Error
	if err == nil {
		// update existing draft
		if len(req.Content) > 0 {
			existing.Content = string(req.Content)
		}
		if len(req.Metadata) > 0 {
			existing.Metadata = string(req.Metadata)
		} else {
			// Ensure metadata is valid JSON if not provided
			existing.Metadata = "{}"
		}
		if err := h.DB.Save(&existing).Error; err != nil {
			h.Logger.Error("failed to update existing draft", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID), zap.String("key", req.Key))
			http.Error(w, "failed to save draft", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, existing)
		return
	}

	// create new draft
	d := storage.Draft{
		TenantID: claims.TenantID,
		OwnerID:  req.OwnerID,
		Key:      req.Key,
	}
	if len(req.Content) > 0 {
		d.Content = string(req.Content)
	} else {
		d.Content = "{}"
	}
	if len(req.Metadata) > 0 {
		d.Metadata = string(req.Metadata)
	} else {
		d.Metadata = "{}"
	}
	if err := h.DB.Create(&d).Error; err != nil {
		h.Logger.Error("failed to create draft", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID), zap.String("key", req.Key))
		// If the underlying cause is missing table, return friendly error
		if h.DB != nil && !h.DB.Migrator().HasTable(&storage.Draft{}) {
			http.Error(w, "server misconfiguration: drafts table missing; run migrations", http.StatusInternalServerError)
			return
		}
		http.Error(w, "failed to create draft", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// @Summary      Update draft
// @Description  Update draft content or metadata
// @Tags         drafts
// @Accept       json
// @Produce      json
// @Param        id       path    int     true  "Draft ID"
// @Param        payload  body    object  true  "Draft update payload"
// @Success      200 {object} object
// @Security     BearerAuth
// @Router       /tenant/drafts/{id} [put]
func (h *Handler) updateDraft(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	id64, _ := uuid.Parse(idStr)
	if id64 == uuid.Nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var d storage.Draft
	if err := h.DB.First(&d, id64).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if d.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Content  json.RawMessage `json:"content"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Content) > 0 {
		d.Content = string(req.Content)
	}
	if len(req.Metadata) > 0 {
		d.Metadata = string(req.Metadata)
	}
	if err := h.DB.Save(&d).Error; err != nil {
		h.Logger.Error("failed to update draft", zap.Error(err))
		http.Error(w, "failed to update draft", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// @Summary      Get draft
// @Description  Retrieve a draft by ID (tenant scoped)
// @Tags         drafts
// @Produce      json
// @Param        id   path  int  true  "Draft ID"
// @Success      200 {object} object
// @Security     BearerAuth
// @Router       /tenant/drafts/{id} [get]
func (h *Handler) getDraft(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	id64, _ := uuid.Parse(idStr)
	if id64 == uuid.Nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Guard: ensure drafts table exists
	if h.DB != nil && !h.DB.Migrator().HasTable(&storage.Draft{}) {
		h.Logger.Warn("Drafts table missing - getDraft refused", zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "server misconfiguration: drafts table missing; run migrations", http.StatusInternalServerError)
		return
	}
	var d storage.Draft
	if err := h.DB.First(&d, id64).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if d.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// @Summary      List drafts
// @Description  List drafts for tenant. Optional ?key= to filter by flow key
// @Tags         drafts
// @Produce      json
// @Param        key   query  string  false  "Draft key"
// @Success      200 {array} object
// @Security     BearerAuth
// @Router       /tenant/drafts [get]
func (h *Handler) listDrafts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Guard: ensure drafts table exists
	if h.DB != nil && !h.DB.Migrator().HasTable(&storage.Draft{}) {
		h.Logger.Warn("Drafts table missing - listDrafts refused", zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "server misconfiguration: drafts table missing; run migrations", http.StatusInternalServerError)
		return
	}

	key := r.URL.Query().Get("key")
	var ds []storage.Draft
	q := h.DB.Where("tenant_id = ?", claims.TenantID)
	if key != "" {
		q = q.Where("key = ?", key)
	}
	if err := q.Order("updated_at DESC").Find(&ds).Error; err != nil {
		h.Logger.Error("failed to list drafts", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "failed to list drafts", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

// @Summary      Delete draft
// @Description  Delete a tenant draft by ID
// @Tags         drafts
// @Param        id   path  int  true  "Draft ID"
// @Success      200 {object} map[string]bool
// @Security     BearerAuth
// @Router       /tenant/drafts/{id} [delete]
func (h *Handler) deleteDraft(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	id64, _ := uuid.Parse(idStr)
	if id64 == uuid.Nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var d storage.Draft
	if err := h.DB.First(&d, id64).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if d.TenantID != claims.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.DB.Delete(&d).Error; err != nil {
		http.Error(w, "failed to delete draft", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
