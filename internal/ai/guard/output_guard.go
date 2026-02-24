package guard

import "strings"

var forbiddenPhrases = []string{
	"i can’t",
	"i cannot",
	"i’m not able",
	"from here",
	"i don’t have access",
	"connect you",
	"process",
	"system",
	"workflow",
	"payment has started", // unless explicitly allowed
	// Decision Bans
	"most popular",
	"customers love",
	"best seller",
	"limited time",
	"i recommend trying", // Specific phrase ban
}

func ValidateOutput(text string) bool {
	l := strings.ToLower(text)
	for _, f := range forbiddenPhrases {
		if strings.Contains(l, f) {
			return false
		}
	}
	return true
}
