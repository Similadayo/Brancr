# Production Readiness Implementation Plan

## Overview
This document outlines the phased approach to making Brancr production-ready for handling many tenants at scale.

## Phase 1: Immediate Fixes (Before Production)

### 1.1 Tenant Quotas System
**Priority**: Critical  
**Status**: In Progress

**Implementation**:
- Create `TenantQuota` model to track limits and usage
- Add quota checks before AI API calls
- Add quota checks before post scheduling
- Add quota checks for storage/media uploads
- Implement quota enforcement middleware (Note: API calls are charged on request entry; expensive operations like uploads and AI are charged on success)
- Add quota reset logic (daily/monthly)

**Files to Create/Modify**:
- `internal/storage/models.go` - Add TenantQuota model
- `internal/quota/quota.go` - Quota service
- `internal/quota/middleware.go` - Quota enforcement
- `internal/ai/caption_optimizer.go` - Add quota checks
- `internal/social/scheduler.go` - Add quota checks

### 1.2 Worker Pools for Background Jobs
**Priority**: Critical  
**Status**: Pending

**Implementation**:
- Create worker pool for scheduler jobs
- Create worker pool for AI processing
- Create worker pool for analytics collection
- Add configurable pool sizes
- Add queue depth monitoring

**Files to Create/Modify**:
- `internal/worker/pool.go` - Generic worker pool
- `internal/social/scheduler.go` - Use worker pool
- `internal/ai/caption_optimizer.go` - Use worker pool
- `internal/analytics/service.go` - Use worker pool

### 1.3 Circuit Breakers for External APIs
**Priority**: Critical  
**Status**: Pending

**Implementation**:
- Add circuit breaker for OpenAI API
- Add circuit breaker for Meta API
- Add circuit breaker for TikTok API
- Add circuit breaker for Cloudinary
- Configure failure thresholds and recovery
- Add metrics for circuit breaker state

**Files to Create/Modify**:
- `internal/circuitbreaker/breaker.go` - Enhance existing breaker
- `internal/ai/client.go` - Use circuit breaker
- `internal/social/instagram.go` - Use circuit breaker
- `internal/social/tiktok.go` - Use circuit breaker

### 1.4 Increase Test Coverage
**Priority**: High  
**Status**: Pending

**Target**: 60%+ coverage

**Implementation**:
- Add tests for quota system
- Add tests for worker pools
- Add tests for circuit breakers
- Add tests for scheduler
- Add tests for AI services
- Add integration tests for critical flows

**Files to Create**:
- `internal/quota/quota_test.go`
- `internal/worker/pool_test.go`
- `internal/circuitbreaker/breaker_test.go`
- `internal/social/scheduler_test.go` (enhance existing)
- `internal/ai/caption_optimizer_test.go`

### 1.5 In-Memory Rate Limiting Fallback
**Priority**: High  
**Status**: Pending

**Implementation**:
- Add in-memory rate limiter using sync.Map
- Fallback to in-memory when Redis unavailable
- Add TTL-based expiration for in-memory entries
- Add metrics for fallback usage

**Files to Modify**:
- `internal/middleware/ratelimit.go` - Add in-memory fallback

## Phase 2: Short-Term (First Month)

### 2.1 Load Testing
**Priority**: High  
**Status**: Pending

**Implementation**:
- Create load testing scripts (k6 or Locust)
- Test with 100+ concurrent tenants
- Test database connection pool limits
- Test rate limiting under load
- Test worker pool capacity
- Document performance benchmarks

**Files to Create**:
- `tests/load/load_test.js` (k6 script)
- `tests/load/README.md`

### 2.2 Database Query Performance Monitoring
**Priority**: High  
**Status**: Pending

**Implementation**:
- Add slow query logging (>1s)
- Add query duration metrics
- Add per-tenant query metrics
- Add database connection pool metrics
- Create dashboard queries

**Files to Modify**:
- `internal/core/gorm_logger.go` - Enhance logging
- `internal/middleware/metrics.go` - Add DB metrics

### 2.3 Alerting Setup
**Priority**: High  
**Status**: Pending

**Implementation**:
- Configure Prometheus alerting rules
- Set up alerts for:
  - High error rates
  - High latency (p95, p99)
  - Database connection pool exhaustion
  - Circuit breaker open states
  - Quota exhaustion
  - Worker pool queue depth
- Integrate with PagerDuty/Slack/Email

**Files to Create**:
- `deploy/alerts/prometheus-alerts.yml`
- `docs/ALERTING.md`

### 2.4 Tenant Resource Usage Dashboard
**Priority**: Medium  
**Status**: Pending

**Implementation**:
- Create API endpoint for tenant usage stats
- Add Grafana dashboard
- Track: AI calls, posts, storage, API calls
- Add quota utilization graphs

**Files to Create**:
- `internal/admin/usage_stats.go`
- `deploy/grafana/dashboards/tenant-usage.json`

### 2.5 API Rate Limiting Per Tenant
**Priority**: Medium  
**Status**: Pending

**Implementation**:
- Enhance existing rate limiter
- Add per-endpoint rate limits
- Add tiered limits (free, pro, enterprise)
- Add rate limit headers in responses

**Files to Modify**:
- `internal/middleware/ratelimit.go` - Enhance per-tenant limits
- `internal/core/router.go` - Apply per-endpoint limits

## Phase 3: Long-Term (Scaling)

### 3.1 Database Read Replicas
**Priority**: Medium  
**Status**: Pending

**Implementation**:
- Configure read replica connection
- Route read queries to replicas
- Keep writes on primary
- Add health checks for replicas

### 3.2 Message Queue (RabbitMQ/Kafka)
**Priority**: Medium  
**Status**: Pending

**Implementation**:
- Replace goroutine-based jobs with queue
- Add job retry logic
- Add job priority queues
- Add dead letter queues

### 3.3 CDN for Static Assets
**Priority**: Low  
**Status**: Pending

**Implementation**:
- Configure CDN (Cloudflare/CloudFront)
- Move static assets to CDN
- Add cache headers

### 3.4 Distributed Tracing
**Priority**: Medium  
**Status**: Pending

**Implementation**:
- Integrate OpenTelemetry
- Add trace context propagation
- Create trace dashboards

### 3.5 Auto-Scaling
**Priority**: Low  
**Status**: Pending

**Implementation**:
- Configure Kubernetes HPA
- Set up scaling metrics
- Test scaling behavior

## Success Metrics

### Phase 1 Success Criteria:
- ✅ All quota checks in place
- ✅ Worker pools prevent resource exhaustion
- ✅ Circuit breakers prevent cascading failures
- ✅ Test coverage > 60%
- ✅ Rate limiting works without Redis

### Phase 2 Success Criteria:
- ✅ Load tested with 100+ tenants
- ✅ Query performance monitored
- ✅ Alerts configured and tested
- ✅ Usage dashboard operational
- ✅ Per-tenant rate limits enforced

### Phase 3 Success Criteria:
- ✅ Read replicas handling read traffic
- ✅ Message queue processing jobs
- ✅ CDN serving static assets
- ✅ Distributed tracing operational
- ✅ Auto-scaling working

## Timeline

- **Phase 1**: 2-3 weeks
- **Phase 2**: 4-6 weeks
- **Phase 3**: 8-12 weeks (ongoing)

## Notes

- Each phase should be completed and tested before moving to the next
- Monitor metrics after each implementation
- Adjust based on production learnings

