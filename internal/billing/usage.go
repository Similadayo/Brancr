package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// UsageService handles usage metering and aggregation
type UsageService struct {
	DB               *gorm.DB
	Logger           *zap.Logger
	UsageSyncService *GupshupUsageSyncService // Optional: for getting actual costs
}

// NewUsageService creates a new usage service
func NewUsageService(db *gorm.DB, logger *zap.Logger) *UsageService {
	return &UsageService{
		DB:     db,
		Logger: logger,
	}
}

// RecordUsageEvent records a usage event for metering
func (s *UsageService) RecordUsageEvent(ctx context.Context, tenantID uuid.UUID, eventType string, quantity int, estimatedCost float64, metadata map[string]interface{}) error {
	event := &storage.UsageEvent{
		TenantID:      tenantID,
		EventType:     eventType,
		Quantity:      quantity,
		EstimatedCost: estimatedCost,
		EventDate:     time.Now(),
	}

	// Serialize metadata
	if metadata != nil {
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			s.Logger.Warn("failed to marshal usage event metadata",
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
		} else {
			event.Metadata = string(metadataJSON)
		}
	}

	if err := s.DB.WithContext(ctx).Create(event).Error; err != nil {
		return fmt.Errorf("failed to record usage event: %w", err)
	}

	// Trigger async aggregation (don't block)
	go s.aggregateUsage(context.Background(), tenantID, time.Now())

	return nil
}

// RecordAIReply records an AI reply usage event
func (s *UsageService) RecordAIReply(ctx context.Context, tenantID uuid.UUID, tokensUsed int, estimatedCost float64, modelName string) error {
	// 1. Record reply event (Cost is attributed here)
	metadata := map[string]interface{}{
		"model":       modelName,
		"tokens_used": tokensUsed,
	}
	if err := s.RecordUsageEvent(ctx, tenantID, "ai_reply_sent", 1, estimatedCost, metadata); err != nil {
		return err
	}

	// 2. Record token usage event (Cost is 0 to avoid double billing)
	// This enables precise token counts in aggregations and dashboard
	return s.RecordUsageEvent(ctx, tenantID, "ai_tokens_used", tokensUsed, 0, map[string]interface{}{"model": modelName})
}

// RecordAITokens records AI token usage
func (s *UsageService) RecordAITokens(ctx context.Context, tenantID uuid.UUID, tokensUsed int, estimatedCost float64, modelName string) error {
	metadata := map[string]interface{}{
		"model": modelName,
	}
	return s.RecordUsageEvent(ctx, tenantID, "ai_tokens_used", tokensUsed, estimatedCost, metadata)
}

// RecordWhatsAppMessage records a WhatsApp message usage event
// Cost: $0.0075 per message for Gupshup Partner (WhatsApp Business Platform pricing)
// This matches the cost calculation in the admin cost dashboard
func (s *UsageService) RecordWhatsAppMessage(ctx context.Context, tenantID uuid.UUID) error {
	// Gupshup Partner WhatsApp message cost: $0.0075 per message
	// This is the standard WhatsApp Business Platform pricing
	whatsappCostPerMessage := 0.0075
	return s.RecordUsageEvent(ctx, tenantID, "whatsapp_message_sent", 1, whatsappCostPerMessage, nil)
}

// RecordPostPublished records a post publication usage event
func (s *UsageService) RecordPostPublished(ctx context.Context, tenantID uuid.UUID, platform string) error {
	metadata := map[string]interface{}{
		"platform": platform,
	}
	return s.RecordUsageEvent(ctx, tenantID, "post_published", 1, 0, metadata)
}

// GetUsageForPeriod gets aggregated usage for a tenant in a specific period
func (s *UsageService) GetUsageForPeriod(ctx context.Context, tenantID uuid.UUID, periodType string, periodStart time.Time) (*storage.UsageAggregation, error) {
	var agg storage.UsageAggregation
	err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND period_type = ? AND period_start = ?", tenantID, periodType, periodStart).
		First(&agg).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Return zero usage if not found
		return &storage.UsageAggregation{
			TenantID:             tenantID,
			PeriodType:           periodType,
			PeriodStart:          periodStart,
			PeriodEnd:            s.calculatePeriodEnd(periodType, periodStart),
			AIReplyCount:         0,
			AITokensUsed:         0,
			WhatsAppMessageCount: 0,
			PostCount:            0,
			TotalCostUSD:         0,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get usage aggregation: %w", err)
	}

	return &agg, nil
}

// GetCurrentMonthUsage gets usage for the current month
func (s *UsageService) GetCurrentMonthUsage(ctx context.Context, tenantID uuid.UUID) (*storage.UsageAggregation, error) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return s.GetUsageForPeriod(ctx, tenantID, "monthly", monthStart)
}

// GetCurrentDayUsage gets usage for the current day
func (s *UsageService) GetCurrentDayUsage(ctx context.Context, tenantID uuid.UUID) (*storage.UsageAggregation, error) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return s.GetUsageForPeriod(ctx, tenantID, "daily", dayStart)
}

// aggregateUsage aggregates usage events into daily/monthly aggregations
func (s *UsageService) aggregateUsage(ctx context.Context, tenantID uuid.UUID, eventDate time.Time) {
	// Aggregate daily
	dayStart := time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), 0, 0, 0, 0, eventDate.Location())
	if err := s.aggregatePeriod(ctx, tenantID, "daily", dayStart); err != nil {
		s.Logger.Warn("failed to aggregate daily usage",
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
	}

	// Aggregate monthly
	monthStart := time.Date(eventDate.Year(), eventDate.Month(), 1, 0, 0, 0, 0, eventDate.Location())
	if err := s.aggregatePeriod(ctx, tenantID, "monthly", monthStart); err != nil {
		s.Logger.Warn("failed to aggregate monthly usage",
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
	}
}

// aggregatePeriod aggregates events for a specific period
func (s *UsageService) aggregatePeriod(ctx context.Context, tenantID uuid.UUID, periodType string, periodStart time.Time) error {
	periodEnd := s.calculatePeriodEnd(periodType, periodStart)

	// Query events for this period
	var events []storage.UsageEvent
	if err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND event_date >= ? AND event_date < ?", tenantID, periodStart, periodEnd).
		Find(&events).Error; err != nil {
		return fmt.Errorf("failed to query events: %w", err)
	}

	// Aggregate
	var aiReplyCount, aiTokensUsed, waMessageCount, postCount int
	var totalCost float64

	for _, event := range events {
		switch event.EventType {
		case "ai_reply_sent":
			aiReplyCount += event.Quantity
			totalCost += event.EstimatedCost
		case "ai_tokens_used":
			aiTokensUsed += event.Quantity
			totalCost += event.EstimatedCost
		case "whatsapp_message_sent":
			waMessageCount += event.Quantity
			totalCost += event.EstimatedCost // Include WhatsApp cost in total
		case "post_published":
			postCount += event.Quantity
		}
	}

	// Upsert aggregation
	agg := storage.UsageAggregation{
		TenantID:             tenantID,
		PeriodType:           periodType,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		AIReplyCount:         aiReplyCount,
		AITokensUsed:         aiTokensUsed,
		WhatsAppMessageCount: waMessageCount,
		PostCount:            postCount,
		TotalCostUSD:         totalCost,
	}

	// Use ON CONFLICT to upsert
	if err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND period_type = ? AND period_start = ?", tenantID, periodType, periodStart).
		Assign(agg).
		FirstOrCreate(&agg).Error; err != nil {
		return fmt.Errorf("failed to upsert aggregation: %w", err)
	}

	return nil
}

// calculatePeriodEnd calculates the end of a period
func (s *UsageService) calculatePeriodEnd(periodType string, periodStart time.Time) time.Time {
	switch periodType {
	case "daily":
		return periodStart.Add(24 * time.Hour)
	case "monthly":
		return periodStart.AddDate(0, 1, 0)
	default:
		return periodStart.Add(24 * time.Hour)
	}
}
