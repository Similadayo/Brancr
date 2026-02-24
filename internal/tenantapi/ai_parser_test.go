package tenantapi

import (
	"context"
	"testing"

	"github.com/similadayo/brancr/internal/ai"
)

// fake AI client for parser tests
type fakeExtractorClient struct{}

func (f *fakeExtractorClient) Generate(ctx context.Context, messages []ai.Message) (string, error) {
	// Return raw JSON with string prices (matching rawParsedItem format)
	return `[{"name": "Jollof Rice", "price": "3500", "currency": "NGN"}]`, nil
}

func (f *fakeExtractorClient) GenerateWithUsage(ctx context.Context, messages []ai.Message) (*ai.GenerateResult, error) {
	return &ai.GenerateResult{
		Content: `[{"name": "Jollof Rice", "price": "3500", "currency": "NGN"}]`,
		TokenUsage: &ai.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (f *fakeExtractorClient) GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error) {
	return "", nil
}
func (f *fakeExtractorClient) GenerateWithVisionWithUsage(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, *ai.TokenUsage, error) {
	return "", &ai.TokenUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}, nil
}
func (f *fakeExtractorClient) AnalyzeImage(ctx context.Context, imageURL string) (string, error) {
	return "", nil
}
func (f *fakeExtractorClient) ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return &ai.ParseMenuTextResult{
		Items: []ai.ParsedMenuItem{
			{Name: "Jollof Rice", Price: 3500.0, Currency: "NGN"},
		},
	}, nil
}

func (f *fakeExtractorClient) GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*ai.BusinessProfileResult, error) {
	return &ai.BusinessProfileResult{
		Name:            "Generated Business",
		Description:     "AI generated description",
		ConfidenceScore: "high",
	}, nil
}

func (f *fakeExtractorClient) GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*ai.KnowledgeResult, error) {
	return &ai.KnowledgeResult{
		FAQs: []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		}{
			{Question: "Test Question", Answer: "Test Answer"},
		},
		Keywords: []string{"test", "keyword"},
		KnowledgeBase: []struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Content  string `json:"content"`
		}{
			{Title: "Test KB", Category: "General", Content: "Test Content"},
		},
	}, nil
}

func (f *fakeExtractorClient) ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return &ai.ParseMenuTextResult{
		Items: []ai.ParsedMenuItem{
			{Name: "Jollof Rice", Price: 3500.0, Currency: "NGN"},
		},
	}, nil
}

func TestAIParser_ParsesJSON(t *testing.T) {
	p := NewAIParser(&fakeExtractorClient{})
	items, err := p.Parse("Jollof Rice - ₦3,500", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "Jollof Rice" {
		t.Fatalf("expected name Jollof Rice, got %s", items[0].Name)
	}
}
