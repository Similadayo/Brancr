package workflows

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

func migrateWorkflowPaymentTestModels(t *testing.T, db *gorm.DB) {
	if err := db.AutoMigrate(
		&storage.Customer{},
		&storage.Interaction{},
		&storage.Order{},
		&storage.Payment{},
	); err != nil {
		t.Fatalf("failed to migrate test models: %v", err)
	}
}

func TestHandlePaymentConfirmation_AttachesToSinglePendingOrder(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateWorkflowPaymentTestModels(t, db)

	w := &EscalationWorkflow{DB: db, Logger: zap.NewNop()}

	tenantID := uuid.New()
	customer := storage.Customer{TenantID: tenantID, Channel: "whatsapp", PlatformID: "2348000000000", CreatedAt: time.Now()}
	if err := db.Create(&customer).Error; err != nil {
		t.Fatalf("failed to create customer: %v", err)
	}

	order := storage.Order{
		TenantID:         tenantID,
		CustomerID:       customer.ID,
		OrderNumber:      "ORD-WF-1",
		PaymentReference: "REF-WF-1",
		Status:           "pending",
		TotalAmount:      15000,
		Currency:         "NGN",
		Platform:         "whatsapp",
		CreatedAt:        time.Now().Add(-2 * time.Hour),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	interaction := storage.Interaction{
		TenantID:       tenantID,
		CustomerID:     customer.ID,
		ConversationID: uuid.Nil,
		Platform:       "whatsapp",
		Direction:      "incoming",
		MessageType:    "text",
		Content:        "I've paid",
		DetectedIntent: "payment",
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&interaction).Error; err != nil {
		t.Fatalf("failed to create interaction: %v", err)
	}

	if err := w.handlePaymentConfirmation(context.Background(), &interaction); err != nil {
		t.Fatalf("handlePaymentConfirmation returned error: %v", err)
	}

	var payment storage.Payment
	if err := db.Where("tenant_id = ? AND order_id = ?", tenantID, order.ID).First(&payment).Error; err != nil {
		// Diagnostic: dump pending orders for debugging in CI
		var pendingOrders []storage.Order
		db.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND status = ? AND total_amount > 0", tenantID, customer.ID, "whatsapp", "pending").Order("created_at DESC").Find(&pendingOrders)
		t.Fatalf("expected order-linked payment, got error: %v. Found %d pending orders: %+v", err, len(pendingOrders), pendingOrders)
	}
	if payment.Amount != order.TotalAmount {
		t.Fatalf("expected payment amount %v, got %v", order.TotalAmount, payment.Amount)
	}
	if payment.Status != "verified" {
		t.Fatalf("expected payment status verified, got %q", payment.Status)
	}
	if payment.ReceiptID == "" || payment.PortalToken == "" {
		t.Fatalf("expected receipt_id and portal_token to be set")
	}

	var updated storage.Order
	if err := db.First(&updated, order.ID).Error; err != nil {
		t.Fatalf("failed to reload order: %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected order status completed, got %q", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Fatalf("expected order completed_at to be set")
	}
}
