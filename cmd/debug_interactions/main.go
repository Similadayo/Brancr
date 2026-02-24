package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Fallback to default local dev string if env not set
		dsn = "host=localhost user=postgres password=postgres dbname=brancr port=5432 sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}

	since := time.Now().Add(-24 * time.Hour)
	until := time.Now()

	fmt.Printf("Checking for interactions between %s and %s\n", since, until)

	var count int64
	err = db.Model(&storage.Interaction{}).
		Where("created_at >= ? AND created_at <= ?", since, until).
		Where("response_type = ?", "auto_reply").
		Where("direction = ?", "outgoing").
		Count(&count).Error

	if err != nil {
		log.Fatal("query failed:", err)
	}

	fmt.Printf("Found %d matching interactions (outgoing auto_replies)\n", count)

	// Also check usage events count
	var usageCount int64
	err = db.Model(&storage.UsageEvent{}).
		Where("event_date >= ? AND event_date <= ?", since, until).
		Where("event_type = ?", "ai_reply_sent").
		Count(&usageCount).Error

	if err != nil {
		fmt.Printf("UsageEvent query failed: %v\n", err)
	} else {
		fmt.Printf("Found %d matching usage events (ai_reply_sent)\n", usageCount)
	}
}
