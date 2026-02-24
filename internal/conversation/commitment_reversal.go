package conversation

// CommitmentReversalSignal represents an ephemeral signal about commitment reversal.
// This is NOT persisted. It's an input to state evaluation.
//
// PHASE 5: This signal is state-only, NOT conversational.
// It must NOT influence tone, wording, or next-step messaging.
type CommitmentReversalSignal struct {
	IsReversal bool
	Confidence float64 // 0.0 to 1.0
	Source     string  // "llm"
	Reasoning  string  // Why the LLM thinks this (for debugging/audit)
}
