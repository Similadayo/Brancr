package domain

type SilenceState int

const (
	SilenceNone SilenceState = iota
	SilenceWaitingUser
	SilenceSoftReengage
	SilenceStop
)
