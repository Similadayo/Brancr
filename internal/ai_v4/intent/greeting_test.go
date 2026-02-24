package intent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/intent"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// MockLLM for testing
type MockLLM struct{}

func (m *MockLLM) ClassifyIntent(msg, ctx string) (*types.IntentResult, error) {
	if msg == "Hi, I need help" {
		return &types.IntentResult{Primary: types.Intent("INVALID_INTENT"), Confidence: 0.9}, nil
	}
	return &types.IntentResult{Primary: types.IntentUnknown, Confidence: 0.1}, nil
}
func (m *MockLLM) ExtractEntities(msg string) (map[string]any, error) { return nil, nil }
func (m *MockLLM) Generate(prompt string) (string, error)             { return "", nil }
func (m *MockLLM) GenerateWithContext(ctx context.Context, req types.GenerateRequest) (*types.GenerateResponse, error) {
	return &types.GenerateResponse{}, nil
}
func (m *MockLLM) InterpretSemantic(ctx context.Context, message string, lastIntent string, businessType string) (*types.SemanticUnderstanding, error) {
	return nil, nil
}

func TestStrictGreetingRule(t *testing.T) {
	logger := zap.NewNop()
	llm := &MockLLM{}
	engine := intent.NewEngine(llm, logger)

	tests := []struct {
		name     string
		input    string
		expected types.Intent
		conf     float64
	}{
		{"Hi", "Hi", types.IntentGreeting, 1.0},
		{"Hello", "hello", types.IntentGreeting, 1.0},
		{"Hey", "Hey", types.IntentGreeting, 1.0},
		{"Good Morning", "Good morning", types.IntentGreeting, 1.0},
		{"Yo", "yo", types.IntentGreeting, 1.0},
		{"Boss", "boss", types.IntentGreeting, 1.0},
		{"Longer message", "Hi, I need help", types.IntentUnknown, 0.2}, // Should fall through to LLM (mock returns Unknown) -> Validation Guard (0.2)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			snap := &v4ctx.ContextSnapshot{
				Message:  tt.input,
				Business: types.BusinessProfile{Name: "Test"},
			}
			facts := &signals.SemanticFacts{}

			res, err := engine.Classify(ctx, snap, facts)
			assert.NoError(t, err)
			assert.NotNil(t, res)
			assert.Equal(t, tt.expected, res.Primary, "Intent mismatch for input: %s", tt.input)
			assert.Equal(t, tt.conf, res.Confidence, "Confidence mismatch for input: %s", tt.input)
		})
	}
}
