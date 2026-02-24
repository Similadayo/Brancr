package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MetricsAggregator pre-computes metrics for fast dashboard queries
type MetricsAggregator struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewMetricsAggregator creates a new metrics aggregator
func NewMetricsAggregator(db *gorm.DB, logger *zap.Logger) *MetricsAggregator {
	return &MetricsAggregator{
		DB:     db,
		Logger: logger,
	}
}

// RunHourlyAggregation aggregates metrics for the specified hour
func (m *MetricsAggregator) RunHourlyAggregation(ctx context.Context, hourStart time.Time) error {
	hourStart = hourStart.Truncate(time.Hour)
	hourEnd := hourStart.Add(time.Hour)

	m.Logger.Info("starting hourly metrics aggregation", zap.Time("hour_start", hourStart))

	// Aggregate messaging metrics
	if err := m.aggregateMessagingMetrics(ctx, hourStart, hourEnd); err != nil {
		m.Logger.Error("failed to aggregate messaging metrics", zap.Error(err))
		// Continue with other aggregations
	}

	// Aggregate AI behavior metrics
	if err := m.aggregateAIBehaviorMetrics(ctx, hourStart, hourEnd); err != nil {
		m.Logger.Error("failed to aggregate AI behavior metrics", zap.Error(err))
	}

	// Aggregate platform health metrics
	if err := m.aggregatePlatformHealthMetrics(ctx, hourStart, hourEnd); err != nil {
		m.Logger.Error("failed to aggregate platform health metrics", zap.Error(err))
	}

	m.Logger.Info("completed hourly metrics aggregation", zap.Time("hour_start", hourStart))
	return nil
}

// RunDailyAggregation aggregates metrics for the specified day
func (m *MetricsAggregator) RunDailyAggregation(ctx context.Context, date time.Time) error {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	m.Logger.Info("starting daily metrics aggregation", zap.Time("date", dayStart))

	// Aggregate cost metrics
	if err := m.aggregateCostMetrics(ctx, dayStart, dayEnd); err != nil {
		m.Logger.Error("failed to aggregate cost metrics", zap.Error(err))
	}

	// Aggregate tenant activity metrics
	if err := m.aggregateTenantActivityMetrics(ctx, dayStart, dayEnd); err != nil {
		m.Logger.Error("failed to aggregate tenant activity metrics", zap.Error(err))
	}

	m.Logger.Info("completed daily metrics aggregation", zap.Time("date", dayStart))
	return nil
}

// aggregateMessagingMetrics aggregates messaging-related metrics
func (m *MetricsAggregator) aggregateMessagingMetrics(ctx context.Context, start, end time.Time) error {
	// Messages received per platform
	var platformCounts []struct {
		Platform string
		Count    int64
	}
	if err := m.DB.WithContext(ctx).
		Model(&storage.Interaction{}).
		Select("platform, COUNT(*) as count").
		Where("created_at >= ? AND created_at < ? AND direction = ?", start, end, "inbound").
		Group("platform").
		Scan(&platformCounts).Error; err != nil {
		return fmt.Errorf("failed to count messages by platform: %w", err)
	}

	for _, pc := range platformCounts {
		if err := m.saveHourlyMetric(ctx, "messaging", nil, fmt.Sprintf("messages_received_%s", pc.Platform), float64(pc.Count), "", start); err != nil {
			return err
		}
	}

	// Messages sent (successful)
	var sentCount int64
	if err := m.DB.WithContext(ctx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND created_at < ? AND direction = ?", start, end, "outbound").
		Count(&sentCount).Error; err != nil {
		return fmt.Errorf("failed to count messages sent: %w", err)
	}
	if err := m.saveHourlyMetric(ctx, "messaging", nil, "messages_sent", float64(sentCount), "", start); err != nil {
		return err
	}

	return nil
}

// aggregateAIBehaviorMetrics aggregates AI behavior metrics
func (m *MetricsAggregator) aggregateAIBehaviorMetrics(ctx context.Context, start, end time.Time) error {
	// AI replies sent
	var aiRepliesCount int64
	if err := m.DB.WithContext(ctx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND created_at < ? AND response_type = ?", start, end, "auto_reply").
		Count(&aiRepliesCount).Error; err != nil {
		return fmt.Errorf("failed to count AI replies: %w", err)
	}
	if err := m.saveHourlyMetric(ctx, "ai_behavior", nil, "ai_replies_sent", float64(aiRepliesCount), "", start); err != nil {
		return err
	}

	// Escalations triggered - count from sales_conversations where support.escalated = true
	var escalationsCount int64
	if err := m.DB.WithContext(ctx).
		Table("sales_conversations").
		Where("updated_at >= ? AND updated_at < ? AND objections_raised::jsonb->'support'->>'escalated' = 'true'", start, end).
		Count(&escalationsCount).Error; err != nil {
		m.Logger.Warn("failed to count escalations (may be due to JSONB structure)", zap.Error(err))
		// Don't fail - this is a new metric
	} else {
		if err := m.saveHourlyMetric(ctx, "ai_behavior", nil, "escalations_triggered", float64(escalationsCount), "", start); err != nil {
			return err
		}
	}

	return nil
}

// aggregatePlatformHealthMetrics aggregates platform health metrics
func (m *MetricsAggregator) aggregatePlatformHealthMetrics(ctx context.Context, start, end time.Time) error {
	// This will be expanded with actual platform-specific error tracking
	// For now, we'll track basic interaction counts per platform

	platforms := []string{"instagram", "facebook", "whatsapp", "tiktok"}
	for _, platform := range platforms {
		var count int64
		if err := m.DB.WithContext(ctx).
			Model(&storage.Interaction{}).
			Where("created_at >= ? AND created_at < ? AND platform = ?", start, end, platform).
			Count(&count).Error; err != nil {
			continue // Log but don't fail
		}

		if err := m.saveHourlyMetric(ctx, "platform_health", nil, fmt.Sprintf("%s_activity", platform), float64(count), `{"platform":"`+platform+`"}`, start); err != nil {
			m.Logger.Warn("failed to save platform health metric", zap.String("platform", platform), zap.Error(err))
		}
	}

	return nil
}

// aggregateCostMetrics aggregates cost-related metrics
func (m *MetricsAggregator) aggregateCostMetrics(ctx context.Context, start, end time.Time) error {
	// Aggregate cost per tenant
	var tenantCosts []struct {
		TenantID uuid.UUID
		CostUsed float64
	}

	if err := m.DB.WithContext(ctx).
		Model(&storage.TenantQuota{}).
		Select("tenant_id, cost_used").
		Where("updated_at >= ? AND updated_at < ? AND cost_used > 0", start, end).
		Scan(&tenantCosts).Error; err != nil {
		return fmt.Errorf("failed to aggregate tenant costs: %w", err)
	}

	for _, tc := range tenantCosts {
		tenantID := tc.TenantID
		if err := m.saveDailyMetric(ctx, "cost", &tenantID, "cost_usd", tc.CostUsed, "", start); err != nil {
			m.Logger.Warn("failed to save tenant cost metric", zap.Stringer("tenant_id", tc.TenantID), zap.Error(err))
		}
	}

	// Total platform cost
	var totalCost float64
	if err := m.DB.WithContext(ctx).
		Model(&storage.TenantQuota{}).
		Select("COALESCE(SUM(cost_used), 0)").
		Where("updated_at >= ? AND updated_at < ?", start, end).
		Scan(&totalCost).Error; err != nil {
		return fmt.Errorf("failed to calculate total cost: %w", err)
	}

	if err := m.saveDailyMetric(ctx, "cost", nil, "total_cost_usd", totalCost, "", start); err != nil {
		return err
	}

	return nil
}

// aggregateTenantActivityMetrics aggregates tenant activity metrics
func (m *MetricsAggregator) aggregateTenantActivityMetrics(ctx context.Context, start, end time.Time) error {
	// Active tenants (tenants with activity in period)
	var activeTenants int64
	if err := m.DB.WithContext(ctx).
		Model(&storage.Interaction{}).
		Distinct("tenant_id").
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&activeTenants).Error; err != nil {
		return fmt.Errorf("failed to count active tenants: %w", err)
	}

	if err := m.saveDailyMetric(ctx, "tenant_activity", nil, "active_tenants", float64(activeTenants), "", start); err != nil {
		return err
	}

	return nil
}

// saveHourlyMetric saves or updates an hourly metric
func (m *MetricsAggregator) saveHourlyMetric(ctx context.Context, metricType string, tenantID *uuid.UUID, metricKey string, value float64, metadata string, hourStart time.Time) error {
	if metadata == "" {
		metadata = "{}"
	}

	metric := storage.AdminMetricHourly{
		MetricType:  metricType,
		TenantID:    tenantID,
		MetricKey:   metricKey,
		MetricValue: value,
		Metadata:    metadata,
		HourStart:   hourStart,
	}

	// Upsert: Check if record exists, then update or insert
	// This works reliably with NULL tenant_id values
	var existing storage.AdminMetricHourly
	query := m.DB.WithContext(ctx).
		Where("metric_type = ? AND metric_key = ? AND hour_start = ?", metric.MetricType, metric.MetricKey, metric.HourStart)

	if metric.TenantID != nil {
		query = query.Where("tenant_id = ?", *metric.TenantID)
	} else {
		query = query.Where("tenant_id IS NULL")
	}

	err := query.First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// Insert new record
		err = m.DB.WithContext(ctx).Create(&metric).Error
	} else if err == nil {
		// Update existing record
		existing.MetricValue = metric.MetricValue
		existing.Metadata = metric.Metadata
		existing.UpdatedAt = time.Now()
		err = m.DB.WithContext(ctx).Save(&existing).Error
	}

	return err
}

// saveDailyMetric saves or updates a daily metric
func (m *MetricsAggregator) saveDailyMetric(ctx context.Context, metricType string, tenantID *uuid.UUID, metricKey string, value float64, metadata string, date time.Time) error {
	if metadata == "" {
		metadata = "{}"
	}

	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())

	metric := storage.AdminMetricDaily{
		MetricType:  metricType,
		TenantID:    tenantID,
		MetricKey:   metricKey,
		MetricValue: value,
		Metadata:    metadata,
		Date:        dateOnly,
	}

	// Upsert: Check if record exists, then update or insert
	// This works reliably with NULL tenant_id values
	var existing storage.AdminMetricDaily
	query := m.DB.WithContext(ctx).
		Where("metric_type = ? AND metric_key = ? AND date = ?", metric.MetricType, metric.MetricKey, metric.Date)

	if metric.TenantID != nil {
		query = query.Where("tenant_id = ?", *metric.TenantID)
	} else {
		query = query.Where("tenant_id IS NULL")
	}

	err := query.First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// Insert new record
		err = m.DB.WithContext(ctx).Create(&metric).Error
	} else if err == nil {
		// Update existing record
		existing.MetricValue = metric.MetricValue
		existing.Metadata = metric.Metadata
		existing.UpdatedAt = time.Now()
		err = m.DB.WithContext(ctx).Save(&existing).Error
	}

	return err
}
