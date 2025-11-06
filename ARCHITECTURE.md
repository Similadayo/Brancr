# Brancr Architecture

## Overview
Brancr is a multi-tenant AI automation platform for managing social media through Telegram bots. This document outlines the codebase structure and architectural patterns.

## Directory Structure

```
├── cmd/
│   └── brancr/          # Application entry point
│       └── main.go
├── internal/
│   ├── admin/             # Admin dashboard (web interface)
│   │   ├── auth.go        # Authentication logic
│   │   ├── handler.go     # HTTP handlers
│   │   ├── router.go      # Route definitions
│   │   └── templates/     # HTML templates
│   ├── ai/                # AI/LLM services
│   │   ├── client.go       # AI client (OpenRouter/OpenAI)
│   │   ├── responder.go   # Conversational AI responder
│   │   ├── memory.go      # Conversation memory
│   │   ├── intent.go       # Intent detection
│   │   └── ...
│   ├── bot/               # Telegram bot management
│   │   ├── tenant_handler.go    # Tenant bot handlers
│   │   ├── tenant_router.go     # Bot manager
│   │   ├── onboarding.go        # Onboarding flow
│   │   └── ...
│   ├── config/            # Configuration management
│   │   └── config.go
│   ├── core/              # Core application setup
│   │   ├── app.go          # Main application struct
│   │   ├── services.go     # Service initialization
│   │   ├── router.go        # HTTP router setup
│   │   ├── health.go        # Health check endpoints
│   │   └── tenant_router.go # Tenant routes
│   ├── middleware/         # HTTP middleware
│   │   ├── auth.go         # Authentication middleware
│   │   ├── recovery.go     # Panic recovery
│   │   ├── logging.go      # Request logging
│   │   ├── request_id.go   # Request ID tracking
│   │   └── metrics.go      # Metrics collection
│   ├── social/             # Social media platform integrations
│   │   ├── connector.go    # Platform connector interface
│   │   ├── instagram.go    # Instagram API
│   │   ├── facebook.go     # Facebook API
│   │   ├── scheduler.go    # Post scheduler
│   │   ├── message_handler.go # Message processing
│   │   └── ...
│   ├── storage/            # Database models and operations
│   │   ├── models.go       # GORM models
│   │   ├── migrate.go      # Database migrations
│   │   ├── conversation.go # Conversation storage
│   │   └── repositories/   # Repository pattern implementations
│   │       ├── tenant_repository.go
│   │       ├── customer_repository.go
│   │       ├── social_account_repository.go
│   │       └── scheduled_post_repository.go
│   ├── testutil/           # Testing utilities
│   │   ├── testutil.go     # Test helpers (DB, logger, config)
│   │   └── mocks.go        # Mock implementations
│   ├── utils/              # Utility functions
│   │   ├── security.go     # Security utilities
│   │   ├── schedule.go     # Scheduling utilities
│   │   └── ...
│   └── onboarding/        # Onboarding helpers
│       └── helpers.go
├── pkg/                    # Public packages (interfaces, errors, metrics)
│   ├── interfaces/        # Service interfaces
│   │   ├── ai.go           # AI service interfaces
│   │   ├── bot.go          # Bot service interfaces
│   │   ├── social.go       # Social platform interfaces
│   │   └── storage.go      # Storage interfaces
│   ├── errors/             # Error types
│   │   └── errors.go
│   └── metrics/            # Metrics collection
│       └── metrics.go
└── scripts/                # Utility scripts
    └── seed_admin.go
```

## Core Architecture Patterns

### 1. Service Layer Pattern

Services are initialized in `internal/core/services.go` with proper dependency injection:

```go
type Services struct {
    // Core dependencies
    DB     *gorm.DB
    Redis  *redis.Client
    Logger *zap.Logger
    Config *config.Config
    Metrics *metrics.Metrics
    
    // AI Services
    AIClient          *ai.AIClient
    MemoryService     *ai.MemoryService
    IntentDetector    *ai.IntentDetector
    CustomerResponder *ai.CustomerResponder
    
    // Bot Services
    BotManager        *bot.TenantBotManager
    
    // Social Services
    MessageHandler    *social.MessageHandler
    MetaWebhookHandler *social.MetaWebhookHandler
    Scheduler         *social.Scheduler
}
```

### 2. Dependency Injection

All services are initialized through `NewServices()` which:
1. Initializes AI components first (no dependencies)
2. Initializes Bot Manager (depends on AI)
3. Initializes Social components (depends on AI)
4. Initializes Metrics for observability

### 3. Interface-Based Design

Interfaces are defined in `pkg/interfaces/` to enable:
- Testing with mocks
- Swapping implementations
- Clear service contracts

### 4. Repository Pattern

Data access is abstracted through repositories in `internal/storage/repositories/`:
- `TenantRepository` - Tenant data operations
- `CustomerRepository` - Customer data operations
- `SocialAccountRepository` - Social account operations
- `ScheduledPostRepository` - Scheduled post operations

Benefits:
- Centralized data access logic
- Easier testing with mock repositories
- Consistent error handling
- Future-proof for database changes

### 5. Error Handling

Consistent error types in `pkg/errors/`:
- `ValidationError` - Input validation failures
- `NotFoundError` - Resource not found
- `UnauthorizedError` - Authentication failures
- `InternalError` - Internal server errors
- `ExternalError` - External service failures

### 6. Middleware Organization

Middleware is organized in `internal/middleware/`:
- `auth.go` - JWT authentication
- `recovery.go` - Panic recovery
- `logging.go` - Request logging
- `request_id.go` - Request ID tracking
- `metrics.go` - Metrics collection

### 7. Observability

Metrics are collected through `pkg/metrics/`:
- HTTP request metrics
- AI request metrics
- Bot message metrics
- Error metrics

All middleware is applied globally in `internal/core/router.go`.

### 8. Testing Infrastructure

Test utilities in `internal/testutil/`:
- `TestDB()` - In-memory SQLite database
- `TestLogger()` - Development logger
- `TestConfig()` - Test configuration
- `TestRedis()` - Test Redis client
- Mock implementations for all interfaces

## Package Responsibilities

### `internal/admin/`
- Admin dashboard web interface
- Admin authentication and authorization
- System monitoring and statistics

### `internal/ai/`
- AI client (OpenRouter/OpenAI integration)
- Intent detection
- Conversation memory and context
- Customer response generation
- Caption optimization

### `internal/bot/`
- Telegram bot management
- Tenant bot lifecycle
- Webhook handling
- Onboarding flow
- Media collection

### `internal/social/`
- Platform connectors (Instagram, Facebook, WhatsApp, etc.)
- Post scheduling
- Message handling
- Webhook processing

### `internal/storage/`
- Database models (GORM)
- Database migrations
- Repository implementations

### `internal/core/`
- Application initialization
- Service orchestration
- Router setup
- Health check endpoints
- Lifecycle management

### `internal/middleware/`
- HTTP middleware for cross-cutting concerns
- Request tracking
- Error handling
- Metrics collection

### `pkg/metrics/`
- Metrics collection and aggregation
- Counter and histogram implementations
- Ready for Prometheus/StatsD integration

## Health Checks

The application provides three health check endpoints:

- `/health` - Basic health check (always returns OK)
- `/health/ready` - Readiness probe (checks DB and Redis connectivity)
- `/health/live` - Liveness probe (checks if service is alive)

These endpoints are Kubernetes-ready for containerized deployments.

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/core/...

# Run tests with coverage
go test -cover ./...
```

### Test Structure

- Unit tests for core services
- Repository tests for data access
- Mock implementations for all interfaces
- Test utilities for common setup

## Future Improvements

1. **Event System**: Add event-driven architecture for decoupling
2. **Package Reorganization**: Break down large packages into sub-packages
3. **Configuration**: Move to structured configuration (YAML/TOML)
4. **Observability**: Integrate Prometheus/StatsD for metrics
5. **Tracing**: Add distributed tracing with OpenTelemetry

## Development Guidelines

1. **New Features**: Add services through `Services` struct initialization
2. **Error Handling**: Use `pkg/errors` for consistent error types
3. **Logging**: Use structured logging with `zap.Logger`
4. **Testing**: Define interfaces in `pkg/interfaces/` for testability
5. **Code Organization**: Keep related functionality in the same package
6. **Data Access**: Use repositories instead of direct DB access
7. **Metrics**: Record metrics for all important operations
