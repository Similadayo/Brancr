package social

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// WebhookRetryWorker processes pending webhook retries
type WebhookRetryWorker struct {
	Processor    *WebhookProcessor
	Logger       *zap.Logger
	PollInterval time.Duration
}

// NewWebhookRetryWorker creates a new retry worker
func NewWebhookRetryWorker(processor *WebhookProcessor, logger *zap.Logger) *WebhookRetryWorker {
	return &WebhookRetryWorker{
		Processor:    processor,
		Logger:       logger,
		PollInterval: 30 * time.Second, // Poll every 30 seconds
	}
}

// Run starts the retry worker loop
func (w *WebhookRetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	w.Logger.Info("webhook retry worker started",
		zap.Duration("poll_interval", w.PollInterval))

	for {
		select {
		case <-ctx.Done():
			w.Logger.Info("webhook retry worker stopped")
			return
		case <-ticker.C:
			w.processPendingRetries(ctx)
		}
	}
}

// processPendingRetries processes events ready for retry
func (w *WebhookRetryWorker) processPendingRetries(ctx context.Context) {
	events, err := w.Processor.RetryManager.GetPendingRetries(ctx, 10) // Process up to 10 at a time
	if err != nil {
		w.Logger.Error("failed to get pending retries", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return // No pending retries
	}

	w.Logger.Info("processing pending webhook retries",
		zap.Int("count", len(events)))

	for _, event := range events {
		// Route to appropriate handler based on platform

		switch event.Platform {
		case "instagram", "facebook", "whatsapp":
			// Meta webhooks - would need access to MetaWebhookHandler
			// For now, log and skip (can be enhanced later)
			w.Logger.Warn("Meta webhook retry not yet implemented",
				zap.String("platform", event.Platform),
				zap.String("event_id", event.EventID))
			continue
		case "gupshup":
			// Gupshup webhooks - would need access to GupshupWebhookHandler
			w.Logger.Warn("Gupshup webhook retry not yet implemented",
				zap.String("event_id", event.EventID))
			continue
		case "respondio":
			// Respond.io webhooks - would need access to RespondIOWebhookHandler
			w.Logger.Warn("Respond.io webhook retry not yet implemented",
				zap.String("event_id", event.EventID))
			continue
		case "tiktok":
			// TikTok webhooks - would need access to TikTokWebhookHandler
			w.Logger.Warn("TikTok webhook retry not yet implemented",
				zap.String("event_id", event.EventID))
			continue
		default:
			w.Logger.Warn("unknown platform for retry",
				zap.String("platform", event.Platform),
				zap.String("event_id", event.EventID))
			continue
		}
		// NOTE: All switch cases currently use continue as handlers aren't implemented yet.
		// When implementing, assign to handler and remove the continue statements.
	}
}
