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
Set environment variables (see `.env.example` or use system environment):
- `POSTGRES_DSN` - PostgreSQL connection string
- `REDIS_ADDR` - Redis address
- `OPENROUTER_API_KEY` - AI API key
- `BRANCR_ADMIN_BOT_TOKEN` - Admin bot token
- `ADMIN_EMAIL` / `ADMIN_PASSWORD` - Admin credentials

### Running
```bash
go run cmd/brancr/main.go
```

## Documentation

For detailed architecture documentation, see [ARCHITECTURE.md](./ARCHITECTURE.md).

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

