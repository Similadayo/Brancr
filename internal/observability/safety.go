package observability

import "errors"

// SafetyController enforces operational limits to prevent abuse and runaway costs.
type SafetyController struct {
	MaxAIResponsesPerSession int
	MaxNegotiationRounds     int
	MaxFailedActions         int
}

func NewSafetyController() *SafetyController {
	return &SafetyController{
		MaxAIResponsesPerSession: 50,
		MaxNegotiationRounds:     5,
		MaxFailedActions:         3,
	}
}

// CheckSessionLimits verifies if the session is still safe to proceed.
// Returns error if locked.
func (s *SafetyController) CheckSessionLimits(aiCount, negCount, failCount int) error {
	if aiCount >= s.MaxAIResponsesPerSession {
		return errors.New("MAX_AI_RESPONSES_EXCEEDED")
	}
	if negCount >= s.MaxNegotiationRounds {
		return errors.New("MAX_NEGOTIATION_ROUNDS_EXCEEDED")
	}
	if failCount >= s.MaxFailedActions {
		return errors.New("MAX_FAILED_ACTIONS_EXCEEDED")
	}
	return nil
}
