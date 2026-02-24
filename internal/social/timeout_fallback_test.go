package social

import (
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/ai"
)

func TestBuildTimeoutFallbackMessage_NoPassiveWaitingCopy(t *testing.T) {
	reply := buildTimeoutFallbackMessage("how much is iphone 13", &ai.DetectedIntent{Intent: "price"})
	lower := strings.ToLower(reply)

	if strings.Contains(lower, "bear with me") || strings.Contains(lower, "one moment") || strings.Contains(lower, "working on") {
		t.Fatalf("timeout fallback should be actionable, got %q", reply)
	}
}

func TestBuildTimeoutFallbackMessage_VariantPrompt(t *testing.T) {
	reply := buildTimeoutFallbackMessage("do you have 512gb variant?", &ai.DetectedIntent{Intent: "inquiry"})
	lower := strings.ToLower(reply)

	if !strings.Contains(lower, "exact variant") {
		t.Fatalf("expected variant-specific guidance, got %q", reply)
	}
}

