# Brancr Architecture Documentation

## Overview

Brancr is a comprehensive social media management platform that enables businesses to schedule posts, manage multiple social media accounts, and analyze performance across platforms including Instagram, Facebook, TikTok, and WhatsApp.

## System Architecture

### High-Level Architecture

```
┌─────────────┐
│   Frontend  │ (React/Vue Dashboard)
└──────┬──────┘
       │ HTTPS
       ▼
┌─────────────────────────────────────────────────┐
│           Brancr API Server (Go)                │
│  ┌──────────────────────────────────────────┐   │
│  │  HTTP Router (Chi)                       │   │
│  │  - Middleware Stack                      │   │
│  │  - Rate Limiting                         │   │
│  │  - Authentication                        │   │
│  └──────────────────────────────────────────┘   │
│                                                   │
│  ┌──────────────┐  ┌──────────────┐            │
│  │ Tenant API   │  │  Bot API     │            │
│  │ Handler      │  │  Handler     │            │
│  └──────┬───────┘  └──────┬───────┘            │
│         │                 │                     │
│  ┌──────▼─────────────────▼──────┐            │
│  │      Service Layer            │            │
│  │  - Scheduler                  │            │
│  │  - AI Services                │            │
│  │  - Analytics                  │            │
│  │  - Social Platform Handlers   │            │
│  └──────┬────────────────────────┘            │
└─────────┼──────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────┐
│           Data Layer                            │
│  ┌──────────────┐  ┌──────────────┐           │
│  │  PostgreSQL  │  │    Redis     │           │
│  │  (Primary DB)│  │  (Cache/Queue)│          │
│  └──────────────┘  └──────────────┘           │
└─────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────┐
│      External Services                          │
│  - Meta (Instagram/Facebook/WhatsApp)          │
│  - TikTok API                                   │
│  - OpenAI (Caption Generation)                 │
│  - Cloudinary (Media Storage)                  │
│  - Telegram Bot API                            │
└─────────────────────────────────────────────────┘
```

## Core Components

### 1. HTTP Layer (`internal/core/router.go`)

- **Router**: Chi router with middleware stack
- **Middleware Chain**:
  1. Request ID (correlation tracking)
  2. Security Headers (CSP, HSTS, etc.)
  3. Input Sanitization (XSS prevention)
  4. Panic Recovery
  5. Request Logging (structured logs)
  6. Metrics Collection (Prometheus)
  7. CORS
  8. Compression (gzip)
  9. Request Size Limits
  10. Rate Limiting

### 2. Service Layer

#### Scheduler (`internal/social/scheduler.go`)
- **Purpose**: Executes scheduled social media posts
- **Features**:
  - Multi-platform posting (Instagram, Facebook, TikTok)
  - AI caption generation and enhancement
  - Retry logic with exponential backoff
  - Activity logging for audit trail
- **Modes**:
  - Mode 1: AI-generated caption (no user caption)
  - Mode 2: Use user caption as-is
  - Mode 3: AI-enhanced user caption

#### AI Services (`internal/ai/`)
- **CaptionOptimizer**: Generates and enhances social media captions
- **CustomerResponder**: Automated customer message responses
- **IntentDetector**: Classifies customer message intents
- **MemoryService**: Maintains conversation context

#### Analytics (`internal/analytics/`)
- **Platform-specific analyzers**: Instagram, Facebook, TikTok
- **Performance metrics**: Engagement rate, impressions, reach
- **Aggregation**: Cross-platform analytics

### 3. Data Layer

#### Database (`internal/storage/`)
- **ORM**: GORM with PostgreSQL
- **Migrations**: golang-migrate for versioned schema changes
- **Models**: 
  - `Tenant`: Organization/account
  - `ScheduledPost`: Posts to be published
  - `MediaAsset`: Uploaded media files
  - `PlatformConnection`: OAuth tokens for platforms
  - `PostActivity`: Audit log of post operations

#### Cache (`internal/cache/`)
- **Redis**: Caching layer for frequently accessed data
- **Use Cases**:
  - Rate limiting counters
  - Session storage
  - Response caching
  - Job queue

### 4. Authentication & Authorization

#### Tenant Authentication (`internal/middleware/tenant_auth.go`)
- **JWT-based**: Secure token-based authentication
- **Claims**: Tenant ID, user ID, roles
- **Context**: Injects tenant info into request context

#### Platform OAuth (`internal/social/`)
- **Meta OAuth**: Instagram, Facebook, WhatsApp
- **TikTok OAuth**: TikTok Business API
- **Token Management**: Secure storage and refresh

### 5. External Integrations

#### Social Platforms
- **Meta API**: Instagram, Facebook, WhatsApp
- **TikTok API**: Video posting and analytics
- **Telegram Bot**: User interaction via Telegram

#### Third-Party Services
- **OpenAI**: Caption generation and enhancement
- **Cloudinary**: Media storage and optimization
- **Email Service**: Notifications and alerts

## Data Flow

### Post Creation Flow

```
1. User creates post via API or Telegram bot
   ↓
2. Handler validates input and creates ScheduledPost
   ↓
3. Post stored in database with status="scheduled"
   ↓
4. Scheduler picks up post at scheduled time
   ↓
5. AI caption generation/enhancement (if enabled)
   ↓
6. Media uploaded to platform
   ↓
7. Post published to each platform
   ↓
8. Status updated to "published" or "failed"
   ↓
9. Analytics collected from platforms
```

### OAuth Flow

```
1. User initiates OAuth via /api/oauth/{platform}/start
   ↓
2. Redirect to platform OAuth page
   ↓
3. User authorizes application
   ↓
4. Platform redirects to /api/oauth/{platform}/callback
   ↓
5. Exchange code for access token
   ↓
6. Store token in PlatformConnection
   ↓
7. Redirect to success page
```

## Security Architecture

### Authentication
- **JWT Tokens**: Signed with HS256, configurable expiration
- **Token Storage**: HTTP-only cookies (optional) or Authorization header
- **Refresh Tokens**: For long-lived sessions

### Authorization
- **Tenant Isolation**: All queries filtered by tenant_id
- **Role-Based Access**: Admin, user roles
- **Platform Permissions**: Scoped OAuth permissions

### Data Protection
- **Input Sanitization**: HTML escaping, SQL injection prevention
- **Rate Limiting**: Per-tenant and global limits
- **Security Headers**: CSP, HSTS, X-Frame-Options
- **Request Size Limits**: Prevent DoS attacks

## Performance Optimizations

### Database
- **Connection Pooling**: Max 100 connections, 10 idle
- **Prepared Statements**: Enabled for query performance
- **Query Logging**: Slow query detection (>1s in production)
- **Indexes**: On frequently queried columns

### Caching
- **Response Caching**: Redis-based HTTP response cache
- **Query Result Caching**: Frequently accessed data
- **TTL Management**: Configurable cache expiration

### Compression
- **Gzip Compression**: Automatic for text responses
- **Content-Type Filtering**: Skips compression for binary data

## Monitoring & Observability

### Metrics (Prometheus)
- **HTTP Metrics**: Request count, duration, status codes
- **Database Metrics**: Query count, duration, errors
- **Business Metrics**: Posts scheduled, published, failed

### Logging (Zap)
- **Structured Logging**: JSON format with correlation IDs
- **Log Levels**: Debug (dev), Info, Warn, Error
- **Request Tracking**: Request ID propagation

### Health Checks
- **`/health`**: Basic liveness check
- **`/health/ready`**: Readiness with dependency checks
- **`/health/live`**: Kubernetes liveness probe

## Deployment Architecture

### Containerization
- **Docker**: Multi-stage builds for optimized images
- **Docker Compose**: Local development environment
- **Health Checks**: Built-in container health monitoring

### Scalability
- **Stateless Design**: Horizontal scaling ready
- **Database Connection Pooling**: Handles concurrent requests
- **Redis**: Shared cache/queue for multiple instances

### Environment Configuration
- **Environment Variables**: 12-factor app principles
- **Configuration Validation**: Startup validation of required vars
- **Secrets Management**: External secret management support

## Development Workflow

### Local Setup
1. **Docker Compose**: PostgreSQL + Redis
2. **Go 1.21+**: Application runtime
3. **Migrations**: Automatic on startup or manual via CLI
4. **Hot Reload**: Development mode with file watching

### Testing
- **Unit Tests**: Go testing package
- **Integration Tests**: Test utilities for HTTP and DB
- **Test Coverage**: Target >70% for critical paths

### Code Quality
- **Linting**: golangci-lint
- **Formatting**: gofmt
- **Pre-commit Hooks**: Automated checks

## API Design

### RESTful Principles
- **Resource-Based URLs**: `/api/posts`, `/api/analytics`
- **HTTP Methods**: GET, POST, PUT, DELETE
- **Status Codes**: Standard HTTP status codes
- **JSON**: Request/response format

### Versioning
- **URL Versioning**: `/api/v1/...` (future)
- **Backward Compatibility**: Maintained for breaking changes

### Documentation
- **OpenAPI/Swagger**: Auto-generated from code annotations
- **Interactive Docs**: Available at `/api/docs/`

## Error Handling

### Error Types
- **Validation Errors**: 400 Bad Request
- **Authentication Errors**: 401 Unauthorized
- **Authorization Errors**: 403 Forbidden
- **Not Found Errors**: 404 Not Found
- **Internal Errors**: 500 Internal Server Error

### Error Response Format
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid input",
    "details": {
      "field": "caption",
      "reason": "Caption is required"
    }
  }
}
```

## Future Enhancements

### Planned Features
- **Webhook Retry Mechanism**: Automatic retry for failed webhooks
- **Background Job Monitoring**: Job queue dashboard
- **API Versioning**: v1, v2 support
- **Distributed Tracing**: OpenTelemetry integration
- **GraphQL API**: Alternative to REST
- **Real-time Updates**: WebSocket support for live updates

### Scalability Improvements
- **Message Queue**: RabbitMQ/Kafka for async processing
- **CDN Integration**: Static asset delivery
- **Database Read Replicas**: Read scaling
- **Microservices**: Service decomposition (future)

## Technology Stack

### Backend
- **Language**: Go 1.21+
- **Web Framework**: Chi router
- **ORM**: GORM
- **Database**: PostgreSQL 15+
- **Cache/Queue**: Redis 7+

### External Services
- **AI**: OpenAI API
- **Media Storage**: Cloudinary
- **Email**: SMTP or service provider
- **Social APIs**: Meta, TikTok

### DevOps
- **Containerization**: Docker
- **Orchestration**: Docker Compose (local), Kubernetes (production)
- **CI/CD**: GitHub Actions (recommended)
- **Monitoring**: Prometheus + Grafana

## References

- [API Documentation](./API_ENHANCE_CAPTION.md)
- [Deployment Guide](./PRODUCTION_DEPLOYMENT.md)
- [Environment Variables](./ENVIRONMENT_VARIABLES.md)
- [Testing Guide](./TESTING_GUIDE.md)

