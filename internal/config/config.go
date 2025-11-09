package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all environment-level configuration for Brancr.
type Config struct {
	AppEnv               string // development | production | staging
	Port                 string // e.g., 8080
	PublicURL            string // e.g. https://api.brancr.com
	OpenRouterMode       string // free | pro
	OpenRouterAPIKey     string
	PostgresDSN          string
	CloudName            string
	CloudAPIKey          string
	CloudAPISecret       string
	RedisAddr            string
	AdminBotToken        string
	AdminEmail           string
	AdminPassword        string
	MetaAppID            string
	MetaAppSecret        string
	MetaRedirectURI      string
	MetaSuccessURL       string
	MetaEmbeddedConfigID string
	MetaEmbeddedExtras   string
	TenantJWTSecret      string
}

// Load loads configuration from .env or system environment variables.
// It never fails hard — logs warnings and provides safe fallbacks.
func Load() *Config {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("ℹ️  No .env file found — using system environment")
	}

	cfg := &Config{
		AppEnv:               getEnv("APP_ENV", "development"),
		Port:                 getEnv("PORT", "8080"),
		PublicURL:            os.Getenv("BRANCR_PUBLIC_URL"),
		OpenRouterMode:       getEnv("OPENROUTER_MODE", "free"),
		OpenRouterAPIKey:     os.Getenv("OPENROUTER_API_KEY"),
		PostgresDSN:          os.Getenv("POSTGRES_DSN"),
		CloudName:            os.Getenv("CLOUDINARY_CLOUD_NAME"),
		CloudAPIKey:          os.Getenv("CLOUDINARY_API_KEY"),
		CloudAPISecret:       os.Getenv("CLOUDINARY_API_SECRET"),
		RedisAddr:            getEnv("REDIS_ADDR", "localhost:6379"),
		AdminBotToken:        os.Getenv("BRANCR_ADMIN_BOT_TOKEN"),
		AdminEmail:           os.Getenv("ADMIN_EMAIL"),
		AdminPassword:        os.Getenv("ADMIN_PASSWORD"),
		MetaAppID:            os.Getenv("META_APP_ID"),
		MetaAppSecret:        os.Getenv("META_APP_SECRET"),
		MetaRedirectURI:      os.Getenv("META_REDIRECT_URI"),
		MetaSuccessURL:       os.Getenv("META_SUCCESS_REDIRECT"),
		MetaEmbeddedConfigID: os.Getenv("META_WHATSAPP_CONFIG_ID"),
		MetaEmbeddedExtras:   os.Getenv("META_WHATSAPP_EXTRAS"),
		TenantJWTSecret:      os.Getenv("TENANT_JWT_SECRET"),
	}

	validateConfig(cfg)
	return cfg
}

// getEnv safely reads environment variables with fallback.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// validateConfig prints gentle warnings for missing required fields.
// This helps catch setup issues during startup.
func validateConfig(cfg *Config) {
	if cfg.PostgresDSN == "" {
		log.Println("⚠️  POSTGRES_DSN not set — database connection may fail.")
	}
	if cfg.AdminBotToken == "" {
		log.Println("⚠️  BRANCR_ADMIN_BOT_TOKEN not set — Telegram bot won't start.")
	}
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		log.Println("⚠️  ADMIN_EMAIL or ADMIN_PASSWORD missing — admin login may fail.")
	}
	if (cfg.MetaAppID == "" || cfg.MetaAppSecret == "") && cfg.MetaRedirectURI != "" {
		log.Println("⚠️  META_APP_ID or META_APP_SECRET missing — Meta OAuth will be disabled.")
	}
	if cfg.MetaRedirectURI == "" {
		log.Println("ℹ️  META_REDIRECT_URI not configured — Meta OAuth routes will remain active but callbacks may fail.")
	}
	if cfg.MetaEmbeddedConfigID == "" {
		log.Println("ℹ️  META_WHATSAPP_CONFIG_ID not set — WhatsApp embedded signup will fallback to sandbox.")
	}
	if cfg.TenantJWTSecret == "" {
		log.Println("⚠️  TENANT_JWT_SECRET missing — tenant authentication cookies will be insecure.")
	}
	log.Printf("🧩 Brancr Config loaded — ENV=%s PORT=%s\n", cfg.AppEnv, cfg.Port)
}
