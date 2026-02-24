package conversation

import (
	"testing"

	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestEvaluateTransactionState_Progress(t *testing.T) {
	tests := []struct {
		name          string
		current       domain.TransactionState
		intent        intent.IntentType
		paymentStatus domain.PaymentStatus
		expected      domain.TransactionState
	}{
		{
			name:     "Browsing -> Committed (on Commit intent)",
			current:  domain.TransactionBrowsing,
			intent:   intent.IntentConfirmOrder,
			expected: domain.TransactionCommitted,
		},
		{
			name:     "Browsing -> Committed (on Price Accept)",
			current:  domain.TransactionBrowsing,
			intent:   intent.IntentAcceptPrice,
			expected: domain.TransactionCommitted,
		},
		{
			name:     "Committed -> AwaitingPayment (on Payment Intent)",
			current:  domain.TransactionCommitted,
			intent:   intent.IntentRequestPayment,
			expected: domain.TransactionAwaitingPayment,
		},
		{
			name:          "AwaitingPayment -> Paid (on Payment Completed)",
			current:       domain.TransactionAwaitingPayment,
			intent:        intent.IntentUnknown,
			paymentStatus: domain.PaymentCompleted,
			expected:      domain.TransactionPaid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			next := EvaluateTransactionState(tt.current, tt.intent, tt.paymentStatus, logger)
			assert.Equal(t, tt.expected, next)
		})
	}
}

func TestEvaluateTransactionState_Stability(t *testing.T) {
	tests := []struct {
		name          string
		current       domain.TransactionState
		intent        intent.IntentType
		paymentStatus domain.PaymentStatus
		expected      domain.TransactionState
	}{
		{
			name:     "Committed + Random Intent -> Locked at Committed",
			current:  domain.TransactionCommitted,
			intent:   intent.IntentGreeting,
			expected: domain.TransactionCommitted,
		},
		{
			name:     "AwaitingPayment + Menu Request -> Locked at AwaitingPayment",
			current:  domain.TransactionAwaitingPayment,
			intent:   intent.IntentRequestMenu,
			expected: domain.TransactionAwaitingPayment,
		},
		{
			name:     "Paid + Random Intent -> Locked at Paid",
			current:  domain.TransactionPaid,
			intent:   intent.IntentItemInquiry,
			expected: domain.TransactionPaid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			next := EvaluateTransactionState(tt.current, tt.intent, tt.paymentStatus, logger)
			assert.Equal(t, tt.expected, next)
		})
	}
}

func TestValidateTransactionState_Regressions(t *testing.T) {
	tests := []struct {
		name    string
		prev    domain.TransactionState
		next    domain.TransactionState
		isValid bool
	}{
		// Valid Forward
		{"Browsing -> Committed", domain.TransactionBrowsing, domain.TransactionCommitted, true},
		{"Committed -> AwaitingPayment", domain.TransactionCommitted, domain.TransactionAwaitingPayment, true},

		// Valid Same
		{"Committed -> Committed", domain.TransactionCommitted, domain.TransactionCommitted, true},

		// Illegal Regressions
		{"AwaitingPayment -> Browsing", domain.TransactionAwaitingPayment, domain.TransactionBrowsing, false},
		{"Committed -> Browsing", domain.TransactionCommitted, domain.TransactionBrowsing, false},
		{"Paid -> Committed", domain.TransactionPaid, domain.TransactionCommitted, false},

		// Valid Reset
		{"Paid -> Browsing", domain.TransactionPaid, domain.TransactionBrowsing, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			valid := ValidateTransactionState(tt.prev, tt.next, logger)
			assert.Equal(t, tt.isValid, valid)
		})
	}
}
