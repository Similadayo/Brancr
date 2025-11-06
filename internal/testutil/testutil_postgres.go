package testutil

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestDBPostgres creates a PostgreSQL database for testing
// Uses POSTGRES_DSN from environment, or skips if not available
func TestDBPostgres(t *testing.T) *gorm.DB {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = os.Getenv("TEST_POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("Skipping test: POSTGRES_DSN or TEST_POSTGRES_DSN not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test PostgreSQL database: %v", err)
	}
	return db
}

