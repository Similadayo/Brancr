package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDB creates an in-memory SQLite database for testing
// Note: Requires CGO_ENABLED=1. If CGO is disabled, tests will be skipped.
func TestDB(t *testing.T) *gorm.DB {
	// Check if CGO is enabled
	// If SQLite fails, we'll skip the test with a helpful message
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub" || 
		   errMsg == "sql: unknown driver \"sqlite3\" (forgotten import?)" {
			t.Skip("Skipping test: SQLite requires CGO. Run with: set CGO_ENABLED=1 && go test ./...")
		}
		t.Fatalf("Failed to create test database: %v", err)
	}
	return db
}

// TestLogger creates a test logger
func TestLogger(t *testing.T) *zap.Logger {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return logger
}

// TestConfig creates a test configuration
func TestConfig() *config.Config {
	return &config.Config{
		AppEnv:           "test",
		Port:             "8080",
		OpenRouterMode:   "free",
		OpenRouterAPIKey: "test-key",
		PostgresDSN:      "",
		RedisAddr:        "localhost:6379",
		AdminBotToken:    "test-token",
		AdminEmail:       "test@example.com",
		AdminPassword:    "test-password",
	}
}

// TestRedis creates a test Redis client (or nil if Redis is not available)
func TestRedis(t *testing.T) *redis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	// Test connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Logf("Redis not available, skipping Redis tests: %v", err)
		return nil
	}

	return rdb
}

// CleanupDB drops all tables from the test database
func CleanupDB(t *testing.T, db *gorm.DB) {
	// SQLite doesn't support DROP SCHEMA, so we'll just close the connection
	// For PostgreSQL, you would use: db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.Close()
	}
}

