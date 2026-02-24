package state

import (
	"errors"
	"fmt"

	"github.com/similadayo/brancr/internal/domain"
)

var (
	ErrInvalidNegotiationTransition = errors.New("invalid negotiation transition")
	ErrNegotiationLocked            = errors.New("negotiation is locked")
)

// CanTransitionNegotiation checks if a transition is allowed purely by state graph.
func CanTransitionNegotiation(from, to domain.NegotiationPhase) bool {
	switch from {
	case domain.NegotiationNone:
		return to == domain.NegotiationStarted
	case domain.NegotiationStarted:
		return to == domain.NegotiationCountered
	case domain.NegotiationCountered:
		return to == domain.NegotiationCountered || to == domain.NegotiationAccepted
	case domain.NegotiationAccepted:
		return to == domain.NegotiationLocked
	case domain.NegotiationLocked:
		return false // Terminal state relative to negotiation logic
	default:
		return false
	}
}

// TransitionNegotiation performs the state transition purely, returning a new copy.
func TransitionNegotiation(current domain.NegotiationState, to domain.NegotiationPhase) (domain.NegotiationState, error) {
	if !CanTransitionNegotiation(current.Phase, to) {
		return current, fmt.Errorf("%w: cannot move from %s to %s", ErrInvalidNegotiationTransition, current.Phase, to)
	}

	// Create new state copy
	newState := current
	newState.Phase = to

	// Increment attempts if active negotiation
	if to == domain.NegotiationCountered {
		newState.Attempts++
	}

	return newState, nil
}
