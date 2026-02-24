package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminDeleteTenantHandler permanently deletes a tenant and all associated data (SuperAdmin only)
func AdminDeleteTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Audit log before deletion
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.delete",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"type":      "hard_delete",
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Error("audit failed", zap.Error(err))
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		// Hard delete: remove all tenant data (including sessions)
		if err := hardDeleteTenant(r.Context(), ctx.DB, ctx.Redis, tenantID); err != nil {
			ctx.Logger.Error("failed to delete tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, fmt.Sprintf("failed to delete tenant: %v", err), http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("tenant deleted", zap.Stringer("tenant_id", tenantID), zap.String("admin", AdminEmailFromRequest(r)))
		respondJSON(w, map[string]string{"status": "ok", "message": "Tenant permanently deleted"})
	}
}

// AdminBlockTenantHandler blocks a tenant from accessing the platform (SuperAdmin only)
func AdminBlockTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.block",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("blocked", true).Error; err != nil {
			ctx.Logger.Error("failed to block tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to block tenant", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("tenant blocked", zap.Stringer("tenant_id", tenantID), zap.String("admin", AdminEmailFromRequest(r)))
		respondJSON(w, map[string]string{"status": "ok", "message": "Tenant blocked"})
	}
}

// AdminUnblockTenantHandler unblocks a tenant (SuperAdmin only)
func AdminUnblockTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.unblock",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("blocked", false).Error; err != nil {
			ctx.Logger.Error("failed to unblock tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to unblock tenant", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("tenant unblocked", zap.Stringer("tenant_id", tenantID), zap.String("admin", AdminEmailFromRequest(r)))
		respondJSON(w, map[string]string{"status": "ok", "message": "Tenant unblocked"})
	}
}

// AdminSuspendTenantHandler suspends a tenant temporarily (SuperAdmin only)
func AdminSuspendTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.suspend",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("suspended", true).Error; err != nil {
			ctx.Logger.Error("failed to suspend tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to suspend tenant", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("tenant suspended", zap.Stringer("tenant_id", tenantID), zap.String("admin", AdminEmailFromRequest(r)))
		respondJSON(w, map[string]string{"status": "ok", "message": "Tenant suspended"})
	}
}

// AdminUnsuspendTenantHandler removes suspension from a tenant (SuperAdmin only)
func AdminUnsuspendTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.unsuspend",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("suspended", false).Error; err != nil {
			ctx.Logger.Error("failed to unsuspend tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to unsuspend tenant", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("tenant unsuspended", zap.Stringer("tenant_id", tenantID), zap.String("admin", AdminEmailFromRequest(r)))
		respondJSON(w, map[string]string{"status": "ok", "message": "Tenant unsuspended"})
	}
}

// hardDeleteTenant permanently removes a tenant and all associated data
func hardDeleteTenant(ctx context.Context, db *gorm.DB, redisClient *redis.Client, tenantID uuid.UUID) error {
	// Get tenant email BEFORE transaction for Redis cleanup (to avoid transaction abort)
	var tenantEmail string
	if redisClient != nil {
		var tenant storage.Tenant
		if err := db.WithContext(ctx).Where("id = ?", tenantID).First(&tenant).Error; err == nil {
			tenantEmail = tenant.Email
		}
	}

	// Start transaction
	// Start transaction
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		opCounter := 0
		// Helper to delete and check error
		deleteModel := func(model interface{}) error {
			opCounter++
			sp := fmt.Sprintf("sp_%d", opCounter)
			if err := tx.SavePoint(sp).Error; err != nil {
				return err
			}

			if err := tx.Unscoped().Where("tenant_id = ?", tenantID).Delete(model).Error; err != nil {
				// Ignore if table doesn't exist (handle 42P01)
				if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "42P01") {
					tx.RollbackTo(sp)
					return nil
				}
				return fmt.Errorf("failed to delete %T: %w", model, err)
			}
			return nil
		}

		// Delete all tenant-related data in order
		// Priority: Delete explicit blockers first

		// 0. Delete Business Profile (User requested priority)
		if err := deleteModel(&storage.BusinessProfile{}); err != nil {
			return err
		}

		// 1. Delete sessions and temporary data
		if err := deleteModel(&storage.PostSession{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.PostSessionV2{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.BusinessSession{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.EscalationResolutionSession{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.PendingEscalation{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.BulkUploadSession{}); err != nil {
			return err
		}

		// 1b. Delete Negotiation History (indirect simple relation via SalesConversation)
		opCounter++
		sp := fmt.Sprintf("sp_%d", opCounter)
		if err := tx.SavePoint(sp).Error; err != nil {
			return err
		}

		// Use raw SQL with casting to handle potential type mismatches (text vs uuid) in database schema
		if err := tx.Exec(`
			DELETE FROM negotiation_histories 
			WHERE sales_conversation_id::text IN (
				SELECT id::text FROM sales_conversations WHERE tenant_id = ?
			)
		`, tenantID).Error; err != nil {
			// Ignore if table doesn't exist
			if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "42P01") {
				tx.RollbackTo(sp)
			} else {
				return fmt.Errorf("failed to delete NegotiationHistory: %w", err)
			}
		}

		// 2. Delete customer interactions and conversations
		if err := deleteModel(&storage.Interaction{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Conversation{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.ConversationUserState{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.SalesConversation{}); err != nil {
			return err
		}

		// 3. Delete customers and memory
		if err := deleteModel(&storage.MemoryItem{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Customer{}); err != nil {
			return err
		}

		// 4. Delete commerce data
		if err := deleteModel(&storage.Order{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Payment{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.TenantPaymentAccount{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Booking{}); err != nil {
			return err
		}

		// 5. Delete products/menu/services/intents
		if err := deleteModel(&storage.Product{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.MenuItem{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Service{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.BusinessMenuItem{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.BusinessFAQ{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.IntentCatalog{}); err != nil {
			return err
		}

		// 6. Delete social accounts and media
		if err := deleteModel(&storage.SocialAccount{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.MediaAsset{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.MediaItem{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.ScheduledPost{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.PostLog{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.RecurringTemplate{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.ContentCalendar{}); err != nil {
			return err
		}

		// 7. Delete notifications, logs, and settings
		if err := deleteModel(&storage.Notification{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.EventLog{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Persona{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.TenantPolicy{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.TenantOffer{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.TenantQuota{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.TenantNotificationSettings{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.Draft{}); err != nil {
			return err
		}

		// 8. Delete WhatsApp data
		if err := deleteModel(&storage.WhatsAppApp{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.GupshupWebhookEvent{}); err != nil {
			return err
		}
		if err := deleteModel(&storage.WhatsAppPhoneNumberRequest{}); err != nil {
			return err
		}
		// Try to delete phone number if owned by tenant
		if err := deleteModel(&storage.WhatsAppPhoneNumber{}); err != nil {
			return err
		}

		// 10. Delete Redis sessions (if Redis is available)
		if redisClient != nil && tenantEmail != "" {
			// Delete session from Redis using email retrieved before transaction
			sessionKey := fmt.Sprintf("session:tenant:%s:%s", tenantID.String(), tenantEmail)
			_ = redisClient.Del(ctx, sessionKey).Err() // Best effort
		}

		// 11. Finally, delete the tenant itself
		if err := tx.Unscoped().Where("id = ?", tenantID).Delete(&storage.Tenant{}).Error; err != nil {
			return fmt.Errorf("failed to delete tenant: %w", err)
		}

		return nil
	})
}
