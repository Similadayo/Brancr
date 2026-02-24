package bot

import (
	"context"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestSendEscalation_SkipsAlertReasons(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	db.AutoMigrate(&storage.Tenant{}, &storage.Interaction{}, &storage.PendingEscalation{})

	logger := testutil.TestLogger(t)
	esch := &EscalationHandler{DB: db, Logger: logger}

	// Create tenant and interaction
	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	inter := &storage.Interaction{TenantID: tenant.ID, Platform: "whatsapp", Direction: "incoming", Content: "Alert message"}
	db.Create(inter)

	// Ensure pending escalations table is empty
	db.Where("1=1").Delete(&storage.PendingEscalation{})

	// Call with an alert-like reason
	if err := esch.SendEscalation(context.Background(), tenant.ID, inter.ID, "Jane", "whatsapp", "I have paid", "reply", "payment", "neutral", 0.9, "payment_alert"); err != nil {
		t.Fatalf("expected no error when skipping alert reason, got %v", err)
	}

	// Check database instead of in-memory map
	var count int64
	db.Model(&storage.PendingEscalation{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no pending escalations after alert reason, got %d", count)
	}
}

func TestSendEscalation_CreatesPendingForNormalReason(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	db.AutoMigrate(&storage.Tenant{}, &storage.Interaction{}, &storage.PendingEscalation{})

	logger := testutil.TestLogger(t)
	esch := &EscalationHandler{DB: db, Logger: logger}

	// Create tenant and interaction
	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	inter := &storage.Interaction{TenantID: tenant.ID, Platform: "whatsapp", Direction: "incoming", Content: "I need help"}
	db.Create(inter)

	// Ensure pending escalations table is empty
	db.Where("1=1").Delete(&storage.PendingEscalation{})

	// Call with a normal escalation reason
	if err := esch.SendEscalation(context.Background(), tenant.ID, inter.ID, "Jane", "whatsapp", "Please advise", "reply", "complaint", "negative", 0.95, "tenant_mode_human"); err != nil {
		t.Fatalf("unexpected error sending escalation: %v", err)
	}

	// Check database instead of in-memory map
	var count int64
	db.Model(&storage.PendingEscalation{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 pending escalation after normal reason, got %d", count)
	}
	// cleanup
	db.Where("1=1").Delete(&storage.PendingEscalation{})
}
