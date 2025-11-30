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
