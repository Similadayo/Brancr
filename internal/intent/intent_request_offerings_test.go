package intent

import "testing"

func TestIsRequestOfferings(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Menu?", true},
		{"what do you guys sell?", true},
		{"What services do you offer?", true},
		{"Please show me the catalog", true},
		{"what do you have", true},
		{"what's on the menu", true},
		// Fail cases
		{"How much is banana bread?", false},
		{"Can you recommend something?", false},
		{"Okay", false},
		{"Good morning", false},
		{"I want to see your products", true}, // "products" is a trigger
		{"Do you have products?", true},
	}

	for _, tt := range tests {
		if got := IsRequestOfferings(tt.input); got != tt.want {
			t.Errorf("IsRequestOfferings(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
