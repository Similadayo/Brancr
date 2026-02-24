package ai

import (
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/rules"
)

// GPTInput represents the strictly controlled context provided to the LLM.
// It explicitly excludes raw DB structs or unnecessary IDs to prevent hallucination.
type GPTInput struct {
	ContextSummary string // Flattened, read-only summary of the situation
	Intent         intent.IntentType
	AllowedActions []rules.ActionType
	Platform       string
	Language       string
}
