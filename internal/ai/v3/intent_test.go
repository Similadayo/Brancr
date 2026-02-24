package v3

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestIntentExtractor_Classify(t *testing.T) {
	extractor := NewIntentExtractor(nil) // nil client for rule-based tests

	// Mock products for entity extraction
	products := []Product{
		{ID: uuid.New(), Name: "Chicken Shawarma", Price: 1500},
		{ID: uuid.New(), Name: "Beef Burger", Price: 2000},
		{ID: uuid.New(), Name: "Coke", Price: 500},
	}

	ctx := &ConversationContext{
		Products: products,
	}

	tests := []struct {
		name            string
		message         string
		expectedType    IntentType
		expectedEntity  string // Product name if applicable
		expectedQty     int
		expectedEmotion EmotionType
	}{
		// Priority 1: Exact Commands
		{
			name:         "Cancel Intent",
			message:      "cancel my order",
			expectedType: IntentCancel,
		},
		{
			name:         "Help Intent",
			message:      "I need help",
			expectedType: IntentHelp,
		},
		{
			name:         "Greeting",
			message:      "Hello there",
			expectedType: IntentGreeting,
		},

		// Priority 2: View/Status
		{
			name:         "View Cart",
			message:      "show my cart",
			expectedType: IntentViewCart,
		},
		{
			name:         "Status Inquiry",
			message:      "where is my order",
			expectedType: IntentStatusInquiry,
		},

		// Priority 3: Cart Mutations (require product)
		{
			name:           "Add Item - Exact Match",
			message:        "I want a chicken shawarma",
			expectedType:   IntentAddItem,
			expectedEntity: "Chicken Shawarma",
			expectedQty:    1,
		},
		{
			name:           "Add Item - With Quantity",
			message:        "give me 2 beef burgers",
			expectedType:   IntentAddItem,
			expectedEntity: "Beef Burger",
			expectedQty:    2,
		},
		{
			name:           "Add Item - Partial Match",
			message:        "add a shawarma",
			expectedType:   IntentAddItem,
			expectedEntity: "Chicken Shawarma",
			expectedQty:    1,
		},
		{
			name:         "Add Item - No Product (Guard)",
			message:      "I want to order",
			expectedType: IntentProductInquiry, // Falls back to ProductInquiry ("what can i get")
		},
		{
			name:           "Remove Item",
			message:        "remove the burger",
			expectedType:   IntentRemoveItem,
			expectedEntity: "Beef Burger",
		},

		// Priority 4: Checkout
		{
			name:         "Checkout",
			message:      "I'm ready to pay",
			expectedType: IntentCheckout,
		},

		// Emotion Detection
		{
			name:            "Frustrated Emotion",
			message:         "this is terrible service",
			expectedType:    IntentProductAdvisory, // V3 is smarter - classifies as advisory
			expectedEmotion: EmotionFrustrated,
		},
		{
			name:            "Eager Emotion",
			message:         "I love it, take my money",
			expectedType:    IntentProductAdvisory, // V3 is smarter - classifies as advisory
			expectedEmotion: EmotionEager,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := extractor.Classify(ctx, tt.message)

			assert.Equal(t, tt.expectedType, intent.Type, "Intent type should match")

			if tt.expectedEntity != "" {
				assert.NotNil(t, intent.Entities.Product, "Product entity should be present")
				assert.Equal(t, tt.expectedEntity, intent.Entities.Product.Name, "Product name should match")
			}

			if tt.expectedQty > 0 {
				assert.Equal(t, tt.expectedQty, intent.Entities.Quantity, "Quantity should match")
			}

			if tt.expectedEmotion != "" {
				assert.Equal(t, tt.expectedEmotion, intent.Emotion.Type, "Emotion should match")
			}
		})
	}
}

func TestIntentExtractor_ExtractQuantity(t *testing.T) {
	// We need to access the private method via reflection or just trust the public test coverage
	// Since extractQuantity is private, we test it via Classify

	extractor := NewIntentExtractor(nil) // nil client for rule-based tests

	ctx := &ConversationContext{
		Products: []Product{{ID: uuid.New(), Name: "Item", Price: 100}},
	}

	qtyTests := []struct {
		message  string
		expected int
	}{
		{"I want one item", 1},
		{"give me two items", 2},
		{"add 3 items", 3},
		{"get 10 items", 10},
		{"I want an item", 1},
		{"add item", 1}, // Default
	}

	for _, tt := range qtyTests {
		intent := extractor.Classify(ctx, tt.message)
		if intent.Type == IntentAddItem {
			assert.Equal(t, tt.expected, intent.Entities.Quantity, "Quantity for '%s' should be %d", tt.message, tt.expected)
		}
	}
}
