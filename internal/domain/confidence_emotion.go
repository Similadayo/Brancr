package domain

type ConfidenceLevel int

const (
	ConfidenceHigh ConfidenceLevel = iota
	ConfidenceMedium
	ConfidenceLow
)

type Emotion string

const (
	EmotionNeutral    Emotion = "NEUTRAL"
	EmotionCurious    Emotion = "CURIOUS"
	EmotionUrgent     Emotion = "URGENT"
	EmotionFrustrated Emotion = "FRUSTRATED"
)
