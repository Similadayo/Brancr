package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// MemoryStore defines the interface for persisting memory.
type MemoryStore interface {
	LoadConversation(ctx context.Context, scope types.MemoryScope) (*ConversationMemory, error)
	SaveConversation(ctx context.Context, scope types.MemoryScope, mem *ConversationMemory) error

	LoadOperational(ctx context.Context, scope types.MemoryScope) (*OperationalMemory, error)
	SaveOperational(ctx context.Context, scope types.MemoryScope, mem *OperationalMemory) error
}

// DBMemoryStore implements MemoryStore using a database.
type DBMemoryStore struct {
	db *gorm.DB
}

type persistedConversationMemory struct {
	LastIntent  string         `json:"last_intent"`
	Mode        string         `json:"mode"`
	State       string         `json:"state"`
	IsEscalated bool           `json:"is_escalated"`
	Metadata    map[string]any `json:"metadata"`
}

type persistedOperationalMemory struct {
	ActiveOrderID string           `json:"active_order_id"`
	PaymentState  *PaymentState    `json:"payment_state,omitempty"`
	Escalation    *EscalationState `json:"escalation_state,omitempty"`
}

// NewDBMemoryStore creates a new DB-backed memory store.
func NewDBMemoryStore(db *gorm.DB) *DBMemoryStore {
	return &DBMemoryStore{db: db}
}

// LoadConversation loads the narrative memory.
func (s *DBMemoryStore) LoadConversation(ctx context.Context, scope types.MemoryScope) (*ConversationMemory, error) {
	mem := &ConversationMemory{
		ConversationID: scope.ConversationID,
		Messages:       []types.Message{},
		Metadata:       make(map[string]any),
	}
	if s.db == nil || scope.ConversationID == uuid.Nil {
		return mem, nil
	}

	type interactionRecord struct {
		Direction string
		Content   string
		CreatedAt time.Time
	}
	var rows []interactionRecord
	if err := s.db.WithContext(ctx).
		Model(&storage.Interaction{}).
		Select("direction, content, created_at").
		Where("conversation_id = ?", scope.ConversationID).
		Order("created_at desc").
		Limit(40).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	// Convert to chronological order.
	for i := len(rows) - 1; i >= 0; i-- {
		content := strings.TrimSpace(rows[i].Content)
		if content == "" {
			continue
		}
		role := types.RoleAssistant
		if strings.EqualFold(strings.TrimSpace(rows[i].Direction), "incoming") {
			role = types.RoleCustomer
		}
		mem.Messages = append(mem.Messages, types.Message{
			Role:      role,
			Content:   content,
			Timestamp: rows[i].CreatedAt,
		})
	}

	var conv storage.Conversation
	if err := s.db.WithContext(ctx).Select("id, ai_state").Where("id = ?", scope.ConversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mem, nil
		}
		return nil, err
	}
	if strings.TrimSpace(conv.AIState) == "" || strings.TrimSpace(conv.AIState) == "{}" {
		return mem, nil
	}

	var stateMap map[string]any
	if err := json.Unmarshal([]byte(conv.AIState), &stateMap); err != nil {
		return mem, nil
	}
	raw, ok := stateMap["_memory"]
	if !ok {
		return mem, nil
	}
	rawBytes, err := json.Marshal(raw)
	if err != nil {
		return mem, nil
	}
	var persisted persistedConversationMemory
	if err := json.Unmarshal(rawBytes, &persisted); err != nil {
		return mem, nil
	}

	if persisted.LastIntent != "" {
		mem.LastIntent = types.Intent(persisted.LastIntent)
	}
	if persisted.Mode != "" {
		mem.Mode = types.ConversationMode(persisted.Mode)
	}
	if persisted.State != "" {
		mem.State = types.ConversationState(persisted.State)
	}
	mem.IsEscalated = persisted.IsEscalated
	if persisted.Metadata != nil {
		mem.Metadata = persisted.Metadata
	}

	return mem, nil
}

// SaveConversation persists the narrative memory.
func (s *DBMemoryStore) SaveConversation(ctx context.Context, scope types.MemoryScope, mem *ConversationMemory) error {
	if s.db == nil || scope.ConversationID == uuid.Nil || mem == nil {
		return nil
	}

	var conv storage.Conversation
	if err := s.db.WithContext(ctx).Select("id, ai_state").Where("id = ?", scope.ConversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	stateMap := map[string]any{}
	if strings.TrimSpace(conv.AIState) != "" && strings.TrimSpace(conv.AIState) != "{}" {
		_ = json.Unmarshal([]byte(conv.AIState), &stateMap)
	}
	stateMap["_memory"] = persistedConversationMemory{
		LastIntent:  string(mem.LastIntent),
		Mode:        string(mem.Mode),
		State:       string(mem.State),
		IsEscalated: mem.IsEscalated,
		Metadata:    mem.Metadata,
	}

	blob, err := json.Marshal(stateMap)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).
		Model(&storage.Conversation{}).
		Where("id = ?", scope.ConversationID).
		Update("ai_state", string(blob)).Error
}

func parseOrderIDFromRawOrderSnapshot(raw string) string {
	type rawOrderSnapshot struct {
		OrderID string `json:"order_id"`
	}
	snapshot := &rawOrderSnapshot{}
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return ""
	}
	if err := json.Unmarshal([]byte(raw), snapshot); err != nil {
		return ""
	}
	return strings.TrimSpace(snapshot.OrderID)
}

// LoadOperational loads the business process memory.
func (s *DBMemoryStore) LoadOperational(ctx context.Context, scope types.MemoryScope) (*OperationalMemory, error) {
	mem := &OperationalMemory{
		ActiveOrderID: nil,
	}
	if s.db == nil || scope.ConversationID == uuid.Nil {
		return mem, nil
	}

	var conv storage.Conversation
	if err := s.db.WithContext(ctx).Select("id, ai_state, ai_order_state").Where("id = ?", scope.ConversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mem, nil
		}
		return nil, err
	}

	// Recover last known order snapshot if present.
	if orderID := parseOrderIDFromRawOrderSnapshot(conv.AIOrderState); orderID != "" {
		if id, err := uuid.Parse(orderID); err == nil {
			mem.ActiveOrderID = &id
		}
	}

	// Confirm with DB active order (authoritative).
	var ord storage.Order
	err := s.db.WithContext(ctx).
		Select("id").
		Where("conversation_id = ? AND status IN ?", scope.ConversationID, []string{"pending", "confirmed", "processing", "verified"}).
		Order("created_at desc").
		First(&ord).Error
	if err == nil {
		mem.ActiveOrderID = &ord.ID
	}

	if strings.TrimSpace(conv.AIState) != "" && strings.TrimSpace(conv.AIState) != "{}" {
		var stateMap map[string]any
		if json.Unmarshal([]byte(conv.AIState), &stateMap) == nil {
			if raw, ok := stateMap["_operational"]; ok {
				rawBytes, _ := json.Marshal(raw)
				var persisted persistedOperationalMemory
				if json.Unmarshal(rawBytes, &persisted) == nil {
					if persisted.ActiveOrderID != "" {
						if id, err := uuid.Parse(persisted.ActiveOrderID); err == nil {
							mem.ActiveOrderID = &id
						}
					}
					mem.PaymentState = persisted.PaymentState
					mem.EscalationState = persisted.Escalation
				}
			}
		}
	}

	return mem, nil
}

// SaveOperational persists the business process memory.
func (s *DBMemoryStore) SaveOperational(ctx context.Context, scope types.MemoryScope, mem *OperationalMemory) error {
	if s.db == nil || scope.ConversationID == uuid.Nil || mem == nil {
		return nil
	}

	var conv storage.Conversation
	if err := s.db.WithContext(ctx).Select("id, ai_state").Where("id = ?", scope.ConversationID).First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	stateMap := map[string]any{}
	if strings.TrimSpace(conv.AIState) != "" && strings.TrimSpace(conv.AIState) != "{}" {
		_ = json.Unmarshal([]byte(conv.AIState), &stateMap)
	}

	activeOrderID := ""
	if mem.ActiveOrderID != nil {
		activeOrderID = mem.ActiveOrderID.String()
	}
	stateMap["_operational"] = persistedOperationalMemory{
		ActiveOrderID: activeOrderID,
		PaymentState:  mem.PaymentState,
		Escalation:    mem.EscalationState,
	}

	blob, err := json.Marshal(stateMap)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).
		Model(&storage.Conversation{}).
		Where("id = ?", scope.ConversationID).
		Update("ai_state", string(blob)).Error
}
