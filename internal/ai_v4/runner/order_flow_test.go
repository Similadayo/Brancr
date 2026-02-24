package runner

import (
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
)

func TestPaymentCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		methods      []string
		wantPOD      bool
		wantTransfer bool
	}{
		{name: "defaults_to_transfer_when_empty", methods: nil, wantPOD: false, wantTransfer: true},
		{name: "supports_both", methods: []string{"pod", "bank_transfer"}, wantPOD: true, wantTransfer: true},
		{name: "normalizes_aliases", methods: []string{"cash on delivery", "pay_now"}, wantPOD: true, wantTransfer: true},
		{name: "pod_only", methods: []string{"cod"}, wantPOD: true, wantTransfer: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPOD, gotTransfer := paymentCapabilities(tt.methods)
			if gotPOD != tt.wantPOD || gotTransfer != tt.wantTransfer {
				t.Fatalf("paymentCapabilities(%v) = (%v,%v), want (%v,%v)", tt.methods, gotPOD, gotTransfer, tt.wantPOD, tt.wantTransfer)
			}
		})
	}
}

func TestDetectPaymentMethodChoice(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		supportsPOD  bool
		supportsBank bool
		want         string
	}{
		{name: "pod_selected", message: "I will pay on delivery", supportsPOD: true, supportsBank: true, want: "pod"},
		{name: "bank_selected", message: "send account details, I want pay now", supportsPOD: true, supportsBank: true, want: "bank_transfer"},
		{name: "ambiguous_when_both_in_message", message: "pay on delivery or bank transfer", supportsPOD: true, supportsBank: true, want: "ambiguous"},
		{name: "no_match", message: "okay", supportsPOD: true, supportsBank: true, want: ""},
		{name: "single_mode_pod", message: "pod", supportsPOD: true, supportsBank: false, want: "pod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPaymentMethodChoice(tt.message, tt.supportsPOD, tt.supportsBank)
			if got != tt.want {
				t.Fatalf("detectPaymentMethodChoice(%q, %v, %v) = %q, want %q", tt.message, tt.supportsPOD, tt.supportsBank, got, tt.want)
			}
		})
	}
}

func TestDetectDeliveryChoiceFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "delivery", message: "Please do delivery", want: "delivery"},
		{name: "pickup", message: "I will pick up myself", want: "pickup"},
		{name: "unknown", message: "okay then", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectDeliveryChoiceFromMessage(tt.message)
			if got != tt.want {
				t.Fatalf("detectDeliveryChoiceFromMessage(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestAdvanceStageFromSnapshotFacts(t *testing.T) {
	t.Run("promotes_to_payment_ready_when_delivery_already_captured", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage:           "awaiting_delivery_choice",
			DeliveryMethod:  "delivery",
			DeliveryAddress: "10 Adeola Odeku, VI",
			RecipientName:   "John Doe",
			RecipientPhone:  "08012345678",
			DeliveryNotes:   "none",
		}

		advanceStageFromSnapshotFacts(snap, storage.DeliveryConfig{DeliveryEnabled: true, PickupEnabled: true})
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected stage payment_ready, got %q", snap.Stage)
		}
	})

	t.Run("promotes_to_payment_ready_when_pickup_already_captured", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage:          "awaiting_pickup_time",
			DeliveryMethod: "pickup",
			PickupTime:     "tomorrow 3pm",
			RecipientName:  "Jane Doe",
			RecipientPhone: "08012345678",
		}

		advanceStageFromSnapshotFacts(snap, storage.DeliveryConfig{DeliveryEnabled: true, PickupEnabled: true})
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected stage payment_ready, got %q", snap.Stage)
		}
	})

	t.Run("auto_selects_delivery_when_only_delivery_enabled", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage: "awaiting_delivery_choice",
		}

		advanceStageFromSnapshotFacts(snap, storage.DeliveryConfig{DeliveryEnabled: true, PickupEnabled: false})
		if snap.Stage != "awaiting_delivery_address" {
			t.Fatalf("expected stage awaiting_delivery_address, got %q", snap.Stage)
		}
		if snap.DeliveryMethod != "delivery" {
			t.Fatalf("expected delivery method delivery, got %q", snap.DeliveryMethod)
		}
	})
}

func TestReplay_NoDeliveryChoiceLoopAfterDetailsCaptured(t *testing.T) {
	config := storage.DeliveryConfig{DeliveryEnabled: true, PickupEnabled: true}

	t.Run("delivery_path_regression_is_auto_corrected_to_payment_ready", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage:           "awaiting_delivery_choice",
			DeliveryMethod:  "delivery",
			DeliveryAddress: "10 Adeola Odeku, Victoria Island, Lagos",
			RecipientName:   "John Doe",
			RecipientPhone:  "08012345678",
			DeliveryNotes:   "none",
		}

		advanceStageFromSnapshotFacts(snap, config)
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected payment_ready after first advance, got %q", snap.Stage)
		}

		// Simulate accidental regression persisted by another component.
		snap.Stage = "awaiting_delivery_choice"
		advanceStageFromSnapshotFacts(snap, config)
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected payment_ready after regression correction, got %q", snap.Stage)
		}
	})

	t.Run("area_only_address_does_not_promote_to_payment_ready", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage:           "awaiting_delivery_choice",
			DeliveryMethod:  "delivery",
			DeliveryAddress: "Surulere, Lagos",
		}

		advanceStageFromSnapshotFacts(snap, config)
		if snap.Stage == "payment_ready" {
			t.Fatalf("expected not to promote to payment_ready for area-only address, got %q", snap.Stage)
		}
	})

	t.Run("pickup_path_regression_is_auto_corrected_to_payment_ready", func(t *testing.T) {
		snap := &v4ctx.OrderSnapshot{
			Stage:          "awaiting_delivery_choice",
			DeliveryMethod: "pickup",
			PickupTime:     "today 5pm",
			RecipientName:  "Jane Doe",
			RecipientPhone: "08012345678",
		}

		advanceStageFromSnapshotFacts(snap, config)
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected payment_ready after first advance, got %q", snap.Stage)
		}

		// Simulate accidental regression persisted by another component.
		snap.Stage = "awaiting_delivery_choice"
		advanceStageFromSnapshotFacts(snap, config)
		if snap.Stage != "payment_ready" {
			t.Fatalf("expected payment_ready after regression correction, got %q", snap.Stage)
		}
	})
}
