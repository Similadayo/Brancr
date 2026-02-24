package catalog

import (
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

func TestResolveLongestMatch_TypoTolerance(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN"},
			{Name: "iPhone 14 (256GB)", Price: 1500000, Currency: "NGN"},
		},
	}

	got := ResolveLongestMatch("do you have iphon 13 pro max 256gb", knowledge)
	if got == nil {
		t.Fatalf("expected a match, got nil")
	}
	if got.Name != "iPhone 13 Pro Max (256GB)" {
		t.Fatalf("expected iPhone 13 Pro Max (256GB), got %q", got.Name)
	}
}

func TestResolveLongestMatch_ExplicitNumericMismatchReturnsNil(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN"},
		},
	}

	got := ResolveLongestMatch("i need iphone 14", knowledge)
	if got != nil {
		t.Fatalf("expected nil match for explicit 14 mismatch, got %q", got.Name)
	}
}

func TestResolveFocusedItem_TypoTolerance(t *testing.T) {
	offerings := []types.Offering{
		{Name: "iPhone 13 Pro Max (256GB)", Type: "product"},
		{Name: "iPhone 14 (256GB)", Type: "product"},
	}

	got := ResolveFocusedItem("how much is iphon 13 pro max", offerings)
	if got != "iPhone 13 Pro Max (256GB)" {
		t.Fatalf("expected focused item to be iPhone 13 Pro Max (256GB), got %q", got)
	}
}

func TestDetectCatalogQueryMode_DetailWithTypoedItem(t *testing.T) {
	offerings := []types.Offering{
		{Name: "iPhone 13 Pro Max (256GB)", Type: "product"},
	}

	mode := DetectCatalogQueryMode("can you explain iphon 13 pro max", offerings)
	if mode != types.CatalogDetail {
		t.Fatalf("expected detail mode, got %q", mode)
	}
}
