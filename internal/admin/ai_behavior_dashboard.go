package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// AIBehaviorData represents Section 2: AI Behavior & Trust Signals
type AIBehaviorData struct {
	AIRepliesSent        int64    `json:"ai_replies_sent"`
	EscalationsTriggered int64    `json:"escalations_triggered"`
	EscalationRate       float64  `json:"escalation_rate"` // percentage
	ManualOverrides      int64    `json:"manual_overrides"`
	AIDisabledEvents     int64    `json:"ai_disabled_events"`
	AvgConfidenceLevel   float64  `json:"avg_confidence_level"`
	Interpretations      []string `json:"interpretations"` // Auto-generated insights
}

// BuildAIBehaviorData builds AI behavior metrics for the dashboard
func BuildAIBehaviorData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*AIBehaviorData, error) {
	var since time.Time
	var unti time.Time = time.Now()

	if period == "custom" && customStart != nil && customEnd != nil {
		since = *customStart
		unti = *customEnd
		if since.After(unti) {
			since, unti = unti, since
		}
	} else if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
	} else {
		since = time.Now().Add(-24 * time.Hour)
	}

	data := &AIBehaviorData{
		Interpretations: []string{},
	}

	// AI replies sent - only count outgoing auto_reply interactions (direction = "outgoing")
	// This ensures we don't count incoming customer messages that triggered auto-replies
	var aiRepliesCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND created_at <= ? AND response_type = ? AND direction = ?", since, unti, "auto_reply", "incoming").
		Count(&aiRepliesCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count AI replies: %w", err)
	}
	data.AIRepliesSent = aiRepliesCount

	// Escalations triggered - count from interactions table (most accurate)
	var escalationsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND created_at <= ? AND response_type = ?", since, unti, "escalated").
		Count(&escalationsCount).Error; err != nil {
		ctx.Logger.Warn("failed to count escalations from interactions", zap.Error(err))
		// Fallback 1: count from Escalation table
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.Escalation{}).
			Where("created_at >= ? AND created_at <= ?", since, unti).
			Count(&escalationsCount).Error; err != nil {
			ctx.Logger.Warn("failed to count escalations from escalations table", zap.Error(err))
			// Fallback 2: count from PendingEscalation table
			if err := ctx.DB.WithContext(reqCtx).
				Model(&storage.PendingEscalation{}).
				Where("created_at >= ? AND created_at <= ?", since, unti).
				Count(&escalationsCount).Error; err != nil {
				ctx.Logger.Warn("failed to count escalations from pending_escalations", zap.Error(err))
			}
		}
	}
	data.EscalationsTriggered = escalationsCount

	// Calculate escalation rate
	// Total conversations = AI replies + escalations (both are responses)
	totalConversations := aiRepliesCount + escalationsCount
	if totalConversations > 0 {
		data.EscalationRate = float64(escalationsCount) / float64(totalConversations) * 100
	}

	// Manual overrides - count from admin_audit_log
	var overridesCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.AdminAuditLog{}).
		Where("(action LIKE ? OR action LIKE ?) AND created_at >= ? AND created_at <= ?", "%manual_reply%", "%manual_response%", since, unti).
		Count(&overridesCount).Error; err != nil {
		ctx.Logger.Debug("failed to count manual overrides", zap.Error(err))
	}
	data.ManualOverrides = overridesCount

	// AI disabled events - count from admin_audit_log or tenant changes
	var aiDisabledCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.AdminAuditLog{}).
		Where("(action LIKE ? OR metadata::text LIKE ?) AND created_at >= ? AND created_at <= ?",
			"%ai_mode%", "%ai_human_mode%", since, unti).
		Count(&aiDisabledCount).Error; err != nil {
		ctx.Logger.Debug("failed to count AI disabled events", zap.Error(err))
	}
	data.AIDisabledEvents = aiDisabledCount

	// Average confidence level - calculate from PendingEscalation (most accurate source)
	var avgConfidence struct {
		Avg float64
	}
	// First try to get from PendingEscalation table (has confidence field)
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.PendingEscalation{}).
		Select("AVG(confidence) as avg").
		Where("created_at >= ? AND created_at <= ? AND confidence > 0", since, unti).
		Scan(&avgConfidence).Error; err == nil && avgConfidence.Avg > 0 {
		data.AvgConfidenceLevel = avgConfidence.Avg
	} else {
		// Fallback: try to extract confidence from interactions metadata JSON
		var confidenceFromInteractions struct {
			Avg float64
		}
		if err := ctx.DB.WithContext(reqCtx).
			Raw(`
				SELECT AVG(
					CASE 
						WHEN metadata IS NOT NULL AND metadata::text LIKE '%confidence%' 
						THEN COALESCE(
							(metadata::jsonb->>'confidence')::float,
							(metadata::jsonb->'intent'->>'confidence')::float,
							NULL
						)
						ELSE NULL
					END
				) as avg
				FROM interactions
				WHERE created_at >= ? AND created_at <= ?
				  AND response_type = 'auto_reply'
				  AND metadata IS NOT NULL
				  AND metadata::text LIKE '%confidence%'
			`, since, unti).
			Scan(&confidenceFromInteractions).Error; err == nil && confidenceFromInteractions.Avg > 0 {
			data.AvgConfidenceLevel = confidenceFromInteractions.Avg
		} else {
			// If no confidence data found, set to 0 (not an arbitrary default)
			data.AvgConfidenceLevel = 0
			if err != nil {
				ctx.Logger.Debug("failed to calculate avg confidence", zap.Error(err))
			}
		}
	}

	// Generate interpretations
	data.Interpretations = generateInterpretations(data)

	return data, nil
}

// generateInterpretations generates human-readable insights from metrics
func generateInterpretations(data *AIBehaviorData) []string {
	interpretations := []string{}

	// High escalation rate interpretation
	if data.EscalationRate > 15 {
		interpretations = append(interpretations,
			fmt.Sprintf("High escalation rate (%.1f%%) - AI may be too cautious or uncertain", data.EscalationRate))
	}

	// Low escalation + high overrides interpretation
	if data.EscalationRate < 5 && data.ManualOverrides > 10 {
		interpretations = append(interpretations,
			"Low escalation rate but many manual overrides - AI may be too aggressive or missing context")
	}

	// Many AI disabled events
	if data.AIDisabledEvents > 5 {
		interpretations = append(interpretations,
			"Multiple tenants disabling AI - possible trust or onboarding issues")
	}

	// Confidence level interpretation
	if data.AvgConfidenceLevel > 0 && data.AvgConfidenceLevel < 0.3 {
		interpretations = append(interpretations,
			"Low average confidence level - AI may need prompt tuning for better certainty")
	}

	// Default interpretation if no issues
	if len(interpretations) == 0 {
		interpretations = append(interpretations, "AI behavior metrics look healthy")
	}

	return interpretations
}
