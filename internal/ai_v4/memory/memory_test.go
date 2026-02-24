package memory

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore is a mock implementation of MemoryStore
type MockStore struct {
	mock.Mock
}

func (m *MockStore) LoadConversation(ctx context.Context, scope types.MemoryScope) (*ConversationMemory, error) {
	args := m.Called(ctx, scope)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ConversationMemory), args.Error(1)
}

func (m *MockStore) SaveConversation(ctx context.Context, scope types.MemoryScope, mem *ConversationMemory) error {
	args := m.Called(ctx, scope, mem)
	return args.Error(0)
}

func (m *MockStore) LoadOperational(ctx context.Context, scope types.MemoryScope) (*OperationalMemory, error) {
	args := m.Called(ctx, scope)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*OperationalMemory), args.Error(1)
}

func (m *MockStore) SaveOperational(ctx context.Context, scope types.MemoryScope, mem *OperationalMemory) error {
	args := m.Called(ctx, scope, mem)
	return args.Error(0)
}

func TestMemoryFlow_LoadAll(t *testing.T) {
	store := new(MockStore)
	flow := NewMemoryFlow(store, 10)
	ctx := context.Background()
	scope := types.MemoryScope{ConversationID: uuid.New()}

	// Case 1: Store returns nil (Fresh start)
	store.On("LoadConversation", ctx, scope).Return(nil, nil).Once()
	store.On("LoadOperational", ctx, scope).Return(nil, nil).Once()

	conv, op, err := flow.LoadAll(ctx, scope)

	assert.NoError(t, err)
	assert.NotNil(t, conv)
	assert.NotNil(t, op)
	assert.Empty(t, conv.Messages)
	assert.Equal(t, scope.ConversationID, conv.ConversationID)
}

func TestMemoryFlow_SaveAll_Truncation(t *testing.T) {
	store := new(MockStore)
	flow := NewMemoryFlow(store, 5) // Max 5 messages
	ctx := context.Background()
	scope := types.MemoryScope{ConversationID: uuid.New()}

	// Create conversation with 10 messages
	messages := make([]types.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = types.Message{Content: "msg"}
	}
	conv := &ConversationMemory{Messages: messages}
	op := &OperationalMemory{}

	// Expect save with truncated messages
	store.On("SaveConversation", ctx, scope, mock.MatchedBy(func(m *ConversationMemory) bool {
		return len(m.Messages) == 5 // Should be truncated to 5
	})).Return(nil)
	store.On("SaveOperational", ctx, scope, op).Return(nil)

	err := flow.SaveAll(ctx, scope, conv, op)

	assert.NoError(t, err)
	store.AssertExpectations(t)
}
