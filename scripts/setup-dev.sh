#!/bin/bash

# Brancr Local Development Setup Script

set -e

echo "🚀 Setting up Brancr development environment..."

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21+ first."
    exit 1
fi

echo "✅ Prerequisites check passed"

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo "📝 Creating .env file..."
    cat > .env << EOF
# Development Environment Variables
APP_ENV=development
PORT=8080
BRANCR_PUBLIC_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000

# Database
POSTGRES_DSN=postgres://brancr:brancr_dev_password@localhost:5432/brancr?sslmode=disable

# Redis
REDIS_ADDR=localhost:6379

# Security
TENANT_JWT_SECRET=dev-jwt-secret-key-must-be-at-least-32-characters-long-for-security
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=admin123

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3001

# Optional: Add your API keys here
# OPENAI_API_KEY=sk-...
# CLOUDINARY_CLOUD_NAME=...
EOF
    echo "✅ Created .env file"
else
    echo "ℹ️  .env file already exists, skipping..."
fi

# Start Docker services
echo "🐳 Starting Docker services (PostgreSQL, Redis)..."
docker-compose up -d postgres redis

# Wait for services to be ready
echo "⏳ Waiting for services to be ready..."
sleep 5

# Check PostgreSQL
until docker-compose exec -T postgres pg_isready -U brancr > /dev/null 2>&1; do
    echo "Waiting for PostgreSQL..."
    sleep 2
done
echo "✅ PostgreSQL is ready"

# Check Redis
until docker-compose exec -T redis redis-cli ping > /dev/null 2>&1; do
    echo "Waiting for Redis..."
    sleep 2
done
echo "✅ Redis is ready"

# Run database migrations
echo "📦 Running database migrations..."
export POSTGRES_DSN="postgres://brancr:brancr_dev_password@localhost:5432/brancr?sslmode=disable"
if command -v migrate &> /dev/null; then
    migrate -path migrations -database "$POSTGRES_DSN" up || echo "⚠️  Migrations may have already been run"
else
    echo "ℹ️  golang-migrate not installed. Migrations will run on application startup."
fi

# Install Go dependencies
echo "📥 Installing Go dependencies..."
go mod download

# Build the application
echo "🔨 Building application..."
go build -o bin/brancr ./cmd/brancr || echo "⚠️  Build failed, but continuing..."

echo ""
echo "✅ Development environment setup complete!"
echo ""
echo "Next steps:"
echo "  1. Start the application: go run ./cmd/brancr"
echo "  2. Or use Docker Compose: docker-compose up brancr"
echo "  3. Access API docs: http://localhost:8080/api/docs/"
echo "  4. Health check: http://localhost:8080/health"
echo ""
echo "To stop services: docker-compose down"

