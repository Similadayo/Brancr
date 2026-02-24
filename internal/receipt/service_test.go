package receipt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func migrateReceiptTestModels(t *testing.T, db *gorm.DB) {
	if err := db.AutoMigrate(&storage.Order{}, &storage.Payment{}); err != nil {
		t.Fatalf("failed to migrate test models: %v", err)
	}
}

func TestGeneratePortalTokenAndURL_BuildsAPIPathAndTrimsSlash(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateReceiptTestModels(t, db)

	logger := zap.NewNop()
	svc := NewReceiptService(db, logger, "", "http://localhost:8080/", nil)

	tenantID := uuid.New()
	order := storage.Order{
		TenantID:         tenantID,
		CustomerID:       uuid.New(),
		OrderNumber:      "ORD-TEST-1",
		PaymentReference: "PR-TEST-1",
		TotalAmount:      100,
		Currency:         "NGN",
		CreatedAt:        time.Now(),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	token, url, err := svc.GeneratePortalTokenAndURL(context.Background(), order.ID, tenantID, svc.PublicURL)
	if err != nil {
		t.Fatalf("GeneratePortalTokenAndURL returned error: %v", err)
	}
	if token == "" {
		t.Fatalf("expected portal token, got empty")
	}
	if !strings.Contains(url, "/api/portal/order?token=") {
		t.Fatalf("expected API portal path in URL, got: %s", url)
	}
}
