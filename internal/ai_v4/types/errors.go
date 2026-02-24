package types

import "errors"

// Common errors for AI pipeline
var (
	ErrMemoryNotFound  = errors.New("conversation memory not found")
	ErrContextBuild    = errors.New("failed to build context")
	ErrIntentClassify  = errors.New("failed to classify intent")
	ErrPolicyViolation = errors.New("policy violation")
	ErrComposerFailed  = errors.New("failed to compose response")
	ErrLLMUnavailable  = errors.New("LLM service unavailable")
	ErrInvalidRequest  = errors.New("invalid AI request")
)
