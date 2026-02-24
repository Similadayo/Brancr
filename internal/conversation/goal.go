package conversation

// Goal represents the current conversational trajectory.
// It is NOT business state and MUST NOT control execution directly.
type Goal string

const (
	// EXPLORE: browsing, learning, initial inquiry, re-entry
	GoalExplore Goal = "EXPLORE"

	// DECIDE: comparison, objections, negotiation, evaluation
	GoalDecide Goal = "DECIDE"

	// COMMIT: agreement, payment, confirmation
	GoalCommit Goal = "COMMIT"

	// RESOLVE: fulfillment, closure, post-payment confirmation
	GoalResolve Goal = "RESOLVE"

	GoalClarify Goal = "CLARIFY"
)
