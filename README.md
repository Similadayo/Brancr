# Brancr — AI Telegram Control Center

## Overview

Brancr is a multi-tenant AI automation platform that allows businesses to manage their social media directly from Telegram.

Each business operates its own Telegram bot (AI assistant) linked to Brancr's shared backend, which handles AI, media, scheduling, and analytics.

## Core Components

- Telegram Bot Network
- Brancr Core Backend
- Admin Dashboard (Web)
- Cloudinary Storage
- PostgreSQL + Redis Infrastructure

## Architecture

The codebase is organized following clean architecture principles:

- **`cmd/`** - Application entry points
- **`internal/`** - Private application code organized by domain
  - **`core/`** - Core application setup and service orchestration
  - **`ai/`** - AI/LLM services (intent detection, memory, responses)
  - **`bot/`** - Telegram bot management and handlers
  - **`social/`** - Social media platform integrations
  - **`admin/`** - Admin dashboard web interface
  - **`storage/`** - Database models and migrations
  - **`middleware/`** - HTTP middleware (auth, logging, recovery)
  - **`config/`** - Configuration management
  - **`utils/`** - Utility functions
- **`pkg/`** - Public packages (interfaces, errors)
  - **`interfaces/`** - Service interfaces for dependency injection
  - **`errors/`** - Consistent error types

## Key Architectural Patterns

### Service Layer Pattern

All services are initialized through `internal/core/services.go` with proper dependency injection. The `Services` struct holds all initialized services, making dependencies explicit and testable.

### Interface-Based Design

Service interfaces are defined in `pkg/interfaces/` to enable:

- Easy testing with mocks
- Swapping implementations
- Clear service contracts

### Error Handling

Consistent error types in `pkg/errors/` provide structured error handling across the application.

### Middleware Organization

Reusable HTTP middleware in `internal/middleware/` for authentication, logging, and recovery.

## Getting Started

### Prerequisites

- Go 1.25+
- PostgreSQL
- Redis
- Telegram Bot Token
- OpenRouter/OpenAI API Key

### Configuration

Copy `env.example` to `.env` (or export the values in your platform settings) and adjust:

**Core App**

- `APP_ENV` — Runtime environment (`development` | `staging` | `production`)
- `PORT` — HTTP port
- `BRANCR_PUBLIC_URL` — Public API/base URL (e.g. `https://api.brancr.com`)
- `BRANCR_PUBLIC_URL` — Optional public URL used when constructing webhooks/links

**Data Stores**

- `POSTGRES_DSN` — PostgreSQL connection string
- `TEST_POSTGRES_DSN` — Optional DSN for integration tests
- `REDIS_ADDR` — Redis address (optional; leave empty to run without Redis)

**AI Providers**

- `OPENROUTER_MODE` — `free` or `pro`
- `OPENROUTER_API_KEY` — OpenRouter API key
- `OPENROUTER_API_BASE` — Optional custom base URL
- `OPENAI_API_KEY` — Fallback OpenAI key if not using OpenRouter
- `AI_MODEL` — Chat/completions model name (e.g. `gpt-4o-mini`)
- `EMBEDDING_MODEL` — Embedding model name (e.g. `text-embedding-3-small`)

**Cloudinary**

- `CLOUDINARY_URL` — Full Cloudinary connection URL (optional shortcut)
- `CLOUDINARY_CLOUD_NAME`
- `CLOUDINARY_API_KEY`
- `CLOUDINARY_API_SECRET`

**Admin & Security**

- `BRANCR_ADMIN_BOT_TOKEN` — Telegram bot token for the admin assistant
- `ADMIN_EMAIL` / `ADMIN_PASSWORD` — Admin dashboard credentials
- `TOKEN_SECRET_KEY` — 32-byte secret used to encrypt stored access/refresh tokens
- `TENANT_JWT_SECRET` — Secret used to sign tenant dashboard session cookies

**Meta OAuth & Webhooks**

- `META_APP_ID` / `META_APP_SECRET` — Meta app credentials
- `META_REDIRECT_URI` — Callback URL registered in the Meta developer console (e.g. `https://your-domain/api/oauth/meta/callback`)
- `META_SUCCESS_REDIRECT` — Optional post-auth success URL (e.g. admin dashboard)
- `META_WEBHOOK_VERIFY_TOKEN` — Verification token used when registering Meta webhooks
- `WEBHOOK_VERIFY_TOKEN` — Legacy fallback verification token
- `META_WHATSAPP_CONFIG_ID` — WhatsApp Embedded Signup configuration ID (Meta Business Manager)
- `META_WHATSAPP_EXTRAS` — Optional extras JSON for embedded signup (defaults to `{"sessionInfoVersion":"3","version":"v3"}`)

**Scheduling Defaults**

- `DEFAULT_TIMEZONE` — Default timezone used for scheduling posts (e.g. `UTC`)

**Other Integrations**

- `YOUTUBE_API_KEY`
- `TIKTOK_CLIENT_KEY` / `TIKTOK_CLIENT_SECRET`
- `TIKTOK_REDIRECT_URI`
- `TIKTOK_SCOPES` — Optional override (defaults to `user.info.basic,video.list,video.upload`)

### Running

```bash
go run cmd/brancr/main.go
```

### Docker

Build the production image:

```bash
docker build -t brancr-api .
```

Run locally (exposes port 8080 by default):

```bash
docker run --rm -p 8080:8080 --env-file .env brancr-api
```

## Documentation

For detailed architecture documentation, see [ARCHITECTURE.md](./ARCHITECTURE.md).

### Meta OAuth Flow

Meta accounts can now be linked through OAuth:

- `GET /api/oauth/meta/start` — Initiates the Meta OAuth flow (requires `tenant_id` and optional platform hints)
- `GET /api/oauth/meta/callback` — Handles the Meta callback, exchanges tokens, and stores Facebook/Instagram/WhatsApp accounts
- `GET /api/oauth/meta/whatsapp/start` — Redirects to WhatsApp Embedded Signup (Business On Behalf Of) using your Meta config
- `POST /api/oauth/meta/whatsapp/callback` — Receives the embedded signup payload (`waba_id`, `phone_number_id`, token) and persists the WhatsApp account

Ensure the `META_REDIRECT_URI` environment variable exactly matches the callback URL registered in the Meta developer console.

## Development Guidelines

1. **New Features**: Add services through `Services` struct in `internal/core/services.go`
2. **Error Handling**: Use `pkg/errors` for consistent error types
3. **Logging**: Use structured logging with `zap.Logger`
4. **Testing**: Define interfaces in `pkg/interfaces/` for testability
5. **Code Organization**: Keep related functionality in the same package
6. **Data Access**: Use repositories from `internal/storage/repositories/` instead of direct DB access
7. **Metrics**: Record metrics for important operations using `pkg/metrics/`

## Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests for a specific package
go test ./internal/core/...

# Run tests with coverage
go test -cover ./...
```

**Note**: Tests use SQLite which requires CGO. On Windows, you may need to enable CGO:

```bash
# Windows (PowerShell/CMD)
set CGO_ENABLED=1
go test ./...

# Or use the Makefile
make test-cgo

# Alternative: Use PostgreSQL for tests (no CGO required)
# Set TEST_POSTGRES_DSN environment variable
set TEST_POSTGRES_DSN=postgres://user:pass@localhost/testdb
make test-postgres
```

**Note**: If CGO is disabled, tests will gracefully skip with a helpful message instead of failing.

## Health Checks

The application provides health check endpoints:

- `GET /health` - Basic health check
- `GET /health/ready` - Readiness probe (checks DB/Redis)
- `GET /health/live` - Liveness probe

## Metrics

The application collects metrics for:

- HTTP requests and response times
- AI requests and response times
- Bot messages
- Social media posts
- Errors

Metrics are available through the middleware and can be exported to Prometheus/StatsD.
