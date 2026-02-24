package ai

import (
	"regexp"
	"strings"
)

// CustomerOutputSafetyGate sanitizes customer-facing replies to remove internal language
// and enforce human phrasing. This is a deterministic post-processor that runs after LLM generation
// and before sending messages to customers via WhatsApp/Instagram/Telegram/etc.
type CustomerOutputSafetyGate struct{}

// NewCustomerOutputSafetyGate creates a new customer output safety gate
func NewCustomerOutputSafetyGate() *CustomerOutputSafetyGate {
	return &CustomerOutputSafetyGate{}
}

// SanitizeCustomerReply removes or rewrites internal phrases that leak into customer messages.
// This ensures customers never see internal system language like "Summary:", "Manager approval", etc.
func (g *CustomerOutputSafetyGate) SanitizeCustomerReply(text string) string {
	if text == "" {
		return text
	}

	cleaned := text

	// Forbidden patterns that must be completely removed or rewritten
	// Pattern: regex -> replacement (empty string = remove, text = rewrite)
	replacements := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		// "Summary:" - completely remove this prefix
		{regexp.MustCompile(`(?i)\s*Summary\s*:\s*`), ""},
		{regexp.MustCompile(`(?i)^Summary\s*:\s*`), ""},

		// "Manager approval" / "approval" in negotiation context
		{regexp.MustCompile(`(?i)manager\s+approval`), "confirmation"},
		{regexp.MustCompile(`(?i)request\s+manager\s+approval`), "confirm something"},
		{regexp.MustCompile(`(?i)get\s+manager\s+approval`), "confirm something"},
		{regexp.MustCompile(`(?i)approval\s+from\s+manager`), "confirmation"},

		// "Check with the team" - rewrite to more natural phrasing
		{regexp.MustCompile(`(?i)check\s+with\s+the\s+team`), "confirm something"},
		{regexp.MustCompile(`(?i)I'll\s+check\s+with\s+the\s+team`), "I'll confirm and get back to you"},
		{regexp.MustCompile(`(?i)let\s+me\s+check\s+with\s+the\s+team`), "Let me confirm and get back to you"},

		// "Escalation" - remove this word entirely
		{regexp.MustCompile(`(?i)\bescalation\b`), ""},
		{regexp.MustCompile(`(?i)\bescalate\b`), "hand this over"},
		{regexp.MustCompile(`(?i)\bescalated\b`), "handed over"},

		// System/policy/internal language - remove entirely
		{regexp.MustCompile(`(?i)\bsystem\b`), ""},
		{regexp.MustCompile(`(?i)\bpolicy\b`), ""},
		{regexp.MustCompile(`(?i)\binternal\b`), ""},
		{regexp.MustCompile(`(?i)\brange\b.*?price`), "price"},
		{regexp.MustCompile(`(?i)price\s+range`), "price"},

		// "Request" in formal contexts - rewrite
		{regexp.MustCompile(`(?i)request\s+(manager|approval|team)`), "confirm something"},
		{regexp.MustCompile(`(?i)request\s+to\s+(manager|approval)`), "confirm something"},
	}

	// Apply all replacements
	for _, r := range replacements {
		cleaned = r.pattern.ReplaceAllString(cleaned, r.replacement)
	}

	// Clean up multiple spaces (preserving newlines)
	// Only collapse horizontal whitespace (spaces, tabs)
	cleaned = regexp.MustCompile(`[ \t]+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)

	// Filter out empty or very short lines
	lines := strings.Split(cleaned, "\n")
	var validLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 { // Keep all non-empty lines (lists might have short items like "- Tea")
			validLines = append(validLines, trimmed)
		}
	}
	cleaned = strings.Join(validLines, "\n")

	// Final cleanup
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

// ValidateCustomerReply checks if a reply contains forbidden patterns and returns an error if found.
// This can be used as a lint check to fail messages that contain internal language.
func (g *CustomerOutputSafetyGate) ValidateCustomerReply(text string) error {
	forbiddenPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bSummary\s*:`),
		regexp.MustCompile(`(?i)\bmanager\s+approval\b`),
		regexp.MustCompile(`(?i)\bescalation\b`),
		regexp.MustCompile(`(?i)\bsystem\b`),
		regexp.MustCompile(`(?i)\bpolicy\b`),
		regexp.MustCompile(`(?i)\binternal\b`),
		regexp.MustCompile(`(?i)\bprice\s+range\b`),
	}

	textLower := strings.ToLower(text)
	for _, pattern := range forbiddenPatterns {
		if pattern.MatchString(textLower) {
			return &ForbiddenPatternError{
				Pattern: pattern.String(),
				Message: text,
			}
		}
	}

	return nil
}

// ForbiddenPatternError indicates a customer reply contains forbidden internal language
type ForbiddenPatternError struct {
	Pattern string
	Message string
}

func (e *ForbiddenPatternError) Error() string {
	return "customer reply contains forbidden pattern: " + e.Pattern
}
