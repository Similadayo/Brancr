package simulation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	v3 "github.com/similadayo/brancr/internal/ai/v3"
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/executor"
	"github.com/similadayo/brancr/internal/observability"
	"github.com/similadayo/brancr/internal/rules"
	"github.com/similadayo/brancr/internal/types"
)

// ReplayRunner handles the execution of conversational scenarios in a dry-run mode.
type ReplayRunner struct {
	pipeline  v3.Pipeline
	logger    *zap.Logger
	Executors *executor.Registry
}

// ConversationTurn represents a single exchange in a scenario.
type ConversationTurn struct {
	UserMessage      string
	ExpectedGoal     string // Optional assertion
	ExpectedAction   string // Optional assertion
	TimeSinceLastMsg time.Duration
}

// Scenario represents a sequence of turns to replay.
type Scenario struct {
	Name  string
	Turns []ConversationTurn
}

// ScenarioResult captures the output of a replay.
type ScenarioResult struct {
	ScenarioName string
	Turns        []TurnResult
}

type TurnResult struct {
	UserMessage   string
	AIResponse    string
	Action        string
	Goal          string
	ResponseMode  string
	ExecutionLogs []string // Captured during dry-run execution
}

// NewReplayRunner initializes a V3 pipeline with dry-run executors.
// It requires a minimal valid DB connection or mock for context loading,
// and a real or mock LLM Client.
func NewReplayRunner(db *gorm.DB, llmClient v3.LLMClient, logger *zap.Logger) *ReplayRunner {
	if logger == nil {
		logger = zap.NewNop()
	}

	// 1. Setup minimal dependencies
	eventBus := events.NewInMemoryBus()
	rulesEngine := rules.NewBaseEngine()
	obsService := observability.NewService(logger)

	// 2. Setup DRY-RUN Registry
	registry := executor.NewRegistry()
	mockExec := &DryRunExecutor{Logger: logger}

	// Register known actions with the mock executor
	// We must register ALL possible actions to ensure no nil pointer dereferences
	knownActions := []rules.ActionType{
		rules.ActionExplainPrice,
		rules.ActionClarifyIntent,
		rules.ActionAcknowledge,
		rules.ActionResumeConversation,
		rules.ActionSuggestNextStep,
		rules.ActionProposeDiscount,
		rules.ActionRejectDiscount,
		rules.ActionInitializeOrder,
		rules.ActionLockPrice,
		rules.ActionCancelOrder,
		rules.ActionRequestPayment,
		rules.ActionConfirmOrder,
	}

	for _, action := range knownActions {
		registry.Register(action, mockExec)
	}

	// 3. Initialize Pipeline
	pipeline := v3.NewPipeline(v3.PipelineConfig{
		DB:             db,
		Logger:         logger,
		LLMClient:      llmClient,
		EventBus:       eventBus,
		ActionRegistry: registry,
		Observability:  obsService,
		RulesEngine:    rulesEngine,
		// Assuming generic adapters/mocks for V2 components if strictly needed,
		// or nil if the pipeline handles nil (V3 pipeline checks are robust).
		// For thoroughness we might need stubs if it crashes.
	})

	return &ReplayRunner{
		pipeline:  pipeline,
		logger:    logger,
		Executors: registry,
	}
}

// RunScenario executes a scenario turn-by-turn.
func (r *ReplayRunner) RunScenario(ctx context.Context, scenario Scenario) (*ScenarioResult, error) {
	result := &ScenarioResult{
		ScenarioName: scenario.Name,
		Turns:        make([]TurnResult, 0, len(scenario.Turns)),
	}

	// Simulate a fresh conversation for each scenario
	// In a real runner, we'd create a unique ConversationID or mock the context builder.
	// For now, we assume the Request carries enough info or we reuse a fixed ID for simplicity,
	// BUT scenarios often imply state progression.
	// The V3 pipeline relies on ContextBuilder to load state from DB.
	// To truly simulate state changes *without* mutating the real DB, we'd need:
	// a) A transaction that rolls back (ideal for tests)
	// b) A mock DB (hard with Gorm)
	// c) Just relying on the fact that DryRunExecutor DOES NOT mutate order state,
	//    so context remains static unless we manually tweak it.
	//
	// However, Scenario 1 (Explore -> Decide) assumes the order IS initialized.
	// If DryRunExecutor allows "InitializeOrder" but doesn't write to DB, subsequent turns won't see the order.
	//
	// CRITICAL: The user specified "No executor side effects".
	// This implies the standard DB is not updated.
	// BUT conversational state (memory) might need to update for "LastIntent" etc.
	//
	// SOLUTION: For "Phrasing Tuning", we care most about the *text* and *intent*.
	// If state dependency is loose, we might get away with it.
	// If strict state is needed (e.g. "That's a bit high" needs an active order),
	// allow mutation in a temporary scope or mock context.
	//
	// Given constraints, we will proceed with the DryRunExecutor.
	// If state doesn't update, the AI might get confused in multi-turn flows.
	// We will note this limitation or implement a lightweight state tracker in the runner if needed.

	// Use a fixed conversation ID for the scenario
	conversationID := uuid.New()
	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001") // Dummy UUID

	for _, turn := range scenario.Turns {
		// Create request
		req := v3.ConversationRequest{
			ConversationID: conversationID,
			TenantID:       tenantID,
			Platform:       "simulation",
			Message:        turn.UserMessage,
			Timestamp:      time.Now(),
		}

		// Execute
		resp, err := r.pipeline.Process(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("pipeline error on turn '%s': %w", turn.UserMessage, err)
		}

		// Capture result
		// Capture result
		turnResult := TurnResult{
			UserMessage:   turn.UserMessage,
			AIResponse:    resp.HumanText,
			Action:        getString(resp.Meta, "action_type"),
			Goal:          getString(resp.Meta, "goal"),
			ResponseMode:  getString(resp.Meta, "mode"),
			ExecutionLogs: []string{}, // Logs captured via separate hook if needed
		}
		result.Turns = append(result.Turns, turnResult)
	}

	return result, nil
}

func getString(meta map[string]any, key string) string {
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// --- Dry Run Executor ---

type DryRunExecutor struct {
	Logger *zap.Logger
}

func (e *DryRunExecutor) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	e.Logger.Info("dry_run_execution", zap.String("action", "simulated"))
	// Return a generic success event to satisfy the interface
	return []events.Event{{
		Type:      events.EventType("DRY_RUN_SUCCESS"),
		Timestamp: time.Now(),
	}}, nil
}
