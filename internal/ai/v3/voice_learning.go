package v3

import (
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

// VoiceSignals represents the raw metrics extracted from a single message
type VoiceSignals struct {
	EmojiRate      float64
	SlangRate      float64
	Formality      float64
	Directness     float64
	SentenceLength float64
	QuestionRate   float64
}

// VoiceProfile is the effective voice config used for generation
// It combines the deterministic fingerprint with persona constraints
type VoiceProfile struct {
	EmojiRate      float64
	SlangRate      float64
	Formality      float64
	Directness     float64
	SentenceLength float64
	QuestionRate   float64
}

// =============================================================================
// 1. ANALYSIS - Extract intent-agnostic signals from text
// =============================================================================

// AnalyzeVoice extracts heuristic signals from a text string
func AnalyzeVoice(text string) VoiceSignals {
	if text == "" {
		return VoiceSignals{}
	}

	return VoiceSignals{
		EmojiRate:      detectEmojiRate(text),
		SlangRate:      detectSlang(text),
		Formality:      detectFormality(text),
		Directness:     detectDirectness(text),
		SentenceLength: normalizeSentenceLength(text),
		QuestionRate:   detectQuestionRate(text),
	}
}

func detectEmojiRate(text string) float64 {
	emojiCount := 0
	for _, r := range text {
		if r >= 0x1F600 && r <= 0x1F64F { // Emoticons
			emojiCount++
		}
		if r >= 0x1F300 && r <= 0x1F5FF { // Misc Symbols
			emojiCount++
		}
	}

	if emojiCount > 0 {
		// 1 emoji = 0.5, 2+ = 1.0 (Caps at 1.0)
		if emojiCount == 1 {
			return 0.5
		}
		return 1.0
	}
	return 0.0
}

func detectSlang(text string) float64 {
	slangWords := []string{"u", "ur", "thx", "pls", "plz", "gonna", "wanna", "k", "cool", "yeah", "nope"}
	lower := strings.ToLower(text)
	words := strings.Fields(lower)

	count := 0
	for _, w := range words {
		for _, s := range slangWords {
			if w == s {
				count++
				break
			}
		}
	}

	if count > 0 {
		return 1.0
	}
	return 0.0
}

func detectFormality(text string) float64 {
	formalMarkers := []string{"regards", "sincerely", "inform", "request", "assistance", "please", "thank you"}
	lower := strings.ToLower(text)

	count := 0
	for _, f := range formalMarkers {
		if strings.Contains(lower, f) {
			count++
		}
	}

	// Score 0.0 (Casual) to 1.0 (Formal)
	// Base is 0.5
	score := 0.5 + (float64(count) * 0.1)
	if score > 1.0 {
		return 1.0
	}
	return score
}

func detectDirectness(text string) float64 {
	hedges := []string{"maybe", "let me check", "i think", "probably", "possibly", "i believe"}
	lower := strings.ToLower(text)

	for _, h := range hedges {
		if strings.Contains(lower, h) {
			return 0.2 // Hedges found -> Low directness
		}
	}
	return 0.8 // No hedges -> High directness
}

func normalizeSentenceLength(text string) float64 {
	words := len(strings.Fields(text))
	// 5 words = 0.0 (Very short)
	// 30 words = 1.0 (Very long)

	val := float64(words-5) / 25.0
	if val < 0 {
		return 0
	}
	if val > 1 {
		return 1
	}
	return val
}

func detectQuestionRate(text string) float64 {
	if strings.Contains(text, "?") {
		return 1.0
	}
	return 0.0
}

// =============================================================================
// 2. LEARNING - Update fingerprint using EMA (Exponential Moving Average)
// =============================================================================

// UpdateFingerprint evolves the fingerprint based on new signals
func UpdateFingerprint(fp *storage.VoiceFingerprint, s VoiceSignals) {
	alpha := 0.15 // Learning rate (15% new, 85% old)

	fp.EmojiRate = ema(fp.EmojiRate, s.EmojiRate, alpha)
	fp.SlangRate = ema(fp.SlangRate, s.SlangRate, alpha)
	fp.Formality = ema(fp.Formality, s.Formality, alpha)
	fp.Directness = ema(fp.Directness, s.Directness, alpha)
	fp.SentenceLength = ema(fp.SentenceLength, s.SentenceLength, alpha)
	fp.QuestionRate = ema(fp.QuestionRate, s.QuestionRate, alpha)

	fp.UpdatedAt = time.Now()
}

func ema(old, new, alpha float64) float64 {
	return old*(1-alpha) + new*alpha
}

// =============================================================================
// 3. APPLICATION - Apply voice capabilities to usage
// =============================================================================

// DeriveTenantFingerprint creates the high-level fingerprint from Persona
func DeriveTenantFingerprint(p storage.Persona) TenantVoiceFingerprint {
	tf := TenantVoiceFingerprint{
		Tone:       strings.ToLower(p.Tone),
		EmojiBias:  EmojiLight,
		SlangLevel: SlangLight,
		Decisive:   true, // Default to decisive per objective
	}

	// Tone mapping
	if strings.Contains(tf.Tone, "enthusiastic") || strings.Contains(tf.Tone, "friendly") {
		tf.EmojiBias = EmojiExpressive
		tf.SlangLevel = SlangLight
	}
	if strings.Contains(tf.Tone, "formal") || strings.Contains(tf.Tone, "professional") {
		tf.EmojiBias = EmojiNone
		tf.SlangLevel = SlangNone
		tf.Decisive = true
	}
	if strings.Contains(tf.Tone, "casual") || strings.Contains(tf.Tone, "chill") {
		tf.EmojiBias = EmojiLight
		tf.SlangLevel = SlangLocal
	}

	// Language overrides
	if p.Language == "en-NG" {
		if tf.SlangLevel == SlangNone {
			tf.SlangLevel = SlangLight // Nigerian English usually has some warmth/slang
		}
	}

	return tf
}

// ToVoiceProfile converts user-understandable settings to numerical constraints
func (tf TenantVoiceFingerprint) ToVoiceProfile() VoiceProfile {
	vp := VoiceProfile{
		Formality:  0.5,
		Directness: 0.8, // Default high directness
	}

	// Map Tone -> Formality
	switch {
	case strings.Contains(tf.Tone, "formal"):
		vp.Formality = 0.9
	case strings.Contains(tf.Tone, "casual"):
		vp.Formality = 0.2
	default:
		vp.Formality = 0.5
	}

	// Map Decisive -> Directness
	if tf.Decisive {
		vp.Directness = 0.95 // Very direct, strip almost all hedges
	} else {
		vp.Directness = 0.6
	}

	// Map EmojiBias -> EmojiRate
	switch tf.EmojiBias {
	case EmojiExpressive:
		vp.EmojiRate = 0.8
	case EmojiNone:
		vp.EmojiRate = 0.0
	default: // light
		vp.EmojiRate = 0.3
	}

	// Map SlangLevel -> SlangRate
	switch tf.SlangLevel {
	case SlangLocal:
		vp.SlangRate = 0.8
	case SlangNone:
		vp.SlangRate = 0.0
	default: // light
		vp.SlangRate = 0.3
	}

	return vp
}

// MergeVoice combines the learned fingerprint with hard Persona limits
func MergeVoice(persona storage.Persona, fp storage.VoiceFingerprint) VoiceProfile {
	// 1. Derive the ideal Tenant Voice from Persona (The "HOW")
	tenantFP := DeriveTenantFingerprint(persona)

	// 2. Convert to numerical constraints
	constrainedProfile := tenantFP.ToVoiceProfile()

	// 3. Blend with Learned Fingerprint (Micro-adjustments)
	// We trust the configured Persona (TenantVoiceFingerprint) MORE than the learned stats
	// because the user objective says "Persona styling... Apply only during final phrasing"
	// but sticking to the defined character.

	// We allow learned stats to influence, but constrainedProfile sets the baseline.

	// If learned EmojiRate is higher than allowed, cap it.
	finalEmoji := fp.EmojiRate
	if constrainedProfile.EmojiRate == 0 {
		finalEmoji = 0
	} else if finalEmoji > constrainedProfile.EmojiRate {
		finalEmoji = constrainedProfile.EmojiRate // Cap it
	}

	return VoiceProfile{
		EmojiRate:      finalEmoji,
		SlangRate:      constrainedProfile.SlangRate, // Enforce slang level
		Formality:      constrainedProfile.Formality,
		Directness:     constrainedProfile.Directness,
		SentenceLength: fp.SentenceLength, // Let this be learned
		QuestionRate:   fp.QuestionRate,   // Let this be learned
	}
}

// ApplyVoice modifies generated messages to match the voice profile
func ApplyVoice(messages []string, v VoiceProfile) []string {
	// 1. Regulate Emojis
	messages = regulateEmojis(messages, v.EmojiRate)

	// 2. Enforce Directness (strip hedging)
	if v.Directness > 0.7 {
		messages = stripHedges(messages)
	}

	// 3. Length / Verbosity Check (Simple)
	// (complex length logic is in composer, this is just a final trim)

	return messages
}

func regulateEmojis(messages []string, rate float64) []string {
	// Logic:
	// Rate < 0.2 : No emojis
	// Rate < 0.5 : 1 emoji allowed total
	// Rate >= 0.5: Emojis allowed as generates

	if rate < 0.2 {
		return stripEmojis(messages) // Reuse existing stripEmojis from composer (need to export or copy)
	}

	if rate < 0.5 {
		// Limit to 1 emoji for the whole batch
		return limitEmojisGlobal(messages, 1)
	}

	return messages
}

func stripHedges(messages []string) []string {
	result := make([]string, len(messages))
	hedges := []string{"i think", "maybe", "probably", "let me check"}

	for i, msg := range messages {
		cleaned := msg
		// Case-insensitive replacement
		// Iterate hedges and remove them regardless of case
		for _, h := range hedges {
			// Find case-insensitive match
			lowerMsg := strings.ToLower(cleaned)
			lowerHedge := h // hedges are already lower

			if idx := strings.Index(lowerMsg, lowerHedge); idx != -1 {
				// Remove matched segmen
				// We need to match the length of the hedge in the original string
				start := idx
				end := idx + len(h)

				// Reconstruct string without the hedge
				cleaned = cleaned[:start] + cleaned[end:]

				// Trim any double spaces created
				cleaned = strings.ReplaceAll(cleaned, "  ", " ")
			}
		}
		result[i] = strings.TrimSpace(cleaned)
	}
	return result
}

// emojiPatterns is already defined in composer.go (package v3)

func limitEmojisGlobal(messages []string, maxEmojis int) []string {
	emojiCount := 0
	result := make([]string, len(messages))

	for i, msg := range messages {
		cleaned := msg
		for _, emoji := range emojiPatterns {
			for strings.Contains(cleaned, emoji) {
				if emojiCount < maxEmojis {
					emojiCount++
					break // Keep this one
				}
				// Remove excess
				cleaned = strings.Replace(cleaned, emoji, "", 1)
			}
		}
		result[i] = strings.TrimSpace(cleaned)
	}
	return result
}
