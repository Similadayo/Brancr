package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// TagTenantRequest represents a request to add/remove tags from a tenant
type TagTenantRequest struct {
	Tags []string `json:"tags"` // Tags to set (replaces existing tags)
}

// TagTenantHandler handles adding/removing tags from tenants
// Permission: tenant.tag (SupportAdmin/Growth & CS only)
func TagTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check permission
		role := AdminRoleFromRequest(r)
		if role != RoleSuperAdmin && role != RoleSupportAdmin {
			http.Error(w, "forbidden: only growth/cs can tag tenants", http.StatusForbidden)
			return
		}

		tenantID, err := parseTenantIDParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req TagTenantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate tags (only allow predefined tags)
		allowedTags := map[string]bool{
			"at_risk":        true,
			"likely_to_pay":  true,
			"needs_followup": true,
			"high_cost":      true,
			"misconfigured":  true,
			"promising":      true,
			"churned":        true,
		}

		validatedTags := []string{}
		for _, tag := range req.Tags {
			if allowedTags[tag] {
				validatedTags = append(validatedTags, tag)
			} else {
				ctx.Logger.Warn("invalid tag attempted", zap.String("tag", tag), zap.Stringer("tenant_id", tenantID))
			}
		}

		// Update tenant tags
		// Note: This assumes AdminTags is stored as pq.StringArray in the database
		// GORM should handle the conversion automatically
		if err := ctx.DB.WithContext(r.Context()).
			Model(&storage.Tenant{}).
			Where("id = ?", tenantID).
			Update("admin_tags", validatedTags).Error; err != nil {
			ctx.Logger.Error("failed to update tenant tags", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update tags", http.StatusInternalServerError)
			return
		}

		// Log audit event
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "tenant.tags_updated",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"tags":      validatedTags,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status": "ok",
			"tags":   validatedTags,
		})
	}
}

func parseTenantIDParam(r *http.Request) (uuid.UUID, error) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid tenant id")
	}
	return id, nil
}
