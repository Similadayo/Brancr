package admin

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
)

// AdminAuditEntry is the minimal, structured representation of an audit event.
type AdminAuditEntry struct {
	AdminEmail string
	AdminRole  string
	Action     string
	TenantID   *uuid.UUID
	CaseID     *uuid.UUID
	Metadata   map[string]interface{}
}

// auditWriter is a hookable function used by handlers/tests. It defaults to WriteAdminAuditOrFail.
var auditWriter = WriteAdminAuditOrFail

// WriteAdminAuditOrFail writes an admin audit to the DB synchronously and returns an error on failure.
// It does not panic and does not swallow errors.
func WriteAdminAuditOrFail(ctx context.Context, ac *AdminContext, entry AdminAuditEntry) error {
	// Prepare metadata JSON
	if entry.Metadata == nil {
		entry.Metadata = map[string]interface{}{}
	}
	if rid := middleware.GetRequestID(ctx); rid != "" {
		// Avoid clobbering if caller set it explicitly.
		if _, exists := entry.Metadata["request_id"]; !exists {
			entry.Metadata["request_id"] = rid
		}
	}
	metaJSON, _ := json.Marshal(entry.Metadata)

	log := storage.AdminAuditLog{
		AdminEmail: entry.AdminEmail,
		AdminRole:  entry.AdminRole,
		Action:     entry.Action,
		TenantID:   entry.TenantID,
		Resource:   "admin", // callers may include specific resource in Metadata
		ResourceID: "",
		Success:    true,
		StatusCode: 200,
		Metadata:   string(metaJSON),
	}

	if entry.CaseID != nil {
		log.Resource = "admin_case"
		log.ResourceID = entry.CaseID.String()
	}
	if entry.TenantID != nil {
		// If no resource set explicitly, mark as tenant
		if log.Resource == "admin" {
			log.Resource = "tenant"
			log.ResourceID = entry.TenantID.String()
		}
	}

	// Perform synchronous DB insert
	if err := ac.DB.WithContext(ctx).Create(&log).Error; err != nil {
		return err
	}
	return nil
}
