package intent

type IntentResult struct {
	Intent     IntentType
	Confidence float64
	Source     string // "RULE" or "AI"
}
