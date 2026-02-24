package bot

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SessionManager handles database-backed session storage
type SessionManager struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewSessionManager creates a new session manager
func NewSessionManager(db *gorm.DB, logger *zap.Logger) *SessionManager {
	return &SessionManager{
		DB:     db,
		Logger: logger,
	}
}

// Session expiration time (24 hours)
const SessionExpiration = 24 * time.Hour

// GetPostSession retrieves a post session from database
func (sm *SessionManager) GetPostSession(ctx context.Context, chatID int64) (*PostSession, bool) {
	var dbSession storage.PostSession
	if err := sm.DB.WithContext(ctx).Where("chat_id = ? AND (expires_at IS NULL OR expires_at > ?)", chatID, time.Now()).
		First(&dbSession).Error; err != nil {
		return nil, false
	}

	// Convert database model to PostSession
	session := &PostSession{
		Step:                 dbSession.Step,
		MediaAssetID:         dbSession.MediaAssetID,
		MediaType:            dbSession.MediaType,
		IsCarousel:           dbSession.IsCarousel,
		Name:                 dbSession.Name,
		Caption:              dbSession.Caption,
		UseAICaption:         dbSession.UseAICaption,
		CaptionMode:          dbSession.CaptionMode,
		ScheduleInput:        dbSession.ScheduleInput,
		BatchSchedule:        dbSession.BatchSchedule,
		BatchSpacing:         dbSession.BatchSpacing,
		TenantID:             dbSession.TenantID,
		TikTokDisableDuet:    dbSession.TikTokDisableDuet,
		TikTokDisableStitch:  dbSession.TikTokDisableStitch,
		TikTokDisableComment: dbSession.TikTokDisableComment,
		TikTokScheduleTime:   dbSession.TikTokScheduleTime,
		TikTokOptionsAsked:   dbSession.TikTokOptionsAsked,
	}

	// Deserialize platforms
	if dbSession.Platforms != "" {
		json.Unmarshal([]byte(dbSession.Platforms), &session.Platforms)
	}

	// Deserialize original inputs
	if dbSession.OriginalInputs != "" {
		json.Unmarshal([]byte(dbSession.OriginalInputs), &session.OriginalInputs)
	}

	return session, true
}

// SavePostSession saves a post session to database
func (sm *SessionManager) SavePostSession(ctx context.Context, chatID int64, session *PostSession) error {
	expiresAt := time.Now().Add(SessionExpiration)

	// Serialize platforms
	platformsJSON, _ := json.Marshal(session.Platforms)
	originalInputsJSON, _ := json.Marshal(session.OriginalInputs)

	dbSession := storage.PostSession{
		ChatID:               chatID,
		TenantID:             session.TenantID,
		Step:                 session.Step,
		MediaAssetID:         session.MediaAssetID,
		MediaType:            session.MediaType,
		IsCarousel:           session.IsCarousel,
		Name:                 session.Name,
		Caption:              session.Caption,
		UseAICaption:         session.UseAICaption,
		CaptionMode:          session.CaptionMode,
		Platforms:            string(platformsJSON),
		ScheduleInput:        session.ScheduleInput,
		BatchSchedule:        session.BatchSchedule,
		BatchSpacing:         session.BatchSpacing,
		OriginalInputs:       string(originalInputsJSON),
		TikTokDisableDuet:    session.TikTokDisableDuet,
		TikTokDisableStitch:  session.TikTokDisableStitch,
		TikTokDisableComment: session.TikTokDisableComment,
		TikTokScheduleTime:   session.TikTokScheduleTime,
		TikTokOptionsAsked:   session.TikTokOptionsAsked,
		ExpiresAt:            &expiresAt,
	}

	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Assign(dbSession).
		FirstOrCreate(&dbSession).Error
}

// DeletePostSession deletes a post session from database
func (sm *SessionManager) DeletePostSession(ctx context.Context, chatID int64) error {
	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Delete(&storage.PostSession{}).Error
}

// GetBusinessSession retrieves a business session from database
func (sm *SessionManager) GetBusinessSession(ctx context.Context, chatID int64) (*BusinessSession, bool) {
	var dbSession storage.BusinessSession
	if err := sm.DB.WithContext(ctx).Where("chat_id = ? AND (expires_at IS NULL OR expires_at > ?)", chatID, time.Now()).
		First(&dbSession).Error; err != nil {
		return nil, false
	}

	session := &BusinessSession{
		Mode: dbSession.Mode,
		Step: dbSession.Step,
	}

	// Deserialize item based on stored type (backward compatible with old BusinessMenuItem)
	if dbSession.MenuItem != "" {
		// Try to detect type from JSON structure
		var itemData map[string]interface{}
		if err := json.Unmarshal([]byte(dbSession.MenuItem), &itemData); err == nil {
			// Check if it has ItemType field (new format)
			if itemType, ok := itemData["ItemType"].(string); ok {
				session.ItemType = itemType
				// Extract the actual item data (MenuItem, Product, or Service)
				switch itemType {
				case "menu":
					if menuData, ok := itemData["MenuItem"].(map[string]interface{}); ok {
						menuJSON, _ := json.Marshal(menuData)
						json.Unmarshal(menuJSON, &session.MenuItem)
					}
				case "product":
					if productData, ok := itemData["Product"].(map[string]interface{}); ok {
						productJSON, _ := json.Marshal(productData)
						json.Unmarshal(productJSON, &session.Product)
					}
				case "service":
					if serviceData, ok := itemData["Service"].(map[string]interface{}); ok {
						serviceJSON, _ := json.Marshal(serviceData)
						json.Unmarshal(serviceJSON, &session.Service)
					}
				}
			} else {
				// Old format - check if it's BusinessMenuItem (has Price as string) or MenuItem (has Price as number)
				if price, ok := itemData["Price"]; ok {
					if _, isString := price.(string); isString {
						// Old BusinessMenuItem format - will be migrated, but try to load as MenuItem for now
						session.ItemType = "menu"
						// Try to convert BusinessMenuItem to MenuItem
						if name, ok := itemData["Name"].(string); ok {
							session.MenuItem.Name = name
						}
						if desc, ok := itemData["Description"].(string); ok {
							session.MenuItem.Description = desc
						}
						if priceStr, ok := price.(string); ok {
							// Parse price string to float64
							priceStr = strings.ReplaceAll(priceStr, "₦", "")
							priceStr = strings.ReplaceAll(priceStr, ",", "")
							priceStr = strings.ReplaceAll(priceStr, " ", "")
							if parsed, err := strconv.ParseFloat(priceStr, 64); err == nil {
								session.MenuItem.Price = parsed
							}
						}
						if cat, ok := itemData["Category"].(string); ok {
							session.MenuItem.Category = cat
						}
						session.MenuItem.Currency = "NGN"
						session.MenuItem.IsActive = true
						session.MenuItem.Availability = "available"
					} else {
						// New format without ItemType - assume menu
						session.ItemType = "menu"
						json.Unmarshal([]byte(dbSession.MenuItem), &session.MenuItem)
					}
				} else {
					// No price field - assume menu
					session.ItemType = "menu"
					json.Unmarshal([]byte(dbSession.MenuItem), &session.MenuItem)
				}
			}
		}
	}

	// Deserialize FAQ
	if dbSession.FAQ != "" {
		json.Unmarshal([]byte(dbSession.FAQ), &session.FAQ)
	}

	return session, true
}

// SaveBusinessSession saves a business session to database
func (sm *SessionManager) SaveBusinessSession(ctx context.Context, chatID int64, tenantID uuid.UUID, session *BusinessSession) error {
	expiresAt := time.Now().Add(SessionExpiration)

	// Serialize the appropriate item type based on ItemType
	var itemJSON []byte
	switch session.ItemType {
	case "product":
		// Include ItemType in JSON for deserialization
		itemData := map[string]interface{}{
			"ItemType": "product",
			"Product":  session.Product,
		}
		itemJSON, _ = json.Marshal(itemData)
	case "service":
		itemData := map[string]interface{}{
			"ItemType": "service",
			"Service":  session.Service,
		}
		itemJSON, _ = json.Marshal(itemData)
	case "menu":
		fallthrough
	default:
		// Default to menu item
		itemData := map[string]interface{}{
			"ItemType": "menu",
			"MenuItem": session.MenuItem,
		}
		itemJSON, _ = json.Marshal(itemData)
	}

	faqJSON, _ := json.Marshal(session.FAQ)

	dbSession := storage.BusinessSession{
		ChatID:    chatID,
		TenantID:  tenantID,
		Mode:      session.Mode,
		Step:      session.Step,
		MenuItem:  string(itemJSON),
		FAQ:       string(faqJSON),
		ExpiresAt: &expiresAt,
	}

	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Assign(dbSession).
		FirstOrCreate(&dbSession).Error
}

// DeleteBusinessSession deletes a business session from database
func (sm *SessionManager) DeleteBusinessSession(ctx context.Context, chatID int64) error {
	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Delete(&storage.BusinessSession{}).Error
}

// GetEscalationResolutionSession retrieves an escalation resolution session from database
func (sm *SessionManager) GetEscalationResolutionSession(ctx context.Context, chatID int64) (*EscalationResolutionSession, bool) {
	var dbSession storage.EscalationResolutionSession
	if err := sm.DB.WithContext(ctx).Where("chat_id = ? AND (expires_at IS NULL OR expires_at > ?)", chatID, time.Now()).
		First(&dbSession).Error; err != nil {
		return nil, false
	}

	return &EscalationResolutionSession{
		InteractionID: dbSession.InteractionID,
		TenantID:      dbSession.TenantID,
		CustomerID:    dbSession.CustomerID,
		Platform:      dbSession.Platform,
		StartedAt:     dbSession.StartedAt,
	}, true
}

// SaveEscalationResolutionSession saves an escalation resolution session to database
func (sm *SessionManager) SaveEscalationResolutionSession(ctx context.Context, chatID int64, session *EscalationResolutionSession) error {
	expiresAt := time.Now().Add(SessionExpiration)

	dbSession := storage.EscalationResolutionSession{
		ChatID:        chatID,
		TenantID:      session.TenantID,
		InteractionID: session.InteractionID,
		CustomerID:    session.CustomerID,
		Platform:      session.Platform,
		StartedAt:     session.StartedAt,
		ExpiresAt:     &expiresAt,
	}

	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Assign(dbSession).
		FirstOrCreate(&dbSession).Error
}

// DeleteEscalationResolutionSession deletes an escalation resolution session from database
func (sm *SessionManager) DeleteEscalationResolutionSession(ctx context.Context, chatID int64) error {
	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Delete(&storage.EscalationResolutionSession{}).Error
}

// CleanupExpiredSessions removes expired sessions (should be called periodically)
func (sm *SessionManager) CleanupExpiredSessions(ctx context.Context) error {
	now := time.Now()

	// Delete expired post sessions
	if err := sm.DB.WithContext(ctx).Where("expires_at < ?", now).
		Delete(&storage.PostSession{}).Error; err != nil {
		sm.Logger.Warn("failed to cleanup expired post sessions", zap.Error(err))
	}

	// Delete expired business sessions
	if err := sm.DB.WithContext(ctx).Where("expires_at < ?", now).
		Delete(&storage.BusinessSession{}).Error; err != nil {
		sm.Logger.Warn("failed to cleanup expired business sessions", zap.Error(err))
	}

	// Delete expired escalation resolution sessions
	if err := sm.DB.WithContext(ctx).Where("expires_at < ?", now).
		Delete(&storage.EscalationResolutionSession{}).Error; err != nil {
		sm.Logger.Warn("failed to cleanup expired escalation resolution sessions", zap.Error(err))
	}

	return nil
}

// ============================================================
// POST SESSION V2 - Simplified Flow
// ============================================================

// GetPostSessionV2 retrieves a V2 post session from database
func (sm *SessionManager) GetPostSessionV2(ctx context.Context, chatID int64) (*PostSessionV2, bool) {
	var dbSession storage.PostSessionV2
	if err := sm.DB.WithContext(ctx).Where("chat_id = ? AND (expires_at IS NULL OR expires_at > ?)", chatID, time.Now()).
		First(&dbSession).Error; err != nil {
		return nil, false
	}

	session := &PostSessionV2{
		Step:         dbSession.Step,
		MediaType:    dbSession.MediaType,
		TenantID:     dbSession.TenantID,
		CaptionMode:  dbSession.CaptionMode,
		CaptionTheme: dbSession.CaptionTheme,
		ScheduleMode: dbSession.ScheduleMode,
		IsBatch:      dbSession.IsBatch,
		BatchSpacing: dbSession.BatchSpacing,
	}

	// Deserialize arrays
	if dbSession.MediaAssetIDs != "" {
		json.Unmarshal([]byte(dbSession.MediaAssetIDs), &session.MediaAssetIDs)
	}
	if dbSession.Platforms != "" {
		json.Unmarshal([]byte(dbSession.Platforms), &session.Platforms)
	}
	if dbSession.Captions != "" {
		json.Unmarshal([]byte(dbSession.Captions), &session.Captions)
	}
	if dbSession.ScheduleTimes != "" {
		json.Unmarshal([]byte(dbSession.ScheduleTimes), &session.ScheduleTimes)
	}
	if dbSession.LastPlatforms != "" {
		json.Unmarshal([]byte(dbSession.LastPlatforms), &session.LastPlatforms)
	}

	return session, true
}

// SavePostSessionV2 saves a V2 post session to database
func (sm *SessionManager) SavePostSessionV2(ctx context.Context, chatID int64, session *PostSessionV2) error {
	expiresAt := time.Now().Add(SessionExpiration)

	// Serialize arrays
	mediaAssetIDsJSON, _ := json.Marshal(session.MediaAssetIDs)
	platformsJSON, _ := json.Marshal(session.Platforms)
	captionsJSON, _ := json.Marshal(session.Captions)
	scheduleTimesJSON, _ := json.Marshal(session.ScheduleTimes)
	lastPlatformsJSON, _ := json.Marshal(session.LastPlatforms)

	dbSession := storage.PostSessionV2{
		ChatID:        chatID,
		TenantID:      session.TenantID,
		Step:          session.Step,
		MediaAssetIDs: string(mediaAssetIDsJSON),
		MediaType:     session.MediaType,
		Platforms:     string(platformsJSON),
		CaptionMode:   session.CaptionMode,
		Captions:      string(captionsJSON),
		CaptionTheme:  session.CaptionTheme,
		ScheduleMode:  session.ScheduleMode,
		ScheduleTimes: string(scheduleTimesJSON),
		IsBatch:       session.IsBatch,
		BatchSpacing:  session.BatchSpacing,
		LastPlatforms: string(lastPlatformsJSON),
		ExpiresAt:     &expiresAt,
	}

	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Assign(dbSession).
		FirstOrCreate(&dbSession).Error
}

// DeletePostSessionV2 deletes a V2 post session from database
func (sm *SessionManager) DeletePostSessionV2(ctx context.Context, chatID int64) error {
	return sm.DB.WithContext(ctx).Where("chat_id = ?", chatID).
		Delete(&storage.PostSessionV2{}).Error
}

// ============================================================
// GENERIC SESSION STORAGE - For V3 and future flows
// ============================================================

// SetGenericSession stores any serializable data as a session
func (sm *SessionManager) SetGenericSession(ctx context.Context, key string, data interface{}, ttl time.Duration) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(ttl)

	session := storage.GenericSession{
		Key:       key,
		Data:      string(jsonData),
		ExpiresAt: &expiresAt,
	}

	return sm.DB.WithContext(ctx).Where("key = ?", key).
		Assign(session).
		FirstOrCreate(&session).Error
}

// GetGenericSession retrieves a generic session and unmarshals it
func (sm *SessionManager) GetGenericSession(ctx context.Context, key string, dest interface{}) error {
	var session storage.GenericSession
	if err := sm.DB.WithContext(ctx).Where("key = ? AND (expires_at IS NULL OR expires_at > ?)", key, time.Now()).
		First(&session).Error; err != nil {
		return err
	}

	return json.Unmarshal([]byte(session.Data), dest)
}

// DeleteSession deletes a generic session by key
func (sm *SessionManager) DeleteSession(ctx context.Context, key string) error {
	return sm.DB.WithContext(ctx).Where("key = ?", key).
		Delete(&storage.GenericSession{}).Error
}
