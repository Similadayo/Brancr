package conversation

// Persona defines the "voice" of the AI for a specific interaction.
// It is descriptive, not behavioral.
type Persona struct {
	FormalityLevel  int    // 1 (Casual) to 5 (Strictly Formal)
	EmojiDensity    int    // 0 (None) to 3 (Heavy)
	SentenceLength  string // "short" | "medium" | "long"
	CulturalContext string // e.g. "NG_WHATSAPP", "GLOBAL"
}
