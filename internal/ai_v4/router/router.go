package router

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/similadayo/brancr/internal/ai_v4/runner"
	"github.com/similadayo/brancr/internal/ai_v4/safety"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// AIRouter is the SINGLE entry point for AI V4.
// It enforces safety switches and delegates strictly to the FlowRunner.
type AIRouter struct {
	v4Runner *runner.FlowRunner
	logger   *zap.Logger
}

// NewAIRouter creates a new V4-only AI router.
func NewAIRouter(
	v4 *runner.FlowRunner,
	logger *zap.Logger,
) *AIRouter {
	return &AIRouter{
		v4Runner: v4,
		logger:   logger,
	}
}

// HandleMessage routes requests to the V4 FlowRunner.
// No fallback. No legacy paths.
func (r *AIRouter) HandleMessage(
	ctx context.Context,
	req *types.AIRequest,
) (*types.AIResponse, error) {

	// 1. SAFETY: MASTER KILL SWITCH
	ks := safety.LoadKillSwitch()
	if ks.DisableV4 {
		r.logger.Warn("v4_killswitch_active_rejecting_request")
		return nil, errors.New("AI temporarily unavailable (killswitch active)")
	}

	// 2. DELEGATE TO FLOW RUNNER (AUTHORITATIVE)
	return r.v4Runner.RunTurn(ctx, req)
}
