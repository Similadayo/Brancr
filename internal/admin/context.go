package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/logger"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminContext carries only the dependencies the admin layer needs.
type AdminContext struct {
	Config         *config.Config
	DB             *gorm.DB
	Redis          *redis.Client
	Logger         *zap.Logger
	LogBroadcaster *logger.LogBroadcaster

	AdminEmail       string
	AdminPassword    string
	Events           *EventBroker
	ProductionBuffer interface {
		UpdateMaxAICallsPerMin(newLimit int)
	} // Production message buffer for cost control updates
	AlertService interface {
		SendAlert(ctx context.Context, tenantID uuid.UUID, alertType, severity, title, message, actionURL, actionLabel string) (*storage.TenantAlert, error)
	}
	NewsletterService interface {
		CreateNewsletter(ctx context.Context, title, subject, content string, categories []string, createdBy string, scheduledAt *time.Time) (*storage.Newsletter, error)
		SendNewsletter(ctx context.Context, newsletterID uuid.UUID) error
		SubscribeTenant(ctx context.Context, tenantID uuid.UUID, email string, categories []string) error
		UnsubscribeTenant(ctx context.Context, token string) error
	}
	UsageSyncService interface {
		SyncAppUsage(ctx context.Context, tenantID uuid.UUID, appID string, fromDate, toDate string) error
		SyncAppDiscount(ctx context.Context, tenantID uuid.UUID, appID string, year, month int) error
		GetActualWhatsAppCost(ctx context.Context, tenantID uuid.UUID, date time.Time) (float64, bool)
	} // Gupshup usage sync service for cost control
	CheckpointService *checkpoint.Service
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
