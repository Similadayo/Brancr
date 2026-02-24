package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// GupshupUsageSyncService handles syncing usage data from Gupshup Partner API
type GupshupUsageSyncService struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	GupshupClient *social.GupshupClient
}

// NewGupshupUsageSyncService creates a new usage sync service
func NewGupshupUsageSyncService(db *gorm.DB, logger *zap.Logger, gupshupClient *social.GupshupClient) *GupshupUsageSyncService {
	return &GupshupUsageSyncService{
		DB:            db,
		Logger:        logger,
		GupshupClient: gupshupClient,
	}
}

// SyncAppUsage syncs usage data for a specific app within a date range
// Fetches from Gupshup API and stores in database
func (s *GupshupUsageSyncService) SyncAppUsage(ctx context.Context, tenantID uuid.UUID, appID string, fromDate, toDate string) error {
	if s.GupshupClient == nil {
		return fmt.Errorf("gupshup client not initialized")
	}

	// Fetch usage data from Gupshup
	usageList, err := s.GupshupClient.GetAppUsage(appID, fromDate, toDate)
	if err != nil {
		return fmt.Errorf("failed to fetch usage from Gupshup: %w", err)
	}

	if len(usageList) == 0 {
		s.Logger.Info("No usage data found for date range",
			zap.String("app_id", appID),
			zap.String("from_date", fromDate),
			zap.String("to_date", toDate))
		return nil
	}

	// Store each daily usage record
	savedCount := 0
	skippedCount := 0
	for _, usage := range usageList {
		// Check if record already exists
		var existing storage.GupshupUsage
		err := s.DB.Where("gupshup_app_id = ? AND date = ?", appID, usage.Date).
			First(&existing).Error

		if err == nil {
			// Update existing record
			existing.Authentication = usage.Authentication
			existing.CumulativeBill = usage.CumulativeBill
			existing.Currency = usage.Currency
			existing.Discount = usage.Discount
			existing.FEP = usage.FEP
			existing.FTC = usage.FTC
			existing.GSCap = usage.GSCap
			existing.GSFees = usage.GSFees
			existing.IncomingMsg = usage.IncomingMsg
			existing.OutgoingMsg = usage.OutgoingMsg
			existing.OutgoingMediaMsg = usage.OutgoingMediaMsg
			existing.Marketing = usage.Marketing
			existing.Service = usage.Service
			existing.TemplateMsg = usage.TemplateMsg
			existing.TemplateMediaMsg = usage.TemplateMediaMsg
			existing.TotalFees = usage.TotalFees
			existing.TotalMsg = usage.TotalMsg
			existing.Utility = usage.Utility
			existing.WAFees = usage.WAFees
			existing.InternationalAuthentication = usage.InternationalAuthentication

			if err := s.DB.Save(&existing).Error; err != nil {
				s.Logger.Warn("Failed to update existing usage record",
					zap.Error(err),
					zap.String("app_id", appID),
					zap.String("date", usage.Date))
				continue
			}
			savedCount++
		} else if err == gorm.ErrRecordNotFound {
			// Create new record
			newUsage := storage.GupshupUsage{
				TenantID:                    tenantID,
				GupshupAppID:                appID,
				Date:                        usage.Date,
				Authentication:              usage.Authentication,
				CumulativeBill:              usage.CumulativeBill,
				Currency:                    usage.Currency,
				Discount:                    usage.Discount,
				FEP:                         usage.FEP,
				FTC:                         usage.FTC,
				GSCap:                       usage.GSCap,
				GSFees:                      usage.GSFees,
				IncomingMsg:                 usage.IncomingMsg,
				OutgoingMsg:                 usage.OutgoingMsg,
				OutgoingMediaMsg:            usage.OutgoingMediaMsg,
				Marketing:                   usage.Marketing,
				Service:                     usage.Service,
				TemplateMsg:                 usage.TemplateMsg,
				TemplateMediaMsg:            usage.TemplateMediaMsg,
				TotalFees:                   usage.TotalFees,
				TotalMsg:                    usage.TotalMsg,
				Utility:                     usage.Utility,
				WAFees:                      usage.WAFees,
				InternationalAuthentication: usage.InternationalAuthentication,
			}

			if err := s.DB.Create(&newUsage).Error; err != nil {
				s.Logger.Warn("Failed to create usage record",
					zap.Error(err),
					zap.String("app_id", appID),
					zap.String("date", usage.Date))
				continue
			}
			savedCount++
		} else {
			s.Logger.Warn("Error checking for existing usage record",
				zap.Error(err),
				zap.String("app_id", appID),
				zap.String("date", usage.Date))
			skippedCount++
		}
	}

	s.Logger.Info("✅ Synced Gupshup usage data",
		zap.String("app_id", appID),
		zap.String("from_date", fromDate),
		zap.String("to_date", toDate),
		zap.Int("total_records", len(usageList)),
		zap.Int("saved", savedCount),
		zap.Int("skipped", skippedCount))

	return nil
}

// SyncAppDiscount syncs discount data for a specific app for a given month
func (s *GupshupUsageSyncService) SyncAppDiscount(ctx context.Context, tenantID uuid.UUID, appID string, year, month int) error {
	if s.GupshupClient == nil {
		return fmt.Errorf("gupshup client not initialized")
	}

	// Fetch discount data from Gupshup
	discountList, err := s.GupshupClient.GetAppDiscount(appID, year, month)
	if err != nil {
		return fmt.Errorf("failed to fetch discount from Gupshup: %w", err)
	}

	if len(discountList) == 0 {
		s.Logger.Info("No discount data found for month",
			zap.String("app_id", appID),
			zap.Int("year", year),
			zap.Int("month", month))
		return nil
	}

	// Store each daily discount record
	savedCount := 0
	skippedCount := 0
	for _, discount := range discountList {
		// Check if record already exists
		var existing storage.GupshupDiscount
		err := s.DB.Where("gupshup_app_id = ? AND year = ? AND month = ? AND day = ?",
			appID, discount.Year, discount.Month, discount.Day).
			First(&existing).Error

		if err == nil {
			// Update existing record
			existing.CumulativeBill = discount.CumulativeBill
			existing.DailyBill = discount.DailyBill
			existing.Discount = discount.Discount
			existing.GSCap = discount.GSCap
			existing.GSFees = discount.GSFees
			existing.PartnerID = discount.PartnerID

			if err := s.DB.Save(&existing).Error; err != nil {
				s.Logger.Warn("Failed to update existing discount record",
					zap.Error(err),
					zap.String("app_id", appID),
					zap.Int("year", discount.Year),
					zap.Int("month", discount.Month),
					zap.Int("day", discount.Day))
				continue
			}
			savedCount++
		} else if err == gorm.ErrRecordNotFound {
			// Create new record
			newDiscount := storage.GupshupDiscount{
				TenantID:       tenantID,
				GupshupAppID:   appID,
				Year:           discount.Year,
				Month:          discount.Month,
				Day:            discount.Day,
				CumulativeBill: discount.CumulativeBill,
				DailyBill:      discount.DailyBill,
				Discount:       discount.Discount,
				GSCap:          discount.GSCap,
				GSFees:         discount.GSFees,
				PartnerID:      discount.PartnerID,
			}

			if err := s.DB.Create(&newDiscount).Error; err != nil {
				s.Logger.Warn("Failed to create discount record",
					zap.Error(err),
					zap.String("app_id", appID),
					zap.Int("year", discount.Year),
					zap.Int("month", discount.Month),
					zap.Int("day", discount.Day))
				continue
			}
			savedCount++
		} else {
			s.Logger.Warn("Error checking for existing discount record",
				zap.Error(err),
				zap.String("app_id", appID),
				zap.Int("year", discount.Year),
				zap.Int("month", discount.Month),
				zap.Int("day", discount.Day))
			skippedCount++
		}
	}

	s.Logger.Info("✅ Synced Gupshup discount data",
		zap.String("app_id", appID),
		zap.Int("year", year),
		zap.Int("month", month),
		zap.Int("total_records", len(discountList)),
		zap.Int("saved", savedCount),
		zap.Int("skipped", skippedCount))

	return nil
}

// SyncAllAppsUsage syncs usage for all active WhatsApp apps
// Typically called daily via cron job
func (s *GupshupUsageSyncService) SyncAllAppsUsage(ctx context.Context, daysBack int) error {
	if daysBack <= 0 {
		daysBack = 1 // Default: sync yesterday's data
	}

	// Calculate date range
	toDate := time.Now().AddDate(0, 0, -1) // Yesterday
	fromDate := toDate.AddDate(0, 0, -daysBack+1)

	fromDateStr := fromDate.Format("2006-01-02")
	toDateStr := toDate.Format("2006-01-02")

	// Get all active WhatsApp apps
	var apps []storage.WhatsAppApp
	if err := s.DB.Where("status = ?", "live").Find(&apps).Error; err != nil {
		return fmt.Errorf("failed to fetch active apps: %w", err)
	}

	if len(apps) == 0 {
		s.Logger.Info("No active WhatsApp apps found for usage sync")
		return nil
	}

	s.Logger.Info("🔄 Starting usage sync for all apps",
		zap.Int("app_count", len(apps)),
		zap.String("from_date", fromDateStr),
		zap.String("to_date", toDateStr))

	// Sync each app
	successCount := 0
	failureCount := 0
	for _, app := range apps {
		if err := s.SyncAppUsage(ctx, app.TenantID, app.GupshupAppID, fromDateStr, toDateStr); err != nil {
			s.Logger.Error("Failed to sync usage for app",
				zap.Error(err),
				zap.String("app_id", app.GupshupAppID),
				zap.Stringer("tenant_id", app.TenantID))
			failureCount++
		} else {
			successCount++
		}
	}

	s.Logger.Info("✅ Completed usage sync for all apps",
		zap.Int("total_apps", len(apps)),
		zap.Int("success", successCount),
		zap.Int("failures", failureCount))

	return nil
}

// GetActualWhatsAppCost returns the actual cost for WhatsApp messages from Gupshup data
// Falls back to estimated cost if actual data not available
func (s *GupshupUsageSyncService) GetActualWhatsAppCost(ctx context.Context, tenantID uuid.UUID, date time.Time) (float64, bool) {
	dateStr := date.Format("2006-01-02")

	// Find WhatsApp app for this tenant
	var app storage.WhatsAppApp
	if err := s.DB.Where("tenant_id = ? AND status = ?", tenantID, "live").
		First(&app).Error; err != nil {
		// No app found - return false (no actual data)
		return 0, false
	}

	// Get usage for this date
	var usage storage.GupshupUsage
	if err := s.DB.Where("tenant_id = ? AND gupshup_app_id = ? AND date = ?",
		tenantID, app.GupshupAppID, dateStr).
		First(&usage).Error; err != nil {
		// No usage data for this date - return false (no actual data)
		return 0, false
	}

	// Return actual total fees (includes WhatsApp fees + Gupshup fees)
	return usage.TotalFees, true
}
