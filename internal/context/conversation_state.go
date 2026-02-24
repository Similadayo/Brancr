package context

// ConversationState tracks session flags.
// This struct ensures we don't repeat certain behaviors.
type ConversationState struct {
	HasAskedQualification bool
}
