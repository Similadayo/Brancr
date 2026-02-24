package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/cache"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MetricsRecorder is an interface for recording cost metrics
// This allows QuotaService to record metrics without tight coupling
type MetricsRecorder interface {
	RecordAICost(tenantID string, model string, costUSD float64)
}

var (
	ErrQuotaExceeded = errors.New("quota exceeded")
	ErrQuotaNotFound = errors.New("quota not found")
)

// QuotaService manages tenant quotas
type QuotaService struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	Cache          *cache.Cache    // Optional cache for quota data
	CostCalculator *CostCalculator // Cost calculator for token-to-dollar conversion
	Metrics        MetricsRecorder // Optional metrics recorder for cost tracking
}

// NewQuotaService creates a new quota service
func NewQuotaService(db *gorm.DB, logger *zap.Logger, cacheService *cache.Cache) *QuotaService {
	return &QuotaService{
		DB:             db,
		Logger:         logger,
		Cache:          cacheService,
		CostCalculator: NewCostCalculator(),
	}
}

// GetOrCreateQuota gets or creates a quota for a tenant
func (s *QuotaService) GetOrCreateQuota(ctx context.Context, tenantID uuid.UUID) (*storage.TenantQuota, error) {
	// Try to get from cache first
	cacheKey := cache.QuotaKey(tenantID)
	if s.Cache != nil {
		var cachedQuota storage.TenantQuota
		if err := s.Cache.Get(ctx, cacheKey, &cachedQuota); err == nil {
			// Cache hit - check if reset is needed (this may modify quota)
			originalLastReset := cachedQuota.LastResetAt
			resetErr := s.checkAndResetQuota(ctx, &cachedQuota)

			// If reset occurred successfully, invalidate cache and reload from DB to get fresh state
			if !originalLastReset.Equal(cachedQuota.LastResetAt) {
				s.Cache.Delete(ctx, cacheKey)
				// Fall through to DB query to get fresh data after reset
			} else if resetErr != nil {
				// Reset was attempted but failed (database error) - invalidate cache and fetch fresh data
				// This prevents using stale cached quota when reset should have happened but failed
				s.Logger.Warn("quota reset failed, invalidating cache and fetching fresh data",
					zap.Stringer("tenant_id", tenantID),
					zap.Error(resetErr))
				s.Cache.Delete(ctx, cacheKey)
				// Fall through to DB query to get accurate current state
			} else {
				// No reset needed and no error - safe to use cached data
				s.Logger.Debug("quota cache hit", zap.Stringer("tenant_id", tenantID))
				return &cachedQuota, nil
			}
		}
	}

	// Cache miss or reset occurred - fetch from database
	var quota storage.TenantQuota
	err := s.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&quota).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create default quota based on plan type
		quota = storage.TenantQuota{
			TenantID:       tenantID,
			AICallsLimit:   100,  // Default: 100 AI calls/day
			PostsLimit:     50,   // Default: 50 posts/day
			StorageLimitMB: 1000, // Default: 1GB
			APICallsLimit:  1000, // Default: 1000 API calls/day
			ResetFrequency: "daily",
			PlanType:       "free",
			LastResetAt:    time.Now(),
		}

		if err := s.DB.WithContext(ctx).Create(&quota).Error; err != nil {
			return nil, fmt.Errorf("failed to create quota: %w", err)
		}

		s.Logger.Info("created default quota for tenant",
			zap.Stringer("tenant_id", tenantID),
			zap.String("plan", quota.PlanType))
	} else if err != nil {
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	// Reset quota if needed
	if err := s.checkAndResetQuota(ctx, &quota); err != nil {
		s.Logger.Warn("failed to reset quota", zap.Error(err))
	}

	// Cache the quota for 5 minutes (usage changes frequently, so shorter TTL)
	if s.Cache != nil {
		if err := s.Cache.SetWithTTL(ctx, cacheKey, quota, 5*time.Minute); err != nil {
			s.Logger.Warn("failed to cache quota", zap.Error(err), zap.Stringer("tenant_id", tenantID))
		}
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
			"ai_calls_used":  0,
			"posts_used":     0,
			"api_calls_used": 0,
			"cost_used":      0, // Reset cost usage
			"last_reset_at":  now,
		}
		// Note: Storage is not reset (cumulative)

		if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to reset quota: %w", err)
		}

		quota.AICallsUsed = 0
		quota.PostsUsed = 0
		quota.APICallsUsed = 0
		quota.CostUsed = 0
		quota.LastResetAt = now

		// Invalidate cache after reset
		if s.Cache != nil {
			s.Cache.Delete(ctx, cache.QuotaKey(quota.TenantID))
		}

		s.Logger.Info("reset quota for tenant",
			zap.Stringer("tenant_id", quota.TenantID),
			zap.String("frequency", quota.ResetFrequency))
	}

	return nil
}

// CheckAICallQuota checks if tenant can make an AI API call
// Also checks cost limit if set
func (s *QuotaService) CheckAICallQuota(ctx context.Context, tenantID uuid.UUID) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// 0 means unlimited
	if quota.AICallsLimit > 0 && quota.AICallsUsed >= quota.AICallsLimit {
		return fmt.Errorf("%w: AI calls limit (%d) exceeded", ErrQuotaExceeded, quota.AICallsLimit)
	}

	// Check cost limit (if set)
	if quota.CostLimit > 0 && quota.CostUsed >= quota.CostLimit {
		return fmt.Errorf("%w: cost limit ($%.2f) exceeded (used: $%.2f)", ErrQuotaExceeded, quota.CostLimit, quota.CostUsed)
	}

	return nil
}

// CheckAICallCost checks if an estimated cost would exceed the cost limit
// This is called before making an AI call to estimate if it would exceed budget
func (s *QuotaService) CheckAICallCost(ctx context.Context, tenantID uuid.UUID, estimatedCost float64) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// 0 means unlimited
	if quota.CostLimit > 0 && (quota.CostUsed+estimatedCost) > quota.CostLimit {
		return fmt.Errorf("%w: estimated cost ($%.4f) would exceed cost limit ($%.2f, used: $%.2f)",
			ErrQuotaExceeded, estimatedCost, quota.CostLimit, quota.CostUsed)
	}

	return nil
}

// IncrementAICallUsage increments AI call usage
func (s *QuotaService) IncrementAICallUsage(ctx context.Context, tenantID uuid.UUID) error {
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

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// IncrementAICallCost increments both AI call count and cost usage
// This should be called after a successful AI call with actual token usage
func (s *QuotaService) IncrementAICallCost(
	ctx context.Context,
	tenantID uuid.UUID,
	model string,
	tokenUsage *TokenUsage,
) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// Calculate cost from token usage
	var cost float64
	if tokenUsage != nil && s.CostCalculator != nil {
		calculatedCost, err := s.CostCalculator.CalculateCost(model, tokenUsage)
		if err != nil {
			s.Logger.Warn("failed to calculate cost, incrementing call count only",
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
			// Fall back to just incrementing call count
			return s.IncrementAICallUsage(ctx, tenantID)
		}
		cost = calculatedCost
	}

	// Check cost limit before incrementing
	if quota.CostLimit > 0 && (quota.CostUsed+cost) > quota.CostLimit {
		return fmt.Errorf("%w: cost limit ($%.2f) would be exceeded (current: $%.2f, additional: $%.4f)",
			ErrQuotaExceeded, quota.CostLimit, quota.CostUsed, cost)
	}

	// Increment both call count and cost
	updates := map[string]interface{}{
		"ai_calls_used": gorm.Expr("ai_calls_used + ?", 1),
	}
	if cost > 0 {
		updates["cost_used"] = gorm.Expr("cost_used + ?", cost)
	}

	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment AI call cost: %w", err)
	}

	// Log cost for monitoring
	if cost > 0 && s.Logger != nil {
		s.Logger.Info("ai_call_cost_tracked",
			zap.Stringer("tenant_id", tenantID),
			zap.String("model", model),
			zap.Float64("cost_usd", cost),
			zap.Int("prompt_tokens", tokenUsage.PromptTokens),
			zap.Int("completion_tokens", tokenUsage.CompletionTokens),
			zap.Int("total_tokens", tokenUsage.TotalTokens),
			zap.Float64("cost_used_total", quota.CostUsed+cost),
			zap.Float64("cost_limit", quota.CostLimit))
	}

	// Record cost in metrics (Prometheus) if available
	if cost > 0 && s.Metrics != nil {
		s.Metrics.RecordAICost(tenantID.String(), model, cost)
	}

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// CheckPostQuota checks if tenant can schedule a post
func (s *QuotaService) CheckPostQuota(ctx context.Context, tenantID uuid.UUID) error {
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

// CheckCanSchedulePost checks if tenant can schedule another post.
// It considers both already executed posts and posts currently scheduled (not cancelled/posted).
func (s *QuotaService) CheckCanSchedulePost(ctx context.Context, tenantID uuid.UUID) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// 0 means unlimited
	if quota.PostsLimit == 0 {
		return nil
	}

	// Count scheduled posts that are not yet posted or cancelled
	var scheduledCount int64
	if err := s.DB.WithContext(ctx).
		Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status = ?", tenantID, "scheduled").
		Count(&scheduledCount).Error; err != nil {
		return fmt.Errorf("failed to count scheduled posts: %w", err)
	}

	if quota.PostsUsed+int(scheduledCount) >= quota.PostsLimit {
		return fmt.Errorf("%w: posts limit (%d) exceeded (used=%d, scheduled=%d)", ErrQuotaExceeded, quota.PostsLimit, quota.PostsUsed, scheduledCount)
	}
	return nil
}

// CheckPlatformLimit checks if tenant can connect another social account (platform).
func (s *QuotaService) CheckPlatformLimit(ctx context.Context, tenantID uuid.UUID) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// 0 means unlimited
	if quota.PlatformsLimit == 0 {
		return nil
	}

	var connected int64
	if err := s.DB.WithContext(ctx).
		Model(&storage.SocialAccount{}).
		Where("tenant_id = ? AND connected = ?", tenantID, true).
		Count(&connected).Error; err != nil {
		return fmt.Errorf("failed to count connected platforms: %w", err)
	}

	if int(connected) >= quota.PlatformsLimit {
		return fmt.Errorf("%w: platforms limit (%d) exceeded", ErrQuotaExceeded, quota.PlatformsLimit)
	}
	return nil
}

// IncrementPostUsage increments post usage
func (s *QuotaService) IncrementPostUsage(ctx context.Context, tenantID uuid.UUID) error {
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

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// CheckStorageQuota checks if tenant has storage space
func (s *QuotaService) CheckStorageQuota(ctx context.Context, tenantID uuid.UUID, sizeMB int) error {
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
func (s *QuotaService) IncrementStorageUsage(ctx context.Context, tenantID uuid.UUID, sizeMB int) error {
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

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// DecrementStorageUsage decrements storage usage (when files are deleted)
func (s *QuotaService) DecrementStorageUsage(ctx context.Context, tenantID uuid.UUID, sizeMB int) error {
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

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// CheckAPICallQuota checks if tenant can make an API call
func (s *QuotaService) CheckAPICallQuota(ctx context.Context, tenantID uuid.UUID) error {
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
func (s *QuotaService) IncrementAPICallUsage(ctx context.Context, tenantID uuid.UUID) error {
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

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// GetQuotaUsage returns current quota usage for a tenant
func (s *QuotaService) GetQuotaUsage(ctx context.Context, tenantID uuid.UUID) (*storage.TenantQuota, error) {
	return s.GetOrCreateQuota(ctx, tenantID)
}

// CheckAndIncrementAPICallQuota checks tenant API call quota and increments the counter.
// Note: This charges on request entry (counts API requests regardless of outcome). This
// behavior is intentional for Brancr; document and monitor accordingly.
func (s *QuotaService) CheckAndIncrementAPICallQuota(ctx context.Context, tenantID uuid.UUID) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// 0 means unlimited
	if quota.APICallsLimit > 0 && quota.APICallsUsed+1 > quota.APICallsLimit {
		return fmt.Errorf("%w: API calls limit (%d) exceeded", ErrQuotaExceeded, quota.APICallsLimit)
	}

	// Fail-open on increment: we prefer to allow API to proceed if accounting increment fails.
	updates := map[string]interface{}{
		"api_calls_used": gorm.Expr("api_calls_used + ?", 1),
	}
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		s.Logger.Warn("failed to increment api_calls_used (fail-open)", zap.Stringer("tenant_id", tenantID), zap.Error(err))
		// Invalidate cache to avoid stale reads
		if s.Cache != nil {
			s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
		}
		return nil
	}

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// CheckAndIncrementStorageUsage checks storage quota for a tenant and increments used storage (in MB).
// This operation is fail-closed: if accounting cannot be performed, the operation should be blocked.
func (s *QuotaService) CheckAndIncrementStorageUsage(ctx context.Context, tenantID uuid.UUID, bytes int64) error {
	quota, err := s.GetOrCreateQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// Convert bytes to MB (ceil)
	const mb = 1024 * 1024
	usedMB := int((bytes + int64(mb) - 1) / int64(mb))

	// Check before incrementing
	if quota.StorageLimitMB > 0 && (quota.StorageUsedMB+usedMB) > quota.StorageLimitMB {
		return fmt.Errorf("%w: storage limit (%d MB) would be exceeded", ErrQuotaExceeded, quota.StorageLimitMB)
	}

	// Increment and fail if accounting fails (fail-closed)
	updates := map[string]interface{}{
		"storage_used_mb": gorm.Expr("storage_used_mb + ?", usedMB),
	}
	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to increment storage usage: %w", err)
	}

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	return nil
}

// UpdateQuotaLimits updates quota limits for a tenant (admin function)
// limits can contain: ai_calls_limit, posts_limit, storage_limit_mb, api_calls_limit (all int)
// costLimit is a float64 for cost limit in USD (0 = unlimited)
// planType is a separate string parameter
func (s *QuotaService) UpdateQuotaLimits(ctx context.Context, tenantID uuid.UUID, limits map[string]int, costLimit float64, planType string) error {
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
	if val, ok := limits["platforms_limit"]; ok {
		updates["platforms_limit"] = val
	}
	if costLimit >= 0 { // Allow 0 to set unlimited
		updates["cost_limit"] = costLimit
	}
	if planType != "" {
		updates["plan_type"] = planType
	}

	if len(updates) == 0 {
		return nil
	}

	if err := s.DB.WithContext(ctx).Model(quota).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update quota limits: %w", err)
	}

	// Invalidate cache after update
	if s.Cache != nil {
		s.Cache.Delete(ctx, cache.QuotaKey(tenantID))
	}

	s.Logger.Info("updated quota limits for tenant",
		zap.Stringer("tenant_id", tenantID),
		zap.Any("limits", limits),
		zap.String("plan_type", planType))

	return nil
}
