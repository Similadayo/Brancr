package conversation

import "github.com/similadayo/brancr/internal/domain"

func FormatRecommendation(recs []domain.Offering) string {
	if len(recs) == 0 {
		return "I can help narrow it down if you tell me what matters most to you."
	}

	// Intro phrasing (No hype)
	out := "If you’d like a starting point, most people go with:\n\n"

	for _, r := range recs {
		out += r.Title
		if r.PriceText != "" {
			out += " — " + r.PriceText
		}
		out += "\n"
	}

	return out
}
