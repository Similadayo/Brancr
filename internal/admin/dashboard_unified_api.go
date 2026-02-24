package admin

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// UnifiedDashboardResponse represents the complete dashboard data with all 7 sections
// Sections are conditionally included based on role permissions
type UnifiedDashboardResponse struct {
	SystemHealth        *SystemHealthData        `json:"system_health,omitempty"`
	AIBehavior          *AIBehaviorData          `json:"ai_behavior,omitempty"`
	TenantOverview      *TenantOverviewData      `json:"tenant_overview,omitempty"`
	MessagingActivity   *MessagingActivityData   `json:"messaging_activity,omitempty"`
	PostingStatus       *PostingStatusData       `json:"posting_status,omitempty"`
	CostControl         *CostControlData         `json:"cost_control,omitempty"`
	QualitativeInsights *QualitativeInsightsData `json:"qualitative_insights,omitempty"`
}

// UnifiedDashboardAPIHandler returns the unified dashboard with all sections based on role
// GET /admin/api/dashboard/v2?period=24h|7d
func UnifiedDashboardAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := AdminRoleFromRequest(r)
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}
		if period != "24h" && period != "7d" {
			period = "24h"
		}

		response := UnifiedDashboardResponse{}

		// Create context with timeout for all database operations
		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}

		// Section 1: System & Platform Health (Founder, PM)
		if hasSectionAccess(role, "system_health") {
			systemHealth, err := BuildSystemHealthData(ctx, period, nil, nil, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build system health data", zap.Error(err))
			} else {
				response.SystemHealth = systemHealth
			}
		}

		// Section 2: AI Behavior & Trust Signals (Founder, Growth/CS)
		if hasSectionAccess(role, "ai_behavior") {
			aiBehavior, err := BuildAIBehaviorData(ctx, period, nil, nil, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build AI behavior data", zap.Error(err))
			} else {
				response.AIBehavior = aiBehavior
			}
		}

		// Section 3: Tenant & Onboarding Overview (All roles)
		if hasSectionAccess(role, "tenant_overview") {
			// Fetches top 10 for dashboard overview
			tenantOverview, err := BuildTenantOverviewData(ctx, map[string]interface{}{}, 10, 0, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build tenant overview data", zap.Error(err))
			} else {
				response.TenantOverview = tenantOverview
			}
		}

		// Section 4: Messaging Activity (Founder, Growth/CS)
		if hasSectionAccess(role, "messaging_activity") {
			messagingActivity, err := BuildMessagingActivityData(ctx, period, nil, nil, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build messaging activity data", zap.Error(err))
			} else {
				response.MessagingActivity = messagingActivity
			}
		}

		// Section 5: Posting Status (Founder, PM)
		if hasSectionAccess(role, "posting_status") {
			postingStatus, err := BuildPostingStatusData(ctx, period, nil, nil, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build posting status data", zap.Error(err))
			} else {
				response.PostingStatus = postingStatus
			}
		}

		// Section 6: Cost Control (Founder only - with filtering for others)
		if hasSectionAccess(role, "cost_control") {
			costData, err := BuildCostControlData(ctx, period, nil, nil, role, reqCtx)
			if err != nil {
				ctx.Logger.Error("failed to build cost control data", zap.Error(err))
				// Continue without this section
			} else {
				// Filter cost data for non-founder roles
				response.CostControl = FilterCostDataForRole(costData, role)
			}
		}

		// Section 7: Qualitative Insights (All roles with access)
		if hasSectionAccess(role, "qualitative_insights") {
			qualitativeInsights, err := BuildQualitativeInsightsData(ctx, period, nil, nil, reqCtx)
			if err != nil {
				ctx.Logger.Warn("failed to build qualitative insights data", zap.Error(err))
			} else {
				response.QualitativeInsights = qualitativeInsights
			}
		}

		respondJSON(w, response)
	}
}
