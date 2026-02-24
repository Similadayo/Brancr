package intent

import (
	"context"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// Engine classifies customer intent from messages using facts and LLM.
type Engine struct {
	llm    types.LLMClient
	logger *zap.Logger
}

// NewEngine creates a new intent engine.
func NewEngine(llm types.LLMClient, logger *zap.Logger) *Engine {
	return &Engine{
		llm:    llm,
		logger: logger,
	}
}

// Classify determines the user intent from the snapshot and facts.
func (e *Engine) Classify(
	ctx context.Context,
	snapshot *v4ctx.ContextSnapshot,
	facts *signals.SemanticFacts,
) (*types.IntentResult, error) {

	// Safety check
	if snapshot == nil || facts == nil {
		return &types.IntentResult{Primary: types.IntentUnknown, Confidence: 0.0}, nil
	}

	// RULE 1: HARD GREETING RULE (Pre-LLM)
	// Matches simple greeting tokens
	if isShortGreeting(snapshot.Message) {
		return &types.IntentResult{
			Primary:    types.IntentGreeting,
			Confidence: 1.0,
			Entities:   make(map[string]any),
		}, nil
	}

	// Step 1: Pre-classification Guards (Weak Signals from Facts)
	candidate := e.classifyFromFacts(facts)

	// Step 2: LLM Intent Classification
	var result *types.IntentResult
	if e.llm != nil {
		var err error
		result, err = e.classifyByLLM(ctx, snapshot, facts)
		if err == nil && result != nil {
			e.logger.Debug("intent_classified_by_llm",
				zap.String("intent", string(result.Primary)),
				zap.Float64("confidence", result.Confidence))
		} else {
			e.logger.Warn("llm_intent_classification_failed", zap.Error(err))
		}
	}

	// Selection Logic
	finalIntent := types.IntentUnknown
	confidence := 0.0

	if result != nil {
		finalIntent = result.Primary
		confidence = result.Confidence
	} else if candidate != nil {
		finalIntent = candidate.Primary
		confidence = candidate.Confidence
	} else {
		// Default Fallback
		finalIntent = types.IntentInquiry // Safe default
		confidence = 0.3
	}

	// RULE 2: GREETING CONFIDENCE FLOOR
	// Greetings do not require precision. Force high confidence.
	if finalIntent == types.IntentGreeting {
		if confidence < 0.85 {
			confidence = 0.85
		}
	}

	// RULE 3: INTENT VALIDATION GUARD (NON-NEGOTIABLE)
	if !types.IsValidIntent(finalIntent) {
		e.logger.Warn("invalid_intent_detected_forcing_unknown", zap.String("intent", string(finalIntent)))
		finalIntent = types.IntentUnknown
		confidence = 0.2
	}

	return &types.IntentResult{
		Primary:    finalIntent,
		Confidence: confidence,
		Entities:   make(map[string]any),
	}, nil
}

func (e *Engine) classifyFromFacts(facts *signals.SemanticFacts) *types.IntentResult {
	// Candidate mapping from semantic facts
	// Max confidence 0.4 as per rules

	if facts.MentionsRefund {
		return &types.IntentResult{Primary: types.IntentRefund, Confidence: 0.4}
	}
	if facts.MentionsComplaint {
		return &types.IntentResult{Primary: types.IntentComplaint, Confidence: 0.4}
	}
	if facts.MentionsHuman {
		return &types.IntentResult{Primary: types.IntentHumanRequest, Confidence: 0.4}
	}
	if facts.MentionsConfirmation {
		// Only a weak signal for ConfirmOrder
		return &types.IntentResult{Primary: types.IntentConfirmOrder, Confidence: 0.4}
	}

	return nil
}

func (e *Engine) classifyByLLM(
	ctx context.Context,
	snapshot *v4ctx.ContextSnapshot,
	facts *signals.SemanticFacts,
) (*types.IntentResult, error) {
	// Prepare simplified context summary for LLM
	contextSummary := "Business: " + snapshot.Business.Name
	if snapshot.Business.Industry != "" {
		contextSummary += " (" + snapshot.Business.Industry + ")"
	}

	result, err := e.llm.ClassifyIntent(snapshot.Message, contextSummary)
	if err != nil {
		return nil, err
	}

	// Clamp confidence 0.3 - 0.95
	if result.Confidence < 0.3 {
		result.Confidence = 0.3
	}
	if result.Confidence > 0.95 {
		result.Confidence = 0.95
	}

	return result, nil
}

func isShortGreeting(msg string) bool {
	m := strings.TrimSpace(strings.ToLower(msg))
	greetings := []string{
		"hi", "hey", "hello",
		"yo", "boss",
		"good morning", "good evening", "good afternoon",
	}
	for _, g := range greetings {
		if m == g {
			return true
		}
	}
	return false
}
