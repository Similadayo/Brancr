package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// CreateNewsletterRequest represents a request to create a newsletter
type CreateNewsletterRequest struct {
	Title       string   `json:"title"`
	Subject     string   `json:"subject"`
	Content     string   `json:"content"` // HTML content
	Categories  []string `json:"categories,omitempty"`
	ScheduledAt *string  `json:"scheduled_at,omitempty"` // ISO 8601 format
}

// CreateNewsletterHandler creates a new newsletter
func CreateNewsletterHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var req CreateNewsletterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Title == "" || req.Subject == "" || req.Content == "" {
			http.Error(w, "title, subject, and content are required", http.StatusBadRequest)
			return
		}

		var scheduledAt *time.Time
		if req.ScheduledAt != nil && *req.ScheduledAt != "" {
			parsed, err := time.Parse(time.RFC3339, *req.ScheduledAt)
			if err == nil {
				scheduledAt = &parsed
			}
		}

		createdBy := AdminEmailFromRequest(r)

		if ctx.NewsletterService == nil {
			http.Error(w, "newsletter service not available (SMTP not configured)", http.StatusServiceUnavailable)
			return
		}

		nl, err := ctx.NewsletterService.CreateNewsletter(
			reqCtx,
			req.Title,
			req.Subject,
			req.Content,
			req.Categories,
			createdBy,
			scheduledAt,
		)
		if err != nil {
			ctx.Logger.Error("failed to create newsletter", zap.Error(err))
			http.Error(w, "failed to create newsletter", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: createdBy,
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "newsletter.create",
			Metadata: map[string]interface{}{
				"newsletter_id": nl.ID,
				"title":         req.Title,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nl)
	}
}

// SendNewsletterHandler sends a newsletter to all subscribers
func SendNewsletterHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		idStr := strings.TrimSpace(r.URL.Query().Get("id"))
		if idStr == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		newsletterID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		if ctx.NewsletterService == nil {
			http.Error(w, "newsletter service not available (SMTP not configured)", http.StatusServiceUnavailable)
			return
		}

		// Send newsletter asynchronously (in production, use a job queue)
		go func() {
			if err := ctx.NewsletterService.SendNewsletter(reqCtx, newsletterID); err != nil {
				ctx.Logger.Error("failed to send newsletter", zap.Error(err), zap.Stringer("newsletter_id", newsletterID))
			}
		}()

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "newsletter.send",
			Metadata: map[string]interface{}{
				"newsletter_id": newsletterID,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"message": "Newsletter sending started",
		})
	}
}

// GetNewslettersHandler returns all newsletters
func GetNewslettersHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var newsletters []storage.Newsletter
		ctx.DB.WithContext(reqCtx).
			Order("created_at DESC").
			Limit(100).
			Find(&newsletters)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newsletters)
	}
}

// GetNewsletterHandler returns a single newsletter
func GetNewsletterHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		idStr := strings.TrimSpace(r.URL.Query().Get("id"))
		if idStr == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		var nl storage.Newsletter
		if err := ctx.DB.WithContext(reqCtx).First(&nl, idStr).Error; err != nil {
			http.Error(w, "newsletter not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nl)
	}
}

// GetSubscriptionsHandler returns newsletter subscriptions
func GetSubscriptionsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var subscriptions []storage.NewsletterSubscription
		query := ctx.DB.WithContext(reqCtx).Model(&storage.NewsletterSubscription{})

		if subscribed := r.URL.Query().Get("subscribed"); subscribed == "true" {
			query = query.Where("subscribed = ?", true)
		}

		query.Order("subscribed_at DESC").Limit(500).Find(&subscriptions)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(subscriptions)
	}
}
