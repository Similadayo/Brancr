package checkpoint

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Service handles experience checkpoint creation and responses
type Service struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewService creates a new checkpoint service
func NewService(db *gorm.DB, logger *zap.Logger) *Service {
	return &Service{
		DB:     db,
		Logger: logger,
	}
}

// CheckpointQuestion defines the question and options for a checkpoint
type CheckpointQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// TriggerCheckpoint creates a checkpoint for a specific event
// It checks if a recent checkpoint for the same event type exists to avoid spamming
func (s *Service) TriggerCheckpoint(ctx context.Context, tenantID uuid.UUID, eventType string, eventID uuid.UUID) error {
	// 1. Defend against spam: Check if we recently triggered this type for this tenant
	// e.g., don't ask about "first_reply" twice, or limit "escalation" feedback to once every 24h
	var recentCount int64
	expirationWindow := time.Now().Add(-24 * time.Hour) // Default 24h cooldown per event type

	if eventType == "first_reply" || eventType == "onboarding_48h" {
		// These happen once per life-cycle usually, or very rarely
		expirationWindow = time.Time{} // Check all time
	}

	if err := s.DB.Model(&storage.ExperienceCheckpoint{}).
		Where("tenant_id = ? AND event_type = ? AND created_at > ?", tenantID, eventType, expirationWindow).
		Count(&recentCount).Error; err != nil {
		return err
	}

	if recentCount > 0 {
		// Already asked recently, skip
		s.Logger.Info("skipping checkpoint trigger",
			zap.String("type", eventType),
			zap.Stringer("tenant_id", tenantID),
			zap.String("reason", "recent_checkpoint_exists"))
		return nil
	}

	// 2. Create the checkpoint
	checkpoint := storage.ExperienceCheckpoint{
		TenantID:  tenantID,
		EventType: eventType,
		EventID:   eventID,
		Status:    "pending",
		ExpiresAt: time.Now().Add(72 * time.Hour), // Expire after 3 days
	}

	if err := s.DB.Create(&checkpoint).Error; err != nil {
		s.Logger.Error("failed to create experience checkpoint", zap.Error(err))
		return err
	}

	s.Logger.Info("created experience checkpoint",
		zap.String("type", eventType),
		zap.Stringer("checkpoint_id", checkpoint.ID))

	return nil
}

// HasEverTriggered checks if a specific event type has ever been triggered for a tenant
func (s *Service) HasEverTriggered(ctx context.Context, tenantID uuid.UUID, eventType string) (bool, error) {
	var count int64
	err := s.DB.Model(&storage.ExperienceCheckpoint{}).
		Where("tenant_id = ? AND event_type = ?", tenantID, eventType).
		Count(&count).Error
	return count > 0, err
}

// GetPendingCheckpoints returns pending checkpoints for a tenant
func (s *Service) GetPendingCheckpoints(ctx context.Context, tenantID uuid.UUID) ([]storage.ExperienceCheckpoint, error) {
	var checkpoints []storage.ExperienceCheckpoint
	err := s.DB.Where("tenant_id = ? AND status = ? AND expires_at > ?", tenantID, "pending", time.Now()).
		Order("created_at DESC").
		Find(&checkpoints).Error
	return checkpoints, err
}

// CheckpointResponseInput is the input payload for submitting a response
type CheckpointResponseInput struct {
	SelectedReason string `json:"selected_reason"`
	TrustScore     *int   `json:"trust_score"`
	FreeText       string `json:"free_text"`
}

// SubmitResponse records a tenant's checkpoint response
func (s *Service) SubmitResponse(ctx context.Context, tenantID uuid.UUID, checkpointID uuid.UUID, response *CheckpointResponseInput) error {
	var checkpoint storage.ExperienceCheckpoint
	if err := s.DB.First(&checkpoint, "id = ? AND tenant_id = ?", checkpointID, tenantID).Error; err != nil {
		return err // Not found or not owned by tenant
	}

	if checkpoint.Status != "pending" {
		return errors.New("checkpoint is not pending")
	}

	// Gather context
	var tenant storage.Tenant
	s.DB.First(&tenant, tenantID)

	// Count approximate stats for context
	// (Simplified) queries for context - in real implementation this might be more complex
	// s.DB.Model(&storage.Message{}).Where("tenant_id = ? AND is_ai = ?", tenantID, true).Count(&aiReplies)

	// Save response
	resp := storage.CheckpointResponse{
		CheckpointID:   checkpointID,
		TenantID:       tenantID,
		EventType:      checkpoint.EventType,
		SelectedReason: response.SelectedReason,
		TrustScore:     response.TrustScore,
		FreeText:       response.FreeText,
		IndustryID:     tenant.IndustryID,
		// PlanID: tenant.PlanID (needs logic if PlanID is stored)
		DaysActive: int(time.Since(tenant.CreatedAt).Hours() / 24),
	}

	tx := s.DB.Begin()
	if err := tx.Create(&resp).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Mark checkpoint as responded
	if err := tx.Model(&checkpoint).Update("status", "responded").Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// DismissCheckpoint marks a checkpoint as dismissed
func (s *Service) DismissCheckpoint(ctx context.Context, tenantID uuid.UUID, checkpointID uuid.UUID) error {
	result := s.DB.Model(&storage.ExperienceCheckpoint{}).
		Where("id = ? AND tenant_id = ?", checkpointID, tenantID).
		Updates(map[string]interface{}{
			"status":       "dismissed",
			"dismissed_at": time.Now(),
		})
	return result.Error
}

// GetQuestionsForEventType returns the question and options for an event type
func (s *Service) GetQuestionsForEventType(eventType string) *CheckpointQuestion {
	switch eventType {
	case "first_reply":
		return &CheckpointQuestion{
			Question: "How did you feel about that first AI reply?",
			Options:  []string{"Impressed", "Acceptable", "Concerned", "Didn't check"},
		}
	case "escalation":
		return &CheckpointQuestion{
			Question: "Why did you take over this conversation?",
			Options:  []string{"Didn't trust reply", "Reply was incorrect", "Customer was sensitive", "Wanted control", "Other"},
		}
	case "ai_toggle":
		return &CheckpointQuestion{
			Question: "What made you turn off AI?",
			Options:  []string{"Needs improvement", "Customer complaints", "Just testing", "Temporary pause", "Other"},
		}
	case "onboarding_48h":
		return &CheckpointQuestion{
			Question: "What's the hardest part of using Brancr so far?",
			Options:  []string{"Onboarding", "Trusting AI", "Dashboard clarity", "Setup complexity", "Actually it's great"},
		}
	default:
		return &CheckpointQuestion{
			Question: "We'd love your feedback on this event.",
			Options:  []string{"Positive", "Neutral", "Negative"},
		}
	}
}
