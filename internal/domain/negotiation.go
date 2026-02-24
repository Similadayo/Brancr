package domain

type NegotiationPhase string

const (
	NegotiationNone      NegotiationPhase = "NONE"
	NegotiationStarted   NegotiationPhase = "STARTED"
	NegotiationCountered NegotiationPhase = "COUNTERED"
	NegotiationAccepted  NegotiationPhase = "ACCEPTED"
	NegotiationLocked    NegotiationPhase = "LOCKED"
)

type NegotiationState struct {
	Phase            NegotiationPhase
	LastOfferedPrice Money
	Attempts         int
}
