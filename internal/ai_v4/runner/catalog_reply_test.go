package runner

import (
	"strings"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

func TestBuildDeterministicCatalogReply_SeriesQuery(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 (128GB)", Price: 1000000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 (256GB)", Price: 1500000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildDeterministicCatalogReply("show me 13 series", "", knowledge)
	if reply == "" {
		t.Fatalf("expected reply, got empty")
	}
	if !strings.Contains(strings.ToLower(reply), "13") {
		t.Fatalf("expected series mention in reply, got %q", reply)
	}
	if !strings.Contains(reply, "iPhone 13") {
		t.Fatalf("expected iphone 13 entries in reply, got %q", reply)
	}
}

func TestIsWeakStallReply(t *testing.T) {
	if !isWeakStallReply("I'm working on your request. Please bear with me for a moment!") {
		t.Fatalf("expected stall reply to be detected")
	}
	if isWeakStallReply("Here are matching options we have in stock.") {
		t.Fatalf("expected non-stall reply to pass")
	}
}

func TestShouldForceCatalogRouting(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 (128GB)", Price: 1000000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	if !shouldForceCatalogRouting("do you have iphon 13 pro max?", "", knowledge) {
		t.Fatalf("expected catalog routing for typo-tolerant product enquiry")
	}

	if !shouldForceCatalogRouting("is this the only variant available? no 512gb?", "iPhone 13 Pro Max (256GB)", knowledge) {
		t.Fatalf("expected catalog routing for follow-up variant enquiry with focus item")
	}

	if shouldForceCatalogRouting("thanks, that's helpful", "", knowledge) {
		t.Fatalf("did not expect catalog routing for non-catalog message")
	}
}

func TestBuildDeterministicCatalogReply_ModelLevelQueryReturnsFamilyVariants(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 Pro Max (256GB)", Price: 1500000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildDeterministicCatalogReply(
		"Hello, do you have iPhone 13 pro max?",
		"iPhone 13 Pro Max (256GB)",
		knowledge,
	)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "iphone 13 pro max (256gb)") {
		t.Fatalf("expected 256gb variant in model-level reply, got %q", reply)
	}
	if !strings.Contains(lower, "iphone 13 pro max (512gb)") {
		t.Fatalf("expected 512gb variant in model-level reply, got %q", reply)
	}
}

func TestBuildDeterministicCatalogReply_ModelLevelQueryWithoutFocusStillReturnsFamilyVariants(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 Pro Max (256GB)", Price: 1500000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildDeterministicCatalogReply(
		"Hello, do you have iPhone 13 pro max?",
		"",
		knowledge,
	)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "iphone 13 pro max (256gb)") {
		t.Fatalf("expected 256gb variant in model-level reply, got %q", reply)
	}
	if !strings.Contains(lower, "iphone 13 pro max (512gb)") {
		t.Fatalf("expected 512gb variant in model-level reply, got %q", reply)
	}
}

func TestBuildDeterministicCatalogReply_FullCatalogRequest(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildDeterministicCatalogReply("Can I see your full catalog?", "", knowledge)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "full catalog") {
		t.Fatalf("expected full catalog header, got %q", reply)
	}
	if !strings.Contains(lower, "iphone 13 pro max") {
		t.Fatalf("expected catalog items in reply, got %q", reply)
	}
}

func TestBuildDeterministicCatalogReply_DoesNotLeakStaleFocusItem(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "Infinix Hot 40 (128GB)", Price: 210000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 13 Pro Max (512GB)", Price: 1350000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildDeterministicCatalogReply(
		"Hello, do you have iPhone 13 pro max?",
		"Infinix Hot 40 (128GB)",
		knowledge,
	)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "iphone 13 pro max") {
		t.Fatalf("expected iPhone reply despite stale focusItem, got %q", reply)
	}
	if strings.Contains(lower, "infinix hot 40") {
		t.Fatalf("did not expect stale focusItem to leak into reply, got %q", reply)
	}
}

func TestBuildDeterministicCatalogReply_PlansRequestReturnsAllServices(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "agency",
		HasServices: true,
		Services: []v4ctx.ServiceSummary{
			{Name: "AI Social Media Management (Starter)", Description: "Starter plan", Price: 120000},
			{Name: "AI Social Media Management – Growth", Description: "Growth plan", Price: 250000},
		},
	}

	reply := buildDeterministicCatalogReply("I want to see your plans", "", knowledge)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "full catalog") {
		t.Fatalf("expected full catalog header, got %q", reply)
	}
	if !strings.Contains(lower, "starter") {
		t.Fatalf("expected starter service in reply, got %q", reply)
	}
	if !strings.Contains(lower, "growth") {
		t.Fatalf("expected growth service in reply, got %q", reply)
	}
}

func TestBuildDeterministicCatalogReply_ServiceCatalogDoesNotIncludeMenuItems(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "agency",
		HasServices: true,
		Services: []v4ctx.ServiceSummary{
			{Name: "AI Social Media Management (Starter)", Description: "Starter plan", Price: 120000},
			{Name: "AI Social Media Management – Growth", Description: "Growth plan", Price: 250000},
		},
		MenuItems: []v4ctx.MenuItemSummary{
			{Name: "Chocolate (Cake Slice)", Price: 3800, Currency: "NGN", Availability: "in_stock"},
		},
	}

	reply := buildDeterministicCatalogReply("I want to see your plans", "", knowledge)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "cake") || strings.Contains(lower, "chocolate") {
		t.Fatalf("did not expect menu items in service catalog, got %q", reply)
	}
	if !strings.Contains(lower, "starter") || !strings.Contains(lower, "growth") {
		t.Fatalf("expected services in reply, got %q", reply)
	}
}

func TestBuildRecommendationReply_BudgetFriendly(t *testing.T) {
	candidates := []availabilityCandidate{
		{name: "Phone A", price: 90000, currency: "NGN", availability: "in_stock", stockCount: -1, kind: "product"},
		{name: "Phone B", price: 150000, currency: "NGN", availability: "in_stock", stockCount: -1, kind: "product"},
		{name: "Phone C", price: 110000, currency: "NGN", availability: "in_stock", stockCount: -1, kind: "product"},
	}

	reply := buildRecommendationReply("I want something cheap, any recommendations?", candidates)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "budget-friendly") {
		t.Fatalf("expected budget-friendly intro, got %q", reply)
	}
	if !strings.Contains(lower, "phone a") || !strings.Contains(lower, "phone c") {
		t.Fatalf("expected cheapest items in reply, got %q", reply)
	}
	if strings.Contains(reply, "<model>") {
		t.Fatalf("expected no <model> placeholder leakage, got %q", reply)
	}
}

func TestDetectUnavailableModelRequest_IgnoresGenericPerformancePhrase(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	requested, alternatives, hit := detectUnavailableModelRequest("Can you recommend another iPhone that can handle heavy task?", knowledge)
	if hit {
		t.Fatalf("expected no unavailable guard hit for generic recommendation phrase, got requested=%q alternatives=%d", requested, len(alternatives))
	}
}

func TestDetectUnavailableModelRequest_HitsExplicitUnknownModel(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	requested, alternatives, hit := detectUnavailableModelRequest("I want iPhone 17 Pro Max 256GB", knowledge)
	if !hit {
		t.Fatalf("expected unavailable guard hit for explicit unknown model")
	}
	if !strings.Contains(strings.ToLower(requested), "iphone 17") {
		t.Fatalf("expected extracted requested model to include iPhone 17, got %q", requested)
	}
	if len(alternatives) == 0 {
		t.Fatalf("expected alternatives for explicit unknown model")
	}
}

func TestBuildStockFollowUpReply_UnknownModelUsesDeterministicRestockLanguage(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}
	conv := &v4ctx.ConversationSnapshot{
		ActiveItem:  "iPhone 17 Pro Max",
		LastAIReply: "iPhone 17 Pro Max is not in our catalog right now.",
	}

	reply := buildStockFollowUpReply("So you don't know when it would be available?", "", conv, knowledge)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "don't have a restock date") {
		t.Fatalf("expected deterministic restock-date response, got %q", reply)
	}
	if strings.Contains(lower, "recently") {
		t.Fatalf("expected no vague 'recently' wording, got %q", reply)
	}
}

func TestBuildStockFollowUpReply_OutOfStockKnownModelNoFakeDate(t *testing.T) {
	knowledge := &v4ctx.TenantKnowledgeSnapshot{
		Industry:    "electronics",
		HasProducts: true,
		Products: []v4ctx.ProductSummary{
			{Name: "iPhone 14 (128GB)", Price: 1050000, Currency: "NGN", Availability: "out_of_stock", StockCount: 0},
			{Name: "iPhone 13 Pro Max (256GB)", Price: 1250000, Currency: "NGN", Availability: "in_stock", StockCount: -1},
		},
	}

	reply := buildStockFollowUpReply("When did it go out of stock?", "iPhone 14 (128GB)", &v4ctx.ConversationSnapshot{}, knowledge)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "don't have the exact date") {
		t.Fatalf("expected deterministic no-fake-date response, got %q", reply)
	}
	if strings.Contains(lower, "marked unavailable recently") {
		t.Fatalf("expected no vague fallback wording, got %q", reply)
	}
}
