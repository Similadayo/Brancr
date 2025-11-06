package admin

import (
	"encoding/json"

	"github.com/similadayo/brancr/internal/storage"
)

// WriteAdminLog stores structured logs in DB. meta may be any struct (it will be marshaled)
func WriteAdminLog(ctx *AdminContext, level, source, message string, meta interface{}) {
	metaS := ""
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			metaS = string(b)
		}
	}

	entry := storage.LogEntry{
		Level:   level,
		Source:  source,
		Message: message,
		Meta:    metaS,
		// CreatedAt will be auto
	}
	// best-effort; don't fail admin flow on DB error
	_ = ctx.DB.Create(&entry).Error
}
