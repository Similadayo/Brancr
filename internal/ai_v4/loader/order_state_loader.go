package loader

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// LoadOrderSnapshot loads the separate order flow state.
// This is critical to prevent the "what would you like to order" loop.
func LoadOrderSnapshot(
	ctx context.Context,
	db *gorm.DB,
	conversationID string,
) (*v4ctx.OrderSnapshot, error) {

	// Default empty snapshot
	snapshot := &v4ctx.OrderSnapshot{
		HasActiveOrder:  false,
		Items:           []v4ctx.OrderItem{},
		Stage:           "collecting",
		TotalPrice:      0,
		Currency:        "NGN",
		SwitchConfirmed: false,
	}

	if conversationID == "" {
		return snapshot, nil
	}

	var conv storage.Conversation
	if err := db.WithContext(ctx).Where("id = ?", conversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return snapshot, nil
		}
		return nil, err
	}

	// Unmarshal AIOrderState if present
	if conv.AIOrderState != "" && conv.AIOrderState != "{}" {
		if err := json.Unmarshal([]byte(conv.AIOrderState), snapshot); err != nil {
			// Log error but fallback
			return snapshot, nil
		}
	}

	// Ensure non-nil slice
	if snapshot.Items == nil {
		snapshot.Items = []v4ctx.OrderItem{}
	}

	// 🔒 CRITICAL FIX: Verify order status against real DB
	// If the snapshot has an OrderID, check if it's actually alive.
	// If it's cancelled/completed, we MUST reset the snapshot to allow a new order.
	if snapshot.OrderID != "" {
		var orderStatus string
		// Check strictly for status
		if err := db.Model(&storage.Order{}).Select("status").Where("id = ?", snapshot.OrderID).Scan(&orderStatus).Error; err == nil {
			// If order is in a terminal state, RESET the snapshot
			// Common terminal states: "cancelled", "completed", "declined", "failed"
			// Common terminal states: "cancelled", "completed", "declined", "failed", "paid", "confirmed"
			terminalStates := []string{"cancelled", "completed", "declined", "failed", "paid", "confirmed"}
			isTerminal := false
			for _, state := range terminalStates {
				if strings.EqualFold(orderStatus, state) {
					isTerminal = true
					break
				}
			}

			if isTerminal {
				// RESET! This order is dead.
				snapshot = &v4ctx.OrderSnapshot{
					HasActiveOrder:  false,
					Items:           []v4ctx.OrderItem{},
					Stage:           "collecting", // Ready for new order
					TotalPrice:      0,
					Currency:        "NGN",
					OrderID:         "", // Clear ID so new one is created
					SwitchConfirmed: false,
				}
			}
		}
	} else {
		// No OrderID but has stage "cancelled"? Reset it.
		// This handles cases where we just set stage="cancelled" in memory but didn't permit a save yet
		if snapshot.Stage == "cancelled" || snapshot.Stage == "completed" {
			snapshot = &v4ctx.OrderSnapshot{
				HasActiveOrder:  false,
				Items:           []v4ctx.OrderItem{},
				Stage:           "collecting",
				TotalPrice:      0,
				Currency:        "NGN",
				OrderID:         "",
				SwitchConfirmed: false,
			}
		}
	}

	// 🔒 SAFETY: Derive active order from items (only if not reset)
	if len(snapshot.Items) > 0 {
		snapshot.HasActiveOrder = true
	}

	return snapshot, nil
}
