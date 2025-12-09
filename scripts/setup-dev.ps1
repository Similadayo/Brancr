# Brancr Local Development Setup Script (PowerShell)

Write-Host "🚀 Setting up Brancr development environment..." -ForegroundColor Cyan

# Check if Docker is installed
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host "❌ Docker is not installed. Please install Docker Desktop first." -ForegroundColor Red
    exit 1
}

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "❌ Go is not installed. Please install Go 1.21+ first." -ForegroundColor Red
    exit 1
}

Write-Host "✅ Prerequisites check passed" -ForegroundColor Green

# Create .env file if it doesn't exist
if (-not (Test-Path .env)) {
    Write-Host "📝 Creating .env file..." -ForegroundColor Yellow
    @"
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
"@ | Out-File -FilePath .env -Encoding utf8
    Write-Host "✅ Created .env file" -ForegroundColor Green
} else {
    Write-Host "ℹ️  .env file already exists, skipping..." -ForegroundColor Yellow
}

# Start Docker services
Write-Host "🐳 Starting Docker services (PostgreSQL, Redis)..." -ForegroundColor Cyan
docker-compose up -d postgres redis

# Wait for services to be ready
Write-Host "⏳ Waiting for services to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 5

# Check PostgreSQL
$maxRetries = 30
$retryCount = 0
while ($retryCount -lt $maxRetries) {
    $result = docker-compose exec -T postgres pg_isready -U brancr 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✅ PostgreSQL is ready" -ForegroundColor Green
        break
    }
    Write-Host "Waiting for PostgreSQL..." -ForegroundColor Yellow
    Start-Sleep -Seconds 2
    $retryCount++
}

# Check Redis
$retryCount = 0
while ($retryCount -lt $maxRetries) {
    $result = docker-compose exec -T redis redis-cli ping 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✅ Redis is ready" -ForegroundColor Green
        break
    }
    Write-Host "Waiting for Redis..." -ForegroundColor Yellow
    Start-Sleep -Seconds 2
    $retryCount++
}

# Install Go dependencies
Write-Host "📥 Installing Go dependencies..." -ForegroundColor Cyan
go mod download

# Build the application
Write-Host "🔨 Building application..." -ForegroundColor Cyan
go build -o bin/brancr.exe ./cmd/brancr
if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ Build successful" -ForegroundColor Green
} else {
    Write-Host "⚠️  Build failed, but continuing..." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "✅ Development environment setup complete!" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Start the application: go run ./cmd/brancr"
Write-Host "  2. Or use Docker Compose: docker-compose up brancr"
Write-Host "  3. Access API docs: http://localhost:8080/api/docs/"
Write-Host "  4. Health check: http://localhost:8080/health"
Write-Host ""
Write-Host "To stop services: docker-compose down" -ForegroundColor Yellow

