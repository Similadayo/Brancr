package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// QualitativeInsight represents a manual note, complaint, or risk flag
type QualitativeInsight struct {
	ID             uuid.UUID  `json:"id"`
	InsightType    string     `json:"insight_type"` // 'complaint', 'aha_moment', 'objection', 'risk_flag', 'onboarding_note'
	TenantID       *uuid.UUID `json:"tenant_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	Content        string     `json:"content"`
	AdminEmail     string     `json:"admin_email"`
	AdminRole      string     `json:"admin_role"`
	Tags           []string   `json:"tags"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// InsightsService handles CRUD operations for qualitative insights
type InsightsService struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewInsightsService creates a new insights service
func NewInsightsService(db *gorm.DB, logger *zap.Logger) *InsightsService {
	return &InsightsService{
		DB:     db,
		Logger: logger,
	}
}

// CreateInsight creates a new qualitative insight
func (s *InsightsService) CreateInsight(ctx context.Context, insight *QualitativeInsight) error {
	dbInsight := storage.AdminQualitativeInsight{
		InsightType:    insight.InsightType,
		TenantID:       insight.TenantID,
		ConversationID: insight.ConversationID,
		Content:        insight.Content,
		AdminEmail:     insight.AdminEmail,
		AdminRole:      insight.AdminRole,
		Tags:           convertTagsToStringArray(insight.Tags),
	}

	if err := s.DB.WithContext(ctx).Create(&dbInsight).Error; err != nil {
		return fmt.Errorf("failed to create insight: %w", err)
	}

	insight.ID = dbInsight.ID
	insight.CreatedAt = dbInsight.CreatedAt
	insight.UpdatedAt = dbInsight.UpdatedAt
	return nil
}

// GetInsightsByType retrieves insights filtered by type
func (s *InsightsService) GetInsightsByType(ctx context.Context, insightType string, limit int) ([]QualitativeInsight, error) {
	if limit <= 0 {
		limit = 10
	}

	var dbInsights []storage.AdminQualitativeInsight
	query := s.DB.WithContext(ctx).
		Where("insight_type = ?", insightType).
		Order("created_at DESC").
		Limit(limit)

	if err := query.Find(&dbInsights).Error; err != nil {
		return nil, fmt.Errorf("failed to get insights: %w", err)
	}

	insights := make([]QualitativeInsight, 0, len(dbInsights))
	for _, db := range dbInsights {
		insights = append(insights, QualitativeInsight{
			ID:             db.ID,
			InsightType:    db.InsightType,
			TenantID:       db.TenantID,
			ConversationID: db.ConversationID,
			Content:        db.Content,
			AdminEmail:     db.AdminEmail,
			AdminRole:      db.AdminRole,
			Tags:           parseTagsFromStringArray(db.Tags),
			CreatedAt:      db.CreatedAt,
			UpdatedAt:      db.UpdatedAt,
		})
	}

	return insights, nil
}

// GetInsightsByTenant retrieves insights for a specific tenant
func (s *InsightsService) GetInsightsByTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]QualitativeInsight, error) {
	if limit <= 0 {
		limit = 20
	}

	var dbInsights []storage.AdminQualitativeInsight
	query := s.DB.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Limit(limit)

	if err := query.Find(&dbInsights).Error; err != nil {
		return nil, fmt.Errorf("failed to get tenant insights: %w", err)
	}

	insights := make([]QualitativeInsight, 0, len(dbInsights))
	for _, db := range dbInsights {
		insights = append(insights, QualitativeInsight{
			ID:             db.ID,
			InsightType:    db.InsightType,
			TenantID:       db.TenantID,
			ConversationID: db.ConversationID,
			Content:        db.Content,
			AdminEmail:     db.AdminEmail,
			AdminRole:      db.AdminRole,
			Tags:           parseTagsFromStringArray(db.Tags),
			CreatedAt:      db.CreatedAt,
			UpdatedAt:      db.UpdatedAt,
		})
	}

	return insights, nil
}

// UpdateInsight updates an existing insight
func (s *InsightsService) UpdateInsight(ctx context.Context, insightID uuid.UUID, updates map[string]interface{}) error {
	updates["updated_at"] = time.Now()

	if tags, ok := updates["tags"]; ok {
		if tagSlice, ok := tags.([]string); ok {
			// Convert []string to pq.StringArray for GORM
			updates["tags"] = convertTagsToStringArray(tagSlice)
		}
	}

	if err := s.DB.WithContext(ctx).
		Model(&storage.AdminQualitativeInsight{}).
		Where("id = ?", insightID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update insight: %w", err)
	}

	return nil
}

// DeleteInsight deletes an insight (soft delete or hard delete based on requirements)
func (s *InsightsService) DeleteInsight(ctx context.Context, insightID uuid.UUID) error {
	if err := s.DB.WithContext(ctx).
		Delete(&storage.AdminQualitativeInsight{}, insightID).Error; err != nil {
		return fmt.Errorf("failed to delete insight: %w", err)
	}

	return nil
}

// Helper functions for tag conversion
func convertTagsToStringArray(tags []string) pq.StringArray {
	if tags == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(tags)
}

func parseTagsFromStringArray(sa pq.StringArray) []string {
	if sa == nil {
		return []string{}
	}
	return []string(sa)
}
