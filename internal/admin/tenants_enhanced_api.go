package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// TenantDetailResponse represents detailed tenant information
type TenantDetailResponse struct {
	Tenant             TenantDetailInfo    `json:"tenant"`
	Quota              interface{}         `json:"quota"` // quota.GetQuotaUsage return type
	SocialAccounts     []SocialAccountInfo `json:"social_accounts"`
	ScheduledPosts     []ScheduledPostInfo `json:"scheduled_posts"`
	PendingEscalations int64               `json:"pending_escalations"`
	Stats              TenantStats         `json:"stats"`
}

// TenantDetailInfo represents tenant basic information
type TenantDetailInfo struct {
	ID                 uuid.UUID  `json:"id"`
	Name               string     `json:"name"`
	Email              string     `json:"email"`
	PlanType           string     `json:"plan_type"`
	Status             string     `json:"status"`
	OnboardingStep     string     `json:"onboarding_step"`
	OnboardingComplete bool       `json:"onboarding_complete"`
	AIHumanMode        string     `json:"ai_human_mode"`
	Industry           string     `json:"industry"`
	AdminTags          []string   `json:"admin_tags"`
	KYBStatus          string     `json:"kyb_status"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
}

// SocialAccountInfo represents social account information
type SocialAccountInfo struct {
	ID        uuid.UUID  `json:"id"`
	Platform  string     `json:"platform"`
	Username  string     `json:"username"`
	Connected bool       `json:"connected"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Mode      string     `json:"mode"`
	AiMode    string     `json:"ai_mode"`
	LastError string     `json:"last_error,omitempty"`
}

// ScheduledPostInfo represents scheduled post information
type ScheduledPostInfo struct {
	ID          uuid.UUID  `json:"id"`
	Platforms   []string   `json:"platforms"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
}

// TenantStats represents tenant activity statistics
type TenantStats struct {
	TotalInteractions int64      `json:"total_interactions"`
	TotalMessages     int64      `json:"total_messages"`
	MessagesLast7Days int64      `json:"messages_last_7_days"`
	EscalationsCount  int64      `json:"escalations_count"`
	FailedSends       int64      `json:"failed_sends"`
	SuccessRate       float64    `json:"success_rate"` // Percentage
	LastActivityAt    *time.Time `json:"last_activity_at,omitempty"`
}

// AdminTenantDetailAPIHandler returns detailed tenant information
func AdminTenantDetailAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantID, err := parseTenantParamFromURL(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		data, err := BuildTenantDetailData(ctx, tenantID, reqCtx)
		if err != nil {
			if strings.Contains(err.Error(), "tenant not found") {
				http.Error(w, "tenant not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to build tenant detail data", zap.Error(err))
			http.Error(w, "failed to build tenant detail", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode tenant detail", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// ... (omitted middle section)

// BuildTenantDetailData builds comprehensive tenant detail data
func BuildTenantDetailData(ctx *AdminContext, tenantID uuid.UUID, reqCtx context.Context) (*TenantDetailResponse, error) {
	// Load tenant
	var tenant storage.Tenant
	if err := ctx.DB.WithContext(reqCtx).First(&tenant, tenantID).Error; err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	// Get industry name
	var industry storage.Industry
	industryName := ""
	if err := ctx.DB.WithContext(reqCtx).Where("id = ?", tenant.IndustryID).First(&industry).Error; err == nil {
		industryName = industry.Name
	}

	// Build tenant info
	tenantInfo := TenantDetailInfo{
		ID:                 tenant.ID,
		Name:               tenant.Name,
		Email:              tenant.Email,
		PlanType:           tenant.PlanType,
		Status:             tenant.Status,
		OnboardingStep:     tenant.OnboardingStep,
		OnboardingComplete: tenant.OnboardingComplete,
		AIHumanMode:        tenant.AIHumanMode,
		Industry:           industryName,
		AdminTags:          tenant.AdminTags,
		KYBStatus:          tenant.KYBStatus,
		CreatedAt:          tenant.CreatedAt,
	}

	// Get quota data
	quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
	quotaData, _ := quotaService.GetQuotaUsage(reqCtx, tenantID)

	// Get social accounts (including WhatsApp from Gupshup)
	var accounts []storage.SocialAccount
	ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ?", tenantID).
		Order("platform ASC, created_at DESC").
		Find(&accounts)

	socialAccounts := make([]SocialAccountInfo, 0, len(accounts))
	for _, acc := range accounts {
		socialAccounts = append(socialAccounts, SocialAccountInfo{
			ID:        acc.ID,
			Platform:  acc.Platform,
			Username:  acc.Username,
			Connected: acc.Connected,
			ExpiresAt: acc.ExpiresAt,
			Mode:      acc.Mode,
			AiMode:    acc.AiMode,
		})
	}

	// Get scheduled posts
	var scheduledPosts []storage.ScheduledPost
	ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ?", tenantID).
		Order("scheduled_at DESC").
		Limit(25).
		Find(&scheduledPosts)

	postInfos := make([]ScheduledPostInfo, 0, len(scheduledPosts))
	for _, post := range scheduledPosts {
		var platforms []string
		if post.Platforms != "" {
			json.Unmarshal([]byte(post.Platforms), &platforms)
		}
		postInfos = append(postInfos, ScheduledPostInfo{
			ID:          post.ID,
			Platforms:   platforms,
			ScheduledAt: post.ScheduledAt,
			Status:      post.Status,
			Error:       post.LastError,
		})
	}

	// Count pending escalations
	var pendingEscalations int64
	ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
		Count(&pendingEscalations)

	// Build stats
	stats, _ := buildTenantStats(ctx, tenantID, reqCtx)

	return &TenantDetailResponse{
		Tenant:             tenantInfo,
		Quota:              quotaData,
		SocialAccounts:     socialAccounts,
		ScheduledPosts:     postInfos,
		PendingEscalations: pendingEscalations,
		Stats:              stats,
	}, nil
}

// buildTenantStats builds tenant activity statistics
func buildTenantStats(ctx *AdminContext, tenantID uuid.UUID, reqCtx context.Context) (TenantStats, error) {
	stats := TenantStats{}

	// Total interactions
	var totalInteractions int64
	ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("tenant_id = ?", tenantID).
		Count(&totalInteractions)
	stats.TotalInteractions = totalInteractions

	// Total messages (interactions with content)
	stats.TotalMessages = totalInteractions // For now, treat as same

	// Messages in last 7 days
	var messagesLast7Days int64
	ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("tenant_id = ? AND created_at >= ?", tenantID, time.Now().Add(-7*24*time.Hour)).
		Count(&messagesLast7Days)
	stats.MessagesLast7Days = messagesLast7Days

	// Escalations count
	var escalationsCount int64
	ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ?", tenantID, "escalated").
		Count(&escalationsCount)
	stats.EscalationsCount = escalationsCount

	// Failed sends
	var failedSends int64
	ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("tenant_id = ? AND direction = ? AND response_status IN ?",
			tenantID, "outbound", []string{"failed", "error"}).
		Count(&failedSends)
	stats.FailedSends = failedSends

	// Calculate success rate
	if totalInteractions > 0 {
		successCount := totalInteractions - failedSends
		stats.SuccessRate = float64(successCount) / float64(totalInteractions) * 100
	} else {
		stats.SuccessRate = 100.0
	}

	// Last activity
	var lastInteraction storage.Interaction
	if err := ctx.DB.WithContext(reqCtx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		First(&lastInteraction).Error; err == nil {
		stats.LastActivityAt = &lastInteraction.CreatedAt
	}

	return stats, nil
}

// parseTenantParamFromURL extracts tenant ID from URL (reuses chi.URLParam)
func parseTenantParamFromURL(r *http.Request) (uuid.UUID, error) {
	idStr := strings.TrimSpace(chi.URLParam(r, "id"))
	if idStr == "" {
		return uuid.Nil, fmt.Errorf("tenant id required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid tenant id")
	}
	return id, nil
}
