package runner

import "testing"

func TestLooksLikeQuestionStandalone(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"What is the price?", true},
		{"How much does it cost?", true},
		{"Can you explain the difference?", true},
		{"Am I getting a worse phone at 1,167,500?", true},
		{"Shey this phone get warranty?", true},
		{"Is it possible to get it for less?", true},
		{"The one for 1.2m, is it new?", true},
		{"Am I getting a worse phone at 1,167,500 than at 1,250,000?", true},
		{"Is 850k a fair price for this?", true},
		{"I'll pay 1m", false},
		{"My budget is 850k", false},
		{"Take 900k", false},
		{"1.2m last price", true}, // "last price" is ambiguous, often a question "last price?"
		{"1.2m final price", false},
		{"ok", false},
		{"yes", false},
		{"deal", false},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := looksLikeQuestionStandalone(tt.msg)
			if got != tt.want {
				t.Errorf("looksLikeQuestionStandalone(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestCustomerAcceptsLastOffer(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"deal", true},
		{"i agree", true},
		{"I accept", true},
		{"that works for me", true},
		{"ok i will pay", true},
		{"ok send account", true},
		{"send account details", true},
		{"Alright i will pay 900k", true},
		// Negatives
		{"I will pay 800k", false}, // Counter-offer, not acceptance of LAST offer (unless aligned, but safeguard for now)
		{"Can you do 800k", false},
		{"What is the price?", false},
		{"No", false},
		{"Okay", false}, // Too ambiguous
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := customerAcceptsLastOffer(tt.msg)
			if got != tt.want {
				t.Errorf("customerAcceptsLastOffer(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}
