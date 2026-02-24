package tenantapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestParseProducts_Handler(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "parse@example.com", "pass", "Parse Tenant")

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse", map[string]interface{}{"raw_input": "Jollof Rice - ₦3,500\nFried Rice - 4000"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.parseProducts(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp []ParsedItem
	rr.BodyJSON(t, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 parsed items, got %d", len(resp))
	}
}

func TestParseProducts_EmptyBody(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "parse2@example.com", "pass", "Parse Tenant 2")

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse", map[string]interface{}{}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.parseProducts(rr, req)

	rr.AssertStatus(t, http.StatusBadRequest)
}

func TestParseProducts_TooLarge(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "parse3@example.com", "pass", "Parse Tenant 3")

	// create input larger than 64KB
	big := make([]byte, 70*1024)
	for i := range big {
		big[i] = 'a'
	}
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse", map[string]interface{}{"raw_input": string(big)}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.parseProducts(rr, req)

	rr.AssertStatus(t, http.StatusRequestEntityTooLarge)
}

func TestParseProducts_TooManyItems(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "parse4@example.com", "pass", "Parse Tenant 4")

	// create 2001 lines
	var sb strings.Builder
	for i := 0; i < 2001; i++ {
		sb.WriteString("Item " + strconv.Itoa(i) + "\n")
	}
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse", map[string]interface{}{"raw_input": sb.String()}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.parseProducts(rr, req)

	rr.AssertStatus(t, http.StatusRequestEntityTooLarge)
}

func TestParseProducts_AIErrorFallsBack(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "parse5@example.com", "pass", "Parse Tenant 5")

	// Replace AI client with one that returns invalid JSON
	h.AIClient = &badAIClient{}
	input := "Jollof Rice - ₦3,500\nFried Rice - ₦4,000"
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse", map[string]interface{}{"raw_input": input}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.parseProducts(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp []ParsedItem
	rr.BodyJSON(t, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 parsed items when AI fails, got %d", len(resp))
	}
}

// badAIClient returns non-JSON to simulate AI failure
type badAIClient struct{}

func (b *badAIClient) Generate(ctx context.Context, messages []ai.Message) (string, error) {
	return "I am not a JSON response", nil
}

func (b *badAIClient) GenerateWithUsage(ctx context.Context, messages []ai.Message) (*ai.GenerateResult, error) {
	return &ai.GenerateResult{
		Content: "I am not a JSON response",
		TokenUsage: &ai.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (b *badAIClient) GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error) {
	return "", nil
}
func (b *badAIClient) GenerateWithVisionWithUsage(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, *ai.TokenUsage, error) {
	return "", &ai.TokenUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}, nil
}
func (b *badAIClient) AnalyzeImage(ctx context.Context, imageURL string) (string, error) {
	return "", nil
}
func (b *badAIClient) ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return &ai.ParseMenuTextResult{
		Items: []ai.ParsedMenuItem{},
	}, nil
}

func (b *badAIClient) GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*ai.BusinessProfileResult, error) {
	return nil, fmt.Errorf("ai error")
}

func (b *badAIClient) GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*ai.KnowledgeResult, error) {
	return nil, fmt.Errorf("ai error")
}

func (b *badAIClient) ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ai.ParseMenuTextResult, error) {
	return nil, fmt.Errorf("ai error")
}
