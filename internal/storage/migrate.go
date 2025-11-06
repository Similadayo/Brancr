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
		&AdminUser{},
		&Customer{},
		&EventLog{},
		&LogEntry{},
		&MediaItem{},
		&MediaAsset{},
		&ScheduledPost{},
		&SocialAccount{},
		&Persona{},
		&MemoryItem{},
		&IntentCatalog{},
		&TenantPolicy{},
		&Escalation{},
		&PostLog{},
		&Conversation{},
		&Interaction{},
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
