package ai

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// RecordTenantEditFeedback stores a high-signal correction event when a tenant sends a manual reply
// that differs from the AI SuggestedReply.
//
// This is the backbone of the "system learns, not the model" feedback loop.
func RecordTenantEditFeedback(ctx context.Context, db *gorm.DB, interactionID uuid.UUID, finalReply string) error {
	if db == nil || interactionID == uuid.Nil {
		return nil
	}
	finalReply = strings.TrimSpace(finalReply)
	if finalReply == "" {
		return nil
	}

	var inter storage.Interaction
	if err := db.WithContext(ctx).First(&inter, interactionID).Error; err != nil {
		return nil
	}

	suggested := strings.TrimSpace(inter.SuggestedReply)
	if suggested == "" || suggested == finalReply {
		return nil
	}

	// Pull decision state from interaction metadata if present.
	decisionState := ""
	decisionMeta := map[string]interface{}{}
	if strings.TrimSpace(inter.Metadata) != "" {
		var meta map[string]interface{}
		_ = json.Unmarshal([]byte(inter.Metadata), &meta)
		if st, ok := meta["staff_state"].(map[string]interface{}); ok {
			if s, ok := st["state"].(string); ok {
				decisionState = s
			}
			decisionMeta["staff_state"] = st
		}
	}
	decisionMetaBytes, _ := json.Marshal(decisionMeta)

	ev := storage.AIFeedbackEvent{
		TenantID:       inter.TenantID,
		CustomerID:     inter.CustomerID,
		ConversationID: inter.ConversationID,
		InteractionID:  inter.ID,
		Type:           "tenant_edit",
		SuggestedReply: suggested,
		FinalReply:     finalReply,
		DecisionState:  decisionState,
		DecisionMeta:   string(decisionMetaBytes),
	}
	_ = db.WithContext(ctx).Create(&ev).Error

	// Mark in interaction metadata for quick aggregation.
	var meta map[string]interface{}
	_ = json.Unmarshal([]byte(inter.Metadata), &meta)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["tenant_edited"] = true
	metaBytes, _ := json.Marshal(meta)
	_ = db.WithContext(ctx).Model(&storage.Interaction{}).Where("id = ?", inter.ID).Update("metadata", string(metaBytes)).Error

	return nil
}
