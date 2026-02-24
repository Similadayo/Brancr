package v3

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/ai/decision"
	"github.com/similadayo/brancr/internal/ai/guard"
	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/ai_contract"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/executor"
	"github.com/similadayo/brancr/internal/fulfillment"
	intent_pkg "github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/observability"
	"github.com/similadayo/brancr/internal/persona"
	"github.com/similadayo/brancr/internal/rules"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/types"
)

// =============================================================================
// PIPELINE - V3 AI Conversation Orchestrator
// =============================================================================

// pipeline implements Pipeline interface
// PHASE-1: Refactored to use AI client boundary
// Pipeline is now a coordinator that delegates to ai_client
type pipeline struct {
	context   ContextBuilder
	intent    IntentExtractor // PHASE-1: Kept for backward compat, will be removed
	evaluator *StateEvaluator // THE THINK LAYER

	// PHASE-8: AI Client for Python service integration
	// Uses ai_contract.AIClient interface
	aiClient ai_contract.AIClient

	// NEW V3 ENGINES
	rulesEngine    rules.Engine
	actionRegistry *executor.Registry
	eventBus       events.EventBus
	observability  *observability.Service
	reasoner       *Reasoner

	// Legacy/Compat components
	resolver ActionResolver // Deprecated: kept for compile, unused in new flow
	executor ActionExecutor // Deprecated: kept for compile, unused in new flow

	composer       ResponseComposer
	grounder       ConversationGrounder
	fullGrounder   *FullConversationGrounder
	logger         *zap.Logger
	diag           *PipelineLogger
	v2StateManager V2StateManager

	// Shadow Mode Safety Flags
	executionDisabled   bool
	eventsDisabled      bool
	persistenceDisabled bool
}

// PipelineConfig holds configuration for the pipeline
type PipelineConfig struct {
	DB        *gorm.DB
	Logger    *zap.Logger
	LLMClient LLMClient

	// PHASE-8: AI Client (uses ai_contract.AIClient interface)
	AIClient ai_contract.AIClient

	// New V3 Components
	RulesEngine    rules.Engine
	ActionRegistry *executor.Registry
	EventBus       events.EventBus
	Observability  *observability.Service

	// V2 Integration
	V2StateManager      V2StateManager
	V2KnowledgeCompiler V2KnowledgeCompiler
	Negotiator          Negotiator
}

// V2StateManager interface for managing V2 conversation state
type V2StateManager interface {
	// GetOrCreateState returns the structured conversation state
	GetOrCreateState(ctx context.Context, tenantID, customerID, conversationID interface{}) (interface{}, error)

	// UpdateState updates the state based on new intent and returns the updated state
	UpdateState(ctx context.Context, tenantID, customerID, conversationID interface{}, intent Intent, customerHistory *CustomerData) (interface{}, error)
}

// V2KnowledgeCompiler interface for compiling tenant knowledge
type V2KnowledgeCompiler interface {
	// Compile returns compiled tenant knowledge (products, FAQs, policies, KB)
	Compile(ctx context.Context, tenantID interface{}) (interface{}, error)
}

// NewPipeline creates a new V3 conversation pipeline
// PHASE-5: AI client is now passed as parameter to break import cycle
func NewPipeline(config PipelineConfig) Pipeline {
	// Initialize global storage access for Executors
	storage.SetDB(config.DB)

	// Use V2-aware context builder if loaders are provided
	var ctxBuilder ContextBuilder
	if config.V2StateManager != nil || config.V2KnowledgeCompiler != nil {
		ctxBuilder = NewContextBuilderWithV2(config.DB, config.Logger, config.V2StateManager, config.V2KnowledgeCompiler)
	} else {
		ctxBuilder = NewContextBuilder(config.DB, config.Logger)
	}

	// PHASE-5: AI client is passed in config, not created here
	// This breaks the import cycle: v3 -> ai_client -> v3
	intentExtractor := NewIntentExtractor(config.LLMClient)

	return &pipeline{
		context:   ctxBuilder,
		intent:    intentExtractor, // PHASE-1: Kept for backward compat
		evaluator: NewStateEvaluator(),

		// PHASE-5: AI Client from config
		aiClient: config.AIClient,

		// New Engines
		rulesEngine:    config.RulesEngine,
		actionRegistry: config.ActionRegistry,
		eventBus:       config.EventBus,
		observability:  config.Observability,
		reasoner:       NewReasoner(config.LLMClient),

		// Legacy (Stubbed/Deprecated)
		resolver: NewActionResolver(),
		executor: NewActionExecutor(config.DB, config.Logger, config.Negotiator),

		composer:       NewResponseComposer(config.LLMClient),
		grounder:       NewConversationGrounder(),
		fullGrounder:   NewFullConversationGrounder(),
		logger:         config.Logger,
		diag:           NewPipelineLogger(config.Logger),
		v2StateManager: config.V2StateManager,
	}
}

// Process handles a customer message through the 5-stage pipeline
func (p *pipeline) Process(ctx context.Context, req ConversationRequest) (*response.AIResponse, error) {
	start := time.Now()

	// 1. Adapters: Convert to Shared Type (No Logic)
	// =========================================================================
	// Build legacy context first (still needed for some helpers, or we adapt parts)
	legacyCtx, err := p.context.Build(ctx, req)
	if err != nil {
		p.logger.Error("context_build_failed", zap.Error(err))
		// Return AIResponse on error
		return &response.AIResponse{HumanText: "I'm having trouble accessing my notes right now."}, nil
	}

	// Convert to Rigid ResolvedContext
	resolvedCtx := ToResolvedContext(legacyCtx)

	// Phase 7: TERMINAL STATE SHORT-CIRCUIT
	// Once paid, no more intent detection, reasoning, or selling
	if resolvedCtx.Conversation.TransactionState == domain.TransactionPaid {
		p.logger.Info("terminal_state_paid",
			zap.String("conversation_id", req.ConversationID.String()),
		)

		return &response.AIResponse{
			HumanText: conversation.PostPaymentAcknowledgement(&resolvedCtx),
			Meta: map[string]any{
				"terminal": true,
				"state":    "paid",
			},
		}, nil
	}

	// Step 1.5: INTENT DETECTION & DOMINANCE GATE
	// =========================================================================

	// 1. RUN LEGACY CLASSIFIER FIRST (System Authority)
	// We use this to drive the State Machine *before* asking AI.
	legacyIntent := p.intent.Classify(legacyCtx, req.Message)
	intentResult := intent_pkg.IntentResult{
		Intent:     intent_pkg.IntentType(strings.ToUpper(string(legacyIntent.Type))),
		Confidence: legacyIntent.Confidence,
		Source:     legacyIntent.Source,
	}

	// REQUIREMENTS COLLECTION MODE INTERCEPTOR
	// When in ModeRequirements, treat customer message as requirement answer
	// UNLESS the user is trying to Cancel or Negotiate (Escape Hatches)
	isBypassIntent := intentResult.Intent == intent_pkg.IntentCancel ||
		intentResult.Intent == intent_pkg.IntentCancelOrder ||
		intentResult.Intent == intent_pkg.IntentNegotiatePrice

	if !isBypassIntent && legacyCtx.StateSnapshot != nil && legacyCtx.StateSnapshot.ConversationMode == ModeRequirements {
		if legacyCtx.CurrentOrder != nil && req.Message != "" {
			fs := fulfillment.NewService(p.context.(*contextBuilder).db, p.logger)

			// Get the pending requirement for this order
			var order storage.Order
			if err := p.context.(*contextBuilder).db.First(&order, legacyCtx.CurrentOrder.ID).Error; err != nil {
				p.logger.Error("failed_to_load_order_for_requirement", zap.Error(err))
			} else {
				reqItem, state, err := fs.GetPrePaymentRequirementForOrder(&order)
				if err != nil {
					p.logger.Error("failed_to_get_requirement", zap.Error(err))
				} else if reqItem != nil && state != nil {
					// Save the customer's answer
					if err := fs.SubmitRequirement(state.ID, reqItem.ID, req.Message); err != nil {
						p.logger.Warn("failed_to_save_requirement_answer", zap.Error(err))
					} else {
						// Get next requirement
						nextReq, _, err := fs.GetPrePaymentRequirementForOrder(&order)
						if err != nil {
							p.logger.Error("failed_to_get_next_requirement", zap.Error(err))
						} else if nextReq != nil {
							// There's another requirement - ask it
							msg := fmt.Sprintf("Got it! Next, please send: %s", nextReq.Label)
							if nextReq.Description != "" {
								msg += fmt.Sprintf("\n%s", nextReq.Description)
							}
							return &response.AIResponse{
								HumanText: msg,
								Meta: map[string]any{
									"mode":                   "requirements",
									"requirements_remaining": true,
								},
							}, nil
						} else {
							// All requirements collected - proceed to payment
							legacyCtx.StateSnapshot.ConversationMode = ModePayment

							// Generate payment details message
							paymentMsg := fmt.Sprintf("Perfect! Here are the payment details:\n\n")
							if legacyCtx.PaymentInfo != nil {
								paymentMsg += fmt.Sprintf("Bank: %s\n", legacyCtx.PaymentInfo.BankName)
								paymentMsg += fmt.Sprintf("Account Number: %s\n", legacyCtx.PaymentInfo.AccountNumber)
								paymentMsg += fmt.Sprintf("Amount: %.2f %s\n\n", legacyCtx.CurrentOrder.Total, legacyCtx.CurrentOrder.Currency)
								paymentMsg += "Please send proof of payment after transfer."
							}

							return &response.AIResponse{
								HumanText: paymentMsg,
								Meta: map[string]any{
									"mode":                  "payment",
									"requirements_complete": true,
								},
							}, nil
						}
					}
				}
			}
		}
	}

	// Step 1.5: INTENT DETECTION & DOMINANCE GATE
	// =========================================================================

	// 1. RUN LEGACY CLASSIFIER FIRST (System Authority)
	// Moved upstream to support Requirements Interceptor bypass

	// 2. EVALUATE TRANSACTION STATE (Phase 2)
	// Update state based on Legacy Intent so AI sees the *result* of the action.
	currentTxState := resolvedCtx.Conversation.TransactionState
	var paymentStatus domain.PaymentStatus
	if resolvedCtx.Order != nil {
		paymentStatus = resolvedCtx.Order.Payment.Status
	}

	resolvedCtx.Conversation.TransactionState = conversation.EvaluateTransactionState(
		currentTxState,
		intentResult.Intent,
		paymentStatus,
		p.logger,
	)

	// Phase 5: Safety Validation
	conversation.ValidateTransactionState(
		currentTxState,
		resolvedCtx.Conversation.TransactionState,
		p.logger,
	)

	// Phase 1: Persistence (Passive)
	// Only persist if state has changed to avoid unnecessary DB writes.
	newTxState := resolvedCtx.Conversation.TransactionState
	if newTxState != currentTxState {
		if err := p.context.UpdateTransactionState(ctx, req.ConversationID, newTxState); err != nil {
			p.logger.Error("transaction_state_update_failed", zap.Error(err))
		} else {
			p.logger.Info("transaction_state_transition_saved",
				zap.String("from", string(currentTxState)),
				zap.String("to", string(newTxState)),
			)
		}

		// Phase 4.8: Order Initialization Hook
		// On Browsing → Committed transition, create draft order if none exists
		if currentTxState == domain.TransactionBrowsing && newTxState == domain.TransactionCommitted {
			if resolvedCtx.Order == nil {
				// TODO: Integrate with order service to create draft order
				// This requires:
				// 1. Access to order service in pipeline
				// 2. Cart items from conversation context
				// 3. Create order with status DRAFT
				// 4. Link order to conversation
				p.logger.Info("commitment_detected_order_creation_needed",
					zap.String("conversation_id", req.ConversationID.String()),
				)
			}
		}
	}

	// 3. CALL PYTHON AI (Refinement & Language)
	var aiSuggestion *ai_contract.AISuggestion
	if p.aiClient != nil {
		// Build input with UPDATED TransactionState
		aiInput := map[string]interface{}{
			"tenant_id":       req.TenantID.String(),
			"conversation_id": req.ConversationID.String(),
			"customer_id":     req.CustomerID.String(),
			"message":         req.Message,
			"platform":        req.Platform,
			"context": map[string]interface{}{
				"memory_hud": buildMemoryHUD(legacyCtx, intentResult, resolvedCtx.Conversation.TransactionState),
			},
		}

		// Try Python AI
		suggestion, err := p.aiClient.Analyze(ctx, aiInput)
		if err == nil && suggestion != nil && suggestion.Confidence > 0 {
			// Python AI succeeded - OVERRIDE intent if better
			aiSuggestion = suggestion

			intentResult = intent_pkg.IntentResult{
				Intent:     intent_pkg.IntentType(strings.ToUpper(suggestion.Intent)),
				Confidence: suggestion.Confidence,
				Source:     suggestion.DetectionMethod,
			}
			p.logger.Info("python_ai_classification",
				zap.String("intent", suggestion.Intent),
				zap.Float64("confidence", suggestion.Confidence),
				zap.Int("action_count", len(suggestion.Actions)),
			)
		} else if err != nil {
			p.logger.Warn("python_ai_failed_using_legacy", zap.Error(err))
		}
	}

	// Store AI suggestion in context for later use
	_ = aiSuggestion // Available for decision engine if needed

	// Classify Intent Priority
	intentClass := intent_pkg.ClassifyIntent(intentResult.Intent)

	// 🔒 INTENT DOMINANCE GATE
	switch intentClass {

	case intent_pkg.IntentClassContent:
		// Priority 1: Content Requests (Terminal)
		var termResp executor.TerminalResponse
		switch intentResult.Intent {
		case intent_pkg.IntentRequestOfferings, intent_pkg.IntentRequestMenu:
			// FIX 2: Kill executor.SendMenu Legacy Pricing
			// We MUST NOT return a terminal response here.
			// We want this to flow through to the AI Reasoner -> Action: SHOW_MENU -> /generate endpoint
			// termResp = executor.SendMenu(&resolvedCtx)
			// Do nothing here, termResp.Stop will remain false

		}

		if termResp.Stop {
			return &response.AIResponse{
				HumanText: termResp.Text,
				Meta:      map[string]any{"terminal": true},
			}, nil
		}

	case intent_pkg.IntentClassAction:
		// Priority 2: Explicit Actions (Terminal or Confirmation)
		var termResp executor.TerminalResponse
		switch intentResult.Intent {
		case intent_pkg.IntentCancel, intent_pkg.IntentCancelOrder:
			termResp = executor.RequestCancelConfirmation(&resolvedCtx)
		case intent_pkg.IntentRequestRecommendation:
			// Recommendation keeps using existing ExecuteRecommend for now (Step 4 handles it)
			return executor.ExecuteRecommend(&resolvedCtx), nil
		}

		if termResp.Stop {
			return &response.AIResponse{
				HumanText: termResp.Text,
				Meta:      map[string]any{"terminal": true},
			}, nil
		}

	case intent_pkg.IntentClassDecision:
		// Priority 3: Delegate Decision (Terminal-ish, but uses restricted LLM)
		if intentResult.Intent == intent_pkg.IntentRequestRecommendation ||
			intentResult.Intent == intent_pkg.IntentDelegateDecision {

			// Use decision package with restricted prompt
			respText, err := decision.Recommend(ctx, &resolvedCtx, func(c context.Context, sys, usr string) (string, error) {
				msgs := []LLMMessage{
					{Role: "system", Content: sys},
					{Role: "user", Content: usr},
				}
				return p.reasoner.Client.Generate(c, msgs)
			})
			if err != nil {
				// Fallback handled inside Recommend or here
				respText = "I'm having trouble deciding. What do you usually like?"
			}

			return &response.AIResponse{
				HumanText: respText,
				Meta: map[string]any{
					"action_type": "decision_recommendation",
					"bypass_llm":  true, // Bypasses the GENERAL reasoner
				},
			}, nil
		}

	case intent_pkg.IntentClassQuestion:
		// Priority 4: Questions
		if intentResult.Intent == intent_pkg.IntentCheckOrder {
			termResp := executor.CheckOrder(&resolvedCtx)
			if termResp.Stop {
				return &response.AIResponse{
					HumanText: termResp.Text,
					Meta:      map[string]any{"terminal": true},
				}, nil
			}
		}
	}

	// Step 1.6: CONTEXT VISIBILITY (Phase 3)
	// Replaces legacy scrubbing with Transaction-Aware rules.
	applyContextVisibility(&resolvedCtx, intentResult.Intent, p.logger)

	// Phase 4: COMMITMENT DETECTION (Semantic, LLM-Assisted)
	// Note: gptOutput not yet available - will be implemented when reasoner is extended
	// For now, this is a placeholder for the commitment logic structure
	// TODO: Implement LLM-based commitment detection in reasoner.Generate()

	// Phase 5: COMMITMENT REVERSAL & COOL-OFF
	// CRITICAL: Reversal is BLOCKED if payment intent detected (Phase 6 lock)
	if resolvedCtx.Conversation.TransactionState == domain.TransactionCommitted {
		// Phase 6: Payment Intent Authority & Lock
		// Once payment intent is detected, lock the transaction
		if conversation.IsPaymentIntent(intentResult.Intent) {
			// Transition immediately to AwaitingPayment (LOCK POINT)
			resolvedCtx.Conversation.TransactionState = domain.TransactionAwaitingPayment
			// Keep CommittedAt intact for audit trail

			p.logger.Info("payment_intent_detected",
				zap.String("from", "Committed"),
				zap.String("to", "AwaitingPayment"),
				zap.String("intent", string(intentResult.Intent)),
			)
			p.logger.Info("transaction_locked_for_payment",
				zap.String("state", "AwaitingPayment"),
			)
		} else {
			// No payment intent - reversal and decay are allowed

			// TODO: Add reversal signal detection when reasoner is extended
			// if rev := gptOutput.CommitmentReversalSignal; rev != nil && rev.IsReversal && rev.Confidence >= 0.7 {
			//     resolvedCtx.Conversation.TransactionState = domain.TransactionBrowsing
			//     resolvedCtx.Conversation.CommittedAt = nil
			//     p.logger.Info("commitment_reversed", zap.Float64("confidence", rev.Confidence))
			// }

			// Time-based cool-off decay (10 minutes)
			// CRITICAL: Only decay during silence, not active conversation
			if resolvedCtx.Conversation.CommittedAt != nil &&
				!resolvedCtx.Conversation.HasPendingAction &&
				time.Since(*resolvedCtx.Conversation.CommittedAt) > 10*time.Minute {

				resolvedCtx.Conversation.TransactionState = domain.TransactionBrowsing
				resolvedCtx.Conversation.CommittedAt = nil

				p.logger.Info("commitment_decayed_due_to_inactivity")
			}
		}
	}

	// Phase 6: Lock Enforcement
	// Block reversals once in AwaitingPayment or Paid
	if resolvedCtx.Conversation.TransactionState >= domain.TransactionAwaitingPayment {
		// TODO: When reversal signals are implemented, log blocked attempts
		// if rev := gptOutput.CommitmentReversalSignal; rev != nil && rev.IsReversal {
		//     p.logger.Warn("reversal_blocked_due_to_payment_lock",
		//         zap.String("state", string(resolvedCtx.Conversation.TransactionState)),
		//         zap.Float64("confidence", rev.Confidence),
		//     )
		// }
	}

	// Step 7: SILENCE HANDLING & RE-ENGAGEMENT
	// Triggered by Empty Message (acting as Timer Event)
	if req.Message == "" {
		// New Logic (Step 5): Use Deterministic Evaluator
		newState := conversation.EvaluateSilence(time.Now(), &resolvedCtx.Conversation)
		resolvedCtx.Conversation.SilenceState = newState // Persist state

		if newState == domain.SilenceSoftReengage {
			reengageMsg := conversation.SoftReengagementMessage()

			// Mark as Re-engaged so we don't spam
			resolvedCtx.Conversation.HasReengaged = true
			resolvedCtx.Conversation.LastAIMessageAt = time.Now()

			return &response.AIResponse{
				HumanText: reengageMsg,
				Meta: map[string]any{
					"goal":          conversation.GoalExplore,
					"reengagement":  true,
					"silence_state": newState,
				},
			}, nil
		}

		// If SilenceEvaluator returns Waiting or Stop, we do nothing.
		return &response.AIResponse{HumanText: ""}, nil
	} else {
		// 7.2 Reset Re-engagement State on User Reply
		resolvedCtx.Conversation.HasReengaged = false
		resolvedCtx.Conversation.SilenceState = domain.SilenceWaitingUser // Active
		resolvedCtx.Conversation.LastMessageTime = time.Now()             // Update User Activity
	}

	// 2. Grounding (Social/Pre-check)
	// =========================================================================
	grounding := p.grounder.Ground(legacyCtx, req.Message)
	legacyCtx.Grounding = grounding

	// Intent Classification moved up to Step 1.5.
	// LLM Bypass Logic moved up to Step 1.5 (Gate).
	// State Guard (Anti-Spam) removed in Step 6.

	// FIX 4: Escalation Check (Proactive Handoff)
	// Must happen BEFORE recommendations/offerings.
	if rules.ShouldRecommendHuman(&resolvedCtx) && !resolvedCtx.Conversation.HasSuggestedHuman {
		resolvedCtx.Conversation.HasSuggestedHuman = true
		handoffMsg := conversation.HumanHandoffSuggestion()
		return &response.AIResponse{
			HumanText: handoffMsg,
			Meta: map[string]any{
				"action_type": "suggest_handoff",
				"bypass_llm":  true,
			},
		}, nil
	}

	// FIX 2.2: Bypass Logic moved to Gate (Step 1.5)

	// Step 0: Calculate Confidence, Emotion, and Style (Fix 12 & 18)
	// =========================================================================
	resolvedCtx.Conversation.TurnCount++
	// NegotiationRounds logic handled by specific intents usually, but we can increment if negotiation intent.
	if intentResult.Intent == intent_pkg.IntentNegotiatePrice {
		resolvedCtx.Conversation.NegotiationRounds++
	}
	resolvedCtx.Conversation.ConfidenceLevel = conversation.EvaluateConfidence(
		&resolvedCtx.Conversation,
	)

	// Emotion (Fix 12.4)
	resolvedCtx.Conversation.Emotion = string(conversation.DetectEmotion(req.Message))

	// Style (Fix 18.1)
	detectedStyle := AnalyzeUserStyle(req.Message)
	resolvedCtx.Conversation.DetectedStyle = string(detectedStyle)

	// Resolve Goal (Restored)
	goal := conversation.ResolveGoal(intentResult.Intent)

	mode := ResolveResponseMode(goal) // Recalculate for metadata (deterministic)

	// Workstream B: Silence Handling (Goal Override) - REMOVED (Step 5 Logic handles this)
	ruleResult := p.rulesEngine.Evaluate(resolvedCtx, intentResult, goal)

	// Observability: Log Decision Context
	if p.observability != nil {
		p.observability.LogAIDecision(ctx, observability.AIDecisionEvent{
			TenantID:   req.TenantID,
			Intent:     string(intentResult.Intent),
			RuleResult: mapActionTypesToStrings(ruleResult.AllowedActions), // Log what was allowed
		})
	}

	// 5. Reasoning (GPT)
	// =========================================================================
	gptOutput, err := p.reasoner.Generate(ctx, resolvedCtx, intentResult, ruleResult.AllowedActions, goal, req.Platform)
	if err != nil {
		p.logger.Error("reasoner_failed", zap.Error(err))
		return p.errorResponse("I'm thinking... but got stuck. Can you say that again?"), nil
	}

	// Phase 4.75B: Fact Validation Guard (CRITICAL)
	// Only validate fact-bearing responses (menus, prices, availability, order summaries)
	if isFactBearingResponse(gptOutput.SuggestedAction) {
		if err := guard.ValidateFactGrounding(gptOutput.MessageToUser, resolvedCtx.Catalog, p.logger); err != nil {
			// Guard failure: Silent to user, fallback to safe response
			p.logger.Warn("fact_grounding_violation_blocked",
				zap.Error(err),
				zap.String("original_output", gptOutput.MessageToUser),
				zap.String("action", gptOutput.SuggestedAction),
			)

			// Fallback to deterministic Go response based on action
			gptOutput.MessageToUser = getFallbackResponse(rules.ActionType(gptOutput.SuggestedAction), &resolvedCtx)
		}
	}

	// 6. Execution (Registry Lookup)
	// =========================================================================
	actionType := rules.ActionType(gptOutput.SuggestedAction)
	var eventsList []events.Event

	actionImpl, ok := p.actionRegistry.Get(actionType)
	if !ok {
		// Panic or Error? Per spec "safe clarification"
		p.logger.Error("registry_miss", zap.String("action", string(actionType)))
		// Fallback to clarification
		actionType = rules.ActionClarifyIntent
		// FIX 6.2: Global Ban of "I'm not sure..."
		gptOutput.MessageToUser = "Got it — let me take care of that."
	} else {
		// Execute
		if !p.executionDisabled {
			eventsList, err = actionImpl.Execute(resolvedCtx)
			if err != nil {
				p.logger.Error("execution_failed", zap.Error(err))
				gptOutput.MessageToUser = "Something went wrong while doing that."
			}
		} else {
			p.logger.Info("shadow_mode_skip_execution", zap.String("action", string(actionType)))
		}
	}

	// 7. Event Bus
	// =========================================================================
	if p.eventBus != nil && len(eventsList) > 0 {
		for _, evt := range eventsList {
			// Enrich event with tenant/customer context if missing
			if evt.TenantID == "" {
				evt.TenantID = req.TenantID.String()
			}
			if !p.eventsDisabled {
				p.eventBus.Publish(evt)
			}
		}
	}

	// 8. Observability (Action Result)
	// =========================================================================
	if p.observability != nil {
		p.observability.LogActionResult(ctx, observability.ActionResultEvent{
			TenantID: req.TenantID,
			Action:   string(actionType),
			Success:  err == nil,
		})
	}

	// 9. Response Composition
	// =========================================================================
	// Retrieve messages from GPT output
	respMsg := gptOutput.MessageToUser
	if respMsg == "" {
		respMsg = "Done."
	}

	// PHASE-8: Replace legacy response with context-aware LLM generation
	// Call Python /generate endpoint with memory HUD
	if p.aiClient != nil {
		enhancedResp, err := p.generateContextAwareResponse(ctx, req, actionType, respMsg, legacyCtx, intentResult, resolvedCtx.Conversation.TransactionState)
		if err == nil && enhancedResp != "" {
			p.logger.Info("using_enhanced_llm_response",
				zap.Int("original_len", len(respMsg)),
				zap.Int("enhanced_len", len(enhancedResp)),
			)
			respMsg = enhancedResp
		} else {
			if err != nil {
				p.logger.Warn("enhanced_response_failed_using_legacy", zap.Error(err))
			}
		}
	}

	// Step 1: INTENT REFLECTION (Active Listening)
	// Must happen BEFORE Tone Formatting
	// Validation: Confidence >= 0.7
	if intentResult.Confidence >= 0.7 {
		reflection := conversation.ReflectionForIntent(intentResult.Intent)
		if reflection != "" {
			respMsg = reflection + " " + respMsg
			// Track reflection usage for Step 3 Normalization Logic
			// Since resolvedCtx is a struct passed by value in previous steps, updating it here
			// only affects subsequent local usage in this function scope, which is exactly what we need
			// for the Objection Normalization rule check below.
			resolvedCtx.Conversation.ReflectionUsed = true
		}
	}

	// Step 3: OBJECTION NORMALIZATION (Validation)
	// Must happen BEFORE Qualification, AFTER Reflection.
	// Prepends if allowed.
	if rules.AllowObjectionNormalization(resolvedCtx, intentResult, goal) {
		norm := conversation.ObjectionNormalizer()
		if norm != "" {
			respMsg = norm + " " + respMsg
		}
	}

	// Step 4: POLITE BOUNDARY SETTING & NEXT-STEP OWNERSHIP
	// Step 4.0: Determine if Action is Blocked (Rule Violation or System Limit)
	actionBlocked := false
	// Check if action is in allowed list
	isAllowed := false
	for _, allowed := range ruleResult.AllowedActions {
		if allowed == actionType {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		actionBlocked = true
	}
	// Check for explicit rejection actions (system limits)
	if actionType == rules.ActionRejectDiscount {
		actionBlocked = true
	}

	// Step 4.1: BOUNDARY RESPONSE (Refusal/Blockage)
	// Must happen BEFORE Ownership. Overrides Ownership.
	if rules.RequiresBoundary(resolvedCtx, actionBlocked) {
		boundary := conversation.BoundaryResponse()
		if boundary != "" {
			respMsg = boundary + " " + respMsg
			resolvedCtx.Conversation.IsBoundaryResponse = true
		}
	}

	// Step 4.2: NEXT-STEP OWNERSHIP (Guidance)
	// Only if NOT a boundary response.
	if !resolvedCtx.Conversation.IsBoundaryResponse {
		// 4.2.1 Determine Refusal State (Transient)
		if intentResult.Intent == intent_pkg.IntentRejectPrice ||
			intentResult.Intent == intent_pkg.IntentCancelOrder {
			resolvedCtx.Conversation.IsRefusal = true
		}

		// 4.2.2 Check rule
		// PHASE-8: Disabled legacy ownership prefix. The LLM handles framing.
		// if rules.AllowOwnership(resolvedCtx, string(actionType)) {
		// 	prefix := conversation.OwnershipPrefix()
		// 	if prefix != "" {
		// 		respMsg = prefix + " " + respMsg
		// 	}
		// }
	}

	// Step 2: QUALIFICATION QUESTIONS
	// FIX 22: KILLED THE LOOP.
	// We no longer append rigid questions. The LLM handles this via Prompt (Phase 19).
	// if rules.AllowQualification(resolvedCtx, goal, intentResult.Intent) {
	// 	q := conversation.PickQualificationQuestion()
	// 	if q != "" {
	// 		respMsg = respMsg + " " + q
	// 	}
	//
	// 	// Update Obligations so we don't ask again (even though we just disabled it)
	// 	// resolvedCtx.Obligations.HasAskedQualification = true // This line was commented out in the original instruction, but it's not present in the original code.
	// }

	// Step 6: CLEAN CLOSURE (Handoff)
	// Must happen AFTER QualificationTypes (Step 2), BEFORE Tone.
	// 6.1 Determine Pending Action State
	// If action is substantive (changes state or requires user input), we have a pending action.
	if actionType != rules.ActionAcknowledge &&
		actionType != rules.ActionResumeConversation &&
		actionType != rules.ActionClarifyIntent {
		resolvedCtx.Conversation.HasPendingAction = true
	}

	// 6.2 Check rule
	// Also check if we already closed (HasClosed check is in rule? No, rule uses HasPendingAction. Code in prompt says !ctx.Conversation.HasClosed)
	if rules.AllowClosure(resolvedCtx, goal) && !resolvedCtx.Conversation.HasClosed {
		closure := conversation.ClosurePhrase()
		if closure != "" {
			respMsg = respMsg + " " + closure
			resolvedCtx.Conversation.HasClosed = true
		}
	}

	// Step 8: CONFIDENCE DECAY (Adaptive Phrasing)
	// Must happen AFTER Closure (content assembly), BEFORE Tone.
	// 8.1 Track Negotiation Rounds
	if intentResult.Intent == intent_pkg.IntentNegotiatePrice ||
		intentResult.Intent == intent_pkg.IntentRejectPrice ||
		intentResult.Intent == intent_pkg.IntentNegotiationPushback ||
		intentResult.Intent == intent_pkg.IntentDelayDecision {
		resolvedCtx.Conversation.NegotiationRounds++
	}

	// FIX 3: Enforce price silence after commitment
	// Once committed or paid, strip prices to prevent drift
	if resolvedCtx.Conversation.TransactionState >= domain.TransactionCommitted {
		respMsg = StripPrices(respMsg)
		p.logger.Debug("price_silence_enforced",
			zap.String("transaction_state", string(resolvedCtx.Conversation.TransactionState)),
		)
	}

	// 8.2 Apply Confidence Adjustment
	confidence := conversation.ConfidenceFromRounds(resolvedCtx.Conversation.NegotiationRounds)
	respMsg = conversation.AdjustForConfidence(respMsg, confidence)

	// Step 9: EMOTION-AWARE PHRASING (Empathy)
	// Must happen AFTER Confidence (Step 8), BEFORE Tone (Step 3).
	// 9.1 Detect Emotion
	emotion := conversation.DetectEmotion(req.Message)
	// 9.2 Apply Adjustment
	respMsg = conversation.AdjustForEmotion(respMsg, emotion)

	// Step 3: APPLY TONE PRESET (Persona Layer)
	// Guaranteed to be:
	// 1. After GPT generation
	// 2. Before sending
	// 3. Deterministic
	tone := persona.TonePreset(resolvedCtx.Merchant.TonePreset)
	formatter := persona.FormatterForTone(tone)
	respMsg = formatter.Format(respMsg)

	// Step 10: HUMAN TYPING CADENCE (Micro-Pauses)
	// 10.1 Determine Cadence Mode (Still useful for Meta, but chunking is moved to channel layer)
	cadence := conversation.CadenceForContext(resolvedCtx, string(actionType))

	// FIX 1: Chunking is REMOVED from the core pipeline.
	// The pipeline returns the raw, complete HumanText string.
	// Chunking must happen at the channel layer (customer_responder.go).

	// Construct New Strict AIResponse
	resp := &response.AIResponse{
		HumanText: respMsg,
		Meta: map[string]any{
			"intent_type":      string(intentResult.Intent),
			"action_type":      string(actionType),
			"confidence":       gptOutput.Confidence,
			"goal":             goal,
			"response_mode":    mode,
			"processing_ms":    time.Since(start).Milliseconds(),
			"cadence_mode":     cadence, // Pass cadence advice to channel layer
			"hidden_commands":  gptOutput.Commands,
			"context_products": legacyCtx.Products,
		},
	}

	// p.diag.LogSayV3(resp)
	// To avoid breaking Diag, we might need to update it or temporarily comment it out if it relies on v3.Response.
	// Given strict instructions "Only clean human text... AIResponse cannot cross channel", Diag is internal.
	// I'll assume I should skip p.diag.LogSay for this strict Refactor or update it.
	// For now, I will comment out the diag call for safety to ensure compilation.
	// p.diag.LogSay(response)

	return resp, nil
}

// =============================================================================
// HELPERS
// =============================================================================
// Helper: Map Action Types (for Observability)
// =============================================================================

func mapActionTypesToStrings(actions []rules.ActionType) []string {
	res := make([]string, len(actions))
	for i, a := range actions {
		res[i] = string(a)
	}
	return res
}

// =============================================================================
// Helper: Error Response
// =============================================================================

func (p *pipeline) errorResponse(msg string) *response.AIResponse {
	return &response.AIResponse{
		HumanText: msg,
		Meta: map[string]any{
			"error": true,
		},
	}
}

// =============================================================================
// Helper: Memory HUD Builder (PHASE-8)
// =============================================================================
// These functions curate what the AI "sees" at each turn.
// Go owns memory authority; Python consumes the HUD.

// buildMemoryHUD creates a curated context snapshot for the AI.
// This is NOT raw chat history - it's structured, meaningful state.
func buildMemoryHUD(ctx *ConversationContext, intentResult intent_pkg.IntentResult, txState domain.TransactionState) map[string]interface{} {
	return map[string]interface{}{
		"transaction_state":       txState,
		"conversation_continuity": buildContinuity(ctx, intentResult),
		"business_state":          buildBusinessState(ctx),
		"customer_context":        buildCustomerContext(ctx),
		"available_products":      ctx.Products,
		"available_services":      ctx.Services,
	}
}

// buildContinuity extracts MEANING from recent conversation, not raw text.
func buildContinuity(ctx *ConversationContext, intentResult intent_pkg.IntentResult) map[string]interface{} {
	continuity := map[string]interface{}{}

	// Only add current_intent if it's been classified (not zero value)
	if intentResult.Intent != "" {
		continuity["current_intent"] = string(intentResult.Intent)
	}

	// Extract topic from recent messages (last AI response)
	if len(ctx.RecentMessages) > 0 {
		// Find last assistant message
		for i := len(ctx.RecentMessages) - 1; i >= 0; i-- {
			msg := ctx.RecentMessages[i]
			if msg.Role == "assistant" {
				// Extract first product/service mentioned (simple heuristic)
				content := msg.Content
				if len(content) > 0 {
					continuity["last_ai_message_preview"] = truncate(content, 100)
				}
				break
			}
		}
	}

	// If there's a pending cart, the topic is likely the last added item
	if len(ctx.PendingCart) > 0 {
		lastItem := ctx.PendingCart[len(ctx.PendingCart)-1]
		continuity["topic"] = lastItem.ProductName
		continuity["last_discussed_product"] = lastItem.ProductName
	}

	return continuity
}

// buildBusinessState returns authoritative business state.
func buildBusinessState(ctx *ConversationContext) map[string]interface{} {
	state := map[string]interface{}{}

	// Cart status
	if len(ctx.PendingCart) == 0 {
		state["cart_status"] = "empty"
		state["cart_items"] = []interface{}{}
	} else {
		state["cart_status"] = fmt.Sprintf("%d items", len(ctx.PendingCart))
		// Serialize cart items
		items := make([]map[string]interface{}, len(ctx.PendingCart))
		for i, item := range ctx.PendingCart {
			items[i] = map[string]interface{}{
				"product_id":   item.ProductID.String(),
				"product_name": item.ProductName,
				"quantity":     item.Quantity,
				"unit_price":   item.UnitPrice,
				"currency":     item.Currency,
			}
		}
		state["cart_items"] = items
	}

	// Order status
	if ctx.CurrentOrder != nil {
		state["order_status"] = string(ctx.CurrentOrder.State)
		state["order_id"] = ctx.CurrentOrder.ID.String()
	} else {
		state["order_status"] = nil
	}

	return state
}

// buildCustomerContext returns stable customer facts.
func buildCustomerContext(ctx *ConversationContext) map[string]interface{} {
	customer := map[string]interface{}{}

	if ctx.CustomerHistory != nil {
		// Basic identity
		if ctx.CustomerHistory.Name != "" {
			customer["name"] = ctx.CustomerHistory.Name
		}

		// Trust signals
		customer["is_returning"] = ctx.CustomerHistory.PreviousOrders > 0
		customer["total_orders"] = ctx.CustomerHistory.PreviousOrders
		customer["total_spent"] = ctx.CustomerHistory.TotalSpend

		// Derive trust level
		if ctx.CustomerHistory.PreviousOrders >= 5 {
			customer["trust_level"] = "high"
		} else if ctx.CustomerHistory.PreviousOrders >= 2 {
			customer["trust_level"] = "medium"
		} else {
			customer["trust_level"] = "new"
		}
	}

	return customer
}

// truncate limits string length for preview.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// =============================================================================
// Helper: Execution Result Extraction (PHASE-8)
// =============================================================================

// extractExecutionResult automatically captures relevant data based on action type.
// This prevents the recurring issue of forgetting to pass execution results to /generate.
func extractExecutionResult(ctx *ConversationContext, actionType rules.ActionType) map[string]interface{} {
	result := make(map[string]interface{})

	// Convert actionType to string for comparison
	action := string(actionType)

	switch action {
	case "SHOW_MENU", "SHOW_PRODUCTS", "PITCH_PRODUCT", "SUGGEST_NEXT_STEP":
		// Menu/product display actions
		if len(ctx.Products) > 0 {
			// FIX 1: Normalize Product Serialization
			// Explicitly map fields to ensure lowercase JSON keys and control over data
			products := make([]map[string]any, len(ctx.Products))
			for i, p := range ctx.Products {
				products[i] = map[string]any{
					"name":        p.Name,
					"price":       p.Price,
					"currency":    p.Currency,
					"category":    p.Category,
					"description": p.Description,
					"in_stock":    p.InStock,
				}
			}
			result["products"] = products
		}
		if len(ctx.Services) > 0 {
			// Normalize Service Serialization as well
			services := make([]map[string]any, len(ctx.Services))
			for i, s := range ctx.Services {
				services[i] = map[string]any{
					"name":        s.Name,
					"price":       s.Price,
					"currency":    s.Currency,
					"description": s.Description,
					"duration":    s.Duration,
				}
			}
			result["services"] = services
		}

	case "SHOW_CART":
		// Cart display action
		if len(ctx.PendingCart) > 0 {
			result["cart"] = ctx.PendingCart
		}

	case "SHOW_STATUS":
		// Order status action
		if ctx.CurrentOrder != nil {
			result["order"] = ctx.CurrentOrder
			// Log order details for debugging
			hasItems := ctx.CurrentOrder.Items != nil && len(ctx.CurrentOrder.Items) > 0
			// Fallback: If order has no items, include cart for display
			if !hasItems && len(ctx.PendingCart) > 0 {
				result["cart"] = ctx.PendingCart
			}
		}

	case "SHOW_PAYMENT":
		// Payment display action
		if len(ctx.PendingCart) > 0 {
			result["cart"] = ctx.PendingCart
		}
		if ctx.PaymentInfo != nil {
			result["payment_info"] = ctx.PaymentInfo
		}

	case "ADD_TO_CART", "REMOVE_FROM_CART":
		// Cart modification actions - show updated cart
		if len(ctx.PendingCart) > 0 {
			result["cart"] = ctx.PendingCart
		}
	}

	return result
}

// =============================================================================
// Helper: Context-Aware Response Generation (PHASE-8)
// =============================================================================

// generateContextAwareResponse calls Python /generate endpoint with memory HUD.
// This replaces legacy templates with LLM-generated, context-aware responses.
func (p *pipeline) generateContextAwareResponse(
	ctx context.Context,
	req ConversationRequest,
	actionType rules.ActionType,
	_ string,
	legacyCtx *ConversationContext,
	intentResult intent_pkg.IntentResult,
	txState domain.TransactionState,
) (string, error) {
	// Build memory HUD
	hud := buildMemoryHUD(legacyCtx, intentResult, txState)

	// PHASE-8: Extract execution result automatically based on action type
	executionResult := extractExecutionResult(legacyCtx, actionType)

	// Log what we extracted for debugging
	logFields := []zap.Field{
		zap.String("action", string(actionType)),
		zap.Int("result_keys", len(executionResult)),
		zap.Bool("has_products", executionResult["products"] != nil),
		zap.Bool("has_cart", executionResult["cart"] != nil),
		zap.Bool("has_order", executionResult["order"] != nil),
	}

	// Add product details if available
	if products, ok := executionResult["products"].([]Product); ok && len(products) > 0 {
		logFields = append(logFields,
			zap.Int("product_count", len(products)),
			zap.String("first_product", products[0].Name),
			zap.Float64("first_price", products[0].Price),
		)
	}

	p.logger.Info("execution_result_extracted", logFields...)

	// Get tenant's tone from business profile
	tone := "friendly" // default
	if legacyCtx.BusinessTone != "" {
		tone = legacyCtx.BusinessTone
	}

	// Build generate request
	genInput := map[string]any{
		"tenant_id":        req.TenantID.String(),
		"conversation_id":  req.ConversationID.String(),
		"customer_id":      req.CustomerID.String(),
		"user_message":     req.Message,
		"final_action":     string(actionType),
		"allowed":          true, // If we got here, action was allowed
		"reason":           "Action completed",
		"memory_hud":       hud,
		"brand_tone":       tone, // Use tenant's actual tone
		"verbosity":        "medium",
		"execution_result": executionResult,        // Automatic extraction
		"industry_type":    legacyCtx.BusinessType, // Industry for terminology (e.g., "Food & Beverage")
	}

	// Call Python /generate endpoint
	message, err := p.aiClient.Generate(ctx, genInput)
	if err != nil {
		return "", err
	}

	return message, nil
}

// =============================================================================
// Helper: Shadow Mode Controls
// =============================================================================

func (p *pipeline) DisableExecution() {
	p.executionDisabled = true
}

func (p *pipeline) DisableEvents() {
	p.eventsDisabled = true
}

func (p *pipeline) DisablePersistence() {
	p.persistenceDisabled = true
}

// applyContextVisibility determines if the Order context should be visible to the AI.
//
// PHASE 3: MEMORY AUTHORITY SHIFT
// Intent no longer controls memory visibility. TransactionState does.
// This prevents "memory wipes" during payment flows.
//
// Rules:
// 1. If TransactionState >= Committed -> Order MUST be visible.
// 2. If Browsing -> Fallback to intent check (UserAskedAboutOrder).
func applyContextVisibility(ctx *types.ResolvedContext, currentIntent intent_pkg.IntentType, logger *zap.Logger) {
	state := ctx.Conversation.TransactionState

	// Rule 1: Committed+ -> Always Visible
	if state == domain.TransactionCommitted ||
		state == domain.TransactionAwaitingPayment ||
		state == domain.TransactionPaid {
		// Do NOT scrub. Keep Order visible.
		if logger != nil {
			logger.Debug("context_visibility_applied",
				zap.String("transaction_state", string(state)),
				zap.Bool("order_visible", ctx.Order != nil),
				zap.String("rule", "committed_plus_always_visible"),
			)
		}
		return
	}

	// Rule 2: Browsing -> Visible only if asked
	// PHASE 3 NOTE: This is the legacy fallback for browsing state.
	// Intent dependency remains here only for backward compatibility.
	orderWasVisible := ctx.Order != nil
	if !intent_pkg.UserAskedAboutOrder(currentIntent) {
		ctx.Order = nil
	}

	if logger != nil {
		logger.Debug("context_visibility_applied",
			zap.String("transaction_state", string(state)),
			zap.Bool("order_visible", ctx.Order != nil),
			zap.Bool("order_scrubbed", orderWasVisible && ctx.Order == nil),
			zap.String("intent", string(currentIntent)),
			zap.String("rule", "browsing_intent_check"),
		)
	}
}

// isFactBearingResponse determines if a response requires fact validation
// PHASE 4.75B: Only fact-bearing responses go through the guard
// FIX 2: Expanded to catch more fact-restating actions
func isFactBearingResponse(action string) bool {
	factBearingActions := map[string]bool{
		"SHOW_MENU":         true,
		"QUOTE_PRICE":       true,
		"CONFIRM_ORDER":     true,
		"SHOW_AVAILABILITY": true,
		"SEND_PAYMENT":      true,
		"SHOW_RECEIPT":      true,
		// FIX 2: These often restate facts
		"SUGGEST_NEXT_STEP":   true,
		"ACKNOWLEDGE":         true,
		"RESUME_CONVERSATION": true,
	}

	return factBearingActions[action]
}

// getFallbackResponse generates a safe deterministic response when fact validation fails
// PHASE 4.75B: Silent fallback, no error messages to user
func getFallbackResponse(action rules.ActionType, ctx *types.ResolvedContext) string {
	switch action {
	case "SHOW_MENU":
		return "Let me get you our menu. One moment..."
	case "QUOTE_PRICE":
		return "Let me check the exact pricing for you."
	case "CONFIRM_ORDER":
		if ctx.Order != nil {
			return "Let me confirm your order details."
		}
		return "Let me help you with that."
	case "RESUME_CONVERSATION", "ACKNOWLEDGE":
		// FIX 5: State-based recall, not LLM-based
		if ctx.Order != nil {
			// Return order summary from state
			return "Let me recap your order."
		}
		return "I'm here to help. What would you like?"
	default:
		return "Got it — let me take care of that."
	}
}

// StripPrices removes price mentions from text to prevent price drift
// FIX 3: Enforce price silence after commitment
func StripPrices(text string) string {
	// Remove currency patterns
	priceRegex := regexp.MustCompile(`₦\s*\d+[,\d]*|\bNGN\s*\d+[,\d]*|\$\s*\d+[,\d]*`)
	return priceRegex.ReplaceAllString(text, "the amount we discussed")
}
