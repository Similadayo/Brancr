package runner

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/catalog"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// executeProposedActions takes Python's proposed actions and marks them for execution
// This is the bridge between Python brain and action execution
func (r *FlowRunner) executeProposedActions(
	ctx context.Context,
	actions []types.ProposedAction,
	scope types.MemoryScope,
	snapshot *v4ctx.ContextSnapshot,
	knowledge *v4ctx.TenantKnowledgeSnapshot,
	orderSnapshot *v4ctx.OrderSnapshot,
	convSnapshot *v4ctx.ConversationSnapshot,
) error {
	_ = ctx
	_ = snapshot
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	for _, proposedAction := range actions {
		switch proposedAction.Type {
		case "send_offerings":
			// Log that we need to send offerings
			// The actual execution happens in the message handler
			// which will see the ProposedActions in the response
			r.deps.Logger.Info("action_proposed_send_offerings",
				zap.String("type", string(proposedAction.Type)),
				zap.Float64("confidence", proposedAction.Confidence))

		case "request_payment":
			r.deps.Logger.Info("action_proposed_payment",
				zap.String("type", string(proposedAction.Type)))
			if orderSnapshot != nil {
				orderSnapshot.Stage = "payment_ready"
				orderSnapshot.HasActiveOrder = len(orderSnapshot.Items) > 0
			}

		case "initialize_order":
			if orderSnapshot == nil {
				recordErr(fmt.Errorf("initialize_order: nil order snapshot"))
				break
			}
			orderSnapshot.HasActiveOrder = true
			if strings.TrimSpace(orderSnapshot.Stage) == "" {
				orderSnapshot.Stage = "collecting"
			}
			if convSnapshot != nil {
				convSnapshot.ConversationMode = "ordering"
			}

		case "add_to_order":
			if err := applyOrderItemsFromAction(proposedAction.Payload, knowledge, orderSnapshot, false); err != nil {
				r.deps.Logger.Warn("action_add_to_order_failed", zap.Error(err))
				recordErr(err)
			} else if convSnapshot != nil {
				convSnapshot.ConversationMode = "ordering"
			}

		case "update_order":
			if err := applyOrderItemsFromAction(proposedAction.Payload, knowledge, orderSnapshot, true); err != nil {
				r.deps.Logger.Warn("action_update_order_failed", zap.Error(err))
				recordErr(err)
			} else if convSnapshot != nil {
				convSnapshot.ConversationMode = "ordering"
			}

		case "confirm_order":
			if orderSnapshot != nil {
				if len(orderSnapshot.Items) > 0 {
					orderSnapshot.HasActiveOrder = true
					if !isActiveOrderFlowStage(orderSnapshot.Stage) {
						orderSnapshot.Stage = "collecting"
					}
				}
			}
			if convSnapshot != nil && orderSnapshot != nil && len(orderSnapshot.Items) > 0 {
				convSnapshot.ConversationMode = "ordering"
			}

		case "cancel_order":
			if orderSnapshot != nil {
				orderSnapshot.HasActiveOrder = false
				orderSnapshot.Items = nil
				orderSnapshot.TotalPrice = 0
				orderSnapshot.Stage = ""
				orderSnapshot.ActiveItemID = ""
				orderSnapshot.ActiveVariantID = ""
			}
			if convSnapshot != nil {
				convSnapshot.ConversationMode = "general"
			}

		case "escalate":
			if convSnapshot != nil {
				convSnapshot.ConversationMode = "support"
			}
			if r.deps.DB != nil && scope.ConversationID != uuid.Nil {
				var sc storage.SalesConversation
				err := r.deps.DB.Where("conversation_id = ? AND tenant_id = ?", scope.ConversationID, scope.TenantID).First(&sc).Error
				if err == nil {
					_ = r.deps.DB.Model(&storage.SalesConversation{}).
						Where("id = ?", sc.ID).
						Updates(map[string]any{
							"escalation_pending": true,
							"stage":              "escalation",
							"last_intent":        "human_request",
						}).Error
				}
			}

		default:
			r.deps.Logger.Warn("unknown_action_type",
				zap.String("type", string(proposedAction.Type)))
		}
	}

	return firstErr
}

func toInt(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(math.Round(float64(v)))
	case float64:
		return int(math.Round(v))
	default:
		return fallback
	}
}

func applyOrderItemsFromAction(payload map[string]any, knowledge *v4ctx.TenantKnowledgeSnapshot, orderSnapshot *v4ctx.OrderSnapshot, replace bool) error {
	if orderSnapshot == nil {
		return fmt.Errorf("nil order snapshot")
	}
	rawItems, ok := payload["items"]
	if !ok {
		return fmt.Errorf("missing items payload")
	}
	itemsAny, ok := rawItems.([]any)
	if !ok {
		return fmt.Errorf("invalid items payload type")
	}

	next := make([]v4ctx.OrderItem, 0, len(itemsAny))
	if !replace {
		next = append(next, orderSnapshot.Items...)
	}
	for _, raw := range itemsAny {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		qty := toInt(m["quantity"], 1)
		if qty <= 0 {
			qty = 1
		}
		unitPrice := int64(toInt(m["unit_price"], 0))
		matched := catalog.ResolveLongestMatch(name, knowledge)
		if matched != nil {
			name = matched.Name
			if unitPrice <= 0 {
				unitPrice = int64(matched.Price)
			}
		}
		if unitPrice < 0 {
			unitPrice = 0
		}
		next = append(next, v4ctx.OrderItem{
			Name:      strings.TrimSpace(name),
			Quantity:  qty,
			UnitPrice: unitPrice,
		})
	}
	if len(next) == 0 {
		return fmt.Errorf("no valid items from action payload")
	}

	orderSnapshot.Items = next
	orderSnapshot.HasActiveOrder = true
	orderSnapshot.Stage = "collecting"
	orderSnapshot.TotalPrice = computeOrderTotalFromSnapshotItems(orderSnapshot.Items)
	if strings.TrimSpace(orderSnapshot.Currency) == "" {
		orderSnapshot.Currency = "NGN"
	}
	if len(orderSnapshot.Items) > 0 {
		applyActiveSKULock(orderSnapshot, orderSnapshot.Items[0].Name, orderSnapshot.Items[0].UnitPrice)
	}
	return nil
}
