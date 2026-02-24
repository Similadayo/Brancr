package ai_v4

import (
	"context"

	v3 "github.com/similadayo/brancr/internal/ai/v3"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/decision"
	"github.com/similadayo/brancr/internal/ai_v4/emotion"
	"github.com/similadayo/brancr/internal/ai_v4/executor"

	// "github.com/similadayo/brancr/internal/ai_v4/intent" // REMOVED
	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/order"
	"github.com/similadayo/brancr/internal/ai_v4/payment"
	"github.com/similadayo/brancr/internal/ai_v4/python"
	"github.com/similadayo/brancr/internal/ai_v4/role"
	"github.com/similadayo/brancr/internal/ai_v4/runner"
	"github.com/similadayo/brancr/internal/ai_v4/semantic"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/social"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/fulfillment"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Re-export types for convenience
type (
	AIRequest         = types.AIRequest
	AIResponse        = types.AIResponse
	MemoryScope       = types.MemoryScope
	Intent            = types.Intent
	IntentResult      = types.IntentResult
	ConversationState = types.ConversationState
	ProposedAction    = types.ProposedAction
	ActionType        = types.ActionType
	EscalationRequest = types.EscalationRequest
)

// PipelineConfig holds dependencies for pipeline creation
type PipelineConfig struct {
	DB                 *gorm.DB
	Logger             *zap.Logger
	LLMClient          types.LLMClient
	V3Executor         v3.ActionExecutor
	AIServiceURL       string
	FulfillmentService *fulfillment.Service // NEW
}

// Pipeline is the main AI V4 orchestrator wrapper
type Pipeline struct {
	Runner *runner.FlowRunner
	logger *zap.Logger
}

// NewPipeline creates a new AI pipeline with all V4 components wired
func NewPipeline(cfg PipelineConfig) *Pipeline {
	logger := cfg.Logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	// 1. Core Flows
	memStore := memory.NewDBMemoryStore(cfg.DB)
	memFlow := memory.NewMemoryFlow(memStore, 20)

	ctxBuilder := v4ctx.NewBuilder(cfg.DB, logger)
	factExtractor := signals.NewFactExtractor(cfg.LLMClient)
	// intentEngine := intent.NewEngine(cfg.LLMClient, logger) // REMOVED
	emotionDetector := emotion.NewDetector(cfg.LLMClient)
	roleLoader := role.NewLoader(cfg.DB, logger)

	decisionEngine := decision.NewEngine(logger)

	semanticInterpreter := semantic.NewLLMInterpreter(cfg.LLMClient, logger)

	// 2. Execution Layer
	execAdapter := executor.NewExecutorAdapter(cfg.V3Executor, logger)

	orderFlow := order.NewOrderFlow(execAdapter, logger)
	paymentFlow := payment.NewPaymentFlow(execAdapter, logger)

	// 3. Social Layer
	socialRenderer := social.NewRenderer(cfg.LLMClient, logger)

	// 4. Python Brain
	pythonClient := python.NewClient(cfg.AIServiceURL)

	// 5. Assemble Runner
	deps := runner.RunnerDeps{
		Memory:  memFlow,
		Context: ctxBuilder,
		Facts:   factExtractor,
		// Intent:   intentEngine, // REMOVED
		Semantic:    semanticInterpreter,
		Emotion:     emotionDetector,
		Role:        roleLoader,
		Decision:    decisionEngine,
		Order:       orderFlow,
		Payment:     paymentFlow,
		Executor:    execAdapter,
		Social:      socialRenderer,
		Logger:      logger,
		DB:          cfg.DB,
		Python:      pythonClient,
		Fulfillment: cfg.FulfillmentService,
	}

	flowRunner := runner.NewFlowRunner(deps)

	return &Pipeline{
		Runner: flowRunner,
		logger: logger,
	}
}

// Process handles a customer message via the V4 FlowRunner
func (p *Pipeline) Process(ctx context.Context, req *types.AIRequest) (*types.AIResponse, error) {
	return p.Runner.RunTurn(ctx, req)
}
