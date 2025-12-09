package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrQuotaExceeded = errors.New("quota exceeded")
	ErrQuotaNotFound = errors.New("quota not found")
)

// QuotaService manages tenant quotas
type QuotaService struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewQuotaService creates a new quota service
func NewQuotaService(db *gorm.DB, logger *zap.Logger) *QuotaService {
	return &QuotaService{
		DB:     db,
		Logger: logger,
	}
}

// GetOrCreateQuota gets or creates a quota for a tenant
func (s *QuotaService) GetOrCreateQuota(ctx context.Context, tenantID uint) (*storage.TenantQuota, error) {
	var quota storage.TenantQuota
	err := s.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&quota).Error
	
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create default quota based on plan type
		quota = storage.TenantQuota{
			TenantID:        tenantID,
			AICallsLimit:    100,  // Default: 100 AI calls/day
			PostsLimit:      50,   // Default: 50 posts/day
			StorageLimitMB:  1000, // Default: 1GB
			APICallsLimit:   1000, // Default: 1000 API calls/day
			ResetFrequency:  "daily",
			PlanType:        "free",
			LastResetAt:     time.Now(),
		}
		
		if err := s.DB.WithContext(ctx).Create(&quota).Error; err != nil {
			return nil, fmt.Errorf("failed to create quota: %w", err)
		}
		
		s.Logger.Info("created default quota for tenant",
			zap.Uint("tenant_id", tenantID),
			zap.String("plan", quota.PlanType))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}
	
	// Reset quota if needed
	if err := s.checkAndResetQuota(ctx, &quota); err != nil {
		s.Logger.Warn("failed to reset quota", zap.Error(err))
	}
	
	return &quota, nil
}

// checkAndResetQuota resets quota if reset period has passed
func (s *QuotaService) checkAndResetQuota(ctx context.Context, quota *storage.TenantQuota) error {
	now := time.Now()
	var shouldReset bool
	
	switch quota.ResetFrequency {
	case "daily":
		shouldReset = now.Sub(quota.LastResetAt) >= 24*time.Hour
	case "weekly":
		shouldReset = now.Sub(quota.LastResetAt) >= 7*24*time.Hour
	case "monthly":
		shouldReset = now.Sub(quota.LastResetAt) >= 30*24*time.Hour
	default:
		shouldReset = now.Sub(quota.LastResetAt) >= 24*time.Hour
	}
	
	if shouldReset {
		updates := map[string]interface{}{
			"ai_calls_used":   0,
			"posts_used":      0,
			"api_calls_used":  0,
			"last_reset_at":   now,
		}
		// Note: Storage is not reset (cumulative)
		
		if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to reset quota: %w", err)
		}
		
		quota.AICallsUsed = 0
		quota.PostsUsed = 0
		quota.APICallsUsed = 0
		quota.LastResetAt = now
		
		s.Logger.Info("reset quota for tenant",
			zap.Uint("tenant_id", quota.TenantID),
			zap.String("frequency", quota.ResetFrequency))
	}
	
	return nil
}

// CheckAICallQuota checks if tenant can make an AI API call
func (s *QuotaService) CheckAICallQuota(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// 0 means unlimited
	if quota.AICallsLimit > 0 && quota.AICallsUsed >= quota.AICallsLimit {
		return fmt.Errorf("%w: AI calls limit (%d) exceeded", ErrQuotaExceeded, quota.AICallsLimit)
	}
	
	return nil
}

// IncrementAICallUsage increments AI call usage
func (s *QuotaService) IncrementAICallUsage(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// Check quota before incrementing
	if quota.AICallsLimit > 0 && quota.AICallsUsed >= quota.AICallsLimit {
		return fmt.Errorf("%w: AI calls limit (%d) exceeded", ErrQuotaExceeded, quota.AICallsLimit)
	}
	
	updates := map[string]interface{}{
		"ai_calls_used": gorm.Expr("ai_calls_used + ?", 1),
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment AI call usage: %w", err)
	}
	
	return nil
}

// CheckPostQuota checks if tenant can schedule a post
func (s *QuotaService) CheckPostQuota(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// 0 means unlimited
	if quota.PostsLimit > 0 && quota.PostsUsed >= quota.PostsLimit {
		return fmt.Errorf("%w: posts limit (%d) exceeded", ErrQuotaExceeded, quota.PostsLimit)
	}
	
	return nil
}

// IncrementPostUsage increments post usage
func (s *QuotaService) IncrementPostUsage(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// Check quota before incrementing
	if quota.PostsLimit > 0 && quota.PostsUsed >= quota.PostsLimit {
		return fmt.Errorf("%w: posts limit (%d) exceeded", ErrQuotaExceeded, quota.PostsLimit)
	}
	
	updates := map[string]interface{}{
		"posts_used": gorm.Expr("posts_used + ?", 1),
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment post usage: %w", err)
	}
	
	return nil
}

// CheckStorageQuota checks if tenant has storage space
func (s *QuotaService) CheckStorageQuota(ctx context.Context, tenantID uint, sizeMB int) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// 0 means unlimited
	if quota.StorageLimitMB > 0 && (quota.StorageUsedMB+sizeMB) > quota.StorageLimitMB {
		return fmt.Errorf("%w: storage limit (%d MB) would be exceeded", ErrQuotaExceeded, quota.StorageLimitMB)
	}
	
	return nil
}

// IncrementStorageUsage increments storage usage
func (s *QuotaService) IncrementStorageUsage(ctx context.Context, tenantID uint, sizeMB int) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// Check quota before incrementing
	if quota.StorageLimitMB > 0 && (quota.StorageUsedMB+sizeMB) > quota.StorageLimitMB {
		return fmt.Errorf("%w: storage limit (%d MB) would be exceeded", ErrQuotaExceeded, quota.StorageLimitMB)
	}
	
	updates := map[string]interface{}{
		"storage_used_mb": gorm.Expr("storage_used_mb + ?", sizeMB),
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment storage usage: %w", err)
	}
	
	return nil
}

// DecrementStorageUsage decrements storage usage (when files are deleted)
func (s *QuotaService) DecrementStorageUsage(ctx context.Context, tenantID uint, sizeMB int) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	updates := map[string]interface{}{
		"storage_used_mb": gorm.Expr("GREATEST(0, storage_used_mb - ?)", sizeMB),
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to decrement storage usage: %w", err)
	}
	
	return nil
}

// CheckAPICallQuota checks if tenant can make an API call
func (s *QuotaService) CheckAPICallQuota(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// 0 means unlimited
	if quota.APICallsLimit > 0 && quota.APICallsUsed >= quota.APICallsLimit {
		return fmt.Errorf("%w: API calls limit (%d) exceeded", ErrQuotaExceeded, quota.APICallsLimit)
	}
	
	return nil
}

// IncrementAPICallUsage increments API call usage
func (s *QuotaService) IncrementAPICallUsage(ctx context.Context, tenantID uint) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	// Check quota before incrementing
	if quota.APICallsLimit > 0 && quota.APICallsUsed >= quota.APICallsLimit {
		return fmt.Errorf("%w: API calls limit (%d) exceeded", ErrQuotaExceeded, quota.APICallsLimit)
	}
	
	updates := map[string]interface{}{
		"api_calls_used": gorm.Expr("api_calls_used + ?", 1),
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment API call usage: %w", err)
	}
	
	return nil
}

// GetQuotaUsage returns current quota usage for a tenant
func (s *QuotaService) GetQuotaUsage(ctx context.Context, tenantID uint) (*storage.TenantQuota, error) {
	return s.GetOrCreateQuota(ctx, tenantID)
}

// UpdateQuotaLimits updates quota limits for a tenant (admin function)
func (s *QuotaService) UpdateQuotaLimits(ctx context.Context, tenantID uint, limits map[string]int) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}
	
	updates := make(map[string]interface{})
	
	if val, ok := limits["ai_calls_limit"]; ok {
		updates["ai_calls_limit"] = val
	}
	if val, ok := limits["posts_limit"]; ok {
		updates["posts_limit"] = val
	}
	if val, ok := limits["storage_limit_mb"]; ok {
		updates["storage_limit_mb"] = val
	}
	if val, ok := limits["api_calls_limit"]; ok {
		updates["api_calls_limit"] = val
	}
	if planType, ok := limits["plan_type"].(string); ok {
		updates["plan_type"] = planType
	}
	
	if len(updates) == 0 {
		return nil
	}
	
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update quota limits: %w", err)
	}
	
	s.Logger.Info("updated quota limits for tenant",
		zap.Uint("tenant_id", tenantID),
		zap.Any("limits", limits))
	
	return nil
}

