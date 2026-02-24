package admin

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// QualitativeInsightsData represents Section 7: Qualitative Insights & Risk Flags
type QualitativeInsightsData struct {
	TopComplaints      []InsightItem `json:"top_complaints"`
	TopAhaMoments      []InsightItem `json:"top_aha_moments"`
	RepeatedObjections []InsightItem `json:"repeated_objections"`
	RiskyEscalations   []InsightItem `json:"risky_escalations"`
	OnboardingNotes    []InsightItem `json:"onboarding_notes"`
}

// InsightItem represents a single qualitative insight
type InsightItem struct {
	ID             uuid.UUID  `json:"id"`
	Content        string     `json:"content"`
	TenantID       *uuid.UUID `json:"tenant_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	AdminEmail     string     `json:"admin_email"`
	Tags           []string   `json:"tags"`
	CreatedAt      time.Time  `json:"created_at"`
}

// BuildQualitativeInsightsData builds qualitative insights for the dashboard
func BuildQualitativeInsightsData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*QualitativeInsightsData, error) {
	insightsService := NewInsightsService(ctx.DB, ctx.Logger)

	data := &QualitativeInsightsData{
		TopComplaints:      []InsightItem{},
		TopAhaMoments:      []InsightItem{},
		RepeatedObjections: []InsightItem{},
		RiskyEscalations:   []InsightItem{},
		OnboardingNotes:    []InsightItem{},
	}

	// Get top complaints
	complaints, err := insightsService.GetInsightsByType(reqCtx, "complaint", 5)
	if err == nil {
		data.TopComplaints = convertToInsightItems(complaints)
	}

	// Get top aha moments
	ahaMoments, err := insightsService.GetInsightsByType(reqCtx, "aha_moment", 5)
	if err == nil {
		data.TopAhaMoments = convertToInsightItems(ahaMoments)
	}

	// Get repeated objections
	objections, err := insightsService.GetInsightsByType(reqCtx, "objection", 5)
	if err == nil {
		data.RepeatedObjections = convertToInsightItems(objections)
	}

	// Get risky escalations
	riskyEscalations, err := insightsService.GetInsightsByType(reqCtx, "risk_flag", 5)
	if err == nil {
		data.RiskyEscalations = convertToInsightItems(riskyEscalations)
	}

	// Get onboarding notes
	onboardingNotes, err := insightsService.GetInsightsByType(reqCtx, "onboarding_note", 5)
	if err == nil {
		data.OnboardingNotes = convertToInsightItems(onboardingNotes)
	}

	return data, nil
}

// convertToInsightItems converts QualitativeInsight to InsightItem
func convertToInsightItems(insights []QualitativeInsight) []InsightItem {
	items := make([]InsightItem, 0, len(insights))
	for _, insight := range insights {
		items = append(items, InsightItem{
			ID:             insight.ID,
			Content:        insight.Content,
			TenantID:       insight.TenantID,
			ConversationID: insight.ConversationID,
			AdminEmail:     insight.AdminEmail,
			Tags:           insight.Tags,
			CreatedAt:      insight.CreatedAt,
		})
	}
	return items
}
