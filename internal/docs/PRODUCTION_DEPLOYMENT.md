# Production Deployment Guide

Complete guide for deploying Brancr to production.

## Overview

This guide covers all aspects of production deployment, from preparation to monitoring.

## Quick Start

1. **Set Required Environment Variables** (see `ENVIRONMENT_VARIABLES.md`)
2. **Run Database Migrations**: `make migrate`
3. **Build Application**: `go build -o bin/brancr ./cmd/brancr`
4. **Start Application**: `./bin/brancr`
5. **Verify Health**: `curl http://localhost:8080/health/ready`

## Production Features

### ✅ Request Size Limits
- **Default**: 10MB per request
- **Location**: `internal/middleware/request_size.go`
- **Applied**: Globally to all POST/PUT/PATCH requests
- **Customization**: Use `RequestSizeLimit(maxSize, logger)` for custom limits

### ✅ Enhanced Health Checks
- **Basic**: `GET /health` - Simple alive check
- **Readiness**: `GET /health/ready` - Includes dependency checks with latency
- **Liveness**: `GET /health/live` - Kubernetes liveness probe

**Readiness Response**:
```json
{
  "status": "ready",
  "timestamp": "2025-01-15T10:00:00Z",
  "checks": {
    "database": {
      "status": "healthy",
      "latency": 5
    },
    "redis": {
      "status": "healthy",
      "latency": 2
    },
    "database_pool": {
      "open_connections": 5,
      "in_use": 2,
      "idle": 3,
      "wait_count": 0
    }
  }
}
```

### ✅ Graceful Shutdown
- Handles SIGINT and SIGTERM signals
- 30-second timeout for HTTP server shutdown
- Properly closes:
  - Job queue
  - Scheduler
  - HTTP server
  - Database connections
  - Redis connections

### ✅ Security Headers
- X-Frame-Options: DENY
- X-Content-Type-Options: nosniff
- X-XSS-Protection: 1; mode=block
- Content-Security-Policy
- Referrer-Policy
- Permissions-Policy

### ✅ Rate Limiting
- Global: 1000 requests/minute per IP
- Tenant-specific: Available per-route
- Platform-specific: Available for external APIs

## Deployment Steps

### 1. Pre-Deployment Checklist
See `DEPLOYMENT_CHECKLIST.md` for complete checklist.

**Critical Items**:
- [ ] All required environment variables set
- [ ] Database accessible and migrations run
- [ ] Redis configured (optional but recommended)
- [ ] OAuth redirect URIs configured
- [ ] Secrets stored securely

### 2. Build
```bash
# Standard build
go build -o bin/brancr ./cmd/brancr

# With optimizations
go build -ldflags="-s -w" -o bin/brancr ./cmd/brancr
```

### 3. Database Setup
```bash
# Run migrations
export POSTGRES_DSN="postgres://user:pass@host:5432/brancr?sslmode=require"
make migrate

# Or manually
migrate -path migrations -database "$POSTGRES_DSN" up
```

### 4. Configuration
```bash
# Create .env file or set environment variables
export APP_ENV=production
export PORT=8080
export POSTGRES_DSN="..."
export TENANT_JWT_SECRET="..."
# ... (see ENVIRONMENT_VARIABLES.md)
```

### 5. Start Application
```bash
# Direct
./bin/brancr

# With systemd
sudo systemctl start brancr

# With Docker
docker run -d --env-file .env brancr:latest
```

## Process Management

### systemd Service File
```ini
[Unit]
Description=Brancr API Server
After=network.target postgresql.service

[Service]
Type=simple
User=brancr
WorkingDirectory=/opt/brancr
ExecStart=/opt/brancr/bin/brancr
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

Environment="APP_ENV=production"
EnvironmentFile=/etc/brancr/env

[Install]
WantedBy=multi-user.target
```

### Docker Compose
```yaml
version: '3.8'
services:
  brancr:
    build: .
    ports:
      - "8080:8080"
    environment:
      - APP_ENV=production
    env_file:
      - .env
    depends_on:
      - postgres
      - redis
    restart: unless-stopped
```

## Monitoring

### Health Check Endpoints
- **Basic**: `GET /health` - Returns 200 if server is running
- **Readiness**: `GET /health/ready` - Returns 200 if ready to accept traffic
- **Liveness**: `GET /health/live` - Returns 200 if process is alive

### Metrics Endpoint
- **Prometheus**: `GET /metrics` - Prometheus-compatible metrics

### Logging
- Structured JSON logs via zap logger
- Request correlation IDs for tracing
- Log levels: ERROR (5xx), WARN (4xx), INFO (2xx/3xx)

### Recommended Monitoring
1. **Uptime Monitoring**: Ping `/health` every minute
2. **Error Tracking**: Sentry, Datadog, or similar
3. **Performance**: Monitor response times
4. **Database**: Monitor connection pool stats
5. **Rate Limits**: Track rate limit violations

## Troubleshooting

### Application Won't Start
1. Check required environment variables: `POSTGRES_DSN`, `PORT`, `TENANT_JWT_SECRET`
2. Verify database connectivity
3. Check port availability
4. Review startup logs for errors

### Health Checks Failing
- Check `/health/ready` response for specific component failures
- Database: Verify `POSTGRES_DSN` and network connectivity
- Redis: Check `REDIS_ADDR` or disable if not needed

### High Error Rates
- Review application logs (check correlation IDs)
- Check rate limiting settings
- Verify external API status (Meta, TikTok, etc.)
- Review database query performance

### Performance Issues
- Check database connection pool stats via `/health/ready`
- Review slow query logs
- Check Redis connectivity
- Monitor rate limiting impact

## Security Checklist

- [ ] All secrets in secure vault (not in code)
- [ ] HTTPS/TLS enabled
- [ ] CORS origins restricted
- [ ] Rate limiting enabled
- [ ] Request size limits configured
- [ ] Security headers enabled
- [ ] JWT secrets are strong (32+ chars)
- [ ] Database uses SSL
- [ ] Regular security updates
- [ ] No sensitive data in logs

## Backup & Recovery

### Database Backups
```bash
# Daily backup
pg_dump $POSTGRES_DSN > backup-$(date +%Y%m%d).sql

# Restore
psql $POSTGRES_DSN < backup-20250115.sql
```

### Configuration Backups
- Backup `.env` files (securely)
- Document all environment variables
- Version control configuration templates

## Scaling Considerations

### Horizontal Scaling
- Stateless application (can run multiple instances)
- Use shared Redis for rate limiting
- Use load balancer with health checks
- Database connection pooling handles multiple instances

### Vertical Scaling
- Increase database connection pool if needed
- Monitor memory usage
- Adjust rate limits based on capacity

## Rollback Procedure

1. Stop current deployment
2. Rollback database migrations (if schema changed)
3. Restore previous application version
4. Verify health checks pass
5. Monitor for issues

## Support & Resources

- **Environment Variables**: See `ENVIRONMENT_VARIABLES.md`
- **Deployment Checklist**: See `DEPLOYMENT_CHECKLIST.md`
- **API Documentation**: `GET /api/docs/`
- **Health Checks**: `GET /health/ready`

## Next Steps After Deployment

1. Set up monitoring and alerts
2. Configure log aggregation
3. Set up automated backups
4. Schedule regular security reviews
5. Plan for scaling

