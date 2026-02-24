package insights

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// InsightInput encapsulates the work unit for generating insights.
// We use a closure (Work) to avoid circular dependencies with the 'ai' package.
type InsightInput struct {
	// Work is the actual insight generation function to run
	Work func(ctx context.Context) error

	// OnFalback is called if Work fails or times out, to set "stale" status
	OnFallback func(ctx context.Context)
}

// GenerateCustomerInsightAsync runs the insight generation in a background goroutine
// with a strict 5-second timeout. It ensures the critical path is never blocked.
func GenerateCustomerInsightAsync(ctx context.Context, logger *zap.Logger, input InsightInput) {
	// Fire-and-forget goroutine
	go func() {
		// Create a detached context with a hard 5-second timeout
		// We use context.Background() as parent because the original request ctx might cancel quickly
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Execute the work
		if err := input.Work(bgCtx); err != nil {
			// Log warning with specific tags for observability
			if logger != nil {
				logger.Warn("customer_insight_generation_failed",
					zap.String("component", "ai.insights"),
					zap.Bool("degraded", true),
					zap.Error(err),
				)
			}

			// Trigger fallback (e.g., mark state as stale)
			if input.OnFallback != nil {
				input.OnFallback(context.Background()) // Use fresh context for fallback update
			}
		}
	}()
}
