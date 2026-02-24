package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// CreateIndexes creates all necessary database indexes for production performance
func CreateIndexes(db *gorm.DB) error {
	indexes := []struct {
		name   string
		table  string
		cols   string
		unique bool
	}{
		// Tenant indexes
		{"idx_tenant_email", "tenants", "email", true},
		{"idx_tenant_telegram_id", "tenants", "owner_telegram_id", false},
		{"idx_tenant_bot_username", "tenants", "bot_username", false},
		{"idx_tenant_status", "tenants", "status", false},

		// Business Profile indexes
		{"idx_business_profile_tenant", "business_profiles", "tenant_id", false},

		// Customer indexes (already has composite unique index)
		{"idx_customer_tenant_channel", "customers", "tenant_id, channel", false},
		{"idx_customer_platform_id", "customers", "platform_id", false},
		{"idx_customer_last_message", "customers", "last_message_at", false},

		// Social Account indexes
		{"idx_social_account_tenant", "social_accounts", "tenant_id", false},
		{"idx_social_account_platform", "social_accounts", "platform", false},
		{"idx_social_account_connected", "social_accounts", "connected", false},
		{"idx_social_account_tenant_platform", "social_accounts", "tenant_id, platform", false},

		// Media Asset indexes
		{"idx_media_asset_tenant", "media_assets", "tenant_id", false},
		{"idx_media_asset_status", "media_assets", "status", false},
		{"idx_media_asset_scheduled", "media_assets", "scheduled_at", false},
		{"idx_media_asset_bulk_upload", "media_assets", "bulk_upload_id", false},
		{"idx_media_asset_tenant_status", "media_assets", "tenant_id, status", false},

		// Scheduled Post indexes
		{"idx_scheduled_post_tenant", "scheduled_posts", "tenant_id", false},
		{"idx_scheduled_post_status", "scheduled_posts", "status", false},
		{"idx_scheduled_post_scheduled_at", "scheduled_posts", "scheduled_at", false},
		{"idx_scheduled_post_tenant_status", "scheduled_posts", "tenant_id, status", false},
		{"idx_scheduled_post_platform", "scheduled_posts", "platforms", false},

		// Conversation indexes
		{"idx_conversation_tenant", "conversations", "tenant_id", false},
		{"idx_conversation_customer", "conversations", "customer_id", false},
		{"idx_conversation_platform", "conversations", "platform", false},
		{"idx_conversation_status", "conversations", "status", false},
		{"idx_conversation_updated", "conversations", "updated_at", false},

		// Interaction indexes
		{"idx_interaction_tenant", "interactions", "tenant_id", false},
		{"idx_interaction_conversation", "interactions", "conversation_id", false},
		{"idx_interaction_customer", "interactions", "customer_id", false},
		{"idx_interaction_created", "interactions", "created_at", false},
		{"idx_interaction_tenant_created", "interactions", "tenant_id, created_at", false},

		// WhatsApp App indexes
		{"idx_whatsapp_app_tenant", "whatsapp_apps", "tenant_id", false},
		{"idx_whatsapp_app_status", "whatsapp_apps", "status", false},
		{"idx_whatsapp_app_gupshup_app_id", "whatsapp_apps", "gupshup_app_id", false},
		{"idx_whatsapp_app_gupshup_app_name", "whatsapp_apps", "gupshup_app_name", false},

		// Gupshup Webhook Event indexes
		{"idx_gupshup_webhook_app", "gupshup_webhook_events", "app_id", false},
		{"idx_gupshup_webhook_created", "gupshup_webhook_events", "created_at", false},
		{"idx_gupshup_webhook_type", "gupshup_webhook_events", "event_type", false},

		// Post Analytics indexes
		{"idx_post_analytics_tenant", "post_analytics", "tenant_id", false},
		{"idx_post_analytics_platform", "post_analytics", "platform", false},
		{"idx_post_analytics_created", "post_analytics", "created_at", false},
		{"idx_post_analytics_tenant_created", "post_analytics", "tenant_id, created_at", false},
		{"idx_post_analytics_scheduled_post", "post_analytics", "scheduled_post_id", false},
		{"idx_post_analytics_platform_post_id", "post_analytics", "platform_post_id", false},

		// Campaign Analytics indexes
		{"idx_campaign_analytics_tenant", "campaign_analytics", "tenant_id", false},
		{"idx_campaign_analytics_campaign_id", "campaign_analytics", "campaign_id", false},
		{"idx_campaign_analytics_name", "campaign_analytics", "name", false},
		{"idx_campaign_analytics_start_date", "campaign_analytics", "start_date", false},
		{"idx_campaign_analytics_end_date", "campaign_analytics", "end_date", false},

		// Event Log indexes
		{"idx_event_log_tenant", "event_logs", "tenant_id", false},
		{"idx_event_log_type", "event_logs", "event_type", false},
		{"idx_event_log_created", "event_logs", "created_at", false},

		// Post Log indexes
		{"idx_post_log_tenant", "post_logs", "tenant_id", false},
		{"idx_post_log_status", "post_logs", "status", false},
		{"idx_post_log_created", "post_logs", "created_at", false},
		{"idx_post_log_platform", "post_logs", "platform", false},

		// Menu Item indexes (for fast tenant queries with soft deletes)
		{"idx_menu_item_tenant", "menu_items", "tenant_id", false},
		{"idx_menu_item_category", "menu_items", "category", false},
		{"idx_menu_item_tenant_deleted", "menu_items", "tenant_id, deleted_at", false},
		{"idx_menu_item_tenant_category_created", "menu_items", "tenant_id, deleted_at, category, created_at", false},

		// Product indexes (for fast tenant queries with soft deletes)
		{"idx_product_tenant", "products", "tenant_id", false},
		{"idx_product_category", "products", "category", false},
		{"idx_product_tenant_deleted", "products", "tenant_id, deleted_at", false},
		{"idx_product_tenant_category_created", "products", "tenant_id, deleted_at, category, created_at", false},
	}

	for _, idx := range indexes {
		var sql string
		if idx.unique {
			sql = fmt.Sprintf(
				"CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s)",
				idx.name, idx.table, idx.cols,
			)
		} else {
			sql = fmt.Sprintf(
				"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
				idx.name, idx.table, idx.cols,
			)
		}

		if err := db.Exec(sql).Error; err != nil {
			// Log but don't fail - indexes might already exist or have different structure
			fmt.Printf("⚠️  Could not create index %s (might already exist): %v\n", idx.name, err)
		}
	}

	return nil
}
