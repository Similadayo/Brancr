package ai

import (
	"testing"
)

// TestQuickClassify_CancelOrder verifies cancel patterns are detected correctly
// even when they contain the word "order"
func TestQuickClassify_CancelOrder(t *testing.T) {
	extractor := &IntentExtractorV2{}

	tests := []struct {
		message        string
		expectedIntent CanonicalIntent
	}{
		{"cancel existing order", IntentCancelOrder},
		{"Cancel my order please", IntentCancelOrder},
		{"I want to cancel the order", IntentCancelOrder},
		{"cancel order", IntentCancelOrder},
		{"cancel this order", IntentCancelOrder},
		{"please cancel my purchase", IntentCancelOrder},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractor.QuickClassify(tt.message)
			if result == nil {
				t.Errorf("QuickClassify(%q) returned nil, expected %s", tt.message, tt.expectedIntent)
				return
			}
			if result.Intent != tt.expectedIntent {
				t.Errorf("QuickClassify(%q) = %s, want %s", tt.message, result.Intent, tt.expectedIntent)
			}
		})
	}
}

// TestQuickClassify_AddToCart verifies genuine add-to-cart intents work
func TestQuickClassify_AddToCart(t *testing.T) {
	extractor := &IntentExtractorV2{}

	tests := []struct {
		message        string
		expectedIntent CanonicalIntent
	}{
		{"I want 2 large shawarmas", IntentAddToCart},
		{"give me the rice and chicken", IntentAddToCart},
		{"buy 3 of those", IntentAddToCart},
		{"I need the extra spicy one", IntentAddToCart},
		{"get me 2 bottles", IntentAddToCart},
		{"I'd like the combo meal", IntentAddToCart},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractor.QuickClassify(tt.message)
			if result == nil {
				t.Errorf("QuickClassify(%q) returned nil, expected %s", tt.message, tt.expectedIntent)
				return
			}
			if result.Intent != tt.expectedIntent {
				t.Errorf("QuickClassify(%q) = %s, want %s", tt.message, result.Intent, tt.expectedIntent)
			}
		})
	}
}

// TestQuickClassify_FalsePositives ensures concept words don't trigger add_to_cart
func TestQuickClassify_FalsePositives(t *testing.T) {
	extractor := &IntentExtractorV2{}

	tests := []struct {
		message string
	}{
		{"order"},
		{"new order"},
		{"existing order"},
		{"my order"},
		{"the order"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractor.QuickClassify(tt.message)
			if result != nil && result.Intent == IntentAddToCart {
				t.Errorf("QuickClassify(%q) = %s, should NOT be add_to_cart", tt.message, result.Intent)
			}
		})
	}
}

// TestQuickClassify_NewOrderIntent verifies start-new-order patterns trigger browsing
func TestQuickClassify_NewOrderIntent(t *testing.T) {
	extractor := &IntentExtractorV2{}

	tests := []struct {
		message        string
		expectedIntent CanonicalIntent
	}{
		{"I want to make a new order", IntentBrowseProducts},
		{"start a new order", IntentBrowseProducts},
		{"place a new order", IntentBrowseProducts},
		{"new order please", IntentBrowseProducts},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractor.QuickClassify(tt.message)
			if result == nil {
				t.Errorf("QuickClassify(%q) returned nil, expected %s", tt.message, tt.expectedIntent)
				return
			}
			if result.Intent != tt.expectedIntent {
				t.Errorf("QuickClassify(%q) = %s, want %s", tt.message, result.Intent, tt.expectedIntent)
			}
		})
	}
}

// TestQuickClassify_Greetings verifies greeting patterns work
func TestQuickClassify_Greetings(t *testing.T) {
	extractor := &IntentExtractorV2{}

	tests := []struct {
		message        string
		expectedIntent CanonicalIntent
	}{
		{"hi", IntentGreeting},
		{"hello", IntentGreeting},
		{"hey", IntentGreeting},
		{"good morning", IntentGreeting},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractor.QuickClassify(tt.message)
			if result == nil {
				t.Errorf("QuickClassify(%q) returned nil, expected %s", tt.message, tt.expectedIntent)
				return
			}
			if result.Intent != tt.expectedIntent {
				t.Errorf("QuickClassify(%q) = %s, want %s", tt.message, result.Intent, tt.expectedIntent)
			}
		})
	}
}
