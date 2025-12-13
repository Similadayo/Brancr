package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func AdminAppEventsSSEHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if ctx == nil || ctx.DB == nil {
			http.Error(w, "admin not configured", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		writeEvent := func(event string, v any) error {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		_ = writeEvent("stats", AdminAppStatsResponse{
			Timestamp:   time.Now().UTC(),
			WindowHours: 24,
			Stats:       GetAnalyticsStats(ctx),
		})

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_ = writeEvent("stats", AdminAppStatsResponse{
					Timestamp:   time.Now().UTC(),
					WindowHours: 24,
					Stats:       GetAnalyticsStats(ctx),
				})
			}
		}
	}
}
