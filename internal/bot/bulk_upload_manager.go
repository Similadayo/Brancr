package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BulkUploadManager handles bulk media uploads and organization
type BulkUploadManager struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewBulkUploadManager creates a new bulk upload manager
func NewBulkUploadManager(db *gorm.DB, logger *zap.Logger) *BulkUploadManager {
	return &BulkUploadManager{
		DB:     db,
		Logger: logger,
	}
}

// DetectBulkUpload checks if a media session contains enough items to be considered bulk
func (m *BulkUploadManager) DetectBulkUpload(tenantID uint, sessionID string) (bool, int, error) {
	var items []storage.MediaItem
	if err := m.DB.Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Find(&items).Error; err != nil {
		m.Logger.Error("failed to detect bulk upload",
			zap.Uint("tenant_id", tenantID),
			zap.String("session_id", sessionID),
			zap.Error(err))
		return false, 0, err
	}

	count := len(items)
	m.Logger.Info("🔍 Bulk upload detection",
		zap.Uint("tenant_id", tenantID),
		zap.String("session_id", sessionID),
		zap.Int("item_count", count))
	
	// Consider 10+ items as bulk upload
	return count >= 10, count, nil
}

// ProcessBulkUpload handles bulk upload organization and splitting
func (m *BulkUploadManager) ProcessBulkUpload(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	sessionID string,
) error {
	// Get all items from session - retry a few times to ensure we have all items
	var items []storage.MediaItem
	maxRetries := 3
	var err error
	
	for i := 0; i < maxRetries; i++ {
		if err = m.DB.Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
			Find(&items).Error; err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	
	if err != nil {
		m.Logger.Error("failed to fetch items after retries",
			zap.Uint("tenant_id", tenantID),
			zap.String("session_id", sessionID),
			zap.Error(err))
		return fmt.Errorf("failed to fetch items: %w", err)
	}

	if len(items) < 10 {
		// Not a bulk upload, handle normally
		m.Logger.Info("not enough items for bulk upload",
			zap.Uint("tenant_id", tenantID),
			zap.Int("item_count", len(items)))
		return nil
	}

	count := len(items)
	m.Logger.Info("📦 Bulk upload detected",
		zap.Uint("tenant_id", tenantID),
		zap.String("session_id", sessionID),
		zap.Int("item_count", count))
	
	// Ensure bot is available
	if bot == nil {
		return fmt.Errorf("bot instance not available for tenant %d", tenantID)
	}

	// Create bulk upload session
	bulkSession := storage.BulkUploadSession{
		TenantID:       tenantID,
		SessionID:      fmt.Sprintf("bulk_%s", sessionID),
		TotalItems:     count,
		ProcessedItems: 0,
		Status:         "pending",
	}

	if err := m.DB.Create(&bulkSession).Error; err != nil {
		m.Logger.Error("failed to create bulk session", zap.Error(err))
		return err
	}

	// Prompt user for organization strategy
	msg := fmt.Sprintf(
		"📦 *Bulk Upload Detected!*\n\n"+
			"I found %d images. How would you like to organize them?\n\n"+
			"1️⃣ Create carousels (split into groups)\n"+
			"2️⃣ Create individual posts\n"+
			"3️⃣ Custom grouping\n"+
			"4️⃣ Let AI organize them",
		count,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1️⃣ Carousels", fmt.Sprintf("bulk:carousels:%s", bulkSession.SessionID)),
			tgbotapi.NewInlineKeyboardButtonData("2️⃣ Individual", fmt.Sprintf("bulk:individual:%s", bulkSession.SessionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("3️⃣ Custom", fmt.Sprintf("bulk:custom:%s", bulkSession.SessionID)),
			tgbotapi.NewInlineKeyboardButtonData("4️⃣ AI Organize", fmt.Sprintf("bulk:ai:%s", bulkSession.SessionID)),
		),
	)

	reply := tgbotapi.NewMessage(chatID, msg)
	reply.ParseMode = "Markdown"
	reply.ReplyMarkup = keyboard
	bot.Send(reply)

	return nil
}

// SplitIntoCarousels splits media items into multiple carousels
func (m *BulkUploadManager) SplitIntoCarousels(
	ctx context.Context,
	tenantID uint,
	items []storage.MediaItem,
	carouselCount int,
	bulkSessionID string,
) ([]storage.MediaAsset, error) {
	if carouselCount <= 0 {
		// Auto-calculate: aim for 10-15 items per carousel
		carouselCount = int(math.Ceil(float64(len(items)) / 12.0))
		if carouselCount < 1 {
			carouselCount = 1
		}
	}

	itemsPerCarousel := int(math.Ceil(float64(len(items)) / float64(carouselCount)))
	if itemsPerCarousel < 2 {
		itemsPerCarousel = 2 // Minimum 2 items per carousel
	}

	var assets []storage.MediaAsset
	for i := 0; i < carouselCount; i++ {
		start := i * itemsPerCarousel
		end := start + itemsPerCarousel
		if end > len(items) {
			end = len(items)
		}
		if start >= len(items) {
			break
		}

		group := items[start:end]
		var urls []string
		for _, item := range group {
			urls = append(urls, item.URL)
		}

		urlsJSON, _ := json.Marshal(urls)
		asset := storage.MediaAsset{
			TenantID:     tenantID,
			Type:         "carousel",
			Name:         fmt.Sprintf("Bulk Upload - Carousel %d", i+1),
			CarouselName: fmt.Sprintf("bulk_%s_carousel_%d", bulkSessionID, i+1),
			URLs:         string(urlsJSON),
			Status:       "uploaded",
			Platforms:    "[]",
			BulkUploadID: bulkSessionID,
			Metadata:     "{}", // Initialize as empty JSON object (required for JSONB column)
		}

		if err := m.DB.Create(&asset).Error; err != nil {
			m.Logger.Error("failed to create carousel asset",
				zap.Error(err),
				zap.Int("carousel_index", i))
			continue
		}

		assets = append(assets, asset)
	}

	m.Logger.Info("✅ Split into carousels",
		zap.Uint("tenant_id", tenantID),
		zap.Int("total_items", len(items)),
		zap.Int("carousel_count", len(assets)),
		zap.Int("items_per_carousel", itemsPerCarousel))

	return assets, nil
}

// HandleBulkCarouselSelection handles user's choice for carousel count
func (m *BulkUploadManager) HandleBulkCarouselSelection(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	bulkSessionID string,
	carouselCount int,
) error {
	// Get bulk session
	var bulkSession storage.BulkUploadSession
	if err := m.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		return fmt.Errorf("bulk session not found: %w", err)
	}

	// Get original session ID from bulk session
	originalSessionID := strings.TrimPrefix(bulkSession.SessionID, "bulk_")

	// Get all items
	var items []storage.MediaItem
	if err := m.DB.Where("tenant_id = ? AND session_id = ?", tenantID, originalSessionID).
		Find(&items).Error; err != nil {
		return fmt.Errorf("failed to fetch items: %w", err)
	}

	// Split into carousels
	assets, err := m.SplitIntoCarousels(ctx, tenantID, items, carouselCount, bulkSessionID)
	if err != nil {
		return err
	}

	// Update bulk session
	bulkSession.Status = "completed"
	bulkSession.ProcessedItems = len(items)
	bulkSession.CarouselCount = len(assets)
	bulkSession.ItemsPerCarousel = len(items) / len(assets)
	m.DB.Save(&bulkSession)

	// Prompt for scheduling
	msg := fmt.Sprintf(
		"✅ Created %d carousels from %d images!\n\n"+
			"When should I schedule them?\n\n"+
			"Option A: Spread over a month (one per week)\n"+
			"Option B: Custom dates\n"+
			"Option C: Optimal times (AI-suggested)",
		len(assets),
		len(items),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("A: Spread", fmt.Sprintf("bulk:schedule:spread:%s", bulkSessionID)),
			tgbotapi.NewInlineKeyboardButtonData("B: Custom", fmt.Sprintf("bulk:schedule:custom:%s", bulkSessionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("C: Optimal", fmt.Sprintf("bulk:schedule:optimal:%s", bulkSessionID)),
		),
	)

	reply := tgbotapi.NewMessage(chatID, msg)
	reply.ReplyMarkup = keyboard
	bot.Send(reply)

	return nil
}

// AutoScheduleCarousels automatically schedules carousels based on strategy
func (m *BulkUploadManager) AutoScheduleCarousels(
	ctx context.Context,
	tenantID uint,
	assets []storage.MediaAsset,
	strategy string, // "spread", "custom", "optimal"
	platforms []string,
) error {
	if len(assets) == 0 {
		return fmt.Errorf("no assets to schedule")
	}

	// Get tenant timezone
	var tenant storage.Tenant
	if err := m.DB.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	timezone := tenant.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	var scheduledTimes []time.Time

	switch strategy {
	case "spread":
		// Spread over a month (one per week)
		weeks := len(assets)
		if weeks > 4 {
			weeks = 4 // Max 4 weeks
		}
		for i := 0; i < len(assets); i++ {
			weekOffset := i % weeks
			scheduledTime := now.AddDate(0, 0, weekOffset*7)
			// Set to 10am in tenant's timezone
			scheduledTime = time.Date(
				scheduledTime.Year(),
				scheduledTime.Month(),
				scheduledTime.Day(),
				10, 0, 0, 0,
				loc,
			)
			scheduledTimes = append(scheduledTimes, scheduledTime)
		}

	case "optimal":
		// Use AI to suggest optimal times (for now, use spread as fallback)
		// TODO: Integrate with OptimalTimeAnalyzer
		return m.AutoScheduleCarousels(ctx, tenantID, assets, "spread", platforms)

	default:
		// Default: spread over next 7 days
		for i := 0; i < len(assets); i++ {
			scheduledTime := now.AddDate(0, 0, i)
			scheduledTime = time.Date(
				scheduledTime.Year(),
				scheduledTime.Month(),
				scheduledTime.Day(),
				10, 0, 0, 0,
				loc,
			)
			scheduledTimes = append(scheduledTimes, scheduledTime)
		}
	}

	// Create scheduled posts
	platformsJSON, _ := json.Marshal(platforms)
	for i, asset := range assets {
		if i >= len(scheduledTimes) {
			break
		}

		assetIDsJSON, _ := json.Marshal([]uint{asset.ID})
		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          asset.Name,
			Caption:       asset.Caption,
			MediaAssetIDs: string(assetIDsJSON),
			Platforms:     string(platformsJSON),
			ScheduledAt:   &scheduledTimes[i],
			Status:        "scheduled",
			Timezone:      timezone,
			EnhanceCaption: false, // Bulk uploads use captions as-is by default
		}

		if err := m.DB.Create(&sp).Error; err != nil {
			m.Logger.Error("failed to create scheduled post",
				zap.Error(err),
				zap.Uint("asset_id", asset.ID))
			continue
		}
	}

	m.Logger.Info("✅ Auto-scheduled carousels",
		zap.Uint("tenant_id", tenantID),
		zap.Int("count", len(assets)),
		zap.String("strategy", strategy))

	return nil
}

