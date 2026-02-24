package context

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestBuilder_BuildSnapshot_Validation(t *testing.T) {
	logger := zap.NewNop()
	builder := NewBuilder(nil, logger) // DB nil for validation check
	ctx := context.Background()

	req := &types.AIRequest{
		Scope: types.MemoryScope{
			Platform: "whatsapp",
		},
		MessageText: "Hello",
		Timestamp:   time.Now(),
	}
	conv := &memory.ConversationMemory{}
	op := &memory.OperationalMemory{}

	// Case 1: Nil Request
	_, err := builder.BuildSnapshot(ctx, nil, conv, op)
	assert.Error(t, err)

	// Case 2: Nil Memory
	_, err = builder.BuildSnapshot(ctx, req, nil, op)
	assert.Error(t, err)

	_, err = builder.BuildSnapshot(ctx, req, conv, nil)
	assert.Error(t, err)
}

func TestBuilder_BuildSnapshot_Success(t *testing.T) {
	logger := zap.NewNop()
	// Mock DB would go here, for now passing nil as we have minimal logic
	builder := NewBuilder(nil, logger)
	ctx := context.Background()

	req := &types.AIRequest{
		Scope: types.MemoryScope{
			TenantID:       uuid.New(),
			CustomerID:     "12345",
			ConversationID: uuid.New(),
			Platform:       "whatsapp",
		},
		MessageText: "Hello",
		Timestamp:   time.Now(),
	}
	conv := &memory.ConversationMemory{}
	op := &memory.OperationalMemory{}

	snapshot, err := builder.BuildSnapshot(ctx, req, conv, op)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, req.MessageText, snapshot.Message)
	assert.Equal(t, conv, snapshot.Conversation)
	assert.Equal(t, op, snapshot.Operational)
	// Verify defaults
	assert.NotNil(t, snapshot.Business)
	assert.Nil(t, snapshot.ActiveOrder)
}
