package v3

import (
	"strings"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

// TestVoiceAnalysis verifies heuristic extraction
func TestVoiceAnalysis(t *testing.T) {
	t.Run("Detect Emojis", func(t *testing.T) {
		text := "Hello! 👋 This is great 🔥"
		signals := AnalyzeVoice(text)
		if signals.EmojiRate != 1.0 { // >1 emoji = 1.0
			t.Errorf("Expected EmojiRate 1.0, got %f", signals.EmojiRate)
		}

		text2 := "One emoji 😊"
		signals2 := AnalyzeVoice(text2)
		if signals2.EmojiRate != 0.5 { // 1 emoji = 0.5
			t.Errorf("Expected EmojiRate 0.5, got %f", signals2.EmojiRate)
		}
	})

	t.Run("Detect Slang", func(t *testing.T) {
		text := "thx u for coming pls"
		signals := AnalyzeVoice(text)
		if signals.SlangRate != 1.0 {
			t.Errorf("Expected SlangRate 1.0, got %f", signals.SlangRate)
		}
	})

	t.Run("Detect Formality", func(t *testing.T) {
		text := "I wish to inform you sincerely"
		signals := AnalyzeVoice(text)
		if signals.Formality < 0.6 {
			t.Errorf("Expected High Formality, got %f", signals.Formality)
		}
	})
}

// TestEMAUpdate verifies learning stability
func TestEMAUpdate(t *testing.T) {
	fp := &storage.VoiceFingerprint{
		EmojiRate: 0.0,
		UpdatedAt: time.Now(),
	}

	// First update: Should move 15% towards new value (1.0)
	// 0.0 * 0.85 + 1.0 * 0.15 = 0.15
	UpdateFingerprint(fp, VoiceSignals{EmojiRate: 1.0})

	if fp.EmojiRate != 0.15 {
		t.Errorf("Expected EmojiRate 0.15 after one update, got %f", fp.EmojiRate)
	}

	// Second update:
	// 0.15 * 0.85 + 1.0 * 0.15 = 0.1275 + 0.15 = 0.2775
	UpdateFingerprint(fp, VoiceSignals{EmojiRate: 1.0})

	if fp.EmojiRate < 0.27 || fp.EmojiRate > 0.28 {
		t.Errorf("Expected EmojiRate ~0.2775, got %f", fp.EmojiRate)
	}
}

// TestVoiceApplication verifies output modification
func TestVoiceApplication(t *testing.T) {
	t.Run("Strip Emojis (Formal)", func(t *testing.T) {
		messages := []string{"Hello! 👋", "Confirmed ✅"}
		// Create profile with 0.0 emoji rate
		profile := VoiceProfile{EmojiRate: 0.1} // Below 0.2 threshold = Strip

		result := ApplyVoice(messages, profile)

		if strings.Contains(result[0], "👋") || strings.Contains(result[1], "✅") {
			t.Error("Failed to strip emojis for formal profile")
		}
	})

	t.Run("Enforce Directness", func(t *testing.T) {
		messages := []string{"I think maybe we can do that", "Let me check on that"}
		profile := VoiceProfile{Directness: 0.9} // Very direct

		result := ApplyVoice(messages, profile)

		if strings.Contains(result[0], "maybe") {
			t.Error("Failed to strip hedge 'maybe'")
		}
		if strings.Contains(result[1], "Let me check") {
			t.Error("Failed to strip hedge 'Let me check'")
		}
	})
}

// TestPersonaIntegration verifies guardrails
func TestPersonaIntegration(t *testing.T) {
	persona := storage.Persona{
		Humor: false, // Should kill emojis
		Tone:  "Strictly Formal",
	}

	// Fingerprint SAYS lots of emojis
	fp := storage.VoiceFingerprint{
		EmojiRate: 0.9,
		Formality: 0.2,
	}

	// Merge should enforce Persona constraints
	profile := MergeVoice(persona, fp)

	if profile.EmojiRate > 0.1 {
		t.Errorf("Persona.Humor=false should cap EmojiRate, got %f", profile.EmojiRate)
	}

	if profile.Formality < 0.7 {
		t.Errorf("Formal tone should enforce min formality, got %f", profile.Formality)
	}
}
