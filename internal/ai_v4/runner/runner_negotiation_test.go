package runner

import (
	"strings"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

func TestBuildEmptyReplyFallback(t *testing.T) {
	tests := []struct {
		name             string
		intent           string
		isCatalog        bool
		focusItem        string
		activeItem       string
		messageText      string
		expectedContains string
	}{
		{
			name:             "negotiation_uses_focus_item",
			intent:           "negotiation_offer",
			isCatalog:        true,
			focusItem:        "iPhone 14",
			activeItem:       "",
			messageText:      "my budget is 850k",
			expectedContains: "iPhone 14",
		},
		{
			name:             "negotiation_uses_active_item_when_focus_missing",
			intent:           "negotiation_offer",
			isCatalog:        true,
			focusItem:        "",
			activeItem:       "Samsung S23",
			messageText:      "my budget is 700k",
			expectedContains: "Samsung S23",
		},
		{
			name:             "negotiation_without_item_asks_for_specific_item",
			intent:           "negotiation_offer",
			isCatalog:        true,
			focusItem:        "",
			activeItem:       "",
			messageText:      "budget is 500k",
			expectedContains: "Which specific item",
		},
		{
			name:             "catalog_non_negotiation_uses_menu_prompt",
			intent:           "price_request",
			isCatalog:        true,
			focusItem:        "",
			activeItem:       "",
			messageText:      "price?",
			expectedContains: "prices/menu",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildEmptyReplyFallback(tc.intent, tc.isCatalog, tc.focusItem, tc.activeItem, tc.messageText)
			if !strings.Contains(got, tc.expectedContains) {
				t.Fatalf("expected fallback to contain %q, got %q", tc.expectedContains, got)
			}
		})
	}
}

func TestIsGenericFocusLabel(t *testing.T) {
	tests := []struct {
		focus string
		want  bool
	}{
		{focus: "iPhones", want: true},
		{focus: "phone", want: true},
		{focus: "iPhone 14 256GB", want: false},
		{focus: "Samsung S23", want: false},
	}

	for _, tc := range tests {
		got := isGenericFocusLabel(tc.focus)
		if got != tc.want {
			t.Fatalf("isGenericFocusLabel(%q) = %v, want %v", tc.focus, got, tc.want)
		}
	}
}

func TestMessageMentionsFocus(t *testing.T) {
	if !messageMentionsFocus("my budget for iphone is 850k", "iPhone") {
		t.Fatalf("expected focus mention to be detected")
	}
	if messageMentionsFocus("my budget is 850k", "iPhones") {
		t.Fatalf("expected no focus mention for generic budget-only message")
	}
}

func TestChoosePersistedActiveItem(t *testing.T) {
	tests := []struct {
		name   string
		prev   string
		curr   string
		target string
		want   string
	}{
		{
			name:   "prefer_variant_current_over_generic_target",
			prev:   "iPhone 13 Pro Max (512GB)",
			curr:   "iPhone 13 Pro Max (256GB)",
			target: "iPhone 13 Pro Max",
			want:   "iPhone 13 Pro Max (256GB)",
		},
		{
			name:   "keep_previous_specific_over_current_generic",
			prev:   "iPhone 14 (256GB)",
			curr:   "iPhones",
			target: "",
			want:   "iPhone 14 (256GB)",
		},
		{
			name:   "prefer_specific_target",
			prev:   "iPhones",
			curr:   "iPhones",
			target: "iPhone 14 (256GB)",
			want:   "iPhone 14 (256GB)",
		},
		{
			name:   "prefer_specific_current_when_no_target",
			prev:   "iPhones",
			curr:   "Samsung S23",
			target: "",
			want:   "Samsung S23",
		},
		{
			name:   "fallback_to_current_when_all_generic",
			prev:   "phones",
			curr:   "iPhones",
			target: "",
			want:   "iPhones",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := choosePersistedActiveItem(tc.prev, tc.curr, tc.target)
			if got != tc.want {
				t.Fatalf("choosePersistedActiveItem(%q, %q, %q) = %q, want %q", tc.prev, tc.curr, tc.target, got, tc.want)
			}
		})
	}
}

func TestComputeOrderTotalFromSnapshotItems(t *testing.T) {
	items := []struct {
		name      string
		quantity  int
		unitPrice int64
	}{
		{name: "iPhone 14", quantity: 1, unitPrice: 1090000},
		{name: "Case", quantity: 2, unitPrice: 10000},
	}

	var orderItems []v4ctx.OrderItem
	for _, it := range items {
		orderItems = append(orderItems, v4ctx.OrderItem{
			Name:      it.name,
			Quantity:  it.quantity,
			UnitPrice: it.unitPrice,
		})
	}

	got := computeOrderTotalFromSnapshotItems(orderItems)
	if got != 1110000 {
		t.Fatalf("expected total 1110000, got %d", got)
	}
}
