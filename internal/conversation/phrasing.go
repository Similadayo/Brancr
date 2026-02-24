package conversation

import "github.com/similadayo/brancr/internal/domain"

type PhraseProfile struct {
	MaxSentences int
	Softening    bool
	Direct       bool
}

func PhraseStyle(conf domain.ConfidenceLevel, emo domain.Emotion) PhraseProfile {

	// Frustration always wins
	if emo == domain.EmotionFrustrated {
		return PhraseProfile{
			MaxSentences: 1,
			Softening:    true,
			Direct:       true,
		}
	}

	switch conf {
	case domain.ConfidenceHigh:
		return PhraseProfile{
			MaxSentences: 2,
			Direct:       true,
		}
	case domain.ConfidenceMedium:
		return PhraseProfile{
			MaxSentences: 2,
			Softening:    true,
		}
	default: // Low
		return PhraseProfile{
			MaxSentences: 1,
			Softening:    true,
		}
	}
}
