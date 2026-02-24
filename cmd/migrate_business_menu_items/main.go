package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// migrateBusinessMenuItems migrates BusinessMenuItem records to industry-specific types
// Usage: go run ./cmd/migrate_business_menu_items [-dry-run]
func main() {
	var dsn string
	var dryRun bool
	flag.StringVar(&dsn, "dsn", "", "PostgreSQL connection string (or use DATABASE_URL env var)")
	flag.BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	flag.Parse()

	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		cfg := config.Load()
		dsn = cfg.PostgresDSN
	}

	if dsn == "" {
		log.Fatal("❌ DATABASE_URL, POSTGRES_DSN, or -dsn flag required")
	}

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("🔌 Connecting to database...")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("❌ Failed to get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("❌ Database ping failed: %v", err)
	}
	logger.Info("✅ Database connected")

	// Initialize industry service
	industryService := industry.NewService(db, logger)

	ctx := context.Background()

	// Get all BusinessMenuItem records
	var businessMenuItems []storage.BusinessMenuItem
	if err := db.Find(&businessMenuItems).Error; err != nil {
		log.Fatalf("❌ Failed to query BusinessMenuItem records: %v", err)
	}

	logger.Info("📊 Found BusinessMenuItem records to migrate",
		zap.Int("count", len(businessMenuItems)))

	if len(businessMenuItems) == 0 {
		logger.Info("✅ No BusinessMenuItem records found - migration already complete or not needed")
		fmt.Println("✅ No BusinessMenuItem records to migrate. Migration complete!")
		return
	}

	if dryRun {
		fmt.Println("🔍 DRY RUN MODE - No changes will be applied")
		fmt.Println("=" + strings.Repeat("=", 60))
	}

	migratedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, bmi := range businessMenuItems {
		// Get tenant's industry capabilities
		capabilities, err := industryService.GetIndustryCapabilities(ctx, bmi.TenantID)
		if err != nil {
			logger.Warn("Failed to get industry capabilities for tenant",
				zap.Stringer("tenant_id", bmi.TenantID),
				zap.Error(err))
			errorCount++
			continue
		}

		// Determine target type based on industry capabilities
		var targetType string
		if capabilities.HasServices {
			targetType = "service"
		} else if capabilities.HasProducts {
			targetType = "product"
		} else if capabilities.HasMenu {
			targetType = "menu"
		} else {
			// Default to menu if no capabilities (backward compatibility)
			targetType = "menu"
			logger.Info("No industry capabilities found, defaulting to menu",
				zap.Stringer("tenant_id", bmi.TenantID))
		}

		// Parse price from string
		price := float64(0)
		if bmi.Price != "" {
			priceStr := strings.ReplaceAll(bmi.Price, "₦", "")
			priceStr = strings.ReplaceAll(priceStr, ",", "")
			priceStr = strings.ReplaceAll(priceStr, " ", "")
			if parsed, err := strconv.ParseFloat(priceStr, 64); err == nil {
				price = parsed
			}
		}

		// Parse tags
		var tags []string
		if bmi.Tags != "" {
			tags = strings.Split(bmi.Tags, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}
		}
		tagsJSON, _ := json.Marshal(tags)
		category := ""
		if len(tags) > 0 {
			category = tags[0] // Use first tag as category
		}

		if dryRun {
			fmt.Printf("\n📦 BusinessMenuItem ID: %s\n", bmi.ID)
			fmt.Printf("   Tenant ID: %s\n", bmi.TenantID)
			fmt.Printf("   Name: %s\n", bmi.Name)
			fmt.Printf("   Price: %s -> %.2f\n", bmi.Price, price)
			fmt.Printf("   Target Type: %s\n", targetType)
			fmt.Printf("   Category: %s\n", category)
			continue
		}

		// Create the appropriate type
		switch targetType {
		case "product":
			product := storage.Product{
				TenantID:        bmi.TenantID,
				Name:            bmi.Name,
				Description:     bmi.Description,
				Price:           price,
				Currency:        "NGN",
				Category:        category,
				Tags:            string(tagsJSON),
				IsActive:        true,
				Availability:    "in_stock",
				NegotiationMode: bmi.NegotiationMode,
			}
			if bmi.NegotiationMinPrice > 0 {
				product.NegotiationMinPrice = bmi.NegotiationMinPrice
			}
			if bmi.NegotiationMaxPrice > 0 {
				product.NegotiationMaxPrice = bmi.NegotiationMaxPrice
			} else {
				product.NegotiationMaxPrice = price
			}

			if err := db.Create(&product).Error; err != nil {
				logger.Error("Failed to create Product",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Info("✅ Migrated BusinessMenuItem to Product",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("product_id", product.ID),
				zap.Stringer("tenant_id", bmi.TenantID))

		case "service":
			// For services, create fixed pricing
			pricing := map[string]interface{}{
				"type":   "fixed",
				"amount": price,
			}
			pricingJSON, _ := json.Marshal(pricing)

			service := storage.Service{
				TenantID:    bmi.TenantID,
				Name:        bmi.Name,
				Description: bmi.Description,
				Category:    category,
				Pricing:     string(pricingJSON),
				IsActive:    true,
			}

			if err := db.Create(&service).Error; err != nil {
				logger.Error("Failed to create Service",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Info("✅ Migrated BusinessMenuItem to Service",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("service_id", service.ID),
				zap.Stringer("tenant_id", bmi.TenantID))

		case "menu":
			fallthrough
		default:
			// Parse dietary info from tags
			dietaryJSON, _ := json.Marshal(tags)

			menuItem := storage.MenuItem{
				TenantID:        bmi.TenantID,
				Name:            bmi.Name,
				Description:     bmi.Description,
				Price:           price,
				Currency:        "NGN",
				Category:        category,
				DietaryInfo:     string(dietaryJSON),
				IsActive:        true,
				Availability:    "available",
				NegotiationMode: bmi.NegotiationMode,
			}
			if bmi.NegotiationMinPrice > 0 {
				menuItem.NegotiationMinPrice = bmi.NegotiationMinPrice
			}
			if bmi.NegotiationMaxPrice > 0 {
				menuItem.NegotiationMaxPrice = bmi.NegotiationMaxPrice
			} else {
				menuItem.NegotiationMaxPrice = price
			}

			if err := db.Create(&menuItem).Error; err != nil {
				logger.Error("Failed to create MenuItem",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Info("✅ Migrated BusinessMenuItem to MenuItem",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("menu_item_id", menuItem.ID),
				zap.Stringer("tenant_id", bmi.TenantID))
		}
	}

	if dryRun {
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Printf("📊 Summary: %d records would be migrated\n", len(businessMenuItems))
		fmt.Println("Run without -dry-run to apply changes")
	} else {
		logger.Info("✅ Migration complete",
			zap.Int("total", len(businessMenuItems)),
			zap.Int("migrated", migratedCount),
			zap.Int("skipped", skippedCount),
			zap.Int("errors", errorCount))
		fmt.Printf("\n✅ Migration complete!\n")
		fmt.Printf("   Total records: %d\n", len(businessMenuItems))
		fmt.Printf("   Migrated: %d\n", migratedCount)
		fmt.Printf("   Skipped: %d\n", skippedCount)
		fmt.Printf("   Errors: %d\n", errorCount)
		fmt.Printf("\n⚠️  Note: BusinessMenuItem records are still in the database.\n")
		fmt.Printf("   After verifying the migration, you can drop the business_menu_items table.\n")
	}
}
