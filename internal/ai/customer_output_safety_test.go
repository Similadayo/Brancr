package ai

import (
	"strings"
	"testing"
)

func TestSanitizeCustomerReply(t *testing.T) {
	gate := NewCustomerOutputSafetyGate()

	tests := []struct {
		name     string
		input    string
		expected string // Expected output (or key phrase that should be absent)
		shouldContain bool // If true, check that expected is present; if false, check it's absent
	}{
		{
			name:          "removes Summary prefix",
			input:         "Summary: you've offered ₦600,000 for the iPhone 15.",
			expected:      "Summary:",
			shouldContain: false,
		},
		{
			name:          "rewrites manager approval",
			input:         "I need to request manager approval for this price.",
			expected:      "manager approval",
			shouldContain: false,
		},
		{
			name:          "rewrites check with team",
			input:         "Let me check with the team on this.",
			expected:      "check with the team",
			shouldContain: false,
		},
		{
			name:          "removes escalation word",
			input:         "This requires escalation to handle.",
			expected:      "escalation",
			shouldContain: false,
		},
		{
			name:          "removes system word",
			input:         "The system indicates the price is available.",
			expected:      "system",
			shouldContain: false,
		},
		{
			name:          "removes policy word",
			input:         "According to our policy, the price is fixed.",
			expected:      "policy",
			shouldContain: false,
		},
		{
			name:          "preserves normal customer-friendly text",
			input:         "Hi! The iPhone 15 is available for ₦750,000. Would you like to proceed?",
			expected:      "iPhone 15 is available",
			shouldContain: true,
		},
		{
			name:          "handles multiple forbidden patterns",
			input:         "Summary: I need manager approval and the system policy requires escalation.",
			expected:      "Summary:",
			shouldContain: false,
		},
		{
			name:          "cleans up extra spaces",
			input:         "Summary:   you've  offered   ₦600k",
			expected:      "  ", // Multiple spaces should be cleaned
			shouldContain: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gate.SanitizeCustomerReply(tt.input)
			
			if tt.shouldContain {
				if !strings.Contains(result, tt.expected) {
					t.Errorf("Expected result to contain %q, got %q", tt.expected, result)
				}
			} else {
				if strings.Contains(strings.ToLower(result), strings.ToLower(tt.expected)) {
					t.Errorf("Expected result to NOT contain %q, got %q", tt.expected, result)
				}
			}
			
			// Result should not be empty (unless input was empty)
			if tt.input != "" && result == "" {
				t.Errorf("Sanitization resulted in empty string, input was: %q", tt.input)
			}
		})
	}
}

func TestValidateCustomerReply(t *testing.T) {
	gate := NewCustomerOutputSafetyGate()

	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "valid customer-friendly message",
			input:       "Hi! The iPhone 15 is available for ₦750,000.",
			expectError: false,
		},
		{
			name:        "contains Summary prefix",
			input:       "Summary: you've offered ₦600k",
			expectError: true,
		},
		{
			name:        "contains manager approval",
			input:       "I need manager approval for this.",
			expectError: true,
		},
		{
			name:        "contains escalation",
			input:       "This requires escalation.",
			expectError: true,
		},
		{
			name:        "contains system",
			input:       "The system says it's available.",
			expectError: true,
		},
		{
			name:        "contains policy",
			input:       "According to our policy, we can't do that.",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gate.ValidateCustomerReply(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("Expected validation error for input %q, got nil", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no validation error for input %q, got %v", tt.input, err)
			}
		})
	}
}

