package prompt

import (
	"fmt"

	"github.com/similadayo/brancr/internal/conversation"
)

func PhrasingBlock(profile conversation.PhraseProfile) string {
	base := "PHRASING CONSTRAINTS:\n"

	if profile.Direct {
		base += "- Be direct and concise.\n"
	}
	if profile.Softening {
		base += "- Use softer, less assertive language.\n- Avoid strong recommendations like 'I highly recommend'.\n"
	}

	if profile.MaxSentences > 0 {
		base += fmt.Sprintf("- MAX LENGTH: %d sentence(s).\n", profile.MaxSentences)
	}

	return base
}
