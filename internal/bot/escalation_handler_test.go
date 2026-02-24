package bot

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func migrateEscalationHandlerTestModels(t *testing.T, db *gorm.DB) {
	err := db.AutoMigrate(
		&storage.Customer{},
		&storage.Interaction{},
		&storage.SalesConversation{},
		&storage.Order{},
		&storage.Payment{},
	)
	if err != nil {
		t.Fatalf("failed to migrate test models: %v", err)
	}
}

func TestEnsureOrderAndPayment_ReusesSinglePendingOrderWhenNoAmountInMessage(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateEscalationHandlerTestModels(t, db)

	logger := zap.NewNop()
	h := &EscalationHandler{DB: db, Logger: logger}

	tenantID := uuid.New()
	customer := storage.Customer{
		TenantID:   tenantID,
		Channel:    "whatsapp",
		PlatformID: "2348000000000",
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&customer).Error; err != nil {
		t.Fatalf("failed to create customer: %v", err)
	}

	order := storage.Order{
		TenantID:         tenantID,
		CustomerID:       customer.ID,
		OrderNumber:      "ORD-TEST-1",
		PaymentReference: "REF-TEST-1",
		Status:           "pending",
		TotalAmount:      25000,
		Currency:         "NGN",
		Platform:         "whatsapp",
		CreatedAt:        time.Now(),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}
	// Ensure it's outside the 30-minute duplicate window, but within the 7-day reuse window.
	if err := db.Model(&order).Update("created_at", time.Now().Add(-2*time.Hour)).Error; err != nil {
		t.Fatalf("failed to backdate order: %v", err)
	}

	interaction := storage.Interaction{
		TenantID:       tenantID,
		CustomerID:     customer.ID,
		ConversationID: uuid.Nil, // simulate missing conversation id
		Platform:       "whatsapp",
		Direction:      "incoming",
		MessageType:    "text",
		Content:        "I've paid",
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&interaction).Error; err != nil {
		t.Fatalf("failed to create interaction: %v", err)
	}

	h.ensureOrderAndPayment(context.Background(), &interaction)

	var payments []storage.Payment
	if err := db.Where("tenant_id = ? AND order_id = ?", tenantID, order.ID).Find(&payments).Error; err != nil {
		t.Fatalf("failed to query payments: %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment for order, got %d", len(payments))
	}

	p := payments[0]
	if p.Amount != order.TotalAmount {
		t.Fatalf("expected payment amount %v, got %v", order.TotalAmount, p.Amount)
	}
	if p.PaymentReference != order.PaymentReference {
		t.Fatalf("expected payment reference %q, got %q", order.PaymentReference, p.PaymentReference)
	}
	if p.ReceiptID == "" {
		t.Fatalf("expected receipt id to be set")
	}
	if p.PortalToken == "" {
		t.Fatalf("expected portal token to be set")
	}
}
