package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all environment-level configuration for Brancr.
type Config struct {
	AppEnv               string // development | production | staging
	Port                 string // e.g., 8080
	PublicURL            string // e.g. https://api.brancr.com
	// OpenRouter removed - using OpenAI direct API only
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
	InstagramAppID       string // Instagram App ID for Business Login (separate from Meta App ID)
	InstagramAppSecret  string // Instagram App Secret for Business Login (separate from Meta App Secret)
	InstagramRedirectURI string // Instagram OAuth redirect URI (can be same as MetaRedirectURI)
	MetaEmbeddedConfigID string
	MetaEmbeddedExtras   string
	MetaSystemUserToken  string
	MetaBusinessID       string
	MetaPaymentMethodID  string // Payment method ID for Brancr's billing (when Payment Processing API access is granted)
	MetaExtendedCreditID string // Extended Credit Line ID for Solution Partner billing model
	MetaSystemUserID     string // System User ID (needed to add to WABAs)
	BrancrWABAID         string // Brancr's provider-owned WABA ID
	BrancrWABAToken      string // Access token for Brancr's WABA (system user token)
	TenantJWTSecret      string
	TikTokClientKey      string
	TikTokClientSecret   string
	TikTokRedirectURI    string
	TikTokScopes         string
	TikTokDefaultPrivacy string
	TikTokWebhookSecret  string // Secret for verifying TikTok webhook signatures
	CORSAllowedOrigins   string
	TenantCookieDomain   string
	RespondIOAPIKey      string // Respond.io API access token for WhatsApp integration
	FrontendURL           string // Frontend URL for redirects after OAuth
	GupshupPartnerUsername string // Gupshup Partner username/email for authentication
	GupshupPartnerPassword string // Gupshup Partner password OR client secret (from Partner Portal Settings)
	// For accounts after Nov 20, 2024: use Client Secret (recommended)
	// For older accounts: use actual account password
	GupshupPartnerAppID   string // Gupshup Partner App ID (single app for all tenants, optional)
	GupshupAPIBaseURL      string // Gupshup API base URL
	SMTPHost              string // SMTP server host
	SMTPPort              string // SMTP server port
	SMTPUser              string // SMTP username
	SMTPPass              string // SMTP password
	SMTPFrom              string // SMTP from email address
	PortalTokenExpiryDays int    // Portal token expiration in days (default: 90)
	DBMaxOpenConns        int    // Database max open connections (default: 100)
	DBMaxIdleConns        int    // Database max idle connections (default: 10)
	WorkerPoolSize        int    // Worker pool size for background jobs (default: 20)
	WorkerQueueSize       int    // Worker pool queue size (default: 100)
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
		// OpenRouter removed - using OpenAI direct API only
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
		InstagramAppID:       os.Getenv("INSTAGRAM_APP_ID"),
		InstagramAppSecret:   os.Getenv("INSTAGRAM_APP_SECRET"),
		InstagramRedirectURI: getEnv("INSTAGRAM_REDIRECT_URI", os.Getenv("META_REDIRECT_URI")), // Defaults to MetaRedirectURI if not set
		MetaEmbeddedConfigID: os.Getenv("META_WHATSAPP_CONFIG_ID"),
		MetaEmbeddedExtras:   os.Getenv("META_WHATSAPP_EXTRAS"),
		MetaSystemUserToken:  getEnv("META_SYSTEM_USER_TOKEN", os.Getenv("SYSTEM_USER_TOKEN")),
		MetaBusinessID:       os.Getenv("META_BUSINESS_ID"),
		MetaPaymentMethodID:  os.Getenv("META_PAYMENT_METHOD_ID"), // Required for billing setup
		MetaExtendedCreditID: os.Getenv("META_EXTENDED_CREDIT_ID"), // Extended Credit Line ID for Solution Partner billing
		MetaSystemUserID:     os.Getenv("META_SYSTEM_USER_ID"),     // System User ID for adding to WABAs
		BrancrWABAID:         os.Getenv("BRANCR_WABA_ID"),         // Provider-owned WABA ID
		BrancrWABAToken:      os.Getenv("BRANCR_WABA_TOKEN"),      // WABA access token
		TenantJWTSecret:      os.Getenv("TENANT_JWT_SECRET"),
		TikTokClientKey:      os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret:   os.Getenv("TIKTOK_CLIENT_SECRET"),
		TikTokRedirectURI:    os.Getenv("TIKTOK_REDIRECT_URI"),
		TikTokScopes:         getEnv("TIKTOK_SCOPES", "user.info.basic,video.publish,video.upload"),
		TikTokDefaultPrivacy: os.Getenv("TIKTOK_DEFAULT_PRIVACY_LEVEL"),
		TikTokWebhookSecret:  os.Getenv("TIKTOK_WEBHOOK_SECRET"),
		CORSAllowedOrigins:   getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),
		TenantCookieDomain:   getEnv("TENANT_COOKIE_DOMAIN", "api.brancr.com"),
		RespondIOAPIKey:      os.Getenv("RESPOND_IO_API_KEY"), // Respond.io API access token
		FrontendURL:           os.Getenv("FRONTEND_URL"),
		GupshupPartnerUsername: os.Getenv("GUPSHUP_PARTNER_USERNAME"),
		GupshupPartnerPassword: os.Getenv("GUPSHUP_PARTNER_PASSWORD"),
		GupshupPartnerAppID:   os.Getenv("GUPSHUP_PARTNER_APP_ID"),
		GupshupAPIBaseURL:      getEnv("GUPSHUP_API_BASE_URL", "https://partner.gupshup.io"),
		SMTPHost:              os.Getenv("SMTP_HOST"),
		SMTPPort:              getEnv("SMTP_PORT", "587"),
		SMTPUser:              os.Getenv("SMTP_USER"),
		SMTPPass:              os.Getenv("SMTP_PASS"),
		SMTPFrom:              getEnv("SMTP_FROM", "noreply@brancr.com"),
		PortalTokenExpiryDays: getEnvInt("PORTAL_TOKEN_EXPIRY_DAYS", 90),
		DBMaxOpenConns:        getEnvInt("DB_MAX_OPEN_CONNS", 100),
		DBMaxIdleConns:        getEnvInt("DB_MAX_IDLE_CONNS", 10),
		WorkerPoolSize:        getEnvInt("WORKER_POOL_SIZE", 20),
		WorkerQueueSize:       getEnvInt("WORKER_QUEUE_SIZE", 100),
	}

	// Run validation (warnings only, doesn't fail)
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

// getEnvInt safely reads integer environment variables with fallback.
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var result int
		if _, err := fmt.Sscanf(v, "%d", &result); err == nil {
			return result
		}
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
	if cfg.MetaExtendedCreditID == "" {
		log.Println("ℹ️  META_EXTENDED_CREDIT_ID not set — Credit sharing (Solution Partner billing) will be skipped. Tenants will be billed directly by Meta.")
	}
	if cfg.MetaSystemUserID == "" {
		log.Println("ℹ️  META_SYSTEM_USER_ID not set — System user won't be added to WABAs automatically. This may cause API permission issues.")
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
