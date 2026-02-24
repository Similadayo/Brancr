package bot

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// EscalationBehavior constants
const (
	EscalationAlwaysOn     = "always_on"    // Always escalate to owner (default)
	EscalationConfigurable = "configurable" // Tenant can enable/disable escalation
	EscalationAdvanced     = "advanced"     // Advanced rules: specific triggers only
)

// EscalationBehaviorService checks tenant's escalation settings
type EscalationBehaviorService struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewEscalationBehaviorService creates a new escalation behavior service
func NewEscalationBehaviorService(db *gorm.DB, logger *zap.Logger) *EscalationBehaviorService {
	return &EscalationBehaviorService{
		DB:     db,
		Logger: logger,
	}
}

// ShouldEscalate determines if an escalation should be sent based on tenant's plan settings
// Returns true if escalation should proceed, false if it should be skipped
// reason: the escalation reason (e.g., "payment", "complaint", "unknown_intent")
// intent: the detected intent (e.g., "payment", "inquiry", "complaint")
func (s *EscalationBehaviorService) ShouldEscalate(ctx context.Context, tenantID uuid.UUID, reason, intent string) bool {
	// Get tenant's plan to check escalation behavior
	var tenantPlan storage.TenantPlan
	err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND billing_status IN (?)", tenantID, []string{"trial", "active"}).
		Order("created_at DESC").
		First(&tenantPlan).Error

	if err != nil {
		// No plan found or error - default to always_on (backward compatible)
		s.Logger.Debug("no tenant plan found, defaulting to always_on escalation",
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
		return true
	}

	behavior := tenantPlan.EscalationBehavior
	if behavior == "" {
		behavior = EscalationAlwaysOn // Default if not set
	}

	switch behavior {
	case EscalationAlwaysOn:
		// Always escalate - this is the current default behavior
		return true

	case EscalationConfigurable:
		// Check if tenant has escalation enabled in their settings
		// For now, configurable still defaults to enabled
		// TODO: Add TenantSettings.EscalationEnabled field for per-tenant toggle
		return s.checkTenantEscalationEnabled(ctx, tenantID)

	case EscalationAdvanced:
		// Advanced: Only escalate for specific high-priority intents
		return s.shouldEscalateAdvanced(ctx, tenantID, reason, intent)

	default:
		// Unknown behavior - default to always_on for safety
		s.Logger.Warn("unknown escalation behavior, defaulting to always_on",
			zap.String("behavior", behavior),
			zap.Stringer("tenant_id", tenantID))
		return true
	}
}

// checkTenantEscalationEnabled checks if tenant has escalation enabled via metadata in text column
// For "configurable" mode - tenant can toggle this in settings
func (s *EscalationBehaviorService) checkTenantEscalationEnabled(ctx context.Context, tenantID uuid.UUID) bool {
	var tenant storage.Tenant
	if err := s.DB.WithContext(ctx).Select("metadata").First(&tenant, tenantID).Error; err != nil {
		return true // Default to enabled if tenant not found
	}

	// Metadata is JSON string in text column
	if tenant.Metadata == "" || tenant.Metadata == "{}" {
		return true // Default to enabled
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(tenant.Metadata), &meta); err != nil {
		s.Logger.Warn("failed to unmarshal tenant metadata", zap.Error(err))
		return true
	}

	// Check for explicit disable
	if enabled, ok := meta["escalation_enabled"].(bool); ok {
		return enabled
	}

	return true // Default to enabled
}

// shouldEscalateAdvanced applies advanced escalation rules
// Only escalates for high-priority intents/reasons
func (s *EscalationBehaviorService) shouldEscalateAdvanced(ctx context.Context, tenantID uuid.UUID, reason, intent string) bool {
	// High-priority intents that always trigger escalation
	highPriorityIntents := map[string]bool{
		"payment":         true, // Payment-related always escalate
		"refund":          true, // Refund requests always escalate
		"complaint":       true, // Complaints always escalate
		"angry_customer":  true, // Angry customers need human attention
		"legal":           true, // Legal matters always escalate
		"urgent":          true, // Urgent requests escalate
		"human_requested": true, // Customer explicitly asked for human
		"unknown":         true, // Unknown intents need human review
	}

	// High-priority reasons that always trigger escalation
	highPriorityReasons := map[string]bool{
		"payment":          true,
		"payment_claim":    true,
		"refund":           true,
		"complaint":        true,
		"low_confidence":   true, // AI not confident, escalate
		"human_request":    true, // Customer requested human
		"complex_inquiry":  true,
		"negotiation":      true, // Price negotiation
		"order_issue":      true,
		"delivery_problem": true,
	}

	// Check if intent is high-priority
	if highPriorityIntents[intent] {
		s.Logger.Debug("escalating due to high-priority intent",
			zap.String("intent", intent),
			zap.Stringer("tenant_id", tenantID))
		return true
	}

	// Check if reason is high-priority
	if highPriorityReasons[reason] {
		s.Logger.Debug("escalating due to high-priority reason",
			zap.String("reason", reason),
			zap.Stringer("tenant_id", tenantID))
		return true
	}

	// For advanced mode, skip non-priority escalations
	s.Logger.Info("skipping escalation in advanced mode (not high-priority)",
		zap.String("intent", intent),
		zap.String("reason", reason),
		zap.Stringer("tenant_id", tenantID))
	return false
}

// GetEscalationBehavior returns the escalation behavior for a tenant
func (s *EscalationBehaviorService) GetEscalationBehavior(ctx context.Context, tenantID uuid.UUID) string {
	var tenantPlan storage.TenantPlan
	err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND billing_status IN (?)", tenantID, []string{"trial", "active"}).
		Order("created_at DESC").
		First(&tenantPlan).Error

	if err != nil {
		return EscalationAlwaysOn // Default
	}

	if tenantPlan.EscalationBehavior == "" {
		return EscalationAlwaysOn
	}

	return tenantPlan.EscalationBehavior
}
