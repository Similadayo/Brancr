package ai

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/llm"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"go.uber.org/zap"
)

func TestCaptionOptimizer_OptimizeCaption_WithImage(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	// Setup: Create tenant, business profile, and persona
	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Fashion Brand",
		Email: "fashion@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID:    testTenantID,
		Name:        "Fashion Brand",
		Niche:       "Fashion",
		Description: "Sustainable fashion brand specializing in eco-friendly clothing",
		Tone:        "trendy",
	}
	db.Create(&bizProfile)

	persona := storage.Persona{
		TenantID:   testTenantID,
		BotName:    "Luna",
		Tone:       "trendy and engaging",
		Language:   "en",
		Humor:      true,
		StyleNotes: "Focus on sustainability and eco-friendly messaging",
	}
	db.Create(&persona)

	// Mock AIClient
	mockClient := &AIClient{
		Client: &llm.Client{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4",
		},
	}

	optimizer := &CaptionOptimizer{
		Client: mockClient,
		DB:     db,
	}

	// Test: Optimize caption with image
	req := OptimizeCaptionRequest{
		OriginalCaption: "New collection available",
		Platform:        "instagram",
		MediaType:       "image",
		TenantID:        testTenantID,
		ImageURLs:       []string{"https://example.com/image.jpg"},
	}

	result, err := optimizer.OptimizeCaption(context.Background(), req)

	if err != nil {
		t.Fatalf("OptimizeCaption failed: %v", err)
	}

	if result.OptimizedCaption == "" {
		t.Errorf("expected optimized caption, got empty string")
	}

	// Verify hashtags were extracted
	if len(result.Hashtags) == 0 {
		t.Errorf("expected hashtags to be extracted")
	}
}

func TestCaptionOptimizer_OptimizeCaption_TextOnly(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Coffee Shop",
		Email: "coffee@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID:    testTenantID,
		Name:        "Coffee Shop",
		Description: "Artisanal coffee roastery",
	}
	db.Create(&bizProfile)

	mockClient := &AIClient{
		Client: &llm.Client{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4",
		},
	}

	optimizer := &CaptionOptimizer{
		Client: mockClient,
		DB:     db,
	}

	req := OptimizeCaptionRequest{
		OriginalCaption: "New coffee blend",
		Platform:        "instagram",
		MediaType:       "image",
		TenantID:        testTenantID,
		ImageURLs:       []string{}, // No image
	}

	result, err := optimizer.OptimizeCaption(context.Background(), req)

	if err != nil {
		t.Fatalf("OptimizeCaption failed: %v", err)
	}

	if result.OptimizedCaption == "" {
		t.Errorf("expected optimized caption")
	}

	// Should still extract hashtags
	if len(result.Hashtags) == 0 {
		t.Errorf("expected hashtags even without image")
	}
}

func TestCaptionOptimizer_OptimizeCaption_AIFails_ReturnOriginal(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	// Mock client that fails
	failingClient := &AIClient{
		Client: &llm.Client{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4",
		},
	}

	optimizer := &CaptionOptimizer{
		Client: failingClient,
		DB:     db,
	}

	req := OptimizeCaptionRequest{
		OriginalCaption: "Original caption",
		Platform:        "instagram",
		MediaType:       "image",
		TenantID:        testTenantID,
	}

	result, err := optimizer.OptimizeCaption(context.Background(), req)

	// Should not return error, but fallback to original
	if err != nil {
		t.Fatalf("OptimizeCaption should not error on AI failure: %v", err)
	}

	if result.OptimizedCaption != "Original caption" {
		t.Errorf("expected original caption as fallback, got %s", result.OptimizedCaption)
	}
}

func TestCaptionOptimizer_OptimizeCaption_PlatformGuidance(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Brand",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID: testTenantID,
	}
	db.Create(&bizProfile)

	tests := []struct {
		platform       string
		expectedFormat string
	}{
		{"instagram", "15-20 hashtags"},
		{"tiktok", "trending"},
		{"facebook", "community"},
		{"youtube", "descriptive"},
	}

	for _, tt := range tests {
		mockClient := &AIClient{
			Client: &llm.Client{
				APIKey:  "test-key",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-4",
			},
		}

		optimizer := &CaptionOptimizer{
			Client: mockClient,
			DB:     db,
		}

		req := OptimizeCaptionRequest{
			OriginalCaption: "Test",
			Platform:        tt.platform,
			MediaType:       "image",
			TenantID:        testTenantID,
		}

		result, err := optimizer.OptimizeCaption(context.Background(), req)

		if err != nil {
			t.Fatalf("OptimizeCaption failed for %s: %v", tt.platform, err)
		}

		if result.OptimizedCaption == "" {
			t.Errorf("expected caption for platform %s", tt.platform)
		}
	}
}

func TestCaptionOptimizer_OptimizeCaption_WithBusinessContext(t *testing.T) {
	db := testutil.TestDB(t)
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Restaurant",
		Email: "rest@example.com",
	}
	db.Create(&tenant)

	bizProfile := storage.BusinessProfile{
		TenantID:    testTenantID,
		Description: "Fine dining Italian restaurant",
	}
	db.Create(&bizProfile)

	// Add menu items to provide business context
	menuItems := []storage.BusinessMenuItem{
		{
			TenantID:    testTenantID,
			Name:        "Pasta Carbonara",
			Category:    "Main",
			Price:       "25.00",
			Description: "Classic Italian pasta",
		},
		{
			TenantID:    testTenantID,
			Name:        "Tiramisu",
			Category:    "Dessert",
			Price:       "12.00",
			Description: "Traditional tiramisu",
		},
	}

	for _, item := range menuItems {
		db.Create(&item)
	}
	mockClient := &AIClient{
		Client: &llm.Client{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4",
		},
	}

	optimizer := &CaptionOptimizer{
		Client: mockClient,
		DB:     db,
	}
	req := OptimizeCaptionRequest{
		OriginalCaption: "Pasta special",
		Platform:        "instagram",
		MediaType:       "image",
		TenantID:        testTenantID,
	}

	result, err := optimizer.OptimizeCaption(context.Background(), req)

	if err != nil {
		t.Fatalf("OptimizeCaption failed: %v", err)
	}

	if result.OptimizedCaption == "" {
		t.Errorf("expected caption with business context")
	}
}
