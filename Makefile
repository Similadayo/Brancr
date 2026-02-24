.PHONY: help build run test clean migrate vet fmt lint

# Default target
help:
	@echo "Available targets:"
	@echo "  build     - Build the application"
	@echo "  run       - Run the application"
	@echo "  test      - Run tests"
	@echo "  vet       - Run go vet"
	@echo "  fmt       - Format code"
	@echo "  lint      - Run linter"
	@echo "  clean     - Clean build artifacts"
	@echo "  migrate   - Run database migrations"

# Build the application
build:
	@echo "Building application..."
	go build -o bin/brancr ./cmd/brancr

# Run the application
run:
	@echo "Running application..."
	go run ./cmd/brancr

# Run tests
test:
	@echo "Running tests..."
	go test ./...

# Run tests with CGO enabled (required for SQLite tests)
test-cgo:
	@echo "Running tests with CGO enabled..."
	@set CGO_ENABLED=1 && go test ./...

# Run tests with PostgreSQL (alternative to SQLite)
test-postgres:
	@echo "Running tests with PostgreSQL..."
	@if [ -z "$$TEST_POSTGRES_DSN" ]; then \
		echo "Warning: TEST_POSTGRES_DSN not set, some tests will be skipped"; \
	fi
	go test ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter (if golangci-lint is installed)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "Running linter..."; \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	go clean

# Database migrations
migrate:
	@echo "Running database migrations..."
	@if [ -z "$$POSTGRES_DSN" ]; then \
		echo "Error: POSTGRES_DSN environment variable not set"; \
		exit 1; \
	fi
	@if command -v migrate >/dev/null 2>&1; then \
		migrate -path migrations -database "$$POSTGRES_DSN" up; \
	else \
		echo "golang-migrate CLI not installed. Install with:"; \
		echo "  go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"; \
		echo ""; \
		echo "Or migrations will run automatically on application startup."; \
	fi

# Create a new migration
migrate-create:
	@if [ -z "$(NAME)" ]; then \
		echo "Error: NAME parameter required. Usage: make migrate-create NAME=add_new_column"; \
		exit 1; \
	fi
	@if command -v migrate >/dev/null 2>&1; then \
		migrate create -ext sql -dir migrations -seq $(NAME); \
	else \
		echo "golang-migrate CLI not installed. Install with:"; \
		echo "  go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"; \
	fi

# Rollback last migration
migrate-down:
	@if [ -z "$$POSTGRES_DSN" ]; then \
		echo "Error: POSTGRES_DSN environment variable not set"; \
		exit 1; \
	fi
	@if command -v migrate >/dev/null 2>&1; then \
		migrate -path migrations -database "$$POSTGRES_DSN" down 1; \
	else \
		echo "golang-migrate CLI not installed"; \
	fi

# Check migration version
migrate-version:
	@if [ -z "$$POSTGRES_DSN" ]; then \
		echo "Error: POSTGRES_DSN environment variable not set"; \
		exit 1; \
	fi
	@if command -v migrate >/dev/null 2>&1; then \
		migrate -path migrations -database "$$POSTGRES_DSN" version; \
	else \
		echo "golang-migrate CLI not installed"; \
	fi

# Development setup
dev-setup: fmt vet lint
	@echo "Development checks completed"

# Install pre-commit hooks
install-hooks:
	@echo "Installing pre-commit hooks..."
	@mkdir -p .git/hooks
	@cp .githooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✅ Pre-commit hooks installed"

# Run development setup script
setup-dev:
	@if [ -f scripts/setup-dev.sh ]; then \
		bash scripts/setup-dev.sh; \
	else \
		echo "Setup script not found"; \
	fi

# Docker Compose commands
docker-up:
	@echo "Starting Docker services..."
	docker-compose up -d

docker-down:
	@echo "Stopping Docker services..."
	docker-compose down

docker-logs:
	@echo "Showing Docker logs..."
	docker-compose logs -f

# Run with hot reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "Air not installed. Install with: go install github.com/cosmtrek/air@latest"; \
		go run ./cmd/brancr; \
	fi

# Generate Swagger docs
swagger:
	@if command -v swag >/dev/null 2>&1; then \
		swag init -g cmd/brancr/main.go; \
	else \
		echo "swag not installed. Install with: go install github.com/swaggo/swag/cmd/swag@latest"; \
	fi

