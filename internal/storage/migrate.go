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
	return db.AutoMigrate(
		&Tenant{},
		&BusinessProfile{},
		&BusinessMenuItem{},
		&BusinessFAQ{},
		&AdminUser{},
		&Customer{},
		&EventLog{},
		&LogEntry{},
		&MediaItem{},
		&MediaAsset{},
		&ScheduledPost{},
		&SocialAccount{},
		&WhatsAppPhoneNumber{},
		&WhatsAppPhoneNumberRequest{},
		&Persona{},
		&MemoryItem{},
		&IntentCatalog{},
		&TenantPolicy{},
		&Escalation{},
		&PostLog{},
		&Conversation{},
		&Interaction{},
		// Recurring templates
		&RecurringTemplate{},
		// Bulk upload sessions
		&BulkUploadSession{},
		// Content calendar
		&ContentCalendar{},
	)
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
