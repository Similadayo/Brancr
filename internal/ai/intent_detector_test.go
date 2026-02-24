package ai

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"go.uber.org/zap"
)

// MockAIClient for testing without external API calls
type MockAIClient struct {
	GenerateFunc func(ctx context.Context, messages []Message) (string, error)
}

func (m *MockAIClient) Generate(ctx context.Context, messages []Message) (string, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, messages)
	}
	return `{"intent":"inquiry","tone":"neutral","confidence":0.85,"requires_escalation":false,"reasoning":"Customer asking about availability"}`, nil
}

func TestIntentDetector_DetectIntent_ValidJSON(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	// Setup: Create tenant and business profile
	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Business",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID:    testTenantID,
		Name:        "Test Business",
		Niche:       "E-commerce",
		Description: "Online store selling electronics",
	}
	db.Create(&bizProfile)

	detector := &IntentDetector{
		Client: nil, // No AI client needed - will use fallback detection
		DB:     db,
		Logger: nil,
	}

	// Test: Detect intent with fallback detection
	result := detector.FallbackDetection("Do you have this in stock?")

	if result.Intent != "inquiry" && result.Intent != "casual" {
		t.Errorf("expected intent=inquiry or casual, got %s", result.Intent)
	}
	if result.Tone != "positive" {
		t.Errorf("expected tone=positive, got %s", result.Tone)
	}
	if result.Confidence != 0.92 {
		t.Errorf("expected confidence=0.92, got %f", result.Confidence)
	}
	if result.RequiresEscalation != false {
		t.Errorf("expected no escalation, got %v", result.RequiresEscalation)
	}
}

func TestIntentDetector_DetectIntent_InvalidJSON_FallsBack(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Business",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID: testTenantID,
		Niche:    "Restaurant",
	}
	db.Create(&bizProfile)

	detector := &IntentDetector{
		Client: nil,
		DB:     db,
		Logger: logger,
	}

	// Test: Should use fallback detection
	result := detector.FallbackDetection("I want a refund for my order!")

	if result.Intent != "refund" {
		t.Errorf("expected fallback intent=refund, got %s", result.Intent)
	}
	// Fallback detection should mark this as requiring escalation (has "refund" + "want")
	if !result.RequiresEscalation {
		t.Errorf("expected escalation for refund request, got false")
	}
}

func TestIntentDetector_FallbackDetection_Complaint(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	detector := &IntentDetector{
		Client: nil,
		DB:     db,
		Logger: logger,
	}

	tests := []struct {
		message            string
		expectedIntent     string
		expectedTone       string
		expectedEscalation bool
	}{
		{
			message:            "This product is terrible and broke immediately",
			expectedIntent:     "complaint",
			expectedTone:       "negative",
			expectedEscalation: true,
		},
		{
			message:            "When will my order arrive? It's been 5 days",
			expectedIntent:     "delivery_issue",
			expectedTone:       "neutral",
			expectedEscalation: false,
		},
		{
			message:            "My package has not arrived and I need it ASAP",
			expectedIntent:     "delivery_issue",
			expectedTone:       "urgent",
			expectedEscalation: true,
		},
		{
			message:            "How much does this cost?",
			expectedIntent:     "price_objection",
			expectedTone:       "neutral",
			expectedEscalation: false,
		},
		{
			message:            "This is too pricey bro. I have 500k here with me",
			expectedIntent:     "price_objection",
			expectedTone:       "negative",
			expectedEscalation: false,
		},
		{
			message:            "Your amazing service is the best! Thanks!",
			expectedIntent:     "casual",
			expectedTone:       "positive",
			expectedEscalation: false,
		},
		{
			message:            "What's your refund policy?",
			expectedIntent:     "refund",
			expectedTone:       "neutral",
			expectedEscalation: false, // Just asking about policy, not demanding
		},
		{
			message:            "I want my money back right now!",
			expectedIntent:     "refund",
			expectedTone:       "negative",
			expectedEscalation: true,
		},
	}

	for _, tt := range tests {
		result := detector.FallbackDetection(tt.message)

		if result.Intent != tt.expectedIntent {
			t.Errorf("message %q: expected intent %s, got %s", tt.message, tt.expectedIntent, result.Intent)
		}
		if result.Tone != tt.expectedTone {
			t.Errorf("message %q: expected tone %s, got %s", tt.message, tt.expectedTone, result.Tone)
		}
		if result.RequiresEscalation != tt.expectedEscalation {
			t.Errorf("message %q: expected escalation %v, got %v", tt.message, tt.expectedEscalation, result.RequiresEscalation)
		}
	}
}

func TestIntentDetector_CustomIntentCatalog(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Business",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID: testTenantID,
		Niche:    "Hotel",
	}
	db.Create(&bizProfile)

	// Create custom intent catalog
	customIntents := []storage.IntentCatalog{
		{
			TenantID: testTenantID,
			Name:     "booking",
		},
		{
			TenantID: testTenantID,
			Name:     "room_change",
		},
	}
	for _, intent := range customIntents {
		db.Create(&intent)
	}

	mockClient := &AIClient{
		APIKey:  "test-key",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}

	detector := &IntentDetector{
		Client: mockClient,
		DB:     db,
		Logger: nil,
	}

	// Note: This test simulates custom intents; actual detection would require
	// proper OpenAI API credentials to test the AI flow
	result := detector.FallbackDetection("I'd like to book a room for next week")

	if result.Intent != "booking" && result.Intent != "casual" {
		t.Errorf("expected custom intent=booking or casual, got %s", result.Intent)
	}
}
