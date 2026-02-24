package state

import (
	"errors"
	"fmt"

	"github.com/similadayo/brancr/internal/domain"
)

var ErrInvalidPaymentTransition = errors.New("invalid payment transition")

func CanTransitionPayment(from, to domain.PaymentStatus) bool {
	switch from {
	case domain.PaymentNotRequested:
		return to == domain.PaymentRequested
	case domain.PaymentRequested:
		return to == domain.PaymentPending
	case domain.PaymentPending:
		return to == domain.PaymentCompleted || to == domain.PaymentFailed
	case domain.PaymentFailed:
		return to == domain.PaymentRequested // Retry flow
	case domain.PaymentCompleted:
		return false // Terminal
	default:
		return false
	}
}

func TransitionPayment(current domain.PaymentState, to domain.PaymentStatus) (domain.PaymentState, error) {
	if !CanTransitionPayment(current.Status, to) {
		return current, fmt.Errorf("%w: cannot move from %s to %s", ErrInvalidPaymentTransition, current.Status, to)
	}

	newState := current
	newState.Status = to
	return newState, nil
}
