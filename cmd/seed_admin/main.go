package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/similadayo/brancr/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found — using system environment variables")
	}

	// Check for POSTGRES_DSN first, then DATABASE_URL (Railway standard)
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		log.Fatal("POSTGRES_DSN or DATABASE_URL environment variable is required")
	}

	email := os.Getenv("ADMIN_EMAIL")
	password := os.Getenv("ADMIN_PASSWORD")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("DB connect failed: %v\nDSN: %s", err, maskDSN(dsn))
	}

	log.Printf("✅ Connected to database successfully")

	// Seed admin user
	if err := storage.SeedAdminUser(db, email, password); err != nil {
		log.Fatal(err)
	}

	// Seed test tenant for Meta App Review
	seedTestTenant(db)
}

func seedTestTenant(db *gorm.DB) {
	testEmail := "meta.reviewer@brancr.test"
	testPassword := "MetaReview2025!"
	businessName := "Meta App Review Test Account"

	// Check if tenant already exists
	var existing storage.Tenant
	if err := db.Where("email = ?", testEmail).First(&existing).Error; err == nil {
		fmt.Printf("✅ Test tenant already exists (ID: %d, Name: %s)\n", existing.ID, existing.Name)
		fmt.Printf("   Email: %s\n", testEmail)
		fmt.Printf("   Password: %s\n", testPassword)
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("⚠️  Failed to hash password: %v", err)
		return
	}

	// Create test tenant
	tenant := storage.Tenant{
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
		log.Printf("⚠️  Failed to create test tenant: %v", err)
		return
	}

	// Create business profile
	profile := storage.BusinessProfile{
		TenantID:    tenant.ID,
		Name:        businessName,
		Niche:       "Test Business",
		Tone:        "friendly",
		Description: "Test account for Meta App Review",
	}

	if err := db.Create(&profile).Error; err != nil {
		log.Printf("⚠️  Warning: Failed to create business profile: %v", err)
	}

	fmt.Printf("✅ Test tenant created successfully!\n")
	fmt.Printf("   Tenant ID: %d\n", tenant.ID)
	fmt.Printf("   Business Name: %s\n", businessName)
	fmt.Printf("   Email: %s\n", testEmail)
	fmt.Printf("   Password: %s\n", testPassword)
}

// maskDSN masks sensitive parts of the DSN for logging
func maskDSN(dsn string) string {
	if len(dsn) < 20 {
		return "***"
	}
	// Show first 10 and last 10 characters
	return dsn[:10] + "***" + dsn[len(dsn)-10:]
}
