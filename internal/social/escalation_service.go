package social

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// EscalationType represents the type of escalation
type EscalationType string

const (
	EscalationTypePricingApproval EscalationType = "pricing_approval" // Negotiation, offers, pricing decisions
	EscalationTypeHumanTakeover   EscalationType = "human_takeover"   // Abuse, legal, confusion, explicit requests
)

// EscalationDecision represents the decision to escalate
type EscalationDecision struct {
	ShouldEscalate bool
	Type           EscalationType
	Reason         string
	PauseAI        bool // For pricing approval, pause AI until tenant responds
	DisableAI      bool // For human takeover, disable AI for conversation
}

// EscalationService handles escalation decisions with clear separation
// Core principle: Escalation is not intent. It's a decision after intent + rules.
type EscalationService struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	NegotiationMgr interface{} // Negotiation manager (optional, set after initialization)
}

// NewEscalationService creates a new escalation service
func NewEscalationService(db *gorm.DB, logger *zap.Logger, negotiationMgr interface{}) *EscalationService {
	return &EscalationService{
		DB:             db,
		Logger:         logger,
		NegotiationMgr: negotiationMgr,
	}
}

// EvaluateEscalation evaluates whether escalation is needed and what type
// This is called AFTER intent detection and BEFORE AI generation
func (e *EscalationService) EvaluateEscalation(
	ctx context.Context,
	msg IncomingMessage,
	customer *storage.Customer,
	intent *ai.DetectedIntent,
	conversationID uuid.UUID,
	salesConv *storage.SalesConversation,
) (*EscalationDecision, error) {
	// Step 1: Check for explicit human request (highest priority)
	if e.isExplicitHumanRequest(msg.Content) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "customer_explicitly_requested_human",
			PauseAI:        false,
			DisableAI:      true, // Disable AI for this conversation
		}, nil
	}

	// Step 2: Check for abuse/legal risk
	if e.isAbuseOrLegalRisk(msg.Content, intent) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "abuse_or_legal_risk",
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 2.5: Check for significant complaints (single strong complaint)
	if intent != nil && (intent.Intent == "complaint" || intent.Intent == "issue") && intent.Confidence > 0.7 {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "customer_complaint_requires_human",
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 3: Check for repeated confusion (5+ truly unclear messages with very low confidence)
	// NOTE: This should NOT trigger on casual greetings like "Hey wassup", "Hello", "Good afternoon"
	// Only trigger on actual confusion (confidence < 0.3 or unknown/unrecognized intent)
	if conversationID != uuid.Nil && e.isRepeatedConfusion(ctx, msg.TenantID, customer.ID, conversationID) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "repeated_confusion",
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 4: Check for pricing approval (negotiation, offers)
	if salesConv != nil {
		offer := e.extractOffer(msg.Content)
		if offer > 0 {
			decision, err := e.evaluatePricingApproval(ctx, msg, salesConv, offer)
			if err != nil {
				e.Logger.Warn("failed to evaluate pricing approval", zap.Error(err))
			} else if decision != nil && decision.ShouldEscalate {
				return decision, nil
			}
		}
	}

	// Step 5: Check tenant/inbox mode (human mode override)
	if e.isHumanModeEnabled(ctx, msg.TenantID, msg.Platform) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "tenant_mode_human",
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 6: Check for critical events (payment confirmation, refund demand)
	events := ai.ExtractEvents(msg.Content)
	if decision := ai.DecideEscalationFromEvents(events); decision.Handled && decision.ShouldEscalate {
		// Determine type based on reason
		escalationType := EscalationTypeHumanTakeover
		if strings.Contains(decision.Reason, "payment_mode") || strings.Contains(decision.Reason, "approval") {
			escalationType = EscalationTypePricingApproval
		}

		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           escalationType,
			Reason:         decision.Reason,
			PauseAI:        escalationType == EscalationTypePricingApproval,
			DisableAI:      escalationType == EscalationTypeHumanTakeover,
		}, nil
	}

	// Step 7: Check sales conversation stage (needs_human, payment_confirmation, refund)
	if salesConv != nil {
		stage := strings.ToLower(strings.TrimSpace(salesConv.Stage))
		switch stage {
		// case "needs_human":
		// 	// User requested to remove generic escalation based on stage.
		// 	// Only escalate on specific triggers (complaint, refund, payment, negotiation).
		// 	return &EscalationDecision{
		// 		ShouldEscalate: true,
		// 		Type:           EscalationTypeHumanTakeover,
		// 		Reason:         "conversation_in_needs_human_stage",
		// 		PauseAI:        false,
		// 		DisableAI:      true,
		// 	}, nil
		case "payment_confirmation":
			// Fix: Don't escalate ordinary greetings/casual messages even in payment mode
			// Let the AI handle them naturally ("Hi, currently verifying your payment...")
			if intent != nil && (intent.Intent == "greeting" || intent.Intent == "casual") {
				break // Skip escalation, let AI reply
			}
			return &EscalationDecision{
				ShouldEscalate: true,
				Type:           EscalationTypeHumanTakeover,
				Reason:         "payment_confirmation_requires_tenant_verification",
				PauseAI:        false,
				DisableAI:      true,
			}, nil
		case "refund":
			return &EscalationDecision{
				ShouldEscalate: true,
				Type:           EscalationTypeHumanTakeover,
				Reason:         "refund_requires_human_judgment",
				PauseAI:        false,
				DisableAI:      true,
			}, nil
		}
	}

	// Step 8: Check for payment confirmation (customer confirms they paid)
	if e.isPaymentConfirmation(msg.Content) && intent.Confidence > 0.35 {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "payment_confirmation_requires_tenant_verification",
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 9: Check for repeated frustration (3+ negative messages in 24h)
	if conversationID != uuid.Nil {
		if e.isRepeatedFrustration(ctx, conversationID) {
			return &EscalationDecision{
				ShouldEscalate: true,
				Type:           EscalationTypeHumanTakeover,
				Reason:         "repeated_frustration",
				PauseAI:        false,
				DisableAI:      true,
			}, nil
		}

		// Step 10: Check for persistent negotiation (same offer 3+ times with AI persuasion)
		if salesConv != nil {
			if e.isPersistentNegotiation(salesConv, msg.Content) {
				return &EscalationDecision{
					ShouldEscalate: true,
					Type:           EscalationTypePricingApproval,
					Reason:         "persistent_negotiation_persistent_offer",
					PauseAI:        true,
					DisableAI:      false,
				}, nil
			}
		}

		// Step 11: Check for follow-up after previous escalation
		if salesConv != nil {
			if e.hasPreviousEscalation(ctx, salesConv.ID) && (intent.Tone == "negative" || intent.Tone == "urgent") {
				return &EscalationDecision{
					ShouldEscalate: true,
					Type:           EscalationTypeHumanTakeover,
					Reason:         "follow_up_after_previous_escalation",
					PauseAI:        false,
					DisableAI:      true,
				}, nil
			}
		}
	}

	// Step 12: Check for critical intent with high confidence (delivery_issue)
	if intent.Intent == "delivery_issue" && intent.Confidence > 0.85 && (intent.Tone == "negative" || intent.Tone == "urgent") {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         fmt.Sprintf("high_confidence_critical_intent_%s_%s", intent.Intent, intent.Tone),
			PauseAI:        false,
			DisableAI:      true,
		}, nil
	}

	// Step 13: Check for media messages (audio, image, video, document, sticker)
	if e.isMediaMessage(msg.MessageType) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypeHumanTakeover,
			Reason:         "media_message_requires_review",
			PauseAI:        false,
			DisableAI:      false, // Can be re-enabled after review
		}, nil
	}

	// Step 14: Check for payment mode confirmation (POD selection)
	if e.isPaymentModeConfirmation(msg.Content) {
		return &EscalationDecision{
			ShouldEscalate: true,
			Type:           EscalationTypePricingApproval,
			Reason:         "payment_mode_confirmation_needed",
			PauseAI:        true,
			DisableAI:      false,
		}, nil
	}

	// No escalation needed
	return &EscalationDecision{
		ShouldEscalate: false,
	}, nil
}

// evaluatePricingApproval evaluates if pricing approval is needed
// This is separate from human takeover - it's about negotiation/offers
func (e *EscalationService) evaluatePricingApproval(
	ctx context.Context,
	msg IncomingMessage,
	salesConv *storage.SalesConversation,
	offer float64,
) (*EscalationDecision, error) {
	// Use NegotiationManager if available (type assertion)
	type NegotiationManager interface {
		CheckEscalationEligibility(ctx context.Context, salesConvID uuid.UUID, productID uuid.UUID, offer float64, buyingIntent bool, proximity float64, maxConcessions int, minPrice float64) (bool, string, error)
		ExecuteEscalation(ctx context.Context, tenantID uuid.UUID, salesConvID uuid.UUID, productID uuid.UUID, discussedPrice float64, minPrice float64, offers []float64, metadata map[string]interface{}, reason string) (*storage.EscalationEvent, error)
	}

	if negotiationMgr, ok := e.NegotiationMgr.(NegotiationManager); ok {
		// Extract product ID from ItemsDiscussed (JSON field)
		// For now, use uuid.Nil if not available (negotiation manager can handle this)
		var productID uuid.UUID = uuid.Nil

		// Check buying intent
		buyingIntent := e.hasBuyingIntent(msg.Content)

		// Get proximity threshold
		proximity := 0.10 // Default
		var policy storage.TenantPolicy
		if err := e.DB.Where("tenant_id = ?", msg.TenantID).First(&policy).Error; err == nil {
			if policy.NegotiationProximityPct > 0 {
				proximity = policy.NegotiationProximityPct
			}
		}

		// Check escalation eligibility
		eligible, reason, err := negotiationMgr.CheckEscalationEligibility(
			ctx, salesConv.ID, productID, offer, buyingIntent, proximity, 3, salesConv.PriceDiscussed*0.8,
		)
		if err != nil {
			return nil, err
		}

		if eligible {
			// Execute escalation (creates escalation event)
			if _, err := negotiationMgr.ExecuteEscalation(
				ctx, msg.TenantID, salesConv.ID, productID, salesConv.PriceDiscussed,
				salesConv.PriceDiscussed*0.8, []float64{offer}, nil, reason,
			); err != nil {
				e.Logger.Error("failed to execute pricing escalation", zap.Error(err))
				return nil, err
			}

			return &EscalationDecision{
				ShouldEscalate: true,
				Type:           EscalationTypePricingApproval,
				Reason:         reason,
				PauseAI:        true, // Pause AI until tenant responds
				DisableAI:      false,
			}, nil
		}
	}

	// Fallback: simple proximity check
	if salesConv.PriceDiscussed > 0 {
		proximity := 0.10
		var policy storage.TenantPolicy
		if err := e.DB.Where("tenant_id = ?", msg.TenantID).First(&policy).Error; err == nil {
			if policy.NegotiationProximityPct > 0 {
				proximity = policy.NegotiationProximityPct
			}
		}

		if offer >= salesConv.PriceDiscussed*(1.0-proximity) {
			return &EscalationDecision{
				ShouldEscalate: true,
				Type:           EscalationTypePricingApproval,
				Reason:         "offer_in_range_for_approval",
				PauseAI:        true,
				DisableAI:      false,
			}, nil
		}
	}

	return nil, nil
}

// isExplicitHumanRequest checks if customer explicitly requested human
func (e *EscalationService) isExplicitHumanRequest(content string) bool {
	contentLower := strings.ToLower(content)
	humanRequestPhrases := []string{
		"speak to a human", "talk to a person", "human agent", "real person",
		"customer service", "support agent", "let me talk to", "connect me to",
		"i want to speak", "i need a human", "get me a person",
	}

	for _, phrase := range humanRequestPhrases {
		if strings.Contains(contentLower, phrase) {
			return true
		}
	}

	return false
}

// isAbuseOrLegalRisk checks for abuse or legal risk indicators
func (e *EscalationService) isAbuseOrLegalRisk(content string, intent *ai.DetectedIntent) bool {
	contentLower := strings.ToLower(content)

	// Legal threats
	legalThreats := []string{"lawyer", "sue", "lawsuit", "legal action", "court", "attorney"}
	for _, threat := range legalThreats {
		if strings.Contains(contentLower, threat) {
			return true
		}
	}

	// Abuse indicators
	if intent != nil && intent.Tone == "negative" && intent.Confidence > 0.8 {
		abusePhrases := []string{"stupid", "idiot", "useless", "terrible service", "worst"}
		for _, phrase := range abusePhrases {
			if strings.Contains(contentLower, phrase) {
				return true
			}
		}
	}

	return false
}

// isRepeatedConfusion checks if customer has sent 3+ unclear messages
// This should ONLY trigger on actual confusion (low confidence, unrecognized intent), NOT casual greetings
func (e *EscalationService) isRepeatedConfusion(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, conversationID uuid.UUID) bool {
	// Count truly unclear messages in last 10 interactions
	// Only count messages with:
	// 1. Very low confidence (< 0.3) - AI is genuinely confused
	// 2. Unrecognized/unknown intent - not casual, inquiry, order, etc.
	var unclearCount int64
	err := e.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND customer_id = ? AND conversation_id = ? AND direction = ?",
			tenantID, customerID, conversationID, "incoming").
		Where("(confidence < ? OR detected_intent = ? OR detected_intent = ?)",
			0.3, "unknown", "unrecognized").
		Order("created_at DESC").
		Limit(10).
		Count(&unclearCount).Error

	if err != nil {
		return false
	}

	// Require 5+ truly unclear messages (not just casual greetings)
	return unclearCount >= 5
}

// extractOffer extracts monetary offer from message
func (e *EscalationService) extractOffer(content string) float64 {
	// Use AI's offer detection
	return ai.DetectOfferInText(content)
}

// hasBuyingIntent checks if message indicates buying intent
func (e *EscalationService) hasBuyingIntent(content string) bool {
	contentLower := strings.ToLower(content)
	buyingPhrases := []string{
		"i'll pay", "i will pay", "i can pay", "i want to buy", "i'll buy",
		"i will buy", "buy now", "i'm buying", "cash", "payment",
	}

	for _, phrase := range buyingPhrases {
		if strings.Contains(contentLower, phrase) {
			return true
		}
	}

	return false
}

// isHumanModeEnabled checks if tenant/inbox has human mode enabled
func (e *EscalationService) isHumanModeEnabled(ctx context.Context, tenantID uuid.UUID, platform string) bool {
	// Check per-inbox/connected account ai_mode first
	var acc storage.SocialAccount
	if err := e.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, platform, true).First(&acc).Error; err == nil {
		mode := strings.ToLower(strings.TrimSpace(acc.AiMode))
		if mode == "human" {
			return true
		}
	}

	// Fallback to tenant-wide ai_human_mode
	var tenant storage.Tenant
	if err := e.DB.Select("id,ai_human_mode").First(&tenant, tenantID).Error; err == nil {
		mode := strings.ToLower(strings.TrimSpace(tenant.AIHumanMode))
		if mode == "human" {
			return true
		}
	}

	return false
}

// isPaymentConfirmation checks if message confirms payment
func (e *EscalationService) isPaymentConfirmation(content string) bool {
	contentLower := strings.ToLower(content)
	confirmationPhrases := []string{
		"i have paid", "i paid", "payment sent", "i sent", "i transferred",
		"payment done", "paid already", "already paid", "payment made",
		"money sent", "funds transferred", "payment completed",
	}

	for _, phrase := range confirmationPhrases {
		if strings.Contains(contentLower, phrase) {
			return true
		}
	}

	return false
}

// isRepeatedFrustration checks if customer has sent 3+ negative messages in 24h
func (e *EscalationService) isRepeatedFrustration(ctx context.Context, conversationID uuid.UUID) bool {
	var negativeCount int64
	err := e.DB.Model(&storage.Interaction{}).
		Where("conversation_id = ? AND direction = ? AND created_at > ?",
			conversationID, "incoming", time.Now().Add(-24*time.Hour)).
		Where("(detected_tone = ? OR detected_tone = ?)", "negative", "urgent").
		Count(&negativeCount).Error

	if err != nil {
		return false
	}

	return negativeCount >= 3
}

// isPersistentNegotiation checks if negotiation is persistent (same offer 3+ times with AI persuasion)
func (e *EscalationService) isPersistentNegotiation(salesConv *storage.SalesConversation, content string) bool {
	if salesConv.SameOfferCount >= 3 && salesConv.AIPersuasionCount >= 3 {
		if salesConv.PriceDiscussed > 0 {
			proximity := math.Abs(salesConv.LastOfferAmount-salesConv.PriceDiscussed) / salesConv.PriceDiscussed
			if proximity <= 0.10 {
				contentLower := strings.ToLower(content)
				buyingCues := []string{"cash", "pay", "now", "last", "final"}
				for _, cue := range buyingCues {
					if strings.Contains(contentLower, cue) {
						return true
					}
				}
			}
		}
	}
	return false
}

// hasPreviousEscalation checks if there was a previous escalation in last 7 days
func (e *EscalationService) hasPreviousEscalation(ctx context.Context, salesConvID uuid.UUID) bool {
	var evt storage.EscalationEvent
	if err := e.DB.Where("sales_conversation_id = ? AND created_at > ?",
		salesConvID, time.Now().Add(-7*24*time.Hour)).First(&evt).Error; err == nil {
		return true
	}

	var ns storage.NegotiationState
	if err := e.DB.Where("sales_conversation_id = ? AND status = ? AND updated_at > ?",
		salesConvID, "escalated", time.Now().Add(-7*24*time.Hour)).First(&ns).Error; err == nil {
		return true
	}

	return false
}

// isMediaMessage checks if message is a media type
func (e *EscalationService) isMediaMessage(messageType string) bool {
	return messageType == "audio" || messageType == "image" || messageType == "video" ||
		messageType == "document" || messageType == "sticker"
}

// isPaymentModeConfirmation checks if customer is confirming payment mode (POD)
func (e *EscalationService) isPaymentModeConfirmation(content string) bool {
	contentLower := strings.ToLower(content)
	paymentModePOD := strings.Contains(contentLower, "pay on delivery") ||
		strings.Contains(contentLower, "cash on delivery") ||
		strings.Contains(contentLower, "pod")

	if !paymentModePOD {
		return false
	}

	// Only escalate if it's a selection/commitment, not a question
	isInquiry := strings.Contains(contentLower, "do you") ||
		strings.Contains(contentLower, "can i") ||
		strings.Contains(contentLower, "can we") ||
		strings.Contains(contentLower, "is it") ||
		strings.Contains(contentLower, "is there") ||
		strings.Contains(contentLower, "available") ||
		strings.Contains(contentLower, "possible") ||
		strings.Contains(contentLower, "how") ||
		strings.Contains(contentLower, "?")

	return !isInquiry
}
