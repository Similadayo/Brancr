package admin

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// WriteAdminLog stores structured logs in DB. meta may be any struct (it will be marshaled)
// tenantID is optional - pass nil for system-wide logs
func WriteAdminLog(ctx *AdminContext, level, source, message string, meta interface{}, tenantID *uuid.UUID) {
	metaS := ""
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			metaS = string(b)
		}
	}

	entry := storage.LogEntry{
		TenantID: tenantID,
		Level:    level,
		Source:   source,
		Message:  message,
		Meta:     metaS,
		// CreatedAt will be auto
	}
	// best-effort; don't fail admin flow on DB error
	_ = ctx.DB.Create(&entry).Error
}
