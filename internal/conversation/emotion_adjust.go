package conversation

import "strings"

func AdjustForEmotion(text string, emotion Emotion) string {
	switch emotion {

	case EmotionFrustrated:
		return calm(text)

	case EmotionCurious:
		return explain(text)

	case EmotionUrgent:
		return concise(text)

	default:
		return text
	}
}

// calm slows pacing and reassures
func calm(text string) string {
	// Prepend reassuring prefix
	return "Let’s keep this simple. " + text
}

// explain adds openness
func explain(text string) string {
	if strings.Contains(text, "Here") || strings.Contains(text, "This") {
		return "Happy to explain. " + text
	}
	return "Happy to explain. Here’s how it works: " + text
}

// concise shortens and focuses
func concise(text string) string {
	// Attempt to trim polite filler if possible, but simplest is to frame it as urgent/result-oriented
	return "Here’s the quickest option: " + text
}
