package main

import (
	"log"
	"os"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	email := os.Getenv("ADMIN_EMAIL")
	password := os.Getenv("ADMIN_PASSWORD")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("DB connect:", err)
	}

	if err := storage.SeedAdminUser(db, email, password); err != nil {
		log.Fatal(err)
	}
}
