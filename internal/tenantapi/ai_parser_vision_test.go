package tenantapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/similadayo/brancr/internal/ai"
)

// Vision-aware fake AI client for testing
type fakeVisionClient struct {
	shouldFailVision bool
}

func (f *fakeVisionClient) Generate(ctx context.Context, messages []ai.Message) (string, error) {
	// Standard text-only response
	items := []ParsedItem{{
		Name:       "Jollof Rice",
		Price:      func() *float64 { v := 3500.0; return &v }(),
		Currency:   "NGN",
		Confidence: 0.9,
	}}
	b, _ := json.Marshal(items)
	return string(b), nil
}

func (f *fakeVisionClient) GenerateWithUsage(ctx context.Context, messages []ai.Message) (*ai.GenerateResult, error) {
	items := []ParsedItem{{Name: "Test Item", Price: func() *float64 { v := 1000.0; return &v }(), Currency: "NGN"}}
	b, _ := json.Marshal(items)
	return &ai.GenerateResult{Content: string(b), TokenUsage: &ai.TokenUsage{TotalTokens: 30}}, nil
}

func (f *fakeVisionClient) GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error) {
	if f.shouldFailVision {
		return "", context.DeadlineExceeded
	}

	// Vision response with items from images
	items := []ParsedItem{
		{
			Name:        "iPhone 15 Pro",
			Description: "256GB Storage, Natural Titanium",
			MinPrice:    func() *float64 { v := 1500000.0; return &v }(),
			MaxPrice:    func() *float64 { v := 1650000.0; return &v }(),
			Currency:    "NGN",
			Type:        "product",
			Confidence:  0.85,
		},
		{
			Name:       "Fried Rice",
			Price:      func() *float64 { v := 4500.0; return &v }(),
			Currency:   "NGN",
			Type:       "menu_item",
			Confidence: 0.9,
		},
	}
	b, _ := json.Marshal(items)
	return string(b), nil
}

func (f *fakeVisionClient) GenerateWithVisionWithUsage(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, *ai.TokenUsage, error) {
	resp, err := f.GenerateWithVision(ctx, systemPrompt, userText, imageURLs)
	return resp, &ai.TokenUsage{TotalTokens: 100}, err
}

func (f *fakeVisionClient) AnalyzeImage(ctx context.Context, imageURL string) (string, error) {
	return "Image contains menu items", nil
}

func (f *fakeVisionClient) ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return &ai.ParseMenuTextResult{Items: []ai.ParsedMenuItem{{Name: "Test", Price: 1000, Currency: "NGN"}}}, nil
}

func (f *fakeVisionClient) ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return &ai.ParseMenuTextResult{Items: []ai.ParsedMenuItem{{Name: "Test", Price: 1000, Currency: "NGN"}}}, nil
}

func (f *fakeVisionClient) GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*ai.BusinessProfileResult, error) {
	return &ai.BusinessProfileResult{Name: "Test Business", ConfidenceScore: "high"}, nil
}

func (f *fakeVisionClient) GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*ai.KnowledgeResult, error) {
	return &ai.KnowledgeResult{FAQs: []struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}{{Question: "Q", Answer: "A"}}}, nil
}

// Test: Parse with images only
func TestParseWithImages_ImagesOnly(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{})

	imageURLs := []string{
		"https://example.com/menu1.jpg",
		"https://example.com/menu2.jpg",
	}

	items, err := p.ParseWithImages("", imageURLs, "products")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Check first item (price range)
	if items[0].Name != "iPhone 15 Pro" {
		t.Errorf("expected name 'iPhone 15 Pro', got %s", items[0].Name)
	}
	if items[0].MinPrice == nil || *items[0].MinPrice != 1500000 {
		t.Errorf("expected min_price 1500000")
	}
	if items[0].MaxPrice == nil || *items[0].MaxPrice != 1650000 {
		t.Errorf("expected max_price 1650000")
	}

	// Check second item (single price)
	if items[1].Name != "Fried Rice" {
		t.Errorf("expected name 'Fried Rice', got %s", items[1].Name)
	}
	if items[1].Price == nil || *items[1].Price != 4500 {
		t.Errorf("expected price 4500")
	}
}

// Test: Parse with text and images
func TestParseWithImages_Mixed(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{})

	text := "Additional context: Nigerian restaurant menu"
	imageURLs := []string{"https://example.com/menu.jpg"}

	items, err := p.ParseWithImages(text, imageURLs, "menu")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) == 0 {
		t.Fatal("expected items to be extracted")
	}
}

// Test: Fallback to text-only when no images
func TestParseWithImages_NoImages(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{})

	text := "Jollof Rice - ₦3,500"
	items, err := p.ParseWithImages(text, []string{}, "menu")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should fall back to text-only parsing
	// Note: May trigger fallback mechanism and return full text as name
	if len(items) == 0 {
		t.Fatal("expected at least 1 item from text parsing")
	}

	// Check that we got an item with a name
	if items[0].Name == "" {
		t.Error("expected item to have a name")
	}

	// Name should contain "Jollof Rice" (either exact match or full line)
	if items[0].Name != "Jollof Rice" && items[0].Name != "Jollof Rice - ₦3,500" {
		t.Errorf("unexpected name: %s", items[0].Name)
	}
}

// Test: Too many images validation
func TestParseWithImages_TooManyImages(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{})

	imageURLs := make([]string, 10)
	for i := 0; i < 10; i++ {
		imageURLs[i] = "https://example.com/image.jpg"
	}

	_, err := p.ParseWithImages("", imageURLs, "products")
	if err == nil {
		t.Fatal("expected error for too many images")
	}

	if err.Error() != "too many images (10). Max allowed is 5" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// Test: Vision fails, fallback to text
func TestParseWithImages_VisionFailsFallback(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{shouldFailVision: true})

	text := "Jollof Rice - ₦3,500"
	imageURLs := []string{"https://example.com/menu.jpg"}

	items, err := p.ParseWithImages(text, imageURLs, "menu")

	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}

	// Should have used text-only fallback
	if len(items) != 1 {
		t.Fatalf("expected 1 item from fallback, got %d", len(items))
	}
}

// Test: Vision fails with no text should error
func TestParseWithImages_VisionFailsNoText(t *testing.T) {
	p := NewAIParser(&fakeVisionClient{shouldFailVision: true})

	imageURLs := []string{"https://example.com/menu.jpg"}

	_, err := p.ParseWithImages("", imageURLs, "products")

	if err == nil {
		t.Fatal("expected error when vision fails and no text provided")
	}
}
