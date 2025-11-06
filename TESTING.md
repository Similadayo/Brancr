# Testing Guide

This guide explains how to run tests in the Brancr codebase.

## Quick Start

```bash
# Run all tests (will skip SQLite tests if CGO is disabled)
make test

# Run tests with CGO enabled (for SQLite)
make test-cgo

# Run tests with PostgreSQL (alternative, no CGO required)
make test-postgres
```

## Test Database Options

### Option 1: SQLite (Default, Requires CGO)

SQLite is used by default for in-memory testing. It requires CGO to be enabled.

**Windows:**
```bash
set CGO_ENABLED=1
go test ./...
```

**Linux/Mac:**
```bash
CGO_ENABLED=1 go test ./...
```

**Note**: If CGO is disabled, tests will gracefully skip with a helpful message.

### Option 2: PostgreSQL (Alternative, No CGO Required)

You can use PostgreSQL for tests instead of SQLite. This doesn't require CGO.

**Setup:**
```bash
# Set test database connection string
set TEST_POSTGRES_DSN=postgres://user:password@localhost/testdb

# Run tests
go test ./...
```

**Note**: Tests will skip if `TEST_POSTGRES_DSN` is not set.

## Running Specific Tests

```bash
# Run tests for a specific package
go test ./internal/core/...

# Run tests with verbose output
go test -v ./internal/storage/repositories/...

# Run tests with coverage
go test -cover ./...

# Run tests with coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Utilities

The `internal/testutil` package provides utilities for testing:

- `TestDB(t)` - Creates an in-memory SQLite database
- `TestDBPostgres(t)` - Creates a PostgreSQL database connection
- `TestLogger(t)` - Creates a development logger
- `TestConfig()` - Creates test configuration
- `TestRedis(t)` - Creates a test Redis client
- `CleanupDB(t, db)` - Cleans up test database

## Mock Implementations

The `internal/testutil/mocks.go` file provides mock implementations:

- `MockAIClient` - Mock AI client
- `MockMemoryService` - Mock memory service
- `MockBotManager` - Mock bot manager
- `MockRepository` - Mock repository

## Example Test

```go
package repositories

import (
    "testing"
    "github.com/similadayo/brancr/internal/testutil"
)

func TestMyRepository(t *testing.T) {
    db := testutil.TestDB(t)
    defer testutil.CleanupDB(t, db)
    
    // Run migrations
    db.AutoMigrate(&MyModel{})
    
    // Your test code here
}
```

## Troubleshooting

### "CGO_ENABLED=0" Error

If you see this error:
```
Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work
```

**Solution**: Enable CGO:
```bash
set CGO_ENABLED=1
go test ./...
```

Or use PostgreSQL instead (see Option 2 above).

### Tests Skipping

If tests are being skipped, check the skip message. Common reasons:
- CGO is disabled (for SQLite tests)
- PostgreSQL connection string not set (for PostgreSQL tests)
- Redis not available (for Redis-dependent tests)

### Missing Dependencies

If you get import errors:
```bash
go mod tidy
go get ./...
```

## CI/CD Integration

For CI/CD pipelines, use PostgreSQL tests:

```yaml
# Example GitHub Actions
- name: Run tests
  env:
    TEST_POSTGRES_DSN: postgres://user:pass@localhost/testdb
  run: go test ./...
```

Or enable CGO for SQLite:

```yaml
- name: Run tests
  env:
    CGO_ENABLED: 1
  run: go test ./...
```

