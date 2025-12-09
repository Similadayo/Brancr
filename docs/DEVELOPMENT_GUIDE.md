# Development Guide

This guide covers setting up and working with the Brancr codebase for development.

## Prerequisites

- **Go 1.21+**: [Install Go](https://golang.org/doc/install)
- **Docker & Docker Compose**: [Install Docker](https://docs.docker.com/get-docker/)
- **PostgreSQL 15+**: (Optional, Docker Compose provides this)
- **Redis 7+**: (Optional, Docker Compose provides this)
- **golang-migrate**: (Optional, for manual migrations)
  ```bash
  go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
  ```
- **golangci-lint**: (Optional, for linting)
  ```bash
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
  ```
- **swag**: (Optional, for Swagger docs)
  ```bash
  go install github.com/swaggo/swag/cmd/swag@latest
  ```

## Quick Start

### 1. Clone and Setup

```bash
# Clone the repository
git clone <repository-url>
cd Brancr

# Run setup script (Linux/Mac)
bash scripts/setup-dev.sh

# Or on Windows (PowerShell)
.\scripts\setup-dev.ps1
```

The setup script will:
- Check prerequisites
- Create `.env` file with default values
- Start PostgreSQL and Redis via Docker Compose
- Run database migrations
- Install Go dependencies

### 2. Configure Environment

Edit `.env` file with your configuration:

```env
APP_ENV=development
PORT=8080
POSTGRES_DSN=postgres://brancr:brancr_dev_password@localhost:5432/brancr?sslmode=disable
REDIS_ADDR=localhost:6379
TENANT_JWT_SECRET=dev-jwt-secret-key-must-be-at-least-32-characters-long-for-security
# Add your API keys...
```

### 3. Run the Application

```bash
# Option 1: Direct run
go run ./cmd/brancr

# Option 2: Build and run
make build
./bin/brancr

# Option 3: With hot reload (requires air)
make dev
```

### 4. Verify Setup

- **Health Check**: http://localhost:8080/health
- **API Docs**: http://localhost:8080/api/docs/
- **Metrics**: http://localhost:8080/metrics

## Project Structure

```
Brancr/
├── cmd/
│   └── brancr/          # Application entry point
├── internal/
│   ├── admin/           # Admin dashboard handlers
│   ├── ai/              # AI services (caption generation, etc.)
│   ├── analytics/       # Analytics and insights
│   ├── bot/             # Telegram bot handlers
│   ├── config/          # Configuration management
│   ├── core/            # Core application setup
│   ├── middleware/      # HTTP middleware
│   ├── social/          # Social platform integrations
│   ├── storage/         # Database models and migrations
│   ├── tenantapi/       # Tenant API handlers
│   └── testutil/        # Testing utilities
├── migrations/          # Database migration files
├── docs/                # Documentation
├── scripts/             # Utility scripts
└── Makefile             # Build and development commands
```

## Development Workflow

### Running Tests

```bash
# Run all tests
make test

# Run tests for specific package
go test ./internal/tenantapi

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run go vet
make vet

# Run all checks
make dev-setup
```

### Database Migrations

```bash
# Create a new migration
make migrate-create NAME=add_new_column

# Run migrations
make migrate

# Rollback last migration
make migrate-down

# Check migration version
make migrate-version
```

### API Documentation

```bash
# Generate Swagger docs
make swagger

# Or manually
swag init -g cmd/brancr/main.go
```

## Docker Development

### Start Services

```bash
# Start PostgreSQL and Redis
make docker-up

# Or manually
docker-compose up -d postgres redis
```

### View Logs

```bash
make docker-logs

# Or for specific service
docker-compose logs -f brancr
```

### Stop Services

```bash
make docker-down
```

## Code Style

### Go Conventions

- Use `gofmt` for formatting (run `make fmt`)
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use meaningful variable and function names
- Add comments for exported functions and types

### Error Handling

- Always handle errors explicitly
- Use `pkg/errors` for error wrapping
- Return descriptive error messages
- Log errors with context

Example:
```go
if err != nil {
    logger.Error("failed to create post",
        zap.Error(err),
        zap.Uint("tenant_id", tenantID))
    return fmt.Errorf("create post: %w", err)
}
```

### Logging

- Use structured logging with `zap`
- Include correlation IDs in logs
- Use appropriate log levels:
  - `Debug`: Detailed information (dev only)
  - `Info`: General information
  - `Warn`: Warnings (slow queries, etc.)
  - `Error`: Errors that need attention

Example:
```go
logger.Info("post created",
    zap.Uint("post_id", post.ID),
    zap.Uint("tenant_id", tenantID),
    zap.String("request_id", requestID))
```

## Testing

### Unit Tests

Create tests in `*_test.go` files:

```go
func TestHandler_CreatePost(t *testing.T) {
    // Setup
    db := testutil.SetupTestDB(t)
    handler := NewHandler(db, logger, config, ...)
    
    // Test
    req := testutil.CreateTestRequest(t, "POST", "/api/posts", body)
    w := testutil.TestRequest(t, handler, req)
    
    // Assert
    assert.Equal(t, http.StatusOK, w.Code)
}
```

### Integration Tests

Use `testutil` package for integration tests:

```go
func TestPostCreationFlow(t *testing.T) {
    db := testutil.SetupTestDB(t)
    tenant := testutil.CreateTestTenant(t, db)
    
    // Test full flow
    // ...
}
```

## Debugging

### Enable Debug Logging

Set in `.env`:
```env
APP_ENV=development
LOG_LEVEL=debug
```

### Database Query Logging

In development, all queries are logged. In production, only slow queries (>1s) are logged.

### Request Tracing

Every request gets a unique `X-Request-ID` header. Use this to trace requests across logs:

```bash
# Search logs for specific request
grep "abc-123-def" logs/app.log
```

## Common Tasks

### Adding a New API Endpoint

1. **Add route** in `internal/tenantapi/handler.go`:
   ```go
   api.Post("/api/new-endpoint", h.newHandler)
   ```

2. **Implement handler**:
   ```go
   func (h *Handler) newHandler(w http.ResponseWriter, r *http.Request) {
       // Implementation
   }
   ```

3. **Add Swagger annotations**:
   ```go
   // @Summary New endpoint
   // @Description Description
   // @Tags posts
   // @Accept json
   // @Produce json
   // @Success 200 {object} ResponseType
   // @Router /api/new-endpoint [post]
   ```

4. **Regenerate docs**: `make swagger`

### Adding a New Database Model

1. **Create model** in `internal/storage/models.go`:
   ```go
   type NewModel struct {
       ID        uint      `gorm:"primaryKey"`
       CreatedAt time.Time
       // Fields...
   }
   ```

2. **Run AutoMigrate** (automatic on startup) or create migration:
   ```bash
   make migrate-create NAME=create_new_model
   ```

3. **Add to AutoMigrate** in `internal/storage/migrations.go`:
   ```go
   db.AutoMigrate(&NewModel{})
   ```

### Adding Middleware

1. **Create middleware** in `internal/middleware/`:
   ```go
   func NewMiddleware() func(http.Handler) http.Handler {
       return func(next http.Handler) http.Handler {
           return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
               // Middleware logic
               next.ServeHTTP(w, r)
           })
       }
   }
   ```

2. **Add to router** in `internal/core/router.go`:
   ```go
   r.Use(middleware.NewMiddleware())
   ```

## Troubleshooting

### Database Connection Issues

```bash
# Check PostgreSQL is running
docker-compose ps postgres

# Check connection
psql postgres://brancr:brancr_dev_password@localhost:5432/brancr

# View PostgreSQL logs
docker-compose logs postgres
```

### Redis Connection Issues

```bash
# Check Redis is running
docker-compose ps redis

# Test connection
redis-cli -h localhost -p 6379 ping

# View Redis logs
docker-compose logs redis
```

### Port Already in Use

```bash
# Find process using port 8080
lsof -i :8080  # Mac/Linux
netstat -ano | findstr :8080  # Windows

# Change port in .env
PORT=8081
```

### Migration Errors

```bash
# Check current migration version
make migrate-version

# Rollback if needed
make migrate-down

# Check migration files
ls -la migrations/
```

## Pre-commit Hooks

Install pre-commit hooks to run checks before each commit:

```bash
make install-hooks
```

This will:
- Format code with `gofmt`
- Run `go vet`
- Run linter (if installed)
- Check for large files

## IDE Setup

### VS Code

Recommended extensions:
- **Go**: Official Go extension
- **Go Test**: Test runner
- **Error Lens**: Inline error display

Settings (`.vscode/settings.json`):
```json
{
  "go.formatTool": "gofmt",
  "go.lintTool": "golangci-lint",
  "go.testFlags": ["-v", "-race"]
}
```

### GoLand

- Enable Go modules
- Configure database tool for PostgreSQL
- Set up run configurations for tests

## Performance Tips

### Database

- Use indexes on frequently queried columns
- Avoid N+1 queries (use `Preload` or joins)
- Use connection pooling (already configured)

### Caching

- Cache frequently accessed data in Redis
- Use response caching for static endpoints
- Set appropriate TTLs

### Logging

- Use structured logging
- Avoid logging in hot paths
- Use appropriate log levels

## Getting Help

- **Documentation**: Check `docs/` directory
- **API Docs**: http://localhost:8080/api/docs/
- **Architecture**: See `docs/ARCHITECTURE.md`
- **Issues**: Create an issue in the repository

## Next Steps

- Read [Architecture Documentation](./ARCHITECTURE.md)
- Review [API Documentation](./API_ENHANCE_CAPTION.md)
- Check [Deployment Guide](./PRODUCTION_DEPLOYMENT.md)
- Explore [Monitoring Setup](./MONITORING_SETUP.md)

