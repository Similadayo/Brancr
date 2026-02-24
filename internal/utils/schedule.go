package utils

import (
	"strings"
	"time"

	"github.com/araddon/dateparse"
)

// ParseSchedule attempts to parse a natural-language datetime into time.Time.
// Returns parsed time (in provided loc) or error.
func ParseSchedule(input string, loc *time.Location) (time.Time, error) {
	// Quick normalization
	s := strings.TrimSpace(input)
	// dateparse will try multiple formats
	t, err := dateparse.ParseLocal(s)
	if err == nil {
		// If we want to normalize to provided loc:
		return t.In(loc), nil
	}
	// Fallback: try to append current year if no year present
	// (optional heuristic)
	return time.Time{}, err
}

// NormalizePlatforms validates platform names and returns canonical list
func NormalizePlatforms(raw string) []string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch p {
		case "instagram", "ig", "fb", "facebook", "tiktok":
			if p == "ig" {
				p = "instagram"
			}
			if p == "fb" {
				p = "facebook"
			}
			if p == "tiktok" {
				p = "tiktok"
			}
			out = append(out, p)
		}
	}
	return out
}
