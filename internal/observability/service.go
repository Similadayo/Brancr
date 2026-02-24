package observability

import (
	"context"

	"go.uber.org/zap"
)

// Service aggregates all observability components
type Service struct {
	logger *zap.Logger
}

func NewService(logger *zap.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// LogAIDecision logs the decision context
func (s *Service) LogAIDecision(ctx context.Context, event AIDecisionEvent) {
	if s.logger == nil {
		return
	}
	s.logger.Info("ai_decision_made",
		zap.Any("tenant_id", event.TenantID),
		zap.String("intent", event.Intent),
		zap.Strings("rule_actions", event.RuleResult),
	)
}

// LogActionResult logs the execution result
func (s *Service) LogActionResult(ctx context.Context, event ActionResultEvent) {
	if s.logger == nil {
		return
	}
	s.logger.Info("action_executed",
		zap.Any("tenant_id", event.TenantID),
		zap.String("action", event.Action),
		zap.Bool("success", event.Success),
	)
}

// AIDecisionEvent structure for the log
type AIDecisionEvent struct {
	TenantID   interface{} // uuid.UUID
	Intent     string
	RuleResult []string // Allowed actions
}

// ActionResultEvent structure for the log
type ActionResultEvent struct {
	TenantID interface{} // uuid.UUID
	Action   string
	Success  bool
}
