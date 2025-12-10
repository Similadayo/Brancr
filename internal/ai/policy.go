package ai

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PolicyDecision - outcome of policy evaluation
type PolicyDecision struct {
	Action      string  // "auto_reply", "ask_clarify", "escalate"
	Confidence  float64 // aggregated confidence
	Reason      string
	ShouldHuman bool
	CreatedAt   time.Time
}

// PolicyEngine holds tenant-level thresholds and global defaults
type PolicyEngine struct {
	DB *gorm.DB // pass in your gorm DB type or interface
	// Default thresholds
	AutoReplyThreshold    float64
	EscalateEmotionLabels []string
}

func NewPolicyEngine() *PolicyEngine {
	var db *gorm.DB
	return &PolicyEngine{
		DB:                    db,
		AutoReplyThreshold:    0.75,
		EscalateEmotionLabels: []string{"urgent", "angry", "frustrated"},
	}
}

// Evaluate decides what to do given intent + emotion results
func (p *PolicyEngine) Evaluate(ctx context.Context, tenantID uint, intent *IntentResult, emotion *EmotionResult) (*PolicyDecision, error) {
	// allow tenant-level overrides (pseudo)
	// settings := p.getTenantPolicy(tenantID)

	aggConfidence := intent.Confidence

	// Handle emotion-based escalations first
	if emotion != nil {
		// reduce effective confidence if emotion indicates urgency/frustration (force human)
		for _, lab := range p.EscalateEmotionLabels {
			if emotion.Label == lab && emotion.Confidence > 0.6 {
				return &PolicyDecision{
					Action:      "escalate",
					Confidence:  aggConfidence,
					Reason:      fmt.Sprintf("emotion=%s", emotion.Label),
					ShouldHuman: true,
					CreatedAt:   time.Now(),
				}, nil
			}
		}
	}

	// Handle payment-related intents smartly:
	// - "account" intent: CAN auto-reply if account details are stored
	// - "payment": MUST escalate (tenant needs to verify & send receipt)
	// - "refund": MUST escalate (requires human judgment)
	// - "complaint": MUST escalate (requires human judgment)
	if intent.Intent == "refund" || intent.Intent == "complaint" {
		return &PolicyDecision{
			Action:      "escalate",
			Confidence:  aggConfidence,
			Reason:      fmt.Sprintf("critical_issue=%s", intent.Intent),
			ShouldHuman: true,
			CreatedAt:   time.Now(),
		}, nil
	}

	// "payment" intent (customer says they paid) - escalate for verification
	if intent.Intent == "payment" && aggConfidence > 0.7 {
		return &PolicyDecision{
			Action:      "escalate",
			Confidence:  aggConfidence,
			Reason:      "payment_requires_tenant_verification",
			ShouldHuman: true,
			CreatedAt:   time.Now(),
		}, nil
	}

	// "account" intent (customer asking for payment account) - can auto-reply
	if intent.Intent == "account" && aggConfidence > 0.75 {
		return &PolicyDecision{
			Action:      "auto_reply",
			Confidence:  aggConfidence,
			Reason:      "account_details_available",
			ShouldHuman: false,
			CreatedAt:   time.Now(),
		}, nil
	}

	// If intent unknown or confidence low -> ask clarify
	if intent.Intent == "unknown" || aggConfidence < p.AutoReplyThreshold {
		return &PolicyDecision{
			Action:      "ask_clarify",
			Confidence:  aggConfidence,
			Reason:      "low_confidence",
			ShouldHuman: false,
			CreatedAt:   time.Now(),
		}, nil
	}

	// otherwise auto reply
	return &PolicyDecision{
		Action:      "auto_reply",
		Confidence:  aggConfidence,
		Reason:      "ok_to_auto",
		ShouldHuman: false,
		CreatedAt:   time.Now(),
	}, nil
}
