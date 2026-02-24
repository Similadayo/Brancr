package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// SocialAccountsResponse represents social accounts list response
type SocialAccountsResponse struct {
	Total int64                 `json:"total"`
	Items []SocialAccountDetail `json:"items"`
}

// SocialAccountDetail represents detailed social account information
type SocialAccountDetail struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	TenantName  string     `json:"tenant_name"`
	Platform    string     `json:"platform"` // instagram, facebook, whatsapp (Gupshup), tiktok, youtube
	Username    string     `json:"username"`
	Connected   bool       `json:"connected"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ExpiresSoon bool       `json:"expires_soon"` // Expires within 72 hours
	Mode        string     `json:"mode"`         // oauth2, gupshup, etc.
	AiMode      string     `json:"ai_mode"`      // ai, human
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SocialAccountsAPIHandler returns all social accounts (including WhatsApp via Gupshup)
func SocialAccountsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get filters
		platform := strings.TrimSpace(r.URL.Query().Get("platform"))
		connectedOnlyStr := r.URL.Query().Get("connected")
		connectedOnly := connectedOnlyStr == "true"
		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))

		// Get pagination
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
				limit = v
			}
		}
		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				offset = v
			}
		}

		data, err := BuildSocialAccountsData(ctx, platform, connectedOnly, tenantIDStr, limit, offset, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build social accounts data", zap.Error(err))
			http.Error(w, "failed to build social accounts data", http.StatusInternalServerError)
			return
		}
		if err != nil {
			ctx.Logger.Error("failed to build social accounts data", zap.Error(err))
			http.Error(w, "failed to build social accounts data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode social accounts data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildSocialAccountsData builds social accounts list with tenant information
func BuildSocialAccountsData(ctx *AdminContext, platform string, connectedOnly bool, tenantIDStr string, limit, offset int, reqCtx context.Context) (*SocialAccountsResponse, error) {
	query := ctx.DB.WithContext(reqCtx).Model(&storage.SocialAccount{})

	// Apply filters
	if platform != "" {
		query = query.Where("platform = ?", strings.ToLower(platform))
	}
	if connectedOnly {
		query = query.Where("connected = ?", true)
	}
	if tenantIDStr != "" {
		query = query.Where("tenant_id = ?", tenantIDStr)
	}

	// Count total
	var total int64
	query.Count(&total)

	// Get accounts
	var accounts []storage.SocialAccount
	if err := query.Order("tenant_id ASC, platform ASC, created_at DESC").
		Limit(500).
		Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("failed to query social accounts: %w", err)
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, acc := range accounts {
		if _, ok := tenantMap[acc.TenantID]; !ok {
			tenantIDs = append(tenantIDs, acc.TenantID)
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	// Check for expiring tokens (within 72 hours)
	threshold := time.Now().Add(72 * time.Hour)

	items := make([]SocialAccountDetail, 0, len(accounts))
	for _, acc := range accounts {
		expiresSoon := false
		if acc.ExpiresAt != nil && acc.ExpiresAt.Before(threshold) {
			expiresSoon = true
		}

		items = append(items, SocialAccountDetail{
			ID:          acc.ID,
			TenantID:    acc.TenantID,
			TenantName:  tenantMap[acc.TenantID],
			Platform:    acc.Platform,
			Username:    acc.Username,
			Connected:   acc.Connected,
			ExpiresAt:   acc.ExpiresAt,
			ExpiresSoon: expiresSoon,
			Mode:        acc.Mode,
			AiMode:      acc.AiMode,
			CreatedAt:   acc.CreatedAt,
		})
	}

	return &SocialAccountsResponse{
		Total: total,
		Items: items,
	}, nil
}
