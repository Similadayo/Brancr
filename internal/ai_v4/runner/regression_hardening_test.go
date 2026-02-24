package runner

import (
	"strings"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
)

func TestRegression_Negotiation_LockedSKU_NoDriftOnConfirm(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "in_stock"},
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock"},
		},
	}
	lock := &v4ctx.OrderSnapshot{
		ActiveItemID:    itemIDFromName("iPhone 14 (128GB)"),
		ActiveVariantID: variantIDFromName("iPhone 14 (128GB)"),
	}
	reqItemID, reqVariantID, reqName := extractRequestedModelVariant("Can I confirm iPhone 13 Pro Max (256GB)?", knowledge)
	if isLockedSKUCompatible(lock.ActiveItemID, lock.ActiveVariantID, reqItemID, reqVariantID) {
		t.Fatalf("expected mismatch for locked SKU")
	}
	reply := buildSwitchRequiredReply("iPhone 14 (128GB)", reqName)
	if !strings.Contains(strings.ToLower(reply), "change") {
		t.Fatalf("expected switch clarification (containing 'change'), got %q", reply)
	}
}

func TestRegression_UnavailableModel_NoAutoSubstitution(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "Samsung S22 (256GB)", Price: 980000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "Tecno Camon 20 Pro (128GB)", Price: 340000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	requested, alternatives, hit := detectUnavailableModelRequest("I want iPhone 17 Pro Max 256GB", knowledge)
	if !hit {
		t.Fatalf("expected unavailable guard hit")
	}
	if requested == "" {
		t.Fatalf("expected requested model")
	}
	if len(alternatives) == 0 || len(alternatives) > 3 {
		t.Fatalf("expected 1-3 alternatives, got %d", len(alternatives))
	}
	reply := buildUnavailableWithAlternativesReply(requested, alternatives)
	if !strings.Contains(strings.ToLower(reply), "alternatives") && !strings.Contains(strings.ToLower(reply), "switch to any") {
		t.Fatalf("expected explicit alternative or switch requirement, got %q", reply)
	}
}

func TestRegression_SpecQuestion_PriorityOverPickupPrompt(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{
				Name:         "iPhone 14 (128GB)",
				Price:        1050000,
				Currency:     "NGN",
				Availability: "in_stock",
				Description:  "Supports eSIM and physical nano-SIM.",
			},
		},
	}
	orderSnapshot := &v4ctx.OrderSnapshot{
		Stage: "awaiting_pickup_time",
		Items: []v4ctx.OrderItem{{Name: "iPhone 14 (128GB)", Quantity: 1, UnitPrice: 1050000}},
	}
	reply := buildSpecPriorityReply("Is the iPhone 14 eSIM or physical SIM?", "iPhone 14 (128GB)", knowledge, orderSnapshot)
	if reply == "" || !strings.Contains(strings.ToLower(reply), "esim") {
		t.Fatalf("expected direct spec answer, got %q", reply)
	}
	if resume := resumePromptForStage(orderSnapshot.Stage); resume == "" {
		t.Fatalf("expected resume prompt for pickup stage")
	}
}

func TestRegression_Closeout_OneFinalMessageOnly(t *testing.T) {
	conv := v4ctx.DefaultConversationSnapshot()
	if !isCloseoutMessage("No thank you") {
		t.Fatalf("expected closeout phrase detection")
	}
	markCloseout(conv)
	if conv.CloseoutState != "closed" || conv.CloseoutMessageCount != 1 {
		t.Fatalf("expected closed state after first closeout, got state=%q count=%d", conv.CloseoutState, conv.CloseoutMessageCount)
	}
	// Repeated closeout should not require reopen intent.
	if isReopenBuyingIntent("okay") {
		t.Fatalf("did not expect reopen intent for neutral acknowledgment")
	}
}

func TestRegression_OfferAcceptance_MustMatchLockedSKU(t *testing.T) {
	activeItemID := itemIDFromName("iPhone 14 (128GB)")
	activeVariantID := variantIDFromName("iPhone 14 (128GB)")
	reqItemID := itemIDFromName("iPhone 13 Pro Max (256GB)")
	reqVariantID := variantIDFromName("iPhone 13 Pro Max (256GB)")

	if isLockedSKUCompatible(activeItemID, activeVariantID, reqItemID, reqVariantID) {
		t.Fatalf("expected locked SKU incompatibility")
	}
}

func TestRegression_DeterministicConfirmation_FromSnapshotOnly(t *testing.T) {
	orderSnapshot := &v4ctx.OrderSnapshot{
		Items: []v4ctx.OrderItem{
			{Name: "iPhone 14 (128GB)", Quantity: 1, UnitPrice: 1050000},
		},
		AgreedUnitPrice: 1000000,
		TotalPrice:      1000000,
		Currency:        "NGN",
	}
	reply := buildDeterministicOrderConfirmation(orderSnapshot, storage.DeliveryConfig{
		DeliveryEnabled: true,
		PickupEnabled:   true,
	})
	if !strings.Contains(reply, "iPhone 14 (128GB)") {
		t.Fatalf("expected snapshot item in confirmation, got %q", reply)
	}
	if !strings.Contains(reply, "TOTAL NGN1,000,000") {
		t.Fatalf("expected deterministic snapshot total, got %q", reply)
	}
	if strings.Contains(reply, "iPhone 13 Pro Max") {
		t.Fatalf("unexpected SKU drift in confirmation: %q", reply)
	}
}
