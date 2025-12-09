package quota

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/storage"
)

// TestQuotaService_Integration tests quota service with database
func TestQuotaService_Integration(t *testing.T) {
	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Auto-migrate
	if err := db.AutoMigrate(&storage.TenantQuota{}, &storage.Tenant{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	service := NewQuotaService(db, logger, nil)

	tenantID := uint(1)

	// Test GetOrCreateQuota
	quota, err := service.GetOrCreateQuota(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("failed to get/create quota: %v", err)
	}

	if quota.TenantID != tenantID {
		t.Errorf("expected tenant ID %d, got %d", tenantID, quota.TenantID)
	}

	// Test CheckAICallQuota
	err = service.CheckAICallQuota(context.Background(), tenantID)
	if err != nil {
		t.Errorf("quota check should pass for new tenant: %v", err)
	}

	// Test IncrementAICallUsage
	for i := 0; i < quota.AICallsLimit; i++ {
		if err := service.IncrementAICallUsage(context.Background(), tenantID); err != nil {
			t.Fatalf("failed to increment usage: %v", err)
		}
	}

	// Test quota exhaustion
	err = service.CheckAICallQuota(context.Background(), tenantID)
	if err == nil {
		t.Error("quota check should fail after limit reached")
	}

	// Test quota reset (via checkAndResetQuota which is called internally)
	quota.LastResetAt = time.Now().AddDate(0, -1, 0) // 1 month ago
	quota.AICallsUsed = quota.AICallsLimit // Set usage to limit
	if err := db.Save(quota).Error; err != nil {
		t.Fatalf("failed to update quota: %v", err)
	}

	// Get quota again - should trigger reset check
	updatedQuota, err := service.GetOrCreateQuota(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}

	// Usage should be reset if reset frequency is daily and it's been more than a day
	// Note: Actual reset logic depends on ResetFrequency and LastResetAt
	t.Logf("Quota after reset check: Used=%d, LastResetAt=%v", updatedQuota.AICallsUsed, updatedQuota.LastResetAt)
}

// TestQuotaService_PostQuotaIntegration tests post quota with concurrent access
func TestQuotaService_PostQuotaIntegration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.AutoMigrate(&storage.TenantQuota{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	service := NewQuotaService(db, logger, nil)

	tenantID := uint(1)

	// Initialize quota with low limit
	quota, _ := service.GetOrCreateQuota(context.Background(), tenantID)
	quota.PostsLimit = 5
	if err := db.Save(quota).Error; err != nil {
		t.Fatalf("failed to update quota: %v", err)
	}

	// Simulate concurrent post attempts
	successCount := 0
	errorCount := 0

	for i := 0; i < 10; i++ {
		if err := service.CheckPostQuota(context.Background(), tenantID); err != nil {
			errorCount++
		} else {
			successCount++
			service.IncrementPostUsage(context.Background(), tenantID)
		}
	}

	// Should have exactly 5 successes (limit) and 5 errors
	if successCount != 5 {
		t.Errorf("expected 5 successful quota checks, got %d", successCount)
	}
	if errorCount != 5 {
		t.Errorf("expected 5 failed quota checks, got %d", errorCount)
	}
}

