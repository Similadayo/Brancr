package ai_contract

import "context"

// =============================================================================
// AI OUTPUT CONTRACT - Advisory Types Only
// =============================================================================
// These types represent AI suggestions and MUST NOT contain:
// - Executable commands
// - Database-level intent
// - Method receivers
// - Execution helpers
// - Direct DB references
//
// PHASE-1: This contract establishes the boundary between AI reasoning
// and business execution. AI produces suggestions; Go decides and acts.
// =============================================================================

// AIClient is the interface for AI analysis services.
// This interface is defined here to avoid import cycles between packages.
type AIClient interface {
	// Analyze takes an AnalyzeInput and returns AI suggestions.
	// The AnalyzeInput must be built by the caller.
	Analyze(ctx context.Context, input interface{}) (*AISuggestion, error)

	// Generate creates a customer-facing response based on decision context.
	// PHASE-8: Uses memory HUD for context-aware response generation.
	Generate(ctx context.Context, input interface{}) (string, error)
}

// AISuggestion represents the complete output from an AI analysis.
// This is advisory only - the system is free to ignore or modify suggestions.
type AISuggestion struct {
	// Intent is the detected user intent (e.g., "add_item", "checkout", "greeting")
	Intent string `json:"intent"`

	// Confidence is the AI's confidence in this interpretation (0.0 to 1.0)
	Confidence float64 `json:"confidence"`

	// Actions is a list of suggested actions the system might take
	// These are SUGGESTIONS, not commands
	Actions []AISuggestedAction `json:"actions"`

	// Explanation is the AI's reasoning for this suggestion
	// Used for observability and debugging
	Explanation string `json:"explanation"`

	// DetectionMethod indicates how the intent was detected
	// (e.g., "llm", "heuristic", "hybrid")
	DetectionMethod string `json:"detection_method"`

	// Emotion represents detected customer emotion
	Emotion EmotionSuggestion `json:"emotion,omitempty"`

	// Entities are extracted structured data from the message
	Entities map[string]interface{} `json:"entities,omitempty"`
}

// AISuggestedAction represents a single action the AI suggests taking.
// The system will evaluate this against business rules before execution.
type AISuggestedAction struct {
	// Type is the action type (e.g., "add_to_cart", "show_menu", "escalate")
	Type string `json:"type"`

	// Params contains action-specific parameters
	// Examples:
	//   - product_id: "uuid"
	//   - quantity: 2
	//   - message: "clarification text"
	Params map[string]interface{} `json:"params"`

	// Score is the AI's confidence in this specific action (0.0 to 1.0)
	Score float64 `json:"score"`

	// Reasoning explains why the AI suggested this action
	Reasoning string `json:"reasoning,omitempty"`
}

// EmotionSuggestion represents detected customer emotion.
type EmotionSuggestion struct {
	// Type is the emotion type (e.g., "neutral", "frustrated", "eager")
	Type string `json:"type"`

	// Intensity is how strong the emotion is (0.0 to 1.0)
	Intensity float64 `json:"intensity"`
}

// =============================================================================
// VALIDATION HELPERS (Read-Only)
// =============================================================================

// IsHighConfidence returns true if the suggestion meets a high confidence threshold.
// This is a read-only helper, not an execution decision.
func (s *AISuggestion) IsHighConfidence() bool {
	return s.Confidence >= 0.8
}

// HasActions returns true if the suggestion includes any actions.
func (s *AISuggestion) HasActions() bool {
	return len(s.Actions) > 0
}

// PrimaryAction returns the highest-scored action, or nil if none exist.
// This is advisory - the system may choose a different action.
func (s *AISuggestion) PrimaryAction() *AISuggestedAction {
	if len(s.Actions) == 0 {
		return nil
	}

	primary := &s.Actions[0]
	for i := range s.Actions {
		if s.Actions[i].Score > primary.Score {
			primary = &s.Actions[i]
		}
	}
	return primary
}

// GetParam safely retrieves a parameter from an action.
// Returns nil if the parameter doesn't exist.
func (a *AISuggestedAction) GetParam(key string) interface{} {
	if a.Params == nil {
		return nil
	}
	return a.Params[key]
}

// GetStringParam retrieves a string parameter, returning empty string if not found.
func (a *AISuggestedAction) GetStringParam(key string) string {
	val := a.GetParam(key)
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}

// GetIntParam retrieves an int parameter, returning 0 if not found.
func (a *AISuggestedAction) GetIntParam(key string) int {
	val := a.GetParam(key)
	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}

// GetFloat64Param retrieves a float64 parameter, returning 0.0 if not found.
func (a *AISuggestedAction) GetFloat64Param(key string) float64 {
	val := a.GetParam(key)
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0.0
	}
}
