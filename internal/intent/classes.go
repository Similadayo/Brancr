package intent

type IntentClass int

const (
	IntentClassContent  IntentClass = iota // menu, prices, offerings
	IntentClassAction                      // cancel, modify, place order
	IntentClassDecision                    // "pick one for me"
	IntentClassQuestion                    // yes/no or factual
	IntentClassSystem                      // fallback, silence handling, unknown
)
