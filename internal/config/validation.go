package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation error [%s]: %s", e.Field, e.Message)
}

// Validate validates the configuration and returns an error if invalid
// This should be called during application startup to fail fast on invalid config
func (c *Config) Validate() error {
	var errors []ValidationError

	// Core required fields
	if c.PostgresDSN == "" {
		errors = append(errors, ValidationError{
			Field:   "POSTGRES_DSN",
			Message: "required for database connection",
		})
	} else {
		// Validate DSN format
		if !strings.HasPrefix(c.PostgresDSN, "postgres://") && !strings.HasPrefix(c.PostgresDSN, "postgresql://") {
			errors = append(errors, ValidationError{
				Field:   "POSTGRES_DSN",
				Message: "must be a valid PostgreSQL connection string",
			})
		}
	}

	// Port validation
	if c.Port == "" {
		errors = append(errors, ValidationError{
			Field:   "PORT",
			Message: "required",
		})
	} else {
		port, err := strconv.Atoi(c.Port)
		if err != nil || port < 1 || port > 65535 {
			errors = append(errors, ValidationError{
				Field:   "PORT",
				Message: "must be a valid port number (1-65535)",
			})
		}
	}

	// Public URL validation (if set)
	if c.PublicURL != "" {
		if _, err := url.Parse(c.PublicURL); err != nil {
			errors = append(errors, ValidationError{
				Field:   "BRANCR_PUBLIC_URL",
				Message: "must be a valid URL",
			})
		}
	}

	// Frontend URL validation (if set)
	if c.FrontendURL != "" {
		if _, err := url.Parse(c.FrontendURL); err != nil {
			errors = append(errors, ValidationError{
				Field:   "FRONTEND_URL",
				Message: "must be a valid URL",
			})
		}
	}

	// Admin credentials validation
	if c.AdminEmail == "" {
		errors = append(errors, ValidationError{
			Field:   "ADMIN_EMAIL",
			Message: "required for admin dashboard access",
		})
	} else if !strings.Contains(c.AdminEmail, "@") {
		errors = append(errors, ValidationError{
			Field:   "ADMIN_EMAIL",
			Message: "must be a valid email address",
		})
	}

	if c.AdminPassword == "" {
		errors = append(errors, ValidationError{
			Field:   "ADMIN_PASSWORD",
			Message: "required for admin dashboard access",
		})
	} else if len(c.AdminPassword) < 8 {
		errors = append(errors, ValidationError{
			Field:   "ADMIN_PASSWORD",
			Message: "must be at least 8 characters long",
		})
	}

	// JWT Secret validation
	if c.TenantJWTSecret == "" {
		errors = append(errors, ValidationError{
			Field:   "TENANT_JWT_SECRET",
			Message: "required for secure tenant authentication",
		})
	} else if len(c.TenantJWTSecret) < 32 {
		errors = append(errors, ValidationError{
			Field:   "TENANT_JWT_SECRET",
			Message: "should be at least 32 characters for security",
		})
	}

	// SMTP validation (if SMTP is configured, validate all fields)
	if c.SMTPHost != "" {
		if c.SMTPPort == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_PORT",
				Message: "required when SMTP_HOST is set",
			})
		} else {
			port, err := strconv.Atoi(c.SMTPPort)
			if err != nil || port < 1 || port > 65535 {
				errors = append(errors, ValidationError{
					Field:   "SMTP_PORT",
					Message: "must be a valid port number (1-65535)",
				})
			}
		}

		if c.SMTPUser == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_USER",
				Message: "required when SMTP_HOST is set",
			})
		}

		if c.SMTPPass == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_PASS",
				Message: "required when SMTP_HOST is set",
			})
		}

		if c.SMTPFrom == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_FROM",
				Message: "required when SMTP_HOST is set",
			})
		} else if !strings.Contains(c.SMTPFrom, "@") {
			errors = append(errors, ValidationError{
				Field:   "SMTP_FROM",
				Message: "must be a valid email address",
			})
		}
	}

	// Meta OAuth validation (if Meta features are enabled)
	if c.MetaRedirectURI != "" {
		if c.MetaAppID == "" || c.MetaAppSecret == "" {
			errors = append(errors, ValidationError{
				Field:   "META_APP_ID/META_APP_SECRET",
				Message: "both required when META_REDIRECT_URI is set",
			})
		}

		if _, err := url.Parse(c.MetaRedirectURI); err != nil {
			errors = append(errors, ValidationError{
				Field:   "META_REDIRECT_URI",
				Message: "must be a valid URL",
			})
		}
	}

	// TikTok OAuth validation (if TikTok features are enabled)
	if c.TikTokRedirectURI != "" {
		if c.TikTokClientKey == "" || c.TikTokClientSecret == "" {
			errors = append(errors, ValidationError{
				Field:   "TIKTOK_CLIENT_KEY/TIKTOK_CLIENT_SECRET",
				Message: "both required when TIKTOK_REDIRECT_URI is set",
			})
		}

		if _, err := url.Parse(c.TikTokRedirectURI); err != nil {
			errors = append(errors, ValidationError{
				Field:   "TIKTOK_REDIRECT_URI",
				Message: "must be a valid URL",
			})
		}
	}

	// Gupshup validation (if Gupshup is configured)
	if c.GupshupPartnerUsername != "" {
		if c.GupshupPartnerPassword == "" {
			errors = append(errors, ValidationError{
				Field:   "GUPSHUP_PARTNER_PASSWORD",
				Message: "required when GUPSHUP_PARTNER_USERNAME is set",
			})
		}

		if c.GupshupAPIBaseURL != "" {
			if _, err := url.Parse(c.GupshupAPIBaseURL); err != nil {
				errors = append(errors, ValidationError{
					Field:   "GUPSHUP_API_BASE_URL",
					Message: "must be a valid URL",
				})
			}
		}
	}

	// Portal token expiry validation
	if c.PortalTokenExpiryDays < 1 || c.PortalTokenExpiryDays > 365 {
		errors = append(errors, ValidationError{
			Field:   "PORTAL_TOKEN_EXPIRY_DAYS",
			Message: "must be between 1 and 365 days",
		})
	}

	// Environment validation
	validEnvs := map[string]bool{
		"development": true,
		"staging":     true,
		"production":  true,
	}
	if !validEnvs[c.AppEnv] {
		errors = append(errors, ValidationError{
			Field:   "APP_ENV",
			Message: fmt.Sprintf("must be one of: development, staging, production (got: %s)", c.AppEnv),
		})
	}

	// Return combined error if any validation failed
	if len(errors) > 0 {
		var messages []string
		for _, err := range errors {
			messages = append(messages, err.Error())
		}
		return fmt.Errorf("configuration validation failed:\n  %s", strings.Join(messages, "\n  "))
	}

	return nil
}

// ValidateRequired validates only the absolutely required fields for basic operation
// This is a lighter check for optional features
func (c *Config) ValidateRequired() error {
	var errors []ValidationError

	if c.PostgresDSN == "" {
		errors = append(errors, ValidationError{
			Field:   "POSTGRES_DSN",
			Message: "required",
		})
	}

	if c.Port == "" {
		errors = append(errors, ValidationError{
			Field:   "PORT",
			Message: "required",
		})
	}

	if c.TenantJWTSecret == "" {
		errors = append(errors, ValidationError{
			Field:   "TENANT_JWT_SECRET",
			Message: "required",
		})
	}

	if len(errors) > 0 {
		var messages []string
		for _, err := range errors {
			messages = append(messages, err.Error())
		}
		return fmt.Errorf("required configuration missing:\n  %s", strings.Join(messages, "\n  "))
	}

	return nil
}

