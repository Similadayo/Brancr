package admin

import (
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminContext carries only the dependencies the admin layer needs.
type AdminContext struct {
	Config *config.Config
	DB     *gorm.DB
	Redis  *redis.Client
	Logger *zap.Logger

	AdminEmail    string
	AdminPassword string
	Events        *EventBroker
}

// --- Admin Case Context Extension ---
type adminCaseKeyType string

const adminCaseKey adminCaseKeyType = "admin_case"

// AdminCaseFromRequest extracts the full AdminCase struct from request context (nil if not present)
func AdminCaseFromRequest(r *http.Request) *storage.AdminCase {
	v := r.Context().Value(adminCaseKey)
	if v == nil {
		return nil
	}
	if ac, ok := v.(*storage.AdminCase); ok {
		return ac
	}
	return nil
}
