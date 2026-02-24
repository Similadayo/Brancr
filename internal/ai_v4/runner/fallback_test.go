package runner

import (
	"strings"
	"testing"
)

func TestBuildEmptyReplyFallback_CatalogAvoidsWaitingLanguage(t *testing.T) {
	reply := buildEmptyReplyFallback("menu_request", true, "", "", "what do you have?")
	lower := strings.ToLower(reply)

	if strings.Contains(lower, "please bear with me") || strings.Contains(lower, "one moment") || strings.Contains(lower, "working on") {
		t.Fatalf("catalog fallback must not use waiting language, got %q", reply)
	}
}

func TestBuildEmptyReplyFallback_NegotiationPromptsForItem(t *testing.T) {
	reply := buildEmptyReplyFallback("negotiation_offer", false, "", "", "last price 700k")
	if !strings.Contains(strings.ToLower(reply), "which specific item") {
		t.Fatalf("expected negotiation fallback to request target item, got %q", reply)
	}
}

