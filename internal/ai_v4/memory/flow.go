package memory

import (
	"context"

	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// MemoryFlow governs the retrieval and persistence of AI memory.
// It is the boundary between the AI system and the database.
type MemoryFlow struct {
	store       MemoryStore
	maxMessages int
}

// NewMemoryFlow creates a new memory flow coordinator.
func NewMemoryFlow(store MemoryStore, maxMessages int) *MemoryFlow {
	return &MemoryFlow{
		store:       store,
		maxMessages: maxMessages,
	}
}

// LoadAll retrieves all necessary memory for a turn.
// It creates empty structures if data is missing, ensuring a safe starting state.
func (f *MemoryFlow) LoadAll(ctx context.Context, scope types.MemoryScope) (*ConversationMemory, *OperationalMemory, error) {
	// 1. Load Conversation Memory
	convMem, err := f.store.LoadConversation(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	// Safety check: Ensure non-nil
	if convMem == nil {
		convMem = &ConversationMemory{
			ConversationID: scope.ConversationID,
			Messages:       []types.Message{},
			Metadata:       make(map[string]any),
		}
	}

	// 2. Load Operational Memory
	opMem, err := f.store.LoadOperational(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	// Safety check: Ensure non-nil
	if opMem == nil {
		opMem = &OperationalMemory{}
	}

	return convMem, opMem, nil
}

// SaveAll persists the current state of memory.
// It enforces limits like message truncation before saving.
func (f *MemoryFlow) SaveAll(ctx context.Context, scope types.MemoryScope, conv *ConversationMemory, op *OperationalMemory) error {
	// 1. Enforce Limits (Truncation)
	if len(conv.Messages) > f.maxMessages {
		// Keep the most recent maxMessages
		start := len(conv.Messages) - f.maxMessages
		conv.Messages = conv.Messages[start:]
	}

	// 2. Save Conversation Memory
	// Inspecting errors independently to ensure partial success if possible,
	// though typically we fail if either fails.
	if err := f.store.SaveConversation(ctx, scope, conv); err != nil {
		return err
	}

	// 3. Save Operational Memory
	if err := f.store.SaveOperational(ctx, scope, op); err != nil {
		return err
	}

	return nil
}
