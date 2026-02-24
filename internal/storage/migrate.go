package storage

import (
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var PgVectorAvailable bool = false

func AutoMigrate(db *gorm.DB) error {
	// 🧩 Ensure pgvector extension is available when Postgres supports it
	// First check if extension exists to avoid ERROR logs in PostgreSQL when extension is not installed
	var extensionExists bool
	checkErr := db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector');`).Scan(&extensionExists).Error

	if checkErr == nil && extensionExists {
		// Extension already exists
		log.Println("✅ pgvector extension ready")
		PgVectorAvailable = true
	} else if checkErr == nil && !extensionExists {
		// Extension doesn't exist, try to create it
		// Use DO block to catch and handle the "not available" error silently
		createErr := db.Exec(`
			DO $$
			BEGIN
				CREATE EXTENSION IF NOT EXISTS vector;
			EXCEPTION
				WHEN OTHERS THEN
					-- Silently handle "extension not available" errors
					-- This prevents ERROR logs in PostgreSQL when pgvector is not installed
					NULL;
			END $$;
		`).Error

		if createErr == nil {
			// Check again if creation succeeded
			var nowExists bool
			if db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector');`).Scan(&nowExists).Error == nil && nowExists {
				log.Println("✅ pgvector extension ready")
				PgVectorAvailable = true
			} else {
				log.Printf("ℹ️ pgvector is not available on this database; semantic-memory features will be disabled until you install the 'vector' extension")
				PgVectorAvailable = false
			}
		} else {
			// Unexpected error during creation attempt
			log.Printf("⚠️ Could not enable pgvector extension (unexpected error): %v", createErr)
			PgVectorAvailable = false
		}
	} else {
		// Error checking extension existence - assume not available
		log.Printf("ℹ️ Could not check pgvector extension status; semantic-memory features will be disabled: %v", checkErr)
		PgVectorAvailable = false
	}

	fmt.Println("📦 Running GORM AutoMigrate for all models...")
	log.Println("Starting AutoMigrate - this will create/update all tables in the database")

	// 🚀 Run all migrations - this creates/updates all tables on Railway Postgres
	// IMPORTANT: This must run successfully for the app to work
	// This creates ALL tables including tenant_quotas, interactions, etc.
	log.Println("📋 Migrating 58 models to database...")
	if err := db.AutoMigrate(
		&Tenant{},
		&BusinessProfile{},
		&BusinessFAQ{},
		// Industry & Products
		&Industry{},
		&Product{},
		&MenuItem{},
		&BusinessMenuItem{}, // Deprecated but needed for migration logic
		&Service{},
		&AdminUser{},
		&Customer{},
		&CustomerInsight{}, // Customer personality insights for AI personalization
		&Order{},
		&Payment{},
		&TenantPaymentAccount{},
		&SalesConversation{},
		&NegotiationHistory{},
		&NegotiationState{}, // Per-conversation negotiation state
		&EscalationEvent{},  // Escalation event context
		&Booking{},
		&EventLog{},
		&LogEntry{},
		&MediaItem{},
		&MediaAsset{},
		&ScheduledPost{},
		&SocialAccount{},
		&WhatsAppPhoneNumber{},
		&WhatsAppPhoneNumberRequest{},
		&WhatsAppApp{},         // Gupshup Partner multi-tenant WhatsApp apps
		&GupshupWebhookEvent{}, // Gupshup webhook events
		&Persona{},
		&MemoryItem{},
		&IntentCatalog{},
		&VoiceFingerprint{}, // Tenant voice learning (communication style)
		&TenantPolicy{},
		&TenantOffer{},
		&Escalation{},
		&PostLog{},
		&Conversation{},
		&ConversationUserState{},
		&Notification{},
		&TenantNotificationSettings{},
		&AdminAuditLog{},
		// Alerts & Newsletters
		&TenantAlert{},
		&NewsletterSubscription{},
		&Newsletter{},
		&NewsletterDelivery{},
		&Interaction{},
		// Recurring templates
		&RecurringTemplate{},
		// Bulk upload sessions
		&BulkUploadSession{},
		// Content calendar
		&ContentCalendar{},
		// Analytics models
		&PostAnalytics{},
		&PostLog{}, // Detailed posting logs
		&CampaignAnalytics{},
		&TrendInsight{},
		&PlatformMetricsSnapshot{},
		&AudienceSegment{},
		&TenantQuota{}, // Quota tracking for tenants
		// Billing & Plans
		&Plan{}, // Subscription plans (dynamic config)
		&PlanTemplate{},
		&TenantPlan{},
		&UsageEvent{},
		&UsageAggregation{},
		&GupshupUsage{},    // Gupshup daily usage data
		&GupshupDiscount{}, // Gupshup daily discount data
		// Session tracking models
		&PendingEscalation{},
		&PostSession{},
		&PostSessionV2{},
		&GenericSession{}, // V3 flexible key-value sessions
		&BusinessSession{},
		&EscalationResolutionSession{},
		// AI state and feedback models
		&ConversationAIState{},
		&AIFeedbackEvent{},
		// Drafts (save user progress across devices)
		&Draft{},
		// Task State (Conversational flows)
		&TaskState{},
		// Admin dashboard models
		&AdminMetricHourly{},
		&AdminMetricDaily{},
		&AdminQualitativeInsight{},
		// Alert templates (editable by admins)
		&AlertTemplate{},
		// Experience Checkpoints
		&ExperienceCheckpoint{},
		&CheckpointResponse{},
		// Knowledge Base & Appointments (Consultancies/Agencies)
		&ServiceKnowledge{},
		&Appointment{},
		// Fulfillment (Post-Payment)
		&Requirement{},
		&ItemRequirement{},
		&FulfillmentState{},
		&RequirementSubmission{},
	); err != nil {
		log.Printf("❌ GORM AutoMigrate FAILED: %v", err)
		return fmt.Errorf("gorm AutoMigrate failed: %w", err)
	}

	fmt.Println("✅ GORM AutoMigrate completed successfully - all tables are up to date")
	log.Println("✅ All database tables have been migrated/created successfully")

	// Create unique indexes for admin metrics tables (handles NULL tenant_id properly)
	if err := createAdminMetricsUniqueIndexes(db); err != nil {
		log.Printf("⚠️ Could not create unique indexes for admin metrics: %v", err)
	}

	// Fix Customer.channel column size (was VARCHAR(5), needs to be VARCHAR(50))
	if err := fixCustomerChannelColumnSize(db); err != nil {
		log.Printf("⚠️ Could not fix customer channel column size: %v", err)
	}

	// Fix Customer.profile_pic_url column size (was VARCHAR(255), needs to be TEXT)
	if err := fixCustomerProfilePicColumnSize(db); err != nil {
		log.Printf("⚠️ Could not fix customer profile_pic_url column size: %v", err)
	}

	// Verify critical tables exist (especially tenant_quotas which was missing)
	var tableCount int64
	var tables []string
	if err := db.Raw(`
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_name IN ('tenants', 'tenant_quotas', 'interactions', 'orders', 'payments', 'sales_conversations')
		ORDER BY table_name
	`).Scan(&tables).Error; err == nil {
		tableCount = int64(len(tables))
		log.Printf("✅ Verified %d critical tables exist: %v", tableCount, tables)

		// Check specifically for tenant_quotas
		var tenantQuotaExists bool
		db.Raw(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'tenant_quotas'
		)`).Scan(&tenantQuotaExists)

		if tenantQuotaExists {
			log.Println("✅ tenant_quotas table exists")
		} else {
			log.Println("❌ CRITICAL: tenant_quotas table does NOT exist - this will cause errors!")
		}

		if tableCount < 5 {
			log.Printf("⚠️ Warning: Expected at least 5 critical tables, found %d", tableCount)
		}
	} else {
		log.Printf("⚠️ Could not verify tables: %v", err)
	}

	// Migrate payment audit logs table (from security package)
	// Import and migrate PaymentAuditLog model
	if err := migratePaymentAuditLogs(db); err != nil {
		log.Printf("⚠️ Could not migrate payment_audit_logs table: %v", err)
	} else {
		log.Println("✅ Payment audit logs table ready")
	}

	return nil
}

// migratePaymentAuditLogs creates the payment_audit_logs table using GORM-compatible approach
func migratePaymentAuditLogs(db *gorm.DB) error {
	// Check if table already exists
	var exists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'payment_audit_logs'
		);
	`).Scan(&exists).Error; err != nil {
		return fmt.Errorf("failed to check if payment_audit_logs table exists: %w", err)
	}

	if exists {
		var tenantIDType string
		_ = db.Raw(`
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = 'public'
			AND table_name = 'payment_audit_logs'
			AND column_name = 'tenant_id'
			LIMIT 1;
		`).Scan(&tenantIDType).Error

		var interactionIDType string
		_ = db.Raw(`
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = 'public'
			AND table_name = 'payment_audit_logs'
			AND column_name = 'interaction_id'
			LIMIT 1;
		`).Scan(&interactionIDType).Error

		if tenantIDType == "uuid" && interactionIDType == "uuid" {
			return nil
		}

		log.Printf("⚠️ payment_audit_logs has legacy schema (tenant_id=%s, interaction_id=%s). Recreating with UUID columns.", tenantIDType, interactionIDType)
		_ = db.Exec(`DROP TABLE IF EXISTS payment_audit_logs_legacy;`).Error
		if err := db.Exec(`ALTER TABLE payment_audit_logs RENAME TO payment_audit_logs_legacy;`).Error; err != nil {
			return fmt.Errorf("failed to rename legacy payment_audit_logs table: %w", err)
		}
	}

	// Create the table with proper structure
	if err := db.Exec(`
		CREATE TABLE payment_audit_logs (
			id SERIAL PRIMARY KEY,
			tenant_id UUID NOT NULL,
			interaction_id UUID NOT NULL,
			action VARCHAR(50) NOT NULL,
			accessed_by BIGINT NOT NULL,
			ip_address VARCHAR(50),
			user_agent TEXT,
			timestamp TIMESTAMP NOT NULL,
			success BOOLEAN DEFAULT true,
			reason TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			deleted_at TIMESTAMP NULL
		);
	`).Error; err != nil {
		return fmt.Errorf("failed to create payment_audit_logs table: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_tenant ON payment_audit_logs(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_interaction ON payment_audit_logs(interaction_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_action ON payment_audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_accessed_by ON payment_audit_logs(accessed_by)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_timestamp ON payment_audit_logs(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_audit_deleted_at ON payment_audit_logs(deleted_at)`,
	}

	for _, idxSQL := range indexes {
		if err := db.Exec(idxSQL).Error; err != nil {
			// Log but don't fail - indexes might already exist
			log.Printf("⚠️ Could not create index (might already exist): %v", err)
		}
	}

	// Add foreign key constraints if interactions table exists
	if err := db.Exec(`
		ALTER TABLE payment_audit_logs
		ADD CONSTRAINT fk_payment_audit_tenant 
		FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;
	`).Error; err != nil {
		log.Printf("⚠️ Could not add tenant foreign key (might already exist): %v", err)
	}

	if err := db.Exec(`
		ALTER TABLE payment_audit_logs
		ADD CONSTRAINT fk_payment_audit_interaction 
		FOREIGN KEY (interaction_id) REFERENCES interactions(id) ON DELETE CASCADE;
	`).Error; err != nil {
		log.Printf("⚠️ Could not add interaction foreign key (might already exist): %v", err)
	}

	return nil
}

// createAdminMetricsUniqueIndexes creates unique indexes for admin metrics tables
// This handles NULL tenant_id values properly for ON CONFLICT clauses
func createAdminMetricsUniqueIndexes(db *gorm.DB) error {
	// Drop existing indexes if they exist (to recreate with correct structure)
	db.Exec(`DROP INDEX IF EXISTS uniq_admin_metrics_hourly`)
	db.Exec(`DROP INDEX IF EXISTS uniq_admin_metrics_daily`)

	// For admin_metrics_hourly: unique constraint on (metric_type, tenant_id, metric_key, hour_start)
	// Use NULLS NOT DISTINCT to treat NULL values as equal for uniqueness (PostgreSQL 15+)
	// This allows ON CONFLICT to work properly with NULL tenant_id values
	if err := db.Exec(`
		CREATE UNIQUE INDEX uniq_admin_metrics_hourly 
		ON admin_metrics_hourly (metric_type, tenant_id, metric_key, hour_start) 
		NULLS NOT DISTINCT;
	`).Error; err != nil {
		// If index already exists, that's fine - continue
		log.Printf("⚠️ Could not create unique index for admin_metrics_hourly (may already exist): %v", err)
	}

	// For admin_metrics_daily: unique constraint on (metric_type, tenant_id, metric_key, date)
	// Use NULLS NOT DISTINCT to treat NULL values as equal for uniqueness (PostgreSQL 15+)
	if err := db.Exec(`
		CREATE UNIQUE INDEX uniq_admin_metrics_daily 
		ON admin_metrics_daily (metric_type, tenant_id, metric_key, date) 
		NULLS NOT DISTINCT;
	`).Error; err != nil {
		// If index already exists, that's fine - continue
		log.Printf("⚠️ Could not create unique index for admin_metrics_daily (may already exist): %v", err)
	}

	log.Println("✅ Created unique indexes for admin metrics tables")
	return nil
}

// fixCustomerChannelColumnSize fixes the Customer.channel column size
// The column was created as VARCHAR(5) but needs to be VARCHAR(50) to hold platform names
// like "instagram" (9 chars), "whatsapp" (8 chars), etc.
func fixCustomerChannelColumnSize(db *gorm.DB) error {
	// Check current column size
	var columnType string
	err := db.Raw(`
		SELECT data_type || '(' || character_maximum_length || ')' as column_type
		FROM information_schema.columns
		WHERE table_name = 'customers' AND column_name = 'channel'
	`).Scan(&columnType).Error

	if err != nil {
		log.Printf("⚠️ Could not check customer channel column type: %v", err)
		// Continue anyway and try to alter
	} else {
		log.Printf("📋 Current customer.channel column type: %s", columnType)
	}

	// Alter column to VARCHAR(50)
	if err := db.Exec(`
		ALTER TABLE customers 
		ALTER COLUMN channel TYPE VARCHAR(50);
	`).Error; err != nil {
		// Check if error is because column doesn't exist or table doesn't exist
		if err.Error() == "ERROR: relation \"customers\" does not exist (SQLSTATE 42P01)" {
			log.Println("ℹ️ customers table doesn't exist yet, will be created with correct size")
			return nil
		}
		return err
	}

	log.Println("✅ Fixed customer.channel column size to VARCHAR(50)")
	return nil
}

// fixCustomerProfilePicColumnSize fixes the Customer.profile_pic_url column size
// The column was created as VARCHAR(255) but needs to be TEXT to hold long social media URLs
func fixCustomerProfilePicColumnSize(db *gorm.DB) error {
	// Check current column size
	var columnType string
	err := db.Raw(`
		SELECT data_type || '(' || character_maximum_length || ')' as column_type
		FROM information_schema.columns
		WHERE table_name = 'customers' AND column_name = 'profile_pic_url'
	`).Scan(&columnType).Error

	if err != nil {
		log.Printf("⚠️ Could not check customer profile_pic_url column type: %v", err)
		// Continue anyway and try to alter
	} else {
		log.Printf("📋 Current customer.profile_pic_url column type: %s", columnType)
	}

	// Alter column to TEXT
	if err := db.Exec(`
		ALTER TABLE customers 
		ALTER COLUMN profile_pic_url TYPE TEXT;
	`).Error; err != nil {
		// Check if error is because column doesn't exist or table doesn't exist
		if err.Error() == "ERROR: relation \"customers\" does not exist (SQLSTATE 42P01)" {
			// Table doesn't exist yet, will be created with correct size
			return nil
		}
		return err
	}

	log.Println("✅ Fixed customer.profile_pic_url column size to TEXT")
	return nil
}

// SeedAdminUser ensures at least one admin user exists
func SeedAdminUser(db *gorm.DB, email, password string) error {
	if email == "" || password == "" {
		return fmt.Errorf("admin credentials missing in env")
	}

	var count int64
	db.Model(&AdminUser{}).Where("email = ?", email).Count(&count)
	if count > 0 {
		fmt.Println("✅ Admin user already exists")
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("bcrypt error: %w", err)
	}

	admin := AdminUser{
		Email:        email,
		PasswordHash: string(hash),
		Role:         "SuperAdmin",
		Active:       true,
		LastLogin:    nil,
	}

	if err := db.Create(&admin).Error; err != nil {
		return fmt.Errorf("failed to seed admin: %w", err)
	}

	fmt.Println("✅ Seeded admin user:", email)
	return nil
}

// SeedTestTenant ensures the Meta App Review test tenant exists
func SeedTestTenant(db *gorm.DB) error {
	testEmail := "meta.reviewer@brancr.test"
	testPassword := "MetaReview2025!"
	businessName := "Meta App Review Test Account"

	// Check if tenant already exists
	var existing Tenant
	if err := db.Where("email = ?", testEmail).First(&existing).Error; err == nil {
		fmt.Println("✅ Test tenant already exists:", testEmail)
		return nil
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("bcrypt error: %w", err)
	}

	// Create test tenant
	tenant := Tenant{
		Name:               businessName,
		Email:              testEmail,
		OwnerName:          "Meta Reviewer",
		PasswordHash:       string(hash),
		PlanType:           "trial",
		Status:             "active",
		OnboardingStep:     "social_connect", // Skip to social connection step
		OnboardingComplete: false,
		Timezone:           "UTC",
	}

	if err := db.Create(&tenant).Error; err != nil {
		return fmt.Errorf("failed to seed test tenant: %w", err)
	}

	// Create business profile
	profile := BusinessProfile{
		TenantID:    tenant.ID,
		Name:        businessName,
		Niche:       "Test Business",
		Tone:        "friendly",
		Description: "Test account for Meta App Review",
	}

	if err := db.Create(&profile).Error; err != nil {
		// Log warning but don't fail - profile is optional
		log.Printf("⚠️  Warning: Failed to create business profile for test tenant: %v", err)
	}

	fmt.Println("✅ Seeded test tenant:", testEmail)
	return nil
}

// SeedIndustries ensures default industries are available
func SeedIndustries(db *gorm.DB) error {
	// First, migrate existing "Restaurant" industry to "Food & Beverage" if it exists
	var oldRestaurant Industry
	if err := db.Where("name = ?", "Restaurant").First(&oldRestaurant).Error; err == nil {
		// Update existing Restaurant industry to Food & Beverage
		if err := db.Model(&oldRestaurant).Updates(map[string]interface{}{
			"name":        "Food & Beverage",
			"description": "Restaurants, cafes, bakeries, pastries, food vendors, and all food service businesses",
		}).Error; err != nil {
			log.Printf("⚠️ Could not migrate Restaurant to Food & Beverage: %v", err)
		} else {
			log.Printf("✅ Migrated Restaurant industry to Food & Beverage")
		}
	}

	industries := []Industry{
		{
			Name:        "E-commerce",
			Category:    "retail",
			Description: "Online retail stores selling physical products",
			HasProducts: true,
			HasMenu:     false,
			HasServices: false,
			Settings:    `{"sales_style": "product_focused", "negotiation_enabled": true, "bulk_discounts": true}`,
		},
		{
			Name:        "Food & Beverage",
			Category:    "food",
			Description: "Restaurants, cafes, bakeries, pastries, food vendors, and all food service businesses",
			HasProducts: false,
			HasMenu:     true,
			HasServices: false,
			Settings:    `{"sales_style": "menu_focused", "recommendations_enabled": true, "dietary_aware": true}`,
		},
		{
			Name:        "Consultant",
			Category:    "professional_services",
			Description: "Consulting and advisory services",
			HasProducts: false,
			HasMenu:     false,
			HasServices: true,
			Settings:    `{"sales_style": "consultative", "relationship_focused": true, "consultation_booking": true}`,
		},
		{
			Name:        "Agency",
			Category:    "professional_services",
			Description: "Marketing, design, and creative agencies",
			HasProducts: false,
			HasMenu:     false,
			HasServices: true,
			Settings:    `{"sales_style": "solution_focused", "package_based": true, "portfolio_references": true}`,
		},
		{
			Name:        "Digital Products",
			Category:    "digital",
			Description: "Sales of digital goods, software, courses, and downloads",
			HasProducts: true,
			HasMenu:     false,
			HasServices: false,
			Settings:    `{"sales_style": "product_focused", "instant_delivery": true, "support_focused": true}`,
		},
		{
			Name:        "Education",
			Category:    "education",
			Description: "Schools, training centers, online courses, and tutors",
			HasProducts: false,
			HasMenu:     false,
			HasServices: true,
			Settings:    `{"sales_style": "consultative", "enrollment_focused": true, "curriculum_aware": true}`,
		},
		{
			Name:        "Logistics",
			Category:    "logistics",
			Description: "Delivery companies, courier services, and transportation",
			HasProducts: false,
			HasMenu:     false,
			HasServices: true,
			Settings:    `{"sales_style": "utility_focused", "tracking_enabled": true, "quote_based": true}`,
		},
		{
			Name:        "Fashion",
			Category:    "retail",
			Description: "Clothing, accessories, and fashion retail",
			HasProducts: true,
			HasMenu:     false,
			HasServices: false,
			Settings:    `{"sales_style": "visual_focused", "size_guide_enabled": true, "trend_aware": true}`,
		},
		{
			Name:        "Real Estate",
			Category:    "real_estate",
			Description: "Real estate agencies, property rentals, and sales",
			HasProducts: true, // Properties treated as products for listing
			HasMenu:     false,
			HasServices: true,
			Settings:    `{"sales_style": "high_touch", "viewing_scheduling": true, "location_aware": true}`,
		},
		{
			Name:        "General",
			Category:    "general",
			Description: "General business (no specific industry)",
			HasProducts: false,
			HasMenu:     false,
			HasServices: false,
			Settings:    `{"sales_style": "general", "flexible": true}`,
		},
	}

	for _, industry := range industries {
		var existing Industry
		if err := db.Where("name = ?", industry.Name).First(&existing).Error; err == nil {
			// Industry exists, skip
			continue
		}

		if err := db.Create(&industry).Error; err != nil {
			log.Printf("⚠️ Could not seed industry %s: %v", industry.Name, err)
		} else {
			log.Printf("✅ Seeded industry: %s", industry.Name)
		}
	}

	return nil
}

// SeedPlans ensures default subscription plans are available in the database
func SeedPlans(db *gorm.DB) error {
	plans := []Plan{
		{
			Code:               "trial",
			Name:               "Trial",
			Description:        "30-day free trial with limited features",
			Price:              0,
			Currency:           "USD",
			BillingPeriod:      "monthly",
			ConversationsLimit: 500,
			SeatsLimit:         2,
			AIRequestsLimit:    1000,
			StorageLimitMB:     100,
			Features:           `["AI-powered customer replies", "1 social channel", "Basic analytics"]`,
			IsActive:           true,
			IsTrial:            true,
			TrialDays:          30,
			SortOrder:          1,
		},
		{
			Code:               "starter",
			Name:               "Starter Plan",
			Description:        "Perfect for small businesses getting started",
			Price:              29,
			Currency:           "USD",
			BillingPeriod:      "monthly",
			ConversationsLimit: 1000,
			SeatsLimit:         3,
			AIRequestsLimit:    5000,
			StorageLimitMB:     500,
			Features:           `["AI-powered customer replies", "3 social channels", "WhatsApp, Instagram integrations", "Email support"]`,
			IsActive:           true,
			IsTrial:            false,
			SortOrder:          2,
		},
		{
			Code:               "growth",
			Name:               "Growth Plan",
			Description:        "For growing businesses with higher volume",
			Price:              89,
			Currency:           "USD",
			BillingPeriod:      "monthly",
			ConversationsLimit: 5000,
			SeatsLimit:         10,
			AIRequestsLimit:    20000,
			StorageLimitMB:     2000,
			Features:           `["AI-powered customer replies", "5,000 monthly conversations", "WhatsApp, Instagram, Facebook integrations", "Priority support", "Advanced analytics"]`,
			IsActive:           true,
			IsTrial:            false,
			SortOrder:          3,
		},
		{
			Code:               "enterprise",
			Name:               "Enterprise",
			Description:        "For large businesses with custom needs",
			Price:              299,
			Currency:           "USD",
			BillingPeriod:      "monthly",
			ConversationsLimit: 50000,
			SeatsLimit:         100,
			AIRequestsLimit:    100000,
			StorageLimitMB:     10000,
			Features:           `["Unlimited conversations", "Unlimited social channels", "Custom AI training", "Dedicated support", "API access", "White-label options"]`,
			IsActive:           true,
			IsTrial:            false,
			SortOrder:          4,
		},
	}

	for _, plan := range plans {
		var existing Plan
		if err := db.Where("code = ?", plan.Code).First(&existing).Error; err == nil {
			// Plan exists, skip
			continue
		}

		if err := db.Create(&plan).Error; err != nil {
			log.Printf("⚠️ Could not seed plan %s: %v", plan.Code, err)
		} else {
			log.Printf("✅ Seeded plan: %s ($%.2f/month)", plan.Name, plan.Price)
		}
	}

	return nil
}
