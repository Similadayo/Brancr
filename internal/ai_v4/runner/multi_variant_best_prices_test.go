package runner

import (
	"strings"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

func TestBuildMultiVariantBestPricesReply_ForBothVariants(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "available", StockCount: 3},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "available", StockCount: 2},
		},
	}

	lastAIReply := "Yes, we have these iphone 13 pro max options:\n" +
		"• iPhone 13 Pro Max (256GB) — NGN1,250,000\n" +
		"• iPhone 13 Pro Max (512GB) — NGN1,350,000"

	got := buildMultiVariantBestPricesReply(
		"What are the best prices for both?",
		"iPhone 13 Pro Max (512GB)",
		lastAIReply,
		knowledge,
	)

	if !strings.Contains(got, "iPhone 13 Pro Max (256GB)") || !strings.Contains(got, "iPhone 13 Pro Max (512GB)") {
		t.Fatalf("expected both variants in reply, got %q", got)
	}
	if !strings.Contains(got, "NGN1,250,000") || !strings.Contains(got, "NGN1,350,000") {
		t.Fatalf("expected both prices in reply, got %q", got)
	}
}

func TestBuildMultiVariantBestPricesReply_ExplicitCapacities(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "retail",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "available", StockCount: 1},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "available", StockCount: 1},
		},
	}

	got := buildMultiVariantBestPricesReply(
		"What are the best prices for the 256GB and the 512GB?",
		"iPhone 13 Pro Max (512GB)",
		"",
		knowledge,
	)

	if !strings.Contains(got, "iPhone 13 Pro Max (256GB)") || !strings.Contains(got, "iPhone 13 Pro Max (512GB)") {
		t.Fatalf("expected both variants in reply, got %q", got)
	}
}

