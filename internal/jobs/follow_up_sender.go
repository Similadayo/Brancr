package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageSender interface for sending system messages
type MessageSender interface {
	SendFollowUp(ctx context.Context, tenantID, customerID uuid.UUID, content string) error
}

// FollowUpSender worker that processes scheduled follow-ups
type FollowUpSender struct {
	DB     *gorm.DB
	Logger *zap.Logger
	Sender MessageSender
}

// NewFollowUpSender creates a new worker
func NewFollowUpSender(db *gorm.DB, logger *zap.Logger, sender MessageSender) *FollowUpSender {
	return &FollowUpSender{
		DB:     db,
		Logger: logger,
		Sender: sender,
	}
}

// Run starts the worker loop
func (s *FollowUpSender) Run(ctx context.Context) {
	s.Logger.Info("🚀 FollowUpSender worker started")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Logger.Info("🛑 FollowUpSender worker stopping")
			return
		case <-ticker.C:
			s.processPending(ctx)
		}
	}
}

// processPending finds and sends pending follow-ups
func (s *FollowUpSender) processPending(ctx context.Context) {
	var pending []storage.FollowUp

	// Find pending follow-ups scheduled for now or in the past
	if err := s.DB.Where("status = ? AND scheduled_at <= ?", "pending", time.Now()).Find(&pending).Error; err != nil {
		s.Logger.Error("failed to fetch pending follow-ups", zap.Error(err))
		return
	}

	if len(pending) > 0 {
		s.Logger.Info("Found pending follow-ups", zap.Int("count", len(pending)))
	}

	for _, fu := range pending {
		// Prepare message
		// For now, use a generic friendly check-in.
		// Future: Context-aware message generation via AI
		msg := "Hi! Just checking in to see if you're still interested? Let me know if you have any questions!"
		if fu.Type == "reminder" {
			msg = "Hi! Just wanted to follow up on our last conversation. Is there anything else I can help you with?"
		}

		if err := s.Sender.SendFollowUp(ctx, fu.TenantID, fu.CustomerID, msg); err != nil {
			s.Logger.Error("failed to send follow-up",
				zap.Error(err),
				zap.Stringer("follow_up_id", fu.ID))

			// If failed, mark as failed or retry?
			// For now, mark failed to avoid infinite loop
			s.DB.Model(&fu).Update("status", "failed")
			continue
		}

		// Mark sent
		s.DB.Model(&fu).Update("status", "sent")
		s.Logger.Info("✅ Sent follow-up", zap.Stringer("customer_id", fu.CustomerID))
	}
}
