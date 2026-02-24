package state

import (
	"errors"
	"fmt"

	"github.com/similadayo/brancr/internal/domain"
)

var ErrInvalidFulfillmentTransition = errors.New("invalid fulfillment transition")

func CanTransitionFulfillment(from, to domain.FulfillmentStatus) bool {
	switch from {
	case domain.FulfillmentPending:
		return to == domain.FulfillmentProcessing
	case domain.FulfillmentProcessing:
		return to == domain.FulfillmentCompleted
	case domain.FulfillmentCompleted:
		return false // Terminal
	default:
		return false
	}
}

func TransitionFulfillment(current domain.FulfillmentState, to domain.FulfillmentStatus) (domain.FulfillmentState, error) {
	if !CanTransitionFulfillment(current.Status, to) {
		return current, fmt.Errorf("%w: cannot move from %s to %s", ErrInvalidFulfillmentTransition, current.Status, to)
	}

	newState := current
	newState.Status = to
	return newState, nil
}
