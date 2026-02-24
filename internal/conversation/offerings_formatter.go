package conversation

import "github.com/similadayo/brancr/internal/domain"

// WARNING:
// Offerings must read like a human message, not a data list.
// Do NOT add bullets, IDs, or narration.
// Violations reintroduce chatbot behavior.

func FormatOfferings(offerings []domain.Offering) string {
	if len(offerings) == 0 {
		return "I can walk you through our main options if that helps."
	}

	// Step 4.5: Limit to 6 items to avoid overwhelm
	limit := 6
	truncated := false
	if len(offerings) > limit {
		offerings = offerings[:limit]
		truncated = true
	}

	var out string

	out += introLine()
	out += "\n\n"

	for i, o := range offerings {
		out += formatOne(o)

		// spacing between items
		if i < len(offerings)-1 {
			out += "\n\n"
		}
	}

	if truncated {
		out += "\n\nIf you’d like to see more options, just let me know."
	}

	return out
}
