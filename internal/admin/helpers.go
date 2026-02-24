package admin

import (
	"net/http"
	"time"
)

// parseCustomDates parses custom start and end dates from request query parameters
func parseCustomDates(r *http.Request) (*time.Time, *time.Time) {
	var customStart, customEnd *time.Time

	if startStr := r.URL.Query().Get("start_date"); startStr != "" {
		if s, err := time.Parse(time.RFC3339, startStr); err == nil {
			customStart = &s
		}
	}

	if endStr := r.URL.Query().Get("end_date"); endStr != "" {
		if e, err := time.Parse(time.RFC3339, endStr); err == nil {
			customEnd = &e
		}
	}

	return customStart, customEnd
}
