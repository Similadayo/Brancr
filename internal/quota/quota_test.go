package quota

import (
	"context"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestQuotaService_GetOrCreateQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	// Create a test tenant
	tenant := &storage.Tenant{
		Name:     "Test Tenant",
		Email:    "test@example.com",
		Status:   "active",
		PlanType: "free",
	}
	if err := db.Create(tenant).Error; err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Get or create quota
	quota, err := service.GetOrCreateQuota(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get or create quota: %v", err)
	}

	if quota.TenantID != tenant.ID {
		t.Errorf("Expected tenant ID %d, got %d", tenant.ID, quota.TenantID)
	}

	if quota.AICallsLimit != 100 {
		t.Errorf("Expected default AI calls limit 100, got %d", quota.AICallsLimit)
	}

	if quota.PostsLimit != 50 {
		t.Errorf("Expected default posts limit 50, got %d", quota.PostsLimit)
	}

	// Get again should return same quota
	quota2, err := service.GetOrCreateQuota(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get quota: %v", err)
	}

	if quota2.ID != quota.ID {
		t.Errorf("Expected same quota ID, got different IDs")
	}
}

func TestQuotaService_CheckAICallQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	tenant := &storage.Tenant{
		Name:   "Test Tenant",
		Email:  "test@example.com",
		Status: "active",
	}
	db.Create(tenant)

	// Create quota with limit
	quota := &storage.TenantQuota{
		TenantID:     tenant.ID,
		AICallsLimit: 10,
		AICallsUsed:  0,
	}
	db.Create(quota)

	// Should pass when under limit
	err := service.CheckAICallQuota(ctx, tenant.ID)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Update to exceed limit
	db.Model(quota).Update("ai_calls_used", 10)

	// Should fail when at limit
	err = service.CheckAICallQuota(ctx, tenant.ID)
	if err == nil {
		t.Error("Expected error when quota exceeded, got nil")
	}
	if err != nil && err != ErrQuotaExceeded && err.Error()[:13] != "quota exceeded" {
		t.Errorf("Expected quota exceeded error, got: %v", err)
	}
}

func TestQuotaService_IncrementAICallUsage(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	tenant := &storage.Tenant{
		Name:   "Test Tenant",
		Email:  "test@example.com",
		Status: "active",
	}
	db.Create(tenant)

	// Should increment successfully
	err := service.IncrementAICallUsage(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to increment: %v", err)
	}

	// Verify increment
	quota, err := service.GetQuotaUsage(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get quota: %v", err)
	}

	if quota.AICallsUsed != 1 {
		t.Errorf("Expected AI calls used to be 1, got %d", quota.AICallsUsed)
	}
}

func TestQuotaService_CheckPostQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	tenant := &storage.Tenant{
		Name:   "Test Tenant",
		Email:  "test@example.com",
		Status: "active",
	}
	db.Create(tenant)

	quota := &storage.TenantQuota{
		TenantID:  tenant.ID,
		PostsLimit: 5,
		PostsUsed:  0,
	}
	db.Create(quota)

	// Should pass when under limit
	err := service.CheckPostQuota(ctx, tenant.ID)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Update to exceed limit
	db.Model(quota).Update("posts_used", 5)

	// Should fail when at limit
	err = service.CheckPostQuota(ctx, tenant.ID)
	if err == nil {
		t.Error("Expected error when quota exceeded, got nil")
	}
}

func TestQuotaService_CheckStorageQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	tenant := &storage.Tenant{
		Name:   "Test Tenant",
		Email:  "test@example.com",
		Status: "active",
	}
	db.Create(tenant)

	quota := &storage.TenantQuota{
		TenantID:      tenant.ID,
		StorageLimitMB: 1000,
		StorageUsedMB: 500,
	}
	db.Create(quota)

	// Should pass when under limit
	err := service.CheckStorageQuota(ctx, tenant.ID, 400)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should fail when would exceed limit
	err = service.CheckStorageQuota(ctx, tenant.ID, 600)
	if err == nil {
		t.Error("Expected error when quota would be exceeded, got nil")
	}
}

func TestQuotaService_ResetQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	service := NewQuotaService(db, logger, nil)

	ctx := context.Background()

	tenant := &storage.Tenant{
		Name:   "Test Tenant",
		Email:  "test@example.com",
		Status: "active",
	}
	db.Create(tenant)

	// Create quota with old reset time
	oldTime := time.Now().Add(-25 * time.Hour)
	quota := &storage.TenantQuota{
		TenantID:     tenant.ID,
		AICallsLimit: 100,
		AICallsUsed:  50,
		PostsUsed:    20,
		LastResetAt:  oldTime,
		ResetFrequency: "daily",
	}
	db.Create(quota)

	// Get quota should trigger reset
	quota, err := service.GetOrCreateQuota(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Failed to get quota: %v", err)
	}

	// Should be reset
	if quota.AICallsUsed != 0 {
		t.Errorf("Expected AI calls to be reset to 0, got %d", quota.AICallsUsed)
	}

	if quota.PostsUsed != 0 {
		t.Errorf("Expected posts to be reset to 0, got %d", quota.PostsUsed)
	}
}

