# Production Deployment Checklist

Complete checklist for deploying Brancr to production.

## Pre-Deployment

### 1. Environment Variables ✅
- [ ] All required variables set (see `ENVIRONMENT_VARIABLES.md`)
- [ ] `POSTGRES_DSN` configured with SSL
- [ ] `TENANT_JWT_SECRET` is 32+ characters and unique
- [ ] `ADMIN_EMAIL` and `ADMIN_PASSWORD` set
- [ ] `APP_ENV=production`
- [ ] `BRANCR_PUBLIC_URL` set to production domain
- [ ] `FRONTEND_URL` set to production frontend
- [ ] OAuth redirect URIs configured for production domains
- [ ] All API keys and secrets are production keys (not test keys)

### 2. Database
- [ ] PostgreSQL database created and accessible
- [ ] Database migrations run successfully
- [ ] Connection pooling configured
- [ ] Backup strategy in place
- [ ] Database user has appropriate permissions

### 3. Redis (Optional but Recommended)
- [ ] Redis instance configured
- [ ] Redis connection tested
- [ ] Redis persistence configured (if needed)
- [ ] Redis memory limits set

### 4. Security
- [ ] All secrets stored in secure vault (not in code)
- [ ] HTTPS/TLS configured
- [ ] CORS origins restricted to production domains
- [ ] Rate limiting enabled
- [ ] Request size limits configured (default: 10MB)
- [ ] Security headers middleware enabled
- [ ] JWT secrets are strong and unique

### 5. Monitoring & Logging
- [ ] Logging configured (structured logs)
- [ ] Health check endpoints accessible
- [ ] Metrics endpoint (`/metrics`) accessible
- [ ] Error tracking configured (Sentry, etc.)
- [ ] Uptime monitoring configured
- [ ] Alerting configured for critical errors

## Deployment Steps

### 1. Build
```bash
# Build the application
go build -o bin/brancr ./cmd/brancr

# Or use Docker
docker build -t brancr:latest .
```

Note: The Docker build will attempt to build the admin SPA located in `adminapp/` if a `package.json` is present. If you do not have the SPA sources in the repo (e.g., building from a backend-only checkout), the Dockerfile creates an empty `adminapp/dist` to allow the image build to succeed. To include the SPA in builds, ensure `adminapp/` is present and contains the build scripts (typically `npm run build`).

### 2. Database Migrations
```bash
# Run migrations
make migrate
# Or manually:
migrate -path migrations -database "$POSTGRES_DSN" up
```

### 3. Verify Configuration
```bash
# Test configuration validation
go run ./cmd/brancr
# Should fail fast if required config is missing
```

### 4. Health Checks
```bash
# Test health endpoints
curl http://localhost:8080/health
curl http://localhost:8080/health/ready
curl http://localhost:8080/health/live
```

### 5. Start Application
```bash
# Direct
./bin/brancr

# Or with process manager (systemd, PM2, etc.)
systemctl start brancr
```

## Post-Deployment Verification

### 1. Health Checks
- [ ] `/health` returns 200
- [ ] `/health/ready` returns 200 (database and Redis connected)
- [ ] `/health/live` returns 200

### 2. API Endpoints
- [ ] Swagger UI accessible at `/api/docs/`
- [ ] Authentication endpoints work (`/api/auth/signup`, `/api/auth/login`)
- [ ] Protected endpoints require authentication
- [ ] Rate limiting is working

### 3. Background Services
- [ ] Scheduler is running (check logs)
- [ ] Telegram bots initialized
- [ ] Analytics scheduler started
- [ ] Job queue processing (if enabled)

### 4. Security
- [ ] Security headers present in responses
- [ ] CORS configured correctly
- [ ] Rate limiting active
- [ ] Request size limits enforced
- [ ] No sensitive data in logs

### 5. Performance
- [ ] Response times acceptable (< 500ms for most endpoints)
- [ ] Database connection pool healthy
- [ ] No memory leaks
- [ ] CPU usage normal

## Monitoring Checklist

### Daily
- [ ] Check error logs
- [ ] Monitor rate limit violations
- [ ] Check database connection pool stats
- [ ] Review slow queries

### Weekly
- [ ] Review performance metrics
- [ ] Check disk space
- [ ] Review security logs
- [ ] Update dependencies (if needed)

### Monthly
- [ ] Review and rotate secrets
- [ ] Database maintenance
- [ ] Performance optimization review
- [ ] Security audit

## Rollback Plan

If deployment fails:

1. **Stop new deployment**
   ```bash
   systemctl stop brancr
   ```

2. **Rollback database migrations** (if needed)
   ```bash
   make migrate-down
   ```

3. **Restore previous version**
   ```bash
   # Restore from backup or previous deployment
   ```

4. **Verify previous version works**
   ```bash
   curl http://localhost:8080/health
   ```

## Troubleshooting

### Application Won't Start
- Check required environment variables
- Verify database connectivity
- Check port availability
- Review startup logs

### Health Checks Failing
- **Database**: Check `POSTGRES_DSN` and network connectivity
- **Redis**: Check `REDIS_ADDR` or disable Redis if not needed
- Review `/health/ready` response for specific errors

### High Error Rates
- Check application logs
- Review rate limiting settings
- Check external API status (Meta, TikTok, etc.)
- Review database performance

### Performance Issues
- Check database query performance
- Review connection pool settings
- Check Redis connectivity
- Review rate limiting impact

## Production Best Practices

1. **Use Process Manager**: systemd, PM2, or similar
2. **Enable Log Rotation**: Prevent disk space issues
3. **Set Resource Limits**: CPU, memory limits
4. **Monitor Resources**: CPU, memory, disk, network
5. **Regular Backups**: Database and configuration
6. **Update Regularly**: Security patches and updates
7. **Document Changes**: Keep deployment log
8. **Test Rollback**: Practice rollback procedure

## Environment-Specific Notes

### Development
- Use local services
- Enable verbose logging
- Relax rate limits for testing

### Staging
- Mirror production setup
- Use staging API keys
- Test all integrations

### Production
- Strict security
- Optimized performance
- Comprehensive monitoring
- Regular backups

## Quick Reference

### Health Check URLs
- Basic: `GET /health`
- Readiness: `GET /health/ready` (includes dependency checks)
- Liveness: `GET /health/live`

### Important Endpoints
- API Docs: `GET /api/docs/`
- Metrics: `GET /metrics` (Prometheus format)
- Swagger JSON: `GET /api/docs/doc.json`

### Log Locations
- Application logs: Check process manager logs
- Structured logs: JSON format via zap logger
- Request logs: Include correlation IDs

## Support

For deployment issues:
1. Check logs first
2. Review health check endpoints
3. Verify environment variables
4. Check database and Redis connectivity
5. Review this checklist

