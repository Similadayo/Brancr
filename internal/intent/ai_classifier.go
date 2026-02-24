package intent

// AIIntentClassifier defines the interface for GPT-based intent detection.
// This is used ONLY when Rule Matching fails or is insufficient.
type AIIntentClassifier interface {
	Classify(message string, contextSummary string) (IntentResult, error)
}
