package emotion

import (
	"context"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLLM is a mock implementation of LLMClient
type MockLLM struct {
	mock.Mock
}

func (m *MockLLM) Generate(prompt string) (string, error) {
	args := m.Called(prompt)
	return args.String(0), args.Error(1)
}

// Satisfy other interface methods with stubs
func (m *MockLLM) ClassifyIntent(msg, ctx string) (*types.IntentResult, error) { return nil, nil }
func (m *MockLLM) ExtractEntities(msg string) (map[string]any, error)          { return nil, nil }
func (m *MockLLM) GenerateWithContext(ctx context.Context, req types.GenerateRequest) (*types.GenerateResponse, error) {
	return nil, nil
}
func (m *MockLLM) InterpretSemantic(ctx context.Context, message string, lastIntent string, businessType string) (*types.SemanticUnderstanding, error) {
	return nil, nil
}

func TestDetector_Heuristics(t *testing.T) {
	detector := NewDetector(nil) // No LLM
	ctx := context.Background()

	tests := []struct {
		msg         string
		wantEmotion types.Emotion
		wantFlag    bool // specific flag check
	}{
		{"What do you mean???", types.EmotionConfused, true}, // IsConfused
		{"This is stupid!!!", types.EmotionAngry, true},      // IsAngry
		{"Perfect, thanks", types.EmotionHappy, true},        // IsSatisfied
	}

	for _, tt := range tests {
		snapshot := &v4ctx.ContextSnapshot{
			Message:  tt.msg,
			Business: types.BusinessProfile{Name: "Test"},
		}
		state, err := detector.Detect(ctx, snapshot)
		assert.NoError(t, err)
		assert.Equal(t, tt.wantEmotion, state.Emotion)

		switch tt.wantEmotion {
		case types.EmotionConfused:
			assert.True(t, state.IsConfused)
		case types.EmotionAngry:
			assert.True(t, state.IsAngry)
		case types.EmotionHappy:
			assert.True(t, state.IsSatisfied)
		}
	}
}

func TestDetector_LLM(t *testing.T) {
	mockLLM := new(MockLLM)
	detector := NewDetector(mockLLM)
	ctx := context.Background()

	snapshot := &v4ctx.ContextSnapshot{
		Message:  "I am really annoyed",
		Business: types.BusinessProfile{Name: "Test"},
	}

	// Expect LLM call
	mockLLM.On("Generate", mock.Anything).Return(`{"emotion": "frustrated", "confidence": 0.85}`, nil)

	state, err := detector.Detect(ctx, snapshot)

	assert.NoError(t, err)
	assert.Equal(t, types.EmotionFrustrated, state.Emotion)
	assert.Equal(t, 0.85, state.Confidence)
	assert.True(t, state.IsFrustrated)
	mockLLM.AssertExpectations(t)
}
