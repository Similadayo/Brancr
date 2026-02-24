package emotion

import "github.com/similadayo/brancr/internal/ai_v4/types"

// EmotionState represents the observational emotional state of the customer.
// It reflects "how they feel", not "what we should do".
type EmotionState struct {
	Emotion    types.Emotion
	Confidence float64

	IsFrustrated bool
	IsAngry      bool
	IsConfused   bool
	IsSatisfied  bool
}
