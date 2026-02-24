package v3

type ResponseMode string

const (
	ModeGentle    ResponseMode = "GENTLE"    // early, exploratory
	ModeBalanced  ResponseMode = "BALANCED"  // deciding, comparing
	ModeConfident ResponseMode = "CONFIDENT" // committing, confirming
)
