package channel

import (
	"strings"
)

func ChunkMessage(text string) []string {
	// If text is short, don't split
	if len(text) < 80 {
		return []string{text}
	}

	// If text contains URL, don't split (safer)
	if strings.Contains(text, "http") {
		return []string{text}
	}

	// Split on sentence boundaries (simple heuristic)
	// Look for ". "
	// But ensure we don't break numbers or currency.

	// Implementation:
	// Find the best split point near the middle or at a sentence end.
	// For simplicity and safety as per manual: "Split on sentence boundaries... Max 2 chunks"

	splits := strings.SplitAfter(text, ". ")
	if len(splits) == 1 {
		return []string{text}
	}

	// Combine into max 2 chunks of roughly equal size or just First Sentence | Rest
	// Heuristic: First sentence is chunk 1. Rest is chunk 2.
	// But if first sentence is tiny ("Okay."), maybe combine.

	// Let's iterate and build chunk 1 until it's substantial, then rest is chunk 2.
	var chunk1 string
	var chunk2 string

	for i, s := range splits {
		if chunk2 != "" {
			chunk2 += s
			continue
		}

		if chunk1 == "" {
			chunk1 = s
			continue
		}

		if len(chunk1) < 40 {
			chunk1 += s
		} else {
			// Chunk 1 is long enough. Start chunk 2.
			if i < len(splits) {
				chunk2 += s
			}
		}
	}

	if chunk2 == "" {
		return []string{chunk1}
	}

	return []string{strings.TrimSpace(chunk1), strings.TrimSpace(chunk2)}
}
