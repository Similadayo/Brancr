package admin

import (
	"net/http"
	"time"
)

type AdminAppStatsResponse struct {
	Timestamp time.Time     `json:"timestamp"`
	WindowHours int         `json:"window_hours"`
	Stats     AnalyticsStats `json:"stats"`
}

func AdminAppStatsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx == nil || ctx.DB == nil {
			http.Error(w, "admin not configured", http.StatusServiceUnavailable)
			return
		}

		respondJSON(w, AdminAppStatsResponse{
			Timestamp: time.Now().UTC(),
			WindowHours: 24,
			Stats:     GetAnalyticsStats(ctx),
		})
	}
}
