package semantic

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// SemanticInput contains the context needed for interpretation.
type SemanticInput struct {
	Message          string
	BusinessName     string
	BusinessDesc     string
	ConversationMode string
	Platform         string
}

// Interpreter defines the interface for semantic understanding.
type Interpreter interface {
	Interpret(ctx context.Context, input SemanticInput) (*types.SemanticUnderstanding, error)
}

// LLMInterpreter uses the LLM to interpret semantic meaning.
type LLMInterpreter struct {
	llm    types.LLMClient
	logger *zap.Logger
}

// NewLLMInterpreter creates a new LLM-based semantic interpreter.
func NewLLMInterpreter(llm types.LLMClient, logger *zap.Logger) *LLMInterpreter {
	return &LLMInterpreter{
		llm:    llm,
		logger: logger,
	}
}

// Interpret translates raw human language into soft semantic signals.
func (i *LLMInterpreter) Interpret(ctx context.Context, input SemanticInput) (*types.SemanticUnderstanding, error) {
	// Defensive guard against nil interpreter or uninitialized client
	if i == nil {
		return nil, nil // Graceful degradation
	}
	if i.llm == nil {
		return nil, fmt.Errorf("semantic_interpreter_not_initialized_with_llm")
	}

	// Call Python Semantic Endpoint
	result, err := i.llm.InterpretSemantic(ctx, input.Message, input.ConversationMode, input.BusinessDesc)
	if err != nil {
		i.logger.Warn("semantic_api_failed", zap.Error(err))
		return nil, nil // Graceful degradation
	}

	return result, nil
}
