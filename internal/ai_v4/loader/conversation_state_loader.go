package loader

import (
	"context"
	"encoding/json"
	"errors"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// LoadConversationSnapshot loads the latest conversation state from the database.
func LoadConversationSnapshot(
	ctx context.Context,
	db *gorm.DB,
	conversationID string,
) (*v4ctx.ConversationSnapshot, error) {
	snapshot := v4ctx.DefaultConversationSnapshot()

	if conversationID == "" {
		return snapshot, nil
	}

	var conv storage.Conversation
	if err := db.WithContext(ctx).Where("id = ?", conversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// New conversation or not found, return default
			return snapshot, nil
		}
		return nil, err
	}

	// Unmarshal AIState if present
	if conv.AIState != "" && conv.AIState != "{}" {
		if err := json.Unmarshal([]byte(conv.AIState), snapshot); err != nil {
			// Log error but fallback to default to prevent blocking
			// In real app, we might log a warning here
			return snapshot, nil
		}
	}

	// Backward-compatible defaults for older snapshots.
	if snapshot.CloseoutState == "" {
		snapshot.CloseoutState = "open"
	}

	return snapshot, nil
}
