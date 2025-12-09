package storage

import (
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RunMigrations runs database migrations from the migrations directory
// This should be called after AutoMigrate to handle schema changes that GORM can't handle
func RunMigrations(db *gorm.DB, migrationsPath string, logger *zap.Logger) error {
	// Get underlying SQL database
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB from gorm.DB: %w", err)
	}

	// Create postgres driver instance
	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", migrationsPath),
		"postgres",
		driver,
	)
	if err != nil {
		// If migrations directory doesn't exist or is empty, that's okay
		// We'll just log a warning and continue
		logger.Warn("migrations directory not found or empty, skipping SQL migrations",
			zap.String("path", migrationsPath),
			zap.Error(err))
		return nil
	}

	// Run migrations
	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			logger.Info("database is up to date, no migrations to run")
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		logger.Warn("database is in dirty state, manual intervention may be required",
			zap.Uint("version", version))
	} else {
		logger.Info("migrations completed successfully",
			zap.Uint("version", version))
	}

	return nil
}

// GetMigrationVersion returns the current migration version
func GetMigrationVersion(db *gorm.DB) (uint, bool, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return 0, false, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("failed to create postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres",
		driver,
	)
	if err != nil {
		return 0, false, err
	}

	version, dirty, err := m.Version()
	if err == migrate.ErrNilVersion {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return version, dirty, nil
}

// EnsureMigrationTable ensures the schema_migrations table exists
// This is called automatically by golang-migrate, but we include it for reference
func EnsureMigrationTable(db *gorm.DB) error {
	// golang-migrate automatically creates the schema_migrations table
	// This function is here for documentation purposes
	return nil
}

// CreateMigrationTable manually creates the migration tracking table (if needed)
func CreateMigrationTable(db *gorm.DB) error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT NOT NULL PRIMARY KEY,
		dirty BOOLEAN NOT NULL
	);
	`
	return db.Exec(createTableSQL).Error
}

// GetMigrationStatus returns migration status information
func GetMigrationStatus(db *gorm.DB) (map[string]interface{}, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	var version sql.NullInt64
	var dirty sql.NullBool

	err = sqlDB.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return map[string]interface{}{
			"version": nil,
			"dirty":   false,
			"status":  "no_migrations",
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query migration status: %w", err)
	}

	status := "clean"
	if dirty.Valid && dirty.Bool {
		status = "dirty"
	}

	return map[string]interface{}{
		"version": version.Int64,
		"dirty":   dirty.Bool,
		"status":  status,
	}, nil
}

