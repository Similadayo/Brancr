package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestMemoryService_SaveConversation(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Tenant",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	memService := &MemoryService{
		DB:      db,
		Encoder: nil, // Not needed for conversation storage
	}

	// Test: Save conversation
	ctx := context.Background()
	err := memService.SaveConversation(ctx, testTenantID, "Do you have stock?", "Yes, we have 10 units available")

	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Verify: Check if memory was stored in DB
	var memories []storage.MemoryItem
	db.Where("tenant_id = ? AND category = ?", testTenantID, "conversation").Find(&memories)

	if len(memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(memories))
	}

	if !strings.Contains(memories[0].Text, "Do you have stock?") {
		t.Errorf("memory text doesn't contain user message")
	}
}

func TestMemoryService_SaveConversation_EmptyMessage(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	memService := &MemoryService{
		DB:      db,
		Encoder: nil,
	}

	// Test: Empty message should not create memory
	err := memService.SaveConversation(context.Background(), testTenantID, "", "Reply")
	if err != nil {
		t.Fatalf("SaveConversation returned error on empty message: %v", err)
	}

	var memories []storage.MemoryItem
	db.Where("tenant_id = ?", testTenantID).Find(&memories)

	if len(memories) != 0 {
		t.Errorf("expected no memories for empty message, got %d", len(memories))
	}
}

func TestMemoryService_SaveFact(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Tenant",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	// Mock embedding client
	memService := &MemoryService{
		DB:      db,
		Encoder: nil, // Test with nil - real encoder would be injected
	}

	// Test: Save fact
	ctx := context.Background()
	fact := "We specialize in sustainable fashion and deliver within 24 hours"
	err := memService.SaveFact(ctx, testTenantID, fact, "business_description")

	if err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}

	// Verify: Fact is stored
	var memories []storage.MemoryItem
	db.Where("tenant_id = ? AND category = ?", testTenantID, "business_description").Find(&memories)

	if len(memories) != 1 {
		t.Errorf("expected 1 fact, got %d", len(memories))
	}
}

func TestMemoryService_SaveFact_Idempotent(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Tenant",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	memService := &MemoryService{
		DB:      db,
		Encoder: nil, // Test with nil
	}

	// Test: Save same fact twice (should not create duplicate)
	ctx := context.Background()
	fact := "Same fact for idempotency test"

	err1 := memService.SaveFact(ctx, testTenantID, fact, "test_category")
	if err1 != nil {
		t.Fatalf("first SaveFact failed: %v", err1)
	}

	err2 := memService.SaveFact(ctx, testTenantID, fact, "test_category")
	if err2 != nil {
		t.Fatalf("second SaveFact failed: %v", err2)
	}

	var memories []storage.MemoryItem
	db.Where("tenant_id = ? AND category = ?", testTenantID, "test_category").Find(&memories)

	if len(memories) != 1 {
		t.Errorf("SaveFact should be idempotent, expected 1 memory, got %d", len(memories))
	}
}

func TestMemoryService_SearchContext(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Tenant",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	// Store mock memories
	memories := []storage.MemoryItem{
		{
			TenantID:  testTenantID,
			Text:      "Organic cotton t-shirts with sustainable packaging",
			Embedding: []float32{0.1, 0.2, 0.3},
			Category:  "product",
			CreatedAt: time.Now().Add(-1 * time.Hour),
		},
		{
			TenantID:  testTenantID,
			Text:      "We deliver worldwide within 5-7 business days",
			Embedding: []float32{0.2, 0.3, 0.1},
			Category:  "shipping",
			CreatedAt: time.Now(),
		},
		{
			TenantID:  testTenantID,
			Text:      "Customer support available 24/7 via chat",
			Embedding: []float32{0.3, 0.1, 0.2},
			Category:  "support",
			CreatedAt: time.Now().Add(-30 * time.Minute),
		},
	}

	for _, mem := range memories {
		db.Create(&mem)
	}

	memService := &MemoryService{
		DB:      db,
		Encoder: nil, // Test with nil
	}

	// Test: Search context
	results := memService.SearchContext(context.Background(), testTenantID, "shipping and delivery", 2)

	if len(results) == 0 {
		t.Errorf("SearchContext returned no results")
	}

	// Results should include delivery-related content
	foundDelivery := false
	for _, r := range results {
		if strings.Contains(r, "deliver") {
			foundDelivery = true
		}
	}

	if !foundDelivery {
		t.Errorf("SearchContext didn't return delivery-related memory")
	}
}

func TestMemoryService_SearchContext_EmptyQuery(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	memService := &MemoryService{
		DB:      db,
		Encoder: nil,
	}

	// Test: Empty query should return nil
	results := memService.SearchContext(context.Background(), testTenantID, "", 5)

	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestMemoryService_StoreContext(t *testing.T) {
	db := testutil.TestDB(t)
	testTenantID := uuid.New()

	tenant := storage.Tenant{
		Base:  storage.Base{ID: testTenantID},
		Name:  "Test Tenant",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	memService := &MemoryService{
		DB:      db,
		Encoder: nil,
	}

	// Test: StoreContext (wrapper around SaveConversation)
	memService.StoreContext(context.Background(), testTenantID, "Hello", "Hi, how can I help?")

	var memories []storage.MemoryItem
	db.Where("tenant_id = ?", testTenantID).Find(&memories)

	if len(memories) != 1 {
		t.Errorf("expected 1 stored context, got %d", len(memories))
	}
}

// Mock EmbeddingClient for testing
type MockEmbeddingClient struct {
	GenerateEmbeddingFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *MockEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if m.GenerateEmbeddingFunc != nil {
		return m.GenerateEmbeddingFunc(ctx, text)
	}
	return nil, errors.New("mock error")
}
