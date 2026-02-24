package observability

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// =============================================================================
// AI DECISION AUDIT - Explainability & Observability
// =============================================================================
// This package provides append-only audit records for all AI-assisted decisions.
//
// PHASE-6: This enables full traceability and explainability.
// Every AI interaction is recorded with:
// - What the AI suggested
// - Why it was accepted or rejected
// - What rule caused the final outcome
//
// RULES:
// - Append-only (no updates or deletes)
// - No business logic depends on this
// - Used for observability and debugging only
// =============================================================================

// AIDecisionAudit is the append-only audit record for AI decisions.
type AIDecisionAudit struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	CreatedAt time.Time `gorm:"index"`

	// Correlation & Identity
	TenantID       uuid.UUID `gorm:"type:uuid;index;not null"`
	ConversationID uuid.UUID `gorm:"type:uuid;index;not null"`
	CustomerID     uuid.UUID `gorm:"type:uuid;index"`
	CorrelationID  string    `gorm:"index"` // Request ID for tracing

	// AI Input
	AIIntent          string  `gorm:"index;not null"` // Detected intent
	AIConfidence      float64 `gorm:"not null"`       // AI's confidence (0.0-1.0)
	AISource          string  `gorm:"index"`          // "python_ai", "legacy_ai", "fallback"
	AIDetectionMethod string  // "llm", "heuristic", "hybrid"

	// AI Suggestions (JSON)
	AISuggestedActions string `gorm:"type:jsonb"` // Array of suggested actions with scores

	// Decision Outcome
	FinalAction    string `gorm:"index"`      // The action that was selected
	DecisionResult string `gorm:"not null"`   // "allowed", "rejected"
	DecisionReason string `gorm:"not null"`   // Human-readable explanation
	RuleTrace      string `gorm:"type:jsonb"` // Ordered list of rules applied

	// Rejected Actions (JSON)
	RejectedActions string `gorm:"type:jsonb"` // Actions that were not selected with reasons

	// Metadata
	UserMessage      string // Original user message (for context)
	ProcessingTimeMs int64  // How long the decision took
}

// TableName specifies the table name for GORM.
func (AIDecisionAudit) TableName() string {
	return "ai_decision_audits"
}

// =============================================================================
// AUDIT SERVICE
// =============================================================================

// AuditService handles AI decision audit logging.
type AuditService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAuditService creates a new audit service.
func NewAuditService(db *gorm.DB, logger *zap.Logger) *AuditService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AuditService{
		db:     db,
		logger: logger,
	}
}

// AIDecisionRecord is the input for recording an AI decision.
type AIDecisionRecord struct {
	// Correlation & Identity
	TenantID       uuid.UUID
	ConversationID uuid.UUID
	CustomerID     uuid.UUID
	CorrelationID  string

	// AI Input
	AIIntent           string
	AIConfidence       float64
	AISource           string
	AIDetectionMethod  string
	AISuggestedActions []SuggestedActionRecord

	// Decision Outcome
	FinalAction     string
	DecisionAllowed bool
	DecisionReason  string
	RuleTrace       []string
	RejectedActions []RejectedActionRecord

	// Metadata
	UserMessage      string
	ProcessingTimeMs int64
}

// SuggestedActionRecord represents a single AI-suggested action.
type SuggestedActionRecord struct {
	Type      string                 `json:"type"`
	Score     float64                `json:"score"`
	Params    map[string]interface{} `json:"params,omitempty"`
	Reasoning string                 `json:"reasoning,omitempty"`
}

// RejectedActionRecord represents an action that was rejected.
type RejectedActionRecord struct {
	Type   string  `json:"type"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
	RuleID string  `json:"rule_id,omitempty"`
}

// RecordDecision creates an append-only audit record for an AI decision.
// PHASE-6: This is called after every decision, regardless of outcome.
func (s *AuditService) RecordDecision(ctx context.Context, record AIDecisionRecord) error {
	// Serialize suggested actions
	suggestedActionsJSON, err := json.Marshal(record.AISuggestedActions)
	if err != nil {
		s.logger.Error("failed to marshal suggested actions", zap.Error(err))
		suggestedActionsJSON = []byte("[]")
	}

	// Serialize rejected actions
	rejectedActionsJSON, err := json.Marshal(record.RejectedActions)
	if err != nil {
		s.logger.Error("failed to marshal rejected actions", zap.Error(err))
		rejectedActionsJSON = []byte("[]")
	}

	// Serialize rule trace
	ruleTraceJSON, err := json.Marshal(record.RuleTrace)
	if err != nil {
		s.logger.Error("failed to marshal rule trace", zap.Error(err))
		ruleTraceJSON = []byte("[]")
	}

	// Determine decision result
	decisionResult := "rejected"
	if record.DecisionAllowed {
		decisionResult = "allowed"
	}

	// Create audit record
	audit := AIDecisionAudit{
		TenantID:           record.TenantID,
		ConversationID:     record.ConversationID,
		CustomerID:         record.CustomerID,
		CorrelationID:      record.CorrelationID,
		AIIntent:           record.AIIntent,
		AIConfidence:       record.AIConfidence,
		AISource:           record.AISource,
		AIDetectionMethod:  record.AIDetectionMethod,
		AISuggestedActions: string(suggestedActionsJSON),
		FinalAction:        record.FinalAction,
		DecisionResult:     decisionResult,
		DecisionReason:     record.DecisionReason,
		RuleTrace:          string(ruleTraceJSON),
		RejectedActions:    string(rejectedActionsJSON),
		UserMessage:        record.UserMessage,
		ProcessingTimeMs:   record.ProcessingTimeMs,
	}

	// PHASE-6: Append-only insert
	if err := s.db.WithContext(ctx).Create(&audit).Error; err != nil {
		s.logger.Error("failed to create audit record",
			zap.Error(err),
			zap.String("correlation_id", record.CorrelationID),
		)
		return err
	}

	// PHASE-6: Centralized decision logging
	s.logger.Info("ai_decision_audited",
		zap.String("correlation_id", record.CorrelationID),
		zap.String("intent", record.AIIntent),
		zap.Float64("confidence", record.AIConfidence),
		zap.String("ai_source", record.AISource),
		zap.String("final_action", record.FinalAction),
		zap.String("decision_result", decisionResult),
		zap.String("decision_reason", record.DecisionReason),
		zap.Int("num_suggestions", len(record.AISuggestedActions)),
		zap.Int("num_rejected", len(record.RejectedActions)),
		zap.Int64("processing_ms", record.ProcessingTimeMs),
	)

	return nil
}

// =============================================================================
// CONFIDENCE THRESHOLD (Observe-Only)
// =============================================================================

const (
	// IntentConfidenceThreshold is the minimum confidence for high-confidence intents.
	// PHASE-6 NOTE: This is for observation only - not enforced yet.
	IntentConfidenceThreshold = 0.7
)

// IsLowConfidence checks if a confidence score is below the threshold.
// PHASE-6 NOTE: This is for logging and observation only.
// No enforcement applied yet.
func IsLowConfidence(confidence float64) bool {
	return confidence < IntentConfidenceThreshold
}

// LogLowConfidence logs a low-confidence AI suggestion.
// PHASE-6 NOTE: Observation only - no behavior change.
func (s *AuditService) LogLowConfidence(
	correlationID string,
	intent string,
	confidence float64,
	source string,
) {
	// PHASE-6 NOTE:
	// Low-confidence AI suggestion observed.
	// No enforcement applied yet.
	s.logger.Warn("low_confidence_ai_suggestion",
		zap.String("correlation_id", correlationID),
		zap.String("intent", intent),
		zap.Float64("confidence", confidence),
		zap.String("ai_source", source),
		zap.Float64("threshold", IntentConfidenceThreshold),
		zap.String("phase_note", "observation_only_no_enforcement"),
	)
}

// =============================================================================
// QUERY HELPERS (Read-Only)
// =============================================================================

// GetDecisionHistory retrieves audit records for a conversation.
// PHASE-6: For debugging and explainability.
func (s *AuditService) GetDecisionHistory(
	ctx context.Context,
	conversationID uuid.UUID,
	limit int,
) ([]AIDecisionAudit, error) {
	var audits []AIDecisionAudit

	err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(limit).
		Find(&audits).Error

	if err != nil {
		return nil, err
	}

	return audits, nil
}

// GetDecisionByCorrelationID retrieves a specific decision by correlation ID.
// PHASE-6: For tracing a specific request end-to-end.
func (s *AuditService) GetDecisionByCorrelationID(
	ctx context.Context,
	correlationID string,
) (*AIDecisionAudit, error) {
	var audit AIDecisionAudit

	err := s.db.WithContext(ctx).
		Where("correlation_id = ?", correlationID).
		First(&audit).Error

	if err != nil {
		return nil, err
	}

	return &audit, nil
}
