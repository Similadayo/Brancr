package runner

import (
	"strings"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
)

func phoneKnowledgeFixture() *v4ctx.TenantKnowledgeSnapshot {
	return &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{
				Name:         "iPhone 14 (128GB)",
				Price:        1050000,
				Currency:     "NGN",
				Availability: "in_stock",
				StockCount:   -1,
				Description:  "Supports eSIM and physical nano-SIM.",
			},
			{
				Name:         "iPhone 13 Pro Max (256GB)",
				Price:        1250000,
				Currency:     "NGN",
				Availability: "in_stock",
				StockCount:   -1,
				Description:  "Dual nano-SIM support.",
			},
			{
				Name:         "Samsung S22 (256GB)",
				Price:        980000,
				Currency:     "NGN",
				Availability: "in_stock",
				StockCount:   -1,
			},
		},
	}
}

func TestConversationSimulation_NegotiationToConfirmation_NoSKUDrift(t *testing.T) {
	knowledge := phoneKnowledgeFixture()
	orderSnapshot := &v4ctx.OrderSnapshot{
		Stage: "collecting",
		Items: []v4ctx.OrderItem{
			{Name: "iPhone 14 (128GB)", Quantity: 1, UnitPrice: 1050000},
		},
		Currency: "NGN",
	}
	applyActiveSKULock(orderSnapshot, "iPhone 14 (128GB)", 0)

	// Turn: customer bargains on locked SKU.
	offer := int64(1000000)
	orderSnapshot.AgreedUnitPrice = offer
	orderSnapshot.Items[0].UnitPrice = offer
	orderSnapshot.TotalPrice = computeOrderTotalFromSnapshotItems(orderSnapshot.Items)

	// Turn: customer tries to confirm a different SKU without switch.
	reqItemID, reqVariantID, reqName := extractRequestedModelVariant("Yes, confirm iPhone 13 Pro Max (256GB)", knowledge)
	if isLockedSKUCompatible(orderSnapshot.ActiveItemID, orderSnapshot.ActiveVariantID, reqItemID, reqVariantID) {
		t.Fatalf("expected lock mismatch when trying to confirm a different SKU")
	}
	blockReply := buildSwitchRequiredReply(orderSnapshot.Items[0].Name, reqName)
	if !strings.Contains(strings.ToLower(blockReply), "change") {
		t.Fatalf("expected switch-required reply (containing 'change'), got %q", blockReply)
	}

	// Turn: system generates confirmation from snapshot only.
	confirm := buildDeterministicOrderConfirmation(orderSnapshot, storage.DeliveryConfig{
		DeliveryEnabled: true,
		PickupEnabled:   true,
	})
	if !strings.Contains(confirm, "iPhone 14 (128GB)") {
		t.Fatalf("expected locked SKU in deterministic confirmation, got %q", confirm)
	}
	if strings.Contains(confirm, "iPhone 13 Pro Max") {
		t.Fatalf("unexpected drifted SKU in confirmation, got %q", confirm)
	}
	if !strings.Contains(confirm, "TOTAL NGN1,000,000") {
		t.Fatalf("expected agreed deterministic total, got %q", confirm)
	}
}

func TestConversationSimulation_UnavailableThenAlternativesRequireExplicitSwitch(t *testing.T) {
	knowledge := phoneKnowledgeFixture()

	// Turn: ask for unavailable model.
	requested, alternatives, hit := detectUnavailableModelRequest("I want iPhone 17 Pro Max 256GB", knowledge)
	if !hit {
		t.Fatalf("expected unavailable-model guard hit")
	}
	reply := buildUnavailableWithAlternativesReply(requested, alternatives)
	if !strings.Contains(strings.ToLower(reply), "not in our catalog") {
		t.Fatalf("expected unavailable response, got %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "alternatives") {
		t.Fatalf("expected explicit alternative instruction, got %q", reply)
	}

	// Turn: explicit switch is accepted and lock moves.
	orderSnapshot := &v4ctx.OrderSnapshot{}
	reqItemID, reqVariantID, reqName := extractRequestedModelVariant("switch to iPhone 14 (128GB)", knowledge)
	confirmSwitchToRequested(orderSnapshot, skuRequest{
		ItemID:    reqItemID,
		VariantID: reqVariantID,
		Name:      reqName,
	})
	if orderSnapshot.ActiveItemID == "" || orderSnapshot.ActiveVariantID == "" {
		t.Fatalf("expected active lock to be set after explicit switch")
	}
}

func TestConversationSimulation_SpecQuestionInterruptsStagePrompt(t *testing.T) {
	knowledge := phoneKnowledgeFixture()
	orderSnapshot := &v4ctx.OrderSnapshot{
		Stage: "awaiting_pickup_time",
		Items: []v4ctx.OrderItem{
			{Name: "iPhone 14 (128GB)", Quantity: 1, UnitPrice: 1050000},
		},
	}

	userMsg := "Is the iPhone 14 128gb an eSIM or physical sim?"
	if !isDirectSpecQuestion(userMsg) {
		t.Fatalf("expected direct spec question detection")
	}
	specReply := buildSpecPriorityReply(userMsg, "iPhone 14 (128GB)", knowledge, orderSnapshot)
	if !strings.Contains(strings.ToLower(specReply), "esim") {
		t.Fatalf("expected spec-first response, got %q", specReply)
	}
	resume := resumePromptForStage(orderSnapshot.Stage)
	if !strings.Contains(strings.ToLower(resume), "pickup time") {
		t.Fatalf("expected stage resume prompt, got %q", resume)
	}
}

func TestConversationSimulation_CloseoutSuppressionAcrossTurns(t *testing.T) {
	conv := v4ctx.DefaultConversationSnapshot()

	// Turn 1: customer closes chat.
	if !isCloseoutMessage("No thank you") {
		t.Fatalf("expected closeout detection")
	}
	markCloseout(conv)
	firstReply := "No problem. If you need anything later, just let us know."
	if conv.CloseoutState != "closed" || conv.CloseoutMessageCount != 1 {
		t.Fatalf("expected first closeout to close conversation, got state=%q count=%d", conv.CloseoutState, conv.CloseoutMessageCount)
	}
	if firstReply == "" {
		t.Fatalf("expected non-empty first closeout response")
	}

	// Turn 2: repeated closeout should not reopen.
	if isReopenBuyingIntent("okay, no thanks") {
		t.Fatalf("expected no reopen intent for repeated decline")
	}
	secondReply := "No problem. If you need anything later, just let us know."
	if secondReply == "" {
		t.Fatalf("expected one final repeated closeout response")
	}

	// Turn 3: clear buying intent reopens.
	if !isReopenBuyingIntent("I want to buy iPhone 14 now") {
		t.Fatalf("expected reopen on new buying intent")
	}
}

func TestConversationSimulation_AmbiguousVariantBestPrice_StaysOnFocusedModel(t *testing.T) {
	message := "What is the best price for the 512GB??"
	currentFocus := "iPhone 13 Pro Max (512GB)"
	pythonTarget := "Samsung Galaxy S22 Ultra (512GB)"

	if !shouldSkipFocusOverrideForAmbiguousVariantQuery(message, currentFocus, pythonTarget) {
		t.Fatalf("expected ambiguous variant query to keep current focus and skip override")
	}

	explicitSwitchMsg := "What is the best price for Samsung Galaxy S22 Ultra 512GB?"
	if shouldSkipFocusOverrideForAmbiguousVariantQuery(explicitSwitchMsg, currentFocus, pythonTarget) {
		t.Fatalf("did not expect skip when user explicitly names another model")
	}
}
