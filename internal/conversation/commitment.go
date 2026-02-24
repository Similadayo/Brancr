package conversation

// CommitmentSignal represents an ephemeral signal about customer commitment.
// This is NOT persisted. It's an input to state evaluation.
//
// PHASE 4: This signal is state-only, NOT conversational.
// It must NOT influence tone, wording, or next-step messaging.
type CommitmentSignal struct {
	IsCommitted bool
	Confidence  float64 // 0.0 to 1.0
	Source      string  // "llm", "rule", "implicit"
	Reasoning   string  // Why the LLM thinks this (for debugging/audit)
}
