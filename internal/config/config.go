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
	MetaSystemUserToken  string
	MetaBusinessID       string
	MetaPaymentMethodID  string // Payment method ID for Brancr's billing (when Payment Processing API access is granted)
	BrancrWABAID         string // Brancr's provider-owned WABA ID
	BrancrWABAToken      string // Access token for Brancr's WABA (system user token)
	TenantJWTSecret      string
	TikTokClientKey      string
	TikTokClientSecret   string
	TikTokRedirectURI    string
	TikTokScopes         string
	TikTokDefaultPrivacy string
	CORSAllowedOrigins   string
	TenantCookieDomain   string
	RespondIOAPIKey      string // Respond.io API access token for WhatsApp integration
	FrontendURL           string // Frontend URL for redirects after OAuth
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
		RedisAddr:            getEnv("REDIS_ADDR", ""),
		AdminBotToken:        os.Getenv("BRANCR_ADMIN_BOT_TOKEN"),
		AdminEmail:           os.Getenv("ADMIN_EMAIL"),
		AdminPassword:        os.Getenv("ADMIN_PASSWORD"),
		MetaAppID:            os.Getenv("META_APP_ID"),
		MetaAppSecret:        os.Getenv("META_APP_SECRET"),
		MetaRedirectURI:      os.Getenv("META_REDIRECT_URI"),
		MetaSuccessURL:       os.Getenv("META_SUCCESS_REDIRECT"),
		MetaEmbeddedConfigID: os.Getenv("META_WHATSAPP_CONFIG_ID"),
		MetaEmbeddedExtras:   os.Getenv("META_WHATSAPP_EXTRAS"),
		MetaSystemUserToken:  getEnv("META_SYSTEM_USER_TOKEN", os.Getenv("SYSTEM_USER_TOKEN")),
		MetaBusinessID:       os.Getenv("META_BUSINESS_ID"),
		MetaPaymentMethodID:  os.Getenv("META_PAYMENT_METHOD_ID"), // Required for billing setup
		BrancrWABAID:         os.Getenv("BRANCR_WABA_ID"),         // Provider-owned WABA ID
		BrancrWABAToken:      os.Getenv("BRANCR_WABA_TOKEN"),      // WABA access token
		TenantJWTSecret:      os.Getenv("TENANT_JWT_SECRET"),
		TikTokClientKey:      os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret:   os.Getenv("TIKTOK_CLIENT_SECRET"),
		TikTokRedirectURI:    os.Getenv("TIKTOK_REDIRECT_URI"),
		TikTokScopes:         getEnv("TIKTOK_SCOPES", "user.info.basic,video.list,video.upload,video.publish"),
		TikTokDefaultPrivacy: os.Getenv("TIKTOK_DEFAULT_PRIVACY_LEVEL"),
		CORSAllowedOrigins:   getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),
		TenantCookieDomain:   getEnv("TENANT_COOKIE_DOMAIN", "api.brancr.com"),
		RespondIOAPIKey:      os.Getenv("RESPOND_IO_API_KEY"), // Respond.io API access token
		FrontendURL:           os.Getenv("FRONTEND_URL"),
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
	if cfg.MetaSystemUserToken == "" {
		log.Println("ℹ️  META_SYSTEM_USER_TOKEN not set — WhatsApp webhook subscription may require manual setup.")
	}
	if cfg.MetaPaymentMethodID == "" {
		log.Println("ℹ️  META_PAYMENT_METHOD_ID not set — WhatsApp billing setup will be skipped. This may prevent ownership claims from succeeding.")
	}
	if cfg.TenantJWTSecret == "" {
		log.Println("⚠️  TENANT_JWT_SECRET missing — tenant authentication cookies will be insecure.")
	}
	if cfg.TikTokClientKey == "" || cfg.TikTokClientSecret == "" {
		log.Println("ℹ️  TikTok credentials not configured — TikTok OAuth will be disabled.")
	}
	if cfg.TikTokRedirectURI == "" {
		log.Println("ℹ️  TIKTOK_REDIRECT_URI not configured — TikTok OAuth callbacks will fail.")
	}
	if cfg.RespondIOAPIKey == "" {
		log.Println("ℹ️  RESPOND_IO_API_KEY not set — Respond.io WhatsApp integration will be disabled.")
	}
	log.Printf("🧩 Brancr Config loaded — ENV=%s PORT=%s\n", cfg.AppEnv, cfg.Port)
}
