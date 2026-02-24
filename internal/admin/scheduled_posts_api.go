package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ScheduledPostsResponse represents scheduled posts list response
type ScheduledPostsResponse struct {
	Total int64                 `json:"total"`
	Items []ScheduledPostDetail `json:"items"`
}

// ScheduledPostDetail represents detailed scheduled post information
type ScheduledPostDetail struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	TenantName  string     `json:"tenant_name"`
	Name        string     `json:"name"`
	Caption     string     `json:"caption"`
	Platforms   []string   `json:"platforms"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	Status      string     `json:"status"` // scheduled, posting, posted, failed, cancelled
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error,omitempty"`
	PostedAt    *time.Time `json:"posted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ScheduledPostsAPIHandler returns all scheduled posts
func ScheduledPostsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get filters
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
		limit := 200
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
				limit = l
			}
		}

		data, err := BuildScheduledPostsData(ctx, status, tenantIDStr, limit, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build scheduled posts data", zap.Error(err))
			http.Error(w, "failed to build scheduled posts data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode scheduled posts data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildScheduledPostsData builds scheduled posts list with tenant information
func BuildScheduledPostsData(ctx *AdminContext, status, tenantIDStr string, limit int, reqCtx context.Context) (*ScheduledPostsResponse, error) {
	query := ctx.DB.WithContext(reqCtx).Model(&storage.ScheduledPost{})

	// Apply filters
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if tenantIDStr != "" {
		tenantID, err := uuid.Parse(tenantIDStr)
		if err == nil {
			query = query.Where("tenant_id = ?", tenantID)
		}
	}

	// Count total
	var total int64
	query.Count(&total)

	// Get posts
	var posts []storage.ScheduledPost
	if err := query.Order("scheduled_at DESC, created_at DESC").
		Limit(limit).
		Find(&posts).Error; err != nil {
		return nil, fmt.Errorf("failed to query scheduled posts: %w", err)
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, post := range posts {
		if _, ok := tenantMap[post.TenantID]; !ok {
			tenantIDs = append(tenantIDs, post.TenantID)
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	items := make([]ScheduledPostDetail, 0, len(posts))
	for _, post := range posts {
		var platforms []string
		if post.Platforms != "" {
			json.Unmarshal([]byte(post.Platforms), &platforms)
		}

		items = append(items, ScheduledPostDetail{
			ID:          post.ID,
			TenantID:    post.TenantID,
			TenantName:  tenantMap[post.TenantID],
			Name:        post.Name,
			Caption:     post.Caption,
			Platforms:   platforms,
			ScheduledAt: post.ScheduledAt,
			Status:      post.Status,
			Attempts:    post.Attempts,
			LastError:   post.LastError,
			PostedAt:    post.PostedAt,
			CreatedAt:   post.CreatedAt,
		})
	}

	return &ScheduledPostsResponse{
		Total: total,
		Items: items,
	}, nil
}

// ForceCancelScheduledPostRequest represents a request to cancel a scheduled post
type ForceCancelScheduledPostRequest struct {
	Reason string `json:"reason"`
}

// ForceCancelScheduledPostHandler cancels a scheduled post
func ForceCancelScheduledPostHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		idStr := chi.URLParam(r, "id")
		postID, err := uuid.Parse(idStr)
		if err != nil || postID == uuid.Nil {
			http.Error(w, "invalid post id", http.StatusBadRequest)
			return
		}

		var req ForceCancelScheduledPostRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "Cancelled by admin"
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "scheduled_post.cancel",
			Metadata: map[string]interface{}{
				"post_id":     postID,
				"reason":      reason,
				"resource":    "post",
				"resource_id": idStr,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		// Update post status
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.ScheduledPost{}).
			Where("id = ?", postID).
			Updates(map[string]interface{}{
				"status":     "cancelled",
				"last_error": "force-cancelled by admin: " + reason,
			}).Error; err != nil {
			ctx.Logger.Error("failed to cancel scheduled post", zap.Stringer("post_id", postID), zap.Error(err))
			http.Error(w, "failed to cancel post", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("Force-cancelled scheduled post",
			zap.Stringer("post_id", postID),
			zap.String("reason", reason))

		respondJSON(w, actionResponse{Status: "ok"})
	}
}
