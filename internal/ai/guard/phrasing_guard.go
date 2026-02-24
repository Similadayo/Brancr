package guard

import (
	"strings"

	"github.com/similadayo/brancr/internal/conversation"
)

var lowConfidenceBans = []string{
	"i'd recommend",
	"you might like",
	"most people",
	"best option",
	"highly recommend",
}

func ValidatePhrasing(text string, profile conversation.PhraseProfile) bool {
	// If softening is required (Low Confidence or Frustrated)
	if profile.Softening {
		l := strings.ToLower(text)
		for _, ban := range lowConfidenceBans {
			if strings.Contains(l, ban) {
				return false
			}
		}
	}
	return true
}
