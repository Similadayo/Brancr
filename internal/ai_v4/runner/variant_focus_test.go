package runner

import (
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

func TestResolveVariantFocusFromMessage_PicksVariantFromCapacityFollowUp(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)"},
			{Name: "iPhone 13 Pro Max (512GB)"},
		},
	}

	lastAIReply := "Yes, we have these iPhone 13 Pro Max options:\n" +
		"• iPhone 13 Pro Max (256GB) — NGN1,250,000\n" +
		"• iPhone 13 Pro Max (512GB) — NGN1,350,000"

	got := resolveVariantFocusFromMessage(
		"What is the best price for the 256GB?",
		"iPhone 13 Pro Max (512GB)",
		lastAIReply,
		knowledge,
	)
	if got != "iPhone 13 Pro Max (256GB)" {
		t.Fatalf("expected 256GB variant, got %q", got)
	}
}

func TestResolveVariantFocusFromMessage_UsesLastReplyContextWhenActiveEmpty(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)"},
			{Name: "iPhone 13 Pro Max (512GB)"},
		},
	}

	lastAIReply := "Available:\n- iPhone 13 Pro Max (256GB)\n- iPhone 13 Pro Max (512GB)"

	got := resolveVariantFocusFromMessage(
		"256GB please",
		"",
		lastAIReply,
		knowledge,
	)
	if got != "iPhone 13 Pro Max (256GB)" {
		t.Fatalf("expected 256GB variant, got %q", got)
	}
}

func TestResolveVariantFocusFromMessage_ReturnsEmptyWhenAmbiguousWithoutContext(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)"},
			{Name: "Samsung S23 (256GB)"},
		},
	}

	got := resolveVariantFocusFromMessage(
		"256GB price?",
		"",
		"",
		knowledge,
	)
	if got != "" {
		t.Fatalf("expected ambiguous variant focus to be empty, got %q", got)
	}
}

func TestResolveVariantFocusFromMessage_DoesNotCrossFamilyOnCapacityOnlyFollowUp(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 14 (256GB)"},
			{Name: "Infinix Hot 40 (128GB)"},
		},
	}

	got := resolveVariantFocusFromMessage(
		"How about the 128GB? How much is that one?",
		"iPhone 14 (256GB)",
		"iPhone 14 (256GB) is NGN 1,100,000.",
		knowledge,
	)
	if got != "" {
		t.Fatalf("expected no cross-family variant resolution, got %q", got)
	}
}
