package admin

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/similadayo/brancr/internal/storage"
)

// AdminAuditEntry is the minimal, structured representation of an audit event.
type AdminAuditEntry struct {
	AdminEmail string
	AdminRole  string
	Action     string
	TenantID   *uint
	CaseID     *uint
	Metadata   map[string]interface{}
}

// auditWriter is a hookable function used by handlers/tests. It defaults to WriteAdminAuditOrFail.
var auditWriter = WriteAdminAuditOrFail

// WriteAdminAuditOrFail writes an admin audit to the DB synchronously and returns an error on failure.
// It does not panic and does not swallow errors.
func WriteAdminAuditOrFail(ctx context.Context, ac *AdminContext, entry AdminAuditEntry) error {
	// Prepare metadata JSON
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
		log.ResourceID = strconvFormatUintPtr(entry.CaseID)
	}
	if entry.TenantID != nil {
		// If no resource set explicitly, mark as tenant
		if log.Resource == "admin" {
			log.Resource = "tenant"
			log.ResourceID = strconvFormatUintPtr(entry.TenantID)
		}
	}

	// Perform synchronous DB insert
	if err := ac.DB.WithContext(ctx).Create(&log).Error; err != nil {
		return err
	}
	return nil
}

func strconvFormatUintPtr(p *uint) string {
	if p == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*p), 10)
}
