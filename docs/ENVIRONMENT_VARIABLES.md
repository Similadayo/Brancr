# Environment Variables Reference

Complete reference for all Brancr environment variables.

## Required Variables

These variables are **required** for the application to start:

| Variable | Description | Example |
|----------|-------------|---------|
| `POSTGRES_DSN` | PostgreSQL connection string | `postgres://user:pass@localhost:5432/brancr?sslmode=disable` |
| `PORT` | HTTP server port | `8080` |
| `TENANT_JWT_SECRET` | Secret for signing tenant JWT tokens (min 32 chars) | `your-secret-key-at-least-32-characters-long` |

## Core Application

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `APP_ENV` | Environment: `development`, `staging`, `production` | `development` | No |
| `BRANCR_PUBLIC_URL` | Public API URL for webhooks/links | - | No |
| `FRONTEND_URL` | Frontend URL for OAuth redirects | - | No |

## Database

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `POSTGRES_DSN` | PostgreSQL connection string | - | **Yes** |
| `REDIS_ADDR` | Redis address (host:port or redis:// URL) | - | No |

## Admin & Security

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `ADMIN_EMAIL` | Admin dashboard email | - | **Yes** |
| `ADMIN_PASSWORD` | Admin dashboard password (min 8 chars) | - | **Yes** |
| `BRANCR_ADMIN_BOT_TOKEN` | Telegram bot token for admin onboarding | - | No |
| `TENANT_JWT_SECRET` | JWT secret for tenant sessions (min 32 chars) | - | **Yes** |
| `TENANT_COOKIE_DOMAIN` | Cookie domain for tenant sessions | `api.brancr.com` | No |
| `CORS_ALLOWED_ORIGINS` | Comma-separated CORS origins | `http://localhost:3000` | No |

## AI Services

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `OPENAI_API_KEY` | OpenAI API key | - | No |
| `AI_MODEL` | AI model name | - | No |
| `EMBEDDING_MODEL` | Embedding model name | - | No |

## Cloudinary (Media Storage)

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CLOUDINARY_CLOUD_NAME` | Cloudinary cloud name | - | No |
| `CLOUDINARY_API_KEY` | Cloudinary API key | - | No |
| `CLOUDINARY_API_SECRET` | Cloudinary API secret | - | No |
| `CLOUDINARY_URL` | Full Cloudinary URL (alternative to above) | - | No |

## Meta/Facebook OAuth

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `META_APP_ID` | Meta App ID | - | No* |
| `META_APP_SECRET` | Meta App Secret | - | No* |
| `META_REDIRECT_URI` | OAuth callback URL | - | No |
| `META_SUCCESS_REDIRECT` | Post-OAuth success URL | - | No |
| `META_BUSINESS_ID` | Meta Business ID | - | No |
| `META_SYSTEM_USER_TOKEN` | System user token for webhooks | - | No |
| `META_SYSTEM_USER_ID` | System User ID | - | No |
| `META_PAYMENT_METHOD_ID` | Payment method ID for billing | - | No |
| `META_EXTENDED_CREDIT_ID` | Extended Credit Line ID | - | No |
| `META_WHATSAPP_CONFIG_ID` | WhatsApp Embedded Signup Config ID | - | No |
| `META_WHATSAPP_EXTRAS` | WhatsApp Embedded Signup extras JSON | - | No |

*Required if `META_REDIRECT_URI` is set

## Instagram OAuth (Business Login)

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `INSTAGRAM_APP_ID` | Instagram App ID (separate from Meta) | - | No |
| `INSTAGRAM_APP_SECRET` | Instagram App Secret (separate from Meta) | - | No |
| `INSTAGRAM_REDIRECT_URI` | Instagram OAuth callback URL | `META_REDIRECT_URI` | No |

## TikTok OAuth

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `TIKTOK_CLIENT_KEY` | TikTok Client Key | - | No |
| `TIKTOK_CLIENT_SECRET` | TikTok Client Secret | - | No |
| `TIKTOK_REDIRECT_URI` | TikTok OAuth callback URL | - | No |
| `TIKTOK_SCOPES` | TikTok OAuth scopes | `user.info.basic,video.publish,video.upload` | No |
| `TIKTOK_DEFAULT_PRIVACY_LEVEL` | Default privacy for videos | - | No |
| `TIKTOK_WEBHOOK_SECRET` | Secret for verifying webhook signatures | - | No |

## WhatsApp Integrations

### Respond.io

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `RESPOND_IO_API_KEY` | Respond.io API access token | - | No |

### Gupshup

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `GUPSHUP_PARTNER_USERNAME` | Gupshup Partner username/email | - | No |
| `GUPSHUP_PARTNER_PASSWORD` | Gupshup Partner password or client secret | - | No |
| `GUPSHUP_PARTNER_APP_ID` | Gupshup Partner App ID | - | No |
| `GUPSHUP_API_BASE_URL` | Gupshup API base URL | `https://partner.gupshup.io` | No |

### Meta WhatsApp

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `BRANCR_WABA_ID` | Brancr's provider-owned WABA ID | - | No |
| `BRANCR_WABA_TOKEN` | Access token for Brancr's WABA | - | No |

## Email (SMTP)

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SMTP_HOST` | SMTP server hostname | - | No |
| `SMTP_PORT` | SMTP server port | `587` | No |
| `SMTP_USER` | SMTP username | - | No |
| `SMTP_PASS` | SMTP password | - | No |
| `SMTP_FROM` | From email address | `noreply@brancr.com` | No |

## Other

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `PORTAL_TOKEN_EXPIRY_DAYS` | Portal token expiration (days) | `90` | No |

## Environment-Specific Notes

### Development
- Use local PostgreSQL and Redis
- Set `APP_ENV=development`
- `CORS_ALLOWED_ORIGINS` should include `http://localhost:3000`

### Staging
- Use staging database
- Set `APP_ENV=staging`
- Configure staging OAuth redirect URIs

### Production
- Use production database with SSL
- Set `APP_ENV=production`
- Ensure all secrets are properly secured
- Use strong `TENANT_JWT_SECRET` (32+ characters)
- Configure production OAuth redirect URIs
- Set up monitoring and alerts

## Validation

The application validates required variables on startup:
- Missing required variables will cause startup to fail
- Missing optional variables will log warnings but continue
- Invalid values (e.g., invalid URLs, ports) will cause startup to fail

## Security Best Practices

1. **Never commit `.env` files** to version control
2. **Use secrets management** (AWS Secrets Manager, HashiCorp Vault, etc.)
3. **Rotate secrets regularly**, especially JWT secrets
4. **Use different secrets** for each environment
5. **Limit access** to production environment variables
6. **Monitor** for exposed secrets in logs

## Example `.env` File

```bash
# Core
APP_ENV=development
PORT=8080
BRANCR_PUBLIC_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000

# Database (Required)
POSTGRES_DSN=postgres://user:password@localhost:5432/brancr?sslmode=disable
REDIS_ADDR=localhost:6379

# Security (Required)
TENANT_JWT_SECRET=your-super-secret-key-at-least-32-characters-long-for-security
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=secure-password-123

# AI
OPENAI_API_KEY=sk-...

# Cloudinary
CLOUDINARY_CLOUD_NAME=your-cloud-name
CLOUDINARY_API_KEY=your-api-key
CLOUDINARY_API_SECRET=your-api-secret

# Meta OAuth (Optional)
META_APP_ID=your-app-id
META_APP_SECRET=your-app-secret
META_REDIRECT_URI=http://localhost:8080/api/oauth/meta/callback

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3001
```

