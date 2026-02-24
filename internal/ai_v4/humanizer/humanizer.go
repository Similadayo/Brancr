package humanizer

import (
	"math/rand"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// Humanizer makes AI responses feel natural and human-like
type Humanizer interface {
	Humanize(response *types.AIResponse, ctx *types.ResolvedContext) *types.AIResponse
}

// DefaultHumanizer implements human-like text transformation
type DefaultHumanizer struct {
	rng *rand.Rand
}

// NewHumanizer creates a new humanizer
func NewHumanizer() *DefaultHumanizer {
	return &DefaultHumanizer{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Humanize transforms the response to feel more natural
func (h *DefaultHumanizer) Humanize(resp *types.AIResponse, ctx *types.ResolvedContext) *types.AIResponse {
	// 1. Add warmth/personality variations
	resp.ReplyText = h.addWarmth(resp.ReplyText)

	// 2. Split into natural message chunks
	resp.ReplySegments = h.naturalSplit(resp.ReplyText)

	// 3. Add casual connectors (occasionally)
	resp.ReplySegments = h.addConnectors(resp.ReplySegments)

	// 4. Platform-specific adjustments
	resp.ReplySegments = h.adaptToPlatform(resp.ReplySegments, ctx.Platform)

	return resp
}

// addWarmth adds personality touches to the text
func (h *DefaultHumanizer) addWarmth(text string) string {
	// Occasionally add casual touches
	if h.rng.Float32() > 0.7 {
		// Add a friendly emoji if none exists
		if !strings.ContainsAny(text, "😊👋🙏👍✨💯🔥") {
			emojis := []string{"😊", "👍", "✨"}
			text = text + " " + emojis[h.rng.Intn(len(emojis))]
		}
	}

	// Soften robotic phrases
	replacements := map[string][]string{
		"I will":        {"I'll", "Let me"},
		"I can not":     {"I can't", "Sorry, I'm unable to"},
		"please wait":   {"just a sec", "one moment"},
		"unfortunately": {"sadly", "I'm afraid"},
		"however":       {"though", "but"},
	}

	for old, options := range replacements {
		if strings.Contains(strings.ToLower(text), strings.ToLower(old)) {
			replacement := options[h.rng.Intn(len(options))]
			text = strings.Replace(text, old, replacement, 1)
			text = strings.Replace(text, strings.ToLower(old), strings.ToLower(replacement), 1)
		}
	}

	return text
}

// naturalSplit splits text into conversational chunks
func (h *DefaultHumanizer) naturalSplit(text string) []string {
	segments := []string{}

	// First, split by obvious breaks (double newline, bullet points)
	parts := strings.Split(text, "\n\n")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// If part is a list (starts with • or -), keep it together
		if strings.HasPrefix(part, "•") || strings.HasPrefix(part, "-") {
			segments = append(segments, part)
			continue
		}

		// For regular text, split by sentences but keep groups of 1-2 sentences
		sentences := splitBySentence(part)
		current := ""
		sentenceCount := 0

		for _, sentence := range sentences {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}

			if current == "" {
				current = sentence
				sentenceCount = 1
			} else if sentenceCount < 2 && len(current) < 100 {
				current = current + " " + sentence
				sentenceCount++
			} else {
				segments = append(segments, current)
				current = sentence
				sentenceCount = 1
			}
		}

		if current != "" {
			segments = append(segments, current)
		}
	}

	if len(segments) == 0 {
		return []string{text}
	}

	return segments
}

// addConnectors adds casual connectors between segments
func (h *DefaultHumanizer) addConnectors(segments []string) []string {
	if len(segments) <= 1 {
		return segments
	}

	connectors := []string{
		"btw,", "oh and,", "also,", "one more thing -",
	}

	result := make([]string, 0, len(segments))
	result = append(result, segments[0])

	for i := 1; i < len(segments); i++ {
		seg := segments[i]

		// Occasionally add connector (20% chance, only for non-list items)
		if h.rng.Float32() < 0.2 && !strings.HasPrefix(seg, "•") && !strings.HasPrefix(seg, "-") {
			connector := connectors[h.rng.Intn(len(connectors))]
			seg = connector + " " + lowercaseFirst(seg)
		}

		result = append(result, seg)
	}

	return result
}

// adaptToPlatform adjusts text for specific platforms
func (h *DefaultHumanizer) adaptToPlatform(segments []string, platform string) []string {
	switch platform {
	case "whatsapp":
		// WhatsApp is more casual, emoji-friendly
		return h.addWhatsAppStyle(segments)

	case "instagram":
		// Instagram is visual, trendy
		return h.addInstagramStyle(segments)

	case "facebook":
		// Facebook is mixed casual/formal
		return segments

	case "telegram":
		// Telegram is tech-savvy, direct
		return h.addTelegramStyle(segments)

	default:
		return segments
	}
}

func (h *DefaultHumanizer) addWhatsAppStyle(segments []string) []string {
	result := make([]string, len(segments))
	for i, seg := range segments {
		// Add occasional emoji for WhatsApp
		if h.rng.Float32() < 0.3 && !containsEmoji(seg) {
			seg = seg + " 😊"
		}
		result[i] = seg
	}
	return result
}

func (h *DefaultHumanizer) addInstagramStyle(segments []string) []string {
	// Keep segments concise for Instagram
	result := make([]string, len(segments))
	for i, seg := range segments {
		// Truncate very long segments
		if len(seg) > 200 {
			seg = seg[:197] + "..."
		}
		result[i] = seg
	}
	return result
}

func (h *DefaultHumanizer) addTelegramStyle(segments []string) []string {
	// Telegram users prefer direct, less fluffy text
	result := make([]string, len(segments))
	for i, seg := range segments {
		// Remove excess emoji for Telegram
		if countEmoji(seg) > 2 {
			seg = removeExcessEmoji(seg, 1)
		}
		result[i] = seg
	}
	return result
}

// Helper functions

func splitBySentence(text string) []string {
	sentences := []string{}
	current := ""

	for _, char := range text {
		current += string(char)
		if char == '.' || char == '!' || char == '?' {
			if len(strings.TrimSpace(current)) > 0 {
				sentences = append(sentences, strings.TrimSpace(current))
				current = ""
			}
		}
	}

	if len(strings.TrimSpace(current)) > 0 {
		sentences = append(sentences, strings.TrimSpace(current))
	}

	return sentences
}

func lowercaseFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(string(s[0])) + s[1:]
}

func containsEmoji(s string) bool {
	emojis := []string{"😊", "👋", "🙏", "👍", "✨", "💯", "🔥", "❤️", "😄", "🎉"}
	for _, e := range emojis {
		if strings.Contains(s, e) {
			return true
		}
	}
	return false
}

func countEmoji(s string) int {
	count := 0
	emojis := []string{"😊", "👋", "🙏", "👍", "✨", "💯", "🔥", "❤️", "😄", "🎉"}
	for _, e := range emojis {
		count += strings.Count(s, e)
	}
	return count
}

func removeExcessEmoji(s string, keep int) string {
	emojis := []string{"😊", "👋", "🙏", "👍", "✨", "💯", "🔥", "❤️", "😄", "🎉"}
	removed := 0
	for _, e := range emojis {
		count := strings.Count(s, e)
		for count > 0 && removed < (countEmoji(s)-keep) {
			s = strings.Replace(s, e, "", 1)
			count--
			removed++
		}
	}
	return s
}
