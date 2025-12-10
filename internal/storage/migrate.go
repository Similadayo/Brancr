package storage

import (
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	// 🧩 Ensure pgvector extension is available
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector;`).Error; err != nil {
		log.Printf("⚠️ Could not enable pgvector extension: %v", err)
	} else {
		log.Println("✅ pgvector extension ready")
	}
	fmt.Println("📦 Running migrations...")

	// 🚀 Run all migrations
	if err := db.AutoMigrate(
		&Tenant{},
		&BusinessProfile{},
		&BusinessMenuItem{},
		&BusinessFAQ{},
		// Industry & Products
		&Industry{},
		&Product{},
		&MenuItem{},
		&Service{},
		&AdminUser{},
		&Customer{},
		&Order{},
		&Payment{},
		&TenantPaymentAccount{},
		&SalesConversation{},
		&NegotiationHistory{},
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
		&TenantPolicy{},
		&Escalation{},
		&PostLog{},
		&Conversation{},
		&ConversationUserState{},
		&Notification{},
		&Interaction{},
		// Recurring templates
		&RecurringTemplate{},
		// Bulk upload sessions
		&BulkUploadSession{},
		// Content calendar
		&ContentCalendar{},
		// Analytics models
		&PostAnalytics{},
		&CampaignAnalytics{},
		&TrendInsight{},
		&PlatformMetricsSnapshot{},
		&AudienceSegment{},
		&TenantQuota{}, // Quota tracking for tenants
	); err != nil {
		return err
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
		return nil // Table already exists
	}

	// Create the table with proper structure
	if err := db.Exec(`
		CREATE TABLE payment_audit_logs (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			interaction_id INTEGER NOT NULL,
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
		`CREATE INDEX idx_payment_audit_tenant ON payment_audit_logs(tenant_id)`,
		`CREATE INDEX idx_payment_audit_interaction ON payment_audit_logs(interaction_id)`,
		`CREATE INDEX idx_payment_audit_action ON payment_audit_logs(action)`,
		`CREATE INDEX idx_payment_audit_accessed_by ON payment_audit_logs(accessed_by)`,
		`CREATE INDEX idx_payment_audit_timestamp ON payment_audit_logs(timestamp)`,
		`CREATE INDEX idx_payment_audit_deleted_at ON payment_audit_logs(deleted_at)`,
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
			Name:        "Restaurant",
			Category:    "food",
			Description: "Restaurants, cafes, and food service businesses",
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
