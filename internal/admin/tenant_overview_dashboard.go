package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// TenantOverviewData represents Section 3: Tenant & Onboarding Overview
type TenantOverviewData struct {
	Tenants []TenantOverviewItem  `json:"tenants"`
	Summary TenantOverviewSummary `json:"summary"`
}

// TenantOverviewItem represents a single tenant in the overview
type TenantOverviewItem struct {
	ID                  uuid.UUID  `json:"id"`
	Name                string     `json:"name"`
	Industry            string     `json:"industry"`
	PlatformsConnected  []string   `json:"platforms_connected"`
	OnboardingStatus    string     `json:"onboarding_status"`
	TimeToFirstValue    string     `json:"time_to_first_value"` // "2h", "1d", etc.
	AIEnabled           bool       `json:"ai_enabled"`
	LastActivity        *time.Time `json:"last_activity,omitempty"`
	IndustryChangeCount int        `json:"industry_change_count"`
	AdminTags           []string   `json:"admin_tags"`
}

// TenantOverviewSummary provides aggregated statistics
type TenantOverviewSummary struct {
	TotalTenants       int `json:"total_tenants"`
	ActiveTenants      int `json:"active_tenants"`
	OnboardingComplete int `json:"onboarding_complete"`
	AIEnabledCount     int `json:"ai_enabled_count"`
	NeedsFollowup      int `json:"needs_followup"`
}

// BuildTenantOverviewData builds tenant overview for the dashboard
func BuildTenantOverviewData(ctx *AdminContext, filters map[string]interface{}, limit, offset int, reqCtx context.Context) (*TenantOverviewData, error) {
	data := &TenantOverviewData{
		Tenants: []TenantOverviewItem{},
		Summary: TenantOverviewSummary{},
	}

	// Base query
	query := ctx.DB.WithContext(reqCtx).Model(&storage.Tenant{})

	// Apply filters
	if tags, ok := filters["tags"].([]string); ok && len(tags) > 0 {
		for _, tag := range tags {
			query = query.Where("? = ANY(admin_tags)", tag)
		}
	}

	// 1. Get Summary Stats (Global, not paginated)
	// Total
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count tenants: %w", err)
	}
	data.Summary.TotalTenants = int(total)

	// Active (Last 7d interaction)
	// This is heavier, might want to optimize or cache, but staying consistent with previous logic:
	var activeCount int64
	ctx.DB.WithContext(reqCtx).Model(&storage.Interaction{}).
		Distinct("tenant_id").
		Where("created_at >= ?", time.Now().Add(-7*24*time.Hour)).
		Count(&activeCount)
	data.Summary.ActiveTenants = int(activeCount)

	// Onboarding Complete
	var obComplete int64
	query.Where("onboarding_complete = ?", true).Count(&obComplete)
	data.Summary.OnboardingComplete = int(obComplete)

	// AI Enabled
	var aiEnabled int64
	query.Where("ai_human_mode = ?", "ai").Count(&aiEnabled)
	data.Summary.AIEnabledCount = int(aiEnabled)

	// Needs Followup
	var needsFollowup int64
	query.Where("'needs_followup' = ANY(admin_tags)").Count(&needsFollowup)
	data.Summary.NeedsFollowup = int(needsFollowup)

	// 2. Get Paginated Tenants
	var tenants []storage.Tenant
	if err := query.Limit(limit).Offset(offset).Order("created_at DESC").Find(&tenants).Error; err != nil {
		return nil, fmt.Errorf("failed to get tenants: %w", err)
	}

	// Build tenant items
	for _, tenant := range tenants {
		item, err := buildTenantOverviewItem(ctx, tenant, reqCtx)
		if err != nil {
			ctx.Logger.Warn("failed to build tenant overview item", zap.Stringer("tenant_id", tenant.ID), zap.Error(err))
			continue
		}
		data.Tenants = append(data.Tenants, *item)
	}

	return data, nil
}

// buildTenantOverviewItem builds an overview item for a single tenant
func buildTenantOverviewItem(ctx *AdminContext, tenant storage.Tenant, reqCtx context.Context) (*TenantOverviewItem, error) {
	item := &TenantOverviewItem{
		ID:               tenant.ID,
		Name:             tenant.Name,
		OnboardingStatus: determineOnboardingStatus(tenant),
		AIEnabled:        tenant.AIHumanMode == "ai",
		AdminTags:        []string{},
	}

	// Get industry name
	var industry storage.Industry
	if err := ctx.DB.WithContext(reqCtx).
		Where("id = ?", tenant.IndustryID).
		First(&industry).Error; err == nil {
		item.Industry = industry.Name
	}

	// Get connected platforms
	var socialAccounts []storage.SocialAccount
	if err := ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ?", tenant.ID).
		Find(&socialAccounts).Error; err == nil {
		platforms := make([]string, 0)
		for _, sa := range socialAccounts {
			if sa.Platform != "" {
				platforms = append(platforms, sa.Platform)
			}
		}
		item.PlatformsConnected = platforms
	}

	// Calculate time to first value (signup -> first AI reply)
	timeToFirstValue, err := calculateTimeToFirstValue(ctx, tenant.ID, tenant.CreatedAt, reqCtx)
	if err == nil {
		item.TimeToFirstValue = formatTimeDuration(timeToFirstValue)
	}

	// Get last activity
	var lastInteraction storage.Interaction
	if err := ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ?", tenant.ID).
		Order("created_at DESC").
		First(&lastInteraction).Error; err == nil {
		item.LastActivity = &lastInteraction.CreatedAt
	}

	// Count industry changes from audit log
	var changeCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.AdminAuditLog{}).
		Where("action LIKE ? AND metadata::text LIKE ?", "%industry%", fmt.Sprintf("%%\"tenant_id\":\"%s\"%%", tenant.ID)).
		Count(&changeCount).Error; err == nil {
		item.IndustryChangeCount = int(changeCount)
	}

	// Get admin tags
	if tenant.AdminTags != nil {
		item.AdminTags = tenant.AdminTags
	}

	return item, nil
}

// determineOnboardingStatus determines onboarding status from tenant
func determineOnboardingStatus(tenant storage.Tenant) string {
	if tenant.OnboardingComplete {
		return "activated"
	}
	if tenant.OnboardingStep == "" || tenant.OnboardingStep == "business_profile" {
		return "not_started"
	}
	return "in_progress"
}

// calculateTimeToFirstValue calculates time from signup to first AI reply
func calculateTimeToFirstValue(ctx *AdminContext, tenantID uuid.UUID, signupTime time.Time, reqCtx context.Context) (time.Duration, error) {
	var firstAIReply storage.Interaction
	if err := ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ? AND response_type = ?", tenantID, "auto_reply").
		Order("created_at ASC").
		First(&firstAIReply).Error; err != nil {
		return 0, err
	}

	return firstAIReply.CreatedAt.Sub(signupTime), nil
}

// formatTimeDuration formats duration as "2h", "1d", etc.
func formatTimeDuration(d time.Duration) string {
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	}
	days := hours / 24
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	weeks := days / 7
	return fmt.Sprintf("%dw", weeks)
}

// hasTag checks if a tag exists in the tags array
func hasTag(tags []string, tag string) bool {
	if tags == nil {
		return false
	}
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
