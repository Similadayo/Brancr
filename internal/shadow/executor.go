package shadow

import (
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
	"go.uber.org/zap"
)

type DryRunExecutor struct {
	Logger *zap.Logger
}

func (e *DryRunExecutor) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if e.Logger != nil {
		e.Logger.Info("shadow_dry_run_execution", zap.String("status", "skipped"))
	}
	// Return a generic success event to satisfy the interface, but do nothing
	return []events.Event{{
		Type:      events.EventType("SHADOW_EXECUTION_SKIPPED"),
		Timestamp: time.Now(),
	}}, nil
}
