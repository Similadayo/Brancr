package channel

import (
	"testing"

	"github.com/similadayo/brancr/internal/ai/response"
)

func TestRenderForHuman(t *testing.T) {
	// Test 1: Normal input
	input := response.AIResponse{
		HumanText: "Hello there!",
		Meta: map[string]any{
			"confidence": "high",
		},
	}
	if got := RenderForHuman(input); got != "Hello there!" {
		t.Errorf("RenderForHuman() = %q, want %q", got, "Hello there!")
	}

	// Test 2: JSON in HumanText should trigger safety checks downstream,
	// but RenderForHuman itself just extracts the string.
	// The assertion logic handles the panic test.
}

func TestAssertHumanSafe(t *testing.T) {
	// Safe cases
	safeTexts := []string{
		"Hello",
		"This is a {bracket} but not JSON",
		"Simple text",
	}
	for _, txt := range safeTexts {
		AssertHumanSafe(txt) // Should not panic
	}

	// Unsafe cases
	unsafeTexts := []string{
		`{"key": "value"}`,
		`["a", "b"]`,
	}

	for _, txt := range unsafeTexts {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("AssertHumanSafe() did not panic for detected JSON: %s", txt)
			}
		}()
		AssertHumanSafe(txt)
	}
}
