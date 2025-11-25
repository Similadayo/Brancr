package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/admin"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// HandleMediaUpdate handles photos/videos uploaded by tenant bots
func (m *TenantBotManager) HandleMediaUpdate(bot *tgbotapi.BotAPI, tenantID uint, upd tgbotapi.Update) {
	ctx := context.Background()
	msg := upd.Message
	if msg == nil {
		return
	}

	if m.Logger == nil {
		l, _ := zap.NewDevelopment()
		m.Logger = l
	}
	logger := m.Logger.With(zap.Uint("tenant_id", tenantID))

	// Send immediate acknowledgment
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "🤔 Got it! Give me a second to prep your upload."))

	cloudURL := os.Getenv("CLOUDINARY_URL")
	if cloudURL == "" {
		logger.Error("CLOUDINARY_URL not set")
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Media storage is not configured. Please contact support."))
		return
	}

	logger.Info("🖼️ HandleMediaUpdate started",
		zap.Int("photo_count", len(msg.Photo)),
		zap.Bool("has_video", msg.Video != nil),
		zap.Bool("has_document", msg.Document != nil),
		zap.Bool("has_animation", msg.Animation != nil))

	sessionID := msg.MediaGroupID
	if sessionID == "" {
		// Generate fallback session for single file uploads
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	var resourceType, fileID string
	if len(msg.Photo) > 0 {
		resourceType = "image"
		fileID = msg.Photo[len(msg.Photo)-1].FileID
		logger.Info("📸 Photo detected", zap.String("file_id", fileID))
	} else if msg.Video != nil {
		resourceType = "video"
		fileID = msg.Video.FileID
		logger.Info("🎥 Video detected", zap.String("file_id", fileID))
	} else if msg.Animation != nil {
		// Animation (GIF) - treat as video for Cloudinary
		resourceType = "video"
		fileID = msg.Animation.FileID
		logger.Info("🎬 Animation/GIF detected", zap.String("file_id", fileID))
	} else if msg.Document != nil {
		// Document - check if it's an image file
		doc := msg.Document
		mimeType := strings.ToLower(doc.MimeType)
		if strings.HasPrefix(mimeType, "image/") {
			resourceType = "image"
			fileID = doc.FileID
			logger.Info("📄 Image document detected", zap.String("file_id", fileID), zap.String("mime_type", mimeType))
		} else if strings.HasPrefix(mimeType, "video/") {
			resourceType = "video"
			fileID = doc.FileID
			logger.Info("📄 Video document detected", zap.String("file_id", fileID), zap.String("mime_type", mimeType))
		} else {
			// Unsupported document type
			logger.Warn("⚠️ Unsupported document type", zap.String("mime_type", mimeType))
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Only images and videos are supported. Your file type ("+mimeType+") cannot be posted."))
			return
		}
	} else {
		logger.Warn("⚠️ No media found in message",
			zap.Int("photo_count", len(msg.Photo)),
			zap.Bool("has_video", msg.Video != nil),
			zap.Bool("has_document", msg.Document != nil),
			zap.Bool("has_animation", msg.Animation != nil))
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ No media found. Please send a photo or video."))
		return
	}

	url, err := social.UploadTelegramMedia(ctx, logger, cloudURL, tenantID, bot.Token, fileID, "upload", "")
	if err != nil {
		logger.Error("upload failed", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Unable to upload your media. Please try again in a moment."))
		return
	}

	item := storage.MediaItem{
		TenantID:     tenantID,
		SessionID:    sessionID,
		URL:          url,
		ResourceType: resourceType,
		FileID:       fileID,
		Status:       "pending",
	}

	if err := m.DB.Create(&item).Error; err != nil {
		logger.Error("failed to store media item", zap.Error(err))
		return
	}

	logger.Info("✅ Uploaded & staged media item",
		zap.String("session", sessionID),
		zap.String("url", url),
		zap.String("type", resourceType))

	// ✅ Launch background finalizer (only once per session)
	key := fmt.Sprintf("%d:%s", tenantID, sessionID)
	if _, already := m.sessionFinalizers.LoadOrStore(key, struct{}{}); already {
		return
	}
	// cleanup guard after a short window so future uploads can reuse the key
	time.AfterFunc(2*time.Minute, func() {
		m.sessionFinalizers.Delete(key)
	})
	// Pass tenantID and chatID instead of bot instance to avoid stale references
	go m.FinalizeCarouselSession(ctx, tenantID, msg.Chat.ID, sessionID)
}

func (m *TenantBotManager) FinalizeCarouselSession(ctx context.Context, tenantID uint, chatID int64, sessionID string) {
	// Get bot instance fresh - it might not have been available when goroutine started
	bot, ok := m.GetBot(tenantID)
	if !ok || bot == nil {
		m.Logger.Warn("bot instance not found for tenant in FinalizeCarouselSession, attempting to initialize",
			zap.Uint("tenant_id", tenantID),
			zap.String("session_id", sessionID))
		// Try to initialize bot if tenant exists
		var tenant storage.Tenant
		if err := m.DB.First(&tenant, tenantID).Error; err == nil && tenant.TelegramBotToken != "" {
			// Decrypt token and create bot
			token, err := utils.DecryptToken(tenant.TelegramBotToken)
			if err == nil {
				newBot, err := tgbotapi.NewBotAPI(token)
				if err == nil {
					m.Mutex.Lock()
					m.Bots[tenantID] = newBot
					m.Mutex.Unlock()
					bot = newBot
					m.Logger.Info("✅ Bot instance initialized in FinalizeCarouselSession",
						zap.Uint("tenant_id", tenantID))
				}
			}
		}
		if bot == nil {
			m.Logger.Error("failed to get bot instance after retry - cannot send response",
				zap.Uint("tenant_id", tenantID),
				zap.Int64("chat_id", chatID))
			// Still process the upload, but can't send messages
		}
	}
	// Smart wait: Check item count and wait accordingly
	// For bulk uploads (20+), we need more time
	maxWaitTime := 30 * time.Second
	checkInterval := 2 * time.Second
	startTime := time.Now()

	var items []storage.MediaItem
	var lastCount int
	var progressMsgID int // Track progress message for editing
	
	// Wait for items to arrive, checking periodically
	for time.Since(startTime) < maxWaitTime {
		time.Sleep(checkInterval)
		
		if err := m.DB.Where("tenant_id = ? AND session_id = ? AND status = ?", tenantID, sessionID, "pending").Find(&items).Error; err != nil {
			m.Logger.Error("failed to fetch session items", zap.Error(err))
			continue
		}
		
		currentCount := len(items)
		
		// Send progress updates for batch uploads (2+ items)
		if bot != nil && currentCount > 1 && currentCount != lastCount {
			// Check total expected count (if we can determine it from processed items)
			var totalItems []storage.MediaItem
			m.DB.Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).Find(&totalItems)
			totalCount := len(totalItems)
			
			if totalCount > 1 {
				progressText := fmt.Sprintf("📤 Uploading... %d/%d images uploaded", currentCount, totalCount)
				if progressMsgID == 0 {
					// First progress message
					msg := tgbotapi.NewMessage(chatID, progressText)
					if sent, err := bot.Send(msg); err == nil {
						progressMsgID = sent.MessageID
					}
				} else {
					// Update existing progress message
					edit := tgbotapi.NewEditMessageText(chatID, progressMsgID, progressText)
					bot.Send(edit)
				}
			}
		}
		
		// If count hasn't changed in 2 checks, assume all items arrived
		if currentCount == lastCount && currentCount > 0 {
			// Wait a bit more for any stragglers
			time.Sleep(2 * time.Second)
			break
		}
		
		// For large uploads (20+), wait longer
		if currentCount >= 20 {
			// Wait additional time for bulk uploads
			time.Sleep(5 * time.Second)
		}
		
		lastCount = currentCount
	}
	
	// Delete progress message if it exists
	if bot != nil && progressMsgID > 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, progressMsgID)
		bot.Send(deleteMsg)
	}
	
	// Final fetch of all items (including processed)
	if err := m.DB.Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).Find(&items).Error; err != nil {
		m.Logger.Error("failed to fetch session items", zap.Error(err))
		return
	}

	if len(items) == 0 {
		m.Logger.Warn("no items found for session",
			zap.Uint("tenant_id", tenantID),
			zap.String("session_id", sessionID))
		return
	}
	
	m.Logger.Info("📦 Finalizing media session",
		zap.Uint("tenant_id", tenantID),
		zap.String("session_id", sessionID),
		zap.Int("item_count", len(items)))

	// ✅ Determine type
	assetType := "image"
	if len(items) > 1 {
		assetType = "carousel"
	} else if items[0].ResourceType == "video" {
		assetType = "video"
	}

	var urls []string
	for _, it := range items {
		urls = append(urls, it.URL)
	}

	urlJSON, _ := json.Marshal(urls)
	carouselName := fmt.Sprintf("auto_%s", sessionID)

	asset := storage.MediaAsset{
		TenantID:     tenantID,
		Type:         assetType,
		Name:         fmt.Sprintf("%s_%d", assetType, time.Now().Unix()),
		CarouselName: carouselName,
		URLs:         string(urlJSON),
		Status:       "uploaded",
		Platforms:    "[]",
	}

	if err := m.DB.Create(&asset).Error; err != nil {
		m.Logger.Error("failed to save media asset", zap.Error(err))
		return
	}

	// ✅ Mark items as processed
	m.DB.Model(&storage.MediaItem{}).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Update("status", "processed")

	m.Logger.Info("✅ Carousel finalized",
		zap.Uint("tenant_id", tenantID),
		zap.String("session_id", sessionID),
		zap.Int("count", len(items)))

	// ✅ Check if this is a bulk upload (10+ items)
	bulkManager := NewBulkUploadManager(m.DB, m.Logger)
	isBulk, itemCount, err := bulkManager.DetectBulkUpload(tenantID, sessionID)
	
	m.Logger.Info("🔍 Bulk upload check",
		zap.Uint("tenant_id", tenantID),
		zap.String("session_id", sessionID),
		zap.Bool("is_bulk", isBulk),
		zap.Int("item_count", itemCount),
		zap.Error(err))
	
	if err == nil && isBulk {
		// Handle as bulk upload
		m.Logger.Info("📦 Processing bulk upload",
			zap.Uint("tenant_id", tenantID),
			zap.Int("item_count", itemCount))
		
		// Send acknowledgment message
		ackMsg := fmt.Sprintf("📦 *Bulk Upload Detected!*\n\nI found %d images. Processing them now...", itemCount)
		ack := tgbotapi.NewMessage(chatID, ackMsg)
		ack.ParseMode = "Markdown"
		if bot != nil {
			bot.Send(ack)
		}
		
		if err := bulkManager.ProcessBulkUpload(ctx, bot, tenantID, chatID, sessionID); err != nil {
			m.Logger.Error("failed to process bulk upload", zap.Error(err))
			// Fallback to normal flow
			if bot != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Bulk upload processing failed. Falling back to normal flow."))
				m.StartPostFlow(bot, chatID, asset.ID)
			}
		}
		return
	}

	// ✅ Normal flow: Trigger interactive post creation flow
	if bot != nil {
	m.StartPostFlow(bot, chatID, asset.ID)
	} else {
		m.Logger.Error("bot instance is nil, cannot start post flow",
			zap.Uint("tenant_id", tenantID),
			zap.Int64("chat_id", chatID))
	}

}

func (m *TenantBotManager) ProcessCarouselGroup(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64, groupID string) {
	time.Sleep(3 * time.Second) // wait for all items to arrive

	items, _ := m.Groups.Flush(ctx, tenantID, groupID)
	if len(items) < 2 {
		return // skip if less than 2 media
	}

	var urls []string
	for _, it := range items {
		urls = append(urls, it.URL)
	}
	urlJSON, _ := json.Marshal(urls)
	carouselName := fmt.Sprintf("auto_%s", groupID)

	asset := storage.MediaAsset{
		TenantID:     tenantID,
		Type:         "carousel",
		Name:         fmt.Sprintf("carousel_%d", time.Now().Unix()),
		CarouselName: carouselName,
		URLs:         string(urlJSON),
		Status:       "uploaded",
		Platforms:    "[]",
	}

	// ✅ Move media from temp_groups to carousels folder
	cloudURL := os.Getenv("CLOUDINARY_URL")
	if cloudURL == "" {
		m.Logger.Warn("CLOUDINARY_URL not set; skipping folder move")
	} else {
		m.moveGroupToCarouselFolder(ctx, cloudURL, tenantID, groupID, carouselName)
	}

	if err := m.DB.Create(&asset).Error; err == nil {
		m.Logger.Info("✅ Carousel saved",
			zap.Uint("tenant_id", tenantID),
			zap.String("carousel_name", carouselName),
			zap.Int("count", len(items)))
		msg := fmt.Sprintf("📸 *Carousel created successfully!*\n\n📁 Folder: `brancr/uploads/tenant_%d/carousels/%s`\n🖼 Total items: %d\n📝 You can rename it:\n`name this carousel LaunchWeek1`", tenantID, carouselName, len(items))
		bot.Send(tgbotapi.NewMessage(chatID, msg))
	}
}

func (m *TenantBotManager) moveGroupToCarouselFolder(ctx context.Context, cloudURL string, tenantID uint, groupID, carouselName string) {
	cld, err := cloudinary.NewFromURL(cloudURL)
	if err != nil {
		m.Logger.Error("cloudinary init failed", zap.Error(err))
		return
	}

	oldFolder := fmt.Sprintf("brancr/uploads/tenant_%d/temp_groups/%s", tenantID, groupID)
	newFolder := fmt.Sprintf("brancr/uploads/tenant_%d/carousels/%s", tenantID, carouselName)

	m.Logger.Info("🚚 Moving group to carousel folder",
		zap.Uint("tenant_id", tenantID),
		zap.String("from", oldFolder),
		zap.String("to", newFolder))

	// List assets under the old folder (prefix)
	params := admin.AssetsParams{
		Prefix:     oldFolder,
		MaxResults: 500,
	}
	res, err := cld.Admin.Assets(ctx, params)
	if err != nil {
		m.Logger.Error("failed to list old group assets", zap.Error(err))
		return
	}

	for _, asset := range res.Assets {
		// asset.PublicID is the full public id path (without file extension)
		// we build the new public id by replacing the folder segment
		oldPublicID := asset.PublicID
		// replace first occurrence of temp_groups/<groupID> with carousels/<carouselName>
		newPublicID := strings.Replace(oldPublicID, fmt.Sprintf("temp_groups/%s", groupID),
			fmt.Sprintf("carousels/%s", carouselName), 1)

		renameParams := uploader.RenameParams{
			FromPublicID: oldPublicID,
			ToPublicID:   newPublicID,
			// you can set ResourceType if needed: ResourceType: uploader.Image,
		}

		_, err := cld.Upload.Rename(ctx, renameParams)
		if err != nil {
			m.Logger.Warn("failed to rename asset", zap.String("public_id", oldPublicID), zap.Error(err))
			continue
		}
		m.Logger.Info("✅ Moved asset", zap.String("from", oldPublicID), zap.String("to", newPublicID))
	}

	m.Logger.Info("✅ Carousel folder finalized",
		zap.Uint("tenant_id", tenantID),
		zap.String("folder", newFolder))
}
