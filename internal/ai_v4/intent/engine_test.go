package intent

import (
	"context"
	"errors"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockLLM is a mock implementation of LLMClient for testing
type MockLLM struct {
	mock.Mock
}

func (m *MockLLM) ClassifyIntent(message string, context string) (*types.IntentResult, error) {
	args := m.Called(message, context)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.IntentResult), args.Error(1)
}

// Stubs for other interface methods
func (m *MockLLM) Generate(prompt string) (string, error)                 { return "", nil }
func (m *MockLLM) ExtractEntities(message string) (map[string]any, error) { return nil, nil }
func (m *MockLLM) GenerateWithContext(ctx context.Context, req types.GenerateRequest) (*types.GenerateResponse, error) {
	return nil, nil
}
func (m *MockLLM) InterpretSemantic(ctx context.Context, message string, lastIntent string, businessType string) (*types.SemanticUnderstanding, error) {
	return nil, nil
}

func TestEngine_Classify_FallbackToFacts(t *testing.T) {
	// Setup: No LLM (nil)
	engine := NewEngine(nil, zap.NewNop())
	ctx := context.Background()

	snapshot := &v4ctx.ContextSnapshot{
		Message:  "I want a refund",
		Business: types.BusinessProfile{Name: "TestBiz"},
	}
	facts := &signals.SemanticFacts{
		MentionsRefund: true,
	}

	result, err := engine.Classify(ctx, snapshot, facts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, types.IntentRefund, result.Primary)
	assert.Equal(t, 0.4, result.Confidence)
}

func TestEngine_Classify_LLMSuccess(t *testing.T) {
	mockLLM := new(MockLLM)
	engine := NewEngine(mockLLM, zap.NewNop())
	ctx := context.Background()

	snapshot := &v4ctx.ContextSnapshot{
		Message:  "I want to buy a cake",
		Business: types.BusinessProfile{Name: "Bakery"},
	}
	facts := &signals.SemanticFacts{
		MentionsProduct: true,
	}

	expectedResult := &types.IntentResult{
		Primary:    types.IntentPurchase,
		Confidence: 0.85,
	}

	// Expect LLM call
	mockLLM.On("ClassifyIntent", snapshot.Message, mock.Anything).Return(expectedResult, nil)

	result, err := engine.Classify(ctx, snapshot, facts)

	assert.NoError(t, err)
	assert.Equal(t, types.IntentPurchase, result.Primary)
	assert.Equal(t, 0.85, result.Confidence)
	mockLLM.AssertExpectations(t)
}

func TestEngine_Classify_LLMFailure_Fallback(t *testing.T) {
	mockLLM := new(MockLLM)
	engine := NewEngine(mockLLM, zap.NewNop())
	ctx := context.Background()

	snapshot := &v4ctx.ContextSnapshot{
		Message:  "I want a refund due to bad service",
		Business: types.BusinessProfile{Name: "TestBiz"},
	}
	facts := &signals.SemanticFacts{
		MentionsRefund: true,
	}

	// Expect LLM call to fail
	mockLLM.On("ClassifyIntent", snapshot.Message, mock.Anything).Return(nil, errors.New("API error"))

	result, err := engine.Classify(ctx, snapshot, facts)

	assert.NoError(t, err)
	// Should fallback to facts
	assert.Equal(t, types.IntentRefund, result.Primary)
	assert.Equal(t, 0.4, result.Confidence)
}
