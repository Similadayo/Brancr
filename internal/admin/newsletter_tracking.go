package admin

import (
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// TrackNewsletterOpenHandler tracks newsletter opens via tracking pixel
func TrackNewsletterOpenHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			// Return 1x1 transparent pixel even if no token
			w.Header().Set("Content-Type", "image/gif")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x04, 0x01, 0x00, 0x3b})
			return
		}

		var delivery storage.NewsletterDelivery
		if err := ctx.DB.Where("open_tracking_token = ?", token).First(&delivery).Error; err == nil {
			// Update delivery record
			if delivery.OpenedAt == nil {
				delivery.OpenedAt = timePtr(time.Now())
				delivery.Status = "opened"
				ctx.DB.Save(&delivery)

				// Update newsletter stats
				var newsletter storage.Newsletter
				if ctx.DB.First(&newsletter, delivery.NewsletterID).Error == nil {
					newsletter.OpenedCount++
					ctx.DB.Save(&newsletter)
				}
			}
		}

		// Return 1x1 transparent pixel
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x04, 0x01, 0x00, 0x3b})
	}
}

// TrackNewsletterClickHandler tracks newsletter link clicks
func TrackNewsletterClickHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		url := r.URL.Query().Get("url")
		if token == "" || url == "" {
			http.Error(w, "token and url required", http.StatusBadRequest)
			return
		}

		var delivery storage.NewsletterDelivery
		if err := ctx.DB.Where("click_tracking_token = ?", token).First(&delivery).Error; err == nil {
			// Update delivery record
			if delivery.ClickedAt == nil {
				delivery.ClickedAt = timePtr(time.Now())
				if delivery.Status != "clicked" {
					delivery.Status = "clicked"
				}
				ctx.DB.Save(&delivery)

				// Update newsletter stats
				var newsletter storage.Newsletter
				if ctx.DB.First(&newsletter, delivery.NewsletterID).Error == nil {
					newsletter.ClickedCount++
					ctx.DB.Save(&newsletter)
				}
			}
		}

		// Redirect to actual URL
		http.Redirect(w, r, url, http.StatusFound)
	}
}

// UnsubscribeNewsletterHandler handles newsletter unsubscribe requests
func UnsubscribeNewsletterHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token required", http.StatusBadRequest)
			return
		}

		if ctx.NewsletterService == nil {
			http.Error(w, "newsletter service not available", http.StatusServiceUnavailable)
			return
		}

		if err := ctx.NewsletterService.UnsubscribeTenant(r.Context(), token); err != nil {
			ctx.Logger.Error("failed to unsubscribe", zap.Error(err))
			http.Error(w, "failed to unsubscribe", http.StatusInternalServerError)
			return
		}

		// Return success page
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<title>Unsubscribed - Brancr</title>
	<style>
		body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
		.success { color: #10b981; font-size: 24px; margin-bottom: 20px; }
	</style>
</head>
<body>
	<div class="success">✓ Successfully Unsubscribed</div>
	<p>You have been unsubscribed from Brancr newsletters.</p>
	<p>You can resubscribe at any time from your tenant dashboard.</p>
</body>
</html>
`))
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

