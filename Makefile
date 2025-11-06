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

# Database migrations (placeholder - adjust based on your migration tool)
migrate:
	@echo "Running migrations..."
	@echo "Note: Adjust this target based on your migration tool"

# Development setup
dev-setup: fmt vet lint
	@echo "Development checks completed"

