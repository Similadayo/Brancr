package repositories

import (
	"context"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestTenantRepository(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Run migrations
	if err := db.AutoMigrate(&storage.Tenant{}, &storage.BusinessProfile{}); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	repo := NewTenantRepository(db)
	ctx := context.Background()

	t.Run("Create and GetByID", func(t *testing.T) {
		tenant := &storage.Tenant{
			Name:            "Test Tenant",
			Email:           "test@example.com",
			OwnerTelegramID: 12345,
			Status:          "active",
		}

		if err := repo.Create(ctx, tenant); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		retrieved, err := repo.GetByID(ctx, tenant.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}

		if retrieved.Name != tenant.Name {
			t.Errorf("GetByID() name = %v, want %v", retrieved.Name, tenant.Name)
		}
	})

	t.Run("GetByOwnerTelegramID", func(t *testing.T) {
		tenant, err := repo.GetByOwnerTelegramID(ctx, 12345)
		if err != nil {
			t.Fatalf("GetByOwnerTelegramID() error = %v", err)
		}

		if tenant.OwnerTelegramID != 12345 {
			t.Errorf("GetByOwnerTelegramID() OwnerTelegramID = %v, want %v", tenant.OwnerTelegramID, 12345)
		}
	})
}

func TestCustomerRepository(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	if err := db.AutoMigrate(&storage.Customer{}); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	repo := NewCustomerRepository(db)
	ctx := context.Background()

	t.Run("CreateOrUpdate", func(t *testing.T) {
		customer := &storage.Customer{
			TenantID:    1,
			Channel:     "telegram",
			PlatformID:  "123",
			DisplayName: "Test User",
		}

		if err := repo.CreateOrUpdate(ctx, customer); err != nil {
			t.Fatalf("CreateOrUpdate() error = %v", err)
		}

		// Update existing
		customer.DisplayName = "Updated User"
		if err := repo.CreateOrUpdate(ctx, customer); err != nil {
			t.Fatalf("CreateOrUpdate() update error = %v", err)
		}
	})
}
