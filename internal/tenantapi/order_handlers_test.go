package tenantapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestGetOrder_ReturnsTopLevelFields(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate models
	if err := db.AutoMigrate(&storage.Tenant{}, &storage.Order{}, &storage.Payment{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	tenant := testutil.CreateTestTenant(t, db, "t@example.com", "pass", "TestTenant")

	order := storage.Order{
		TenantID:         tenant.ID,
		CustomerID:       uuid.New(),
		OrderNumber:      "ORD-1",
		PaymentReference: "PR-1",
		TotalAmount:      100,
		Currency:         "NGN",
		CustomerName:     "John Doe",
		Items:            "[]",
		Status:           "pending",
		Platform:         "whatsapp",
		CreatedAt:        time.Now(),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	h := NewHandler(db, testutil.TestLogger(t), testutil.TestConfig(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := testutil.NewTestResponseRecorder()
	req := testutil.TestRequestWithTenant(t, "GET", "/api/tenant/orders/1", nil, tenant.ID, tenant.Email)

	// Use the handler directly
	h.getOrder(rr, req)

	rr.AssertStatus(t, 200)

	var resp map[string]interface{}
	rr.BodyJSON(t, &resp)

	if _, ok := resp["order"]; !ok {
		t.Fatalf("expected order key in response")
	}
	if _, ok := resp["status"]; !ok {
		t.Fatalf("expected top-level status in response")
	}
}
