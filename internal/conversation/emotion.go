package conversation

import "strings"

type Emotion string

const (
	EmotionNeutral    Emotion = "NEUTRAL"
	EmotionFrustrated Emotion = "FRUSTRATED"
	EmotionCurious    Emotion = "CURIOUS"
	EmotionUrgent     Emotion = "URGENT"
)

// DetectEmotion uses lightweight phrase matching.
// It does NOT label emotions explicitly in responses.
func DetectEmotion(message string) Emotion {
	msg := strings.ToLower(message)

	switch {
	case containsAny(msg, []string{
		"annoying", "frustrated", "tired", "this is too much",
		"why is this", "this doesn't make sense",
	}):
		return EmotionFrustrated

	case containsAny(msg, []string{
		"just curious", "wondering", "how does this work",
		"can you explain", "what does that mean",
	}):
		return EmotionCurious

	case containsAny(msg, []string{
		"urgent", "asap", "quick", "now", "immediately",
	}):
		return EmotionUrgent

	default:
		return EmotionNeutral
	}
}

func containsAny(src string, list []string) bool {
	for _, s := range list {
		if strings.Contains(src, s) {
			return true
		}
	}
	return false
}
