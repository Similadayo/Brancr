# Architecture Comparison: Current Implementation vs. Recommended Architecture

## Executive Summary

We've built a **provider-owned multi-tenant WhatsApp messaging service** that aligns well with the recommended architecture, but several critical components are missing or incomplete. This document identifies gaps and provides actionable recommendations.

---

## ✅ What We Have Implemented

### 1. **Ownership Model** ✅
- **Provider-owned model**: Brancr owns WABA, manages phone numbers
- Phone number provisioning and assignment to tenants
- System user token management for provider-owned model
- ✅ **Status**: Fully implemented

### 2. **Webhook Receiver (Central)** ✅
- **Location**: `internal/social/meta_webhook.go`
- **Endpoint**: `/api/webhooks/meta`
- Webhook verification (GET requests)
- Processes WhatsApp, Instagram, Facebook webhooks
- ✅ **Status**: Implemented, but missing status webhook handling

### 3. **Tenant Router / Dispatcher** ✅
- **Location**: `internal/social/meta_webhook.go::resolveTenantFromExternalID()`
- Routes by `phone_number_id` (WhatsApp), `page_id` (Facebook), `instagram_business_account.id` (Instagram)
- Persists mapping in `SocialAccount` table
- ✅ **Status**: Fully implemented

### 4. **AI DM Pipeline** ✅
- **Location**: `internal/social/message_handler.go::ProcessMessage()`
- Intent detection (`internal/ai/intent_detector.go`)
- Persona/tone analysis
- LLM reply generation (`internal/ai/responder.go`)
- Safety checks and confidence scoring
- ✅ **Status**: Implemented, but synchronous (no queue)

### 5. **Send Engine** ✅
- **Location**: `internal/social/message_handler.go::sendWhatsAppReply()`
- Respects 24-hour session rules (error detection)
- Error handling with specific error codes
- Retry logic (in webhook subscription, not message sending)
- ✅ **Status**: Implemented, but missing rate limiting

### 6. **Human Escalation** ✅
- **Location**: `internal/social/message_handler.go::escalateToTenant()`
- Telegram-based escalation
- Escalation handler (`internal/bot/escalation_handler.go`)
- ✅ **Status**: Fully implemented

### 7. **Admin / Tenant Dashboard** ✅
- Multiple endpoints for managing numbers, templates, conversations
- Analytics endpoints
- ✅ **Status**: Partially implemented (missing some features)

### 8. **Security & Auth** ✅
- JWT-based tenant authentication
- Token encryption (`internal/utils/encryption.go`)
- Admin authentication
- ✅ **Status**: Implemented

---

## ❌ Critical Gaps & Missing Components

### 1. **Status Webhook Handling** ❌ **CRITICAL**

**What's Missing:**
- We receive `messages` webhooks but **don't process `statuses` webhooks**
- No tracking of message delivery status (sent → delivered → read)
- Can't detect failed deliveries or read receipts

**Impact:**
- Can't track message delivery (one tick vs two ticks issue)
- No visibility into delivery failures
- Can't optimize retry logic based on delivery status

**Recommendation:**
```go
// Add to processWhatsAppWebhook()
if len(change.Value.Statuses) > 0 {
    // Process status updates
    for _, status := range change.Value.Statuses {
        // Update message status in database
        // Track delivery/read receipts
    }
}
```

**Priority:** 🔴 **HIGH** - Required for production

---

### 2. **Message Queue System** ❌ **CRITICAL**

**What's Missing:**
- Currently processing messages synchronously in goroutines
- No queue (RabbitMQ/Kafka/SQS) between webhook receiver and AI workers
- Webhook handler blocks on AI processing

**Impact:**
- Webhook timeout risk (Meta expects < 20s response)
- No retry mechanism for failed AI processing
- Can't scale horizontally
- No message ordering guarantees

**Current Code:**
```go
// internal/social/meta_webhook.go:647
go h.MessageHandler.ProcessMessage(ctx, incomingMsg) // Direct call, no queue
```

**Recommendation:**
- Implement Redis-based queue (we already have Redis)
- Webhook receiver → enqueue → return 200 OK immediately
- Separate worker pool processes queue
- Add retry logic with exponential backoff

**Priority:** 🔴 **HIGH** - Required for scale

---

### 3. **24-Hour Session Window Tracking** ❌ **HIGH**

**What's Missing:**
- No explicit tracking of conversation session windows
- Only error detection when sending (error code 131047)
- No proactive session window management

**Impact:**
- Can't prevent sending outside session window
- Can't automatically switch to templates
- Poor user experience (messages rejected)

**Current Code:**
```go
// We detect error 131047 after sending fails
if apiResponse.Error.Code == 131047 {
    errorMsg += "\n\n⚠️ 24-hour messaging window expired..."
}
```

**Recommendation:**
- Track `LastMessageAt` in `Conversation` table (we have this!)
- Check session window before sending
- Auto-select template if outside window
- Store session window in Redis for fast lookups

**Priority:** 🟡 **MEDIUM-HIGH** - Improves UX significantly

---

### 4. **Message Template Management** ⚠️ **PARTIAL**

**What's Missing:**
- We have `RecurringTemplate` for scheduled posts, but **no WhatsApp message templates**
- No template approval workflow
- No template selection logic for business-initiated messages
- No template substitution engine

**Impact:**
- Can't send business-initiated messages outside session window
- No structured message templates
- Missing compliance with WhatsApp template requirements

**Recommendation:**
```go
// New model needed
type WhatsAppTemplate struct {
    ID          uint
    TenantID    uint
    Name        string
    Category    string // AUTHENTICATION, MARKETING, UTILITY
    Language    string
    Status      string // PENDING, APPROVED, REJECTED
    TemplateID  string // Meta's template ID
    Components  string // JSON: header, body, footer, buttons
    CreatedAt   time.Time
}
```

**Priority:** 🟡 **MEDIUM** - Required for business-initiated messaging

---

### 5. **Rate Limiting Per Tenant** ❌ **HIGH**

**What's Missing:**
- No rate limiting implementation
- No per-tenant message quotas
- No throttling mechanism

**Impact:**
- Risk of hitting Meta rate limits
- No fair usage enforcement
- Potential account suspension

**Recommendation:**
- Implement Redis-based rate limiter
- Per-tenant limits (e.g., 1000 messages/hour)
- Sliding window algorithm
- Return 429 Too Many Requests when exceeded

**Priority:** 🟡 **MEDIUM-HIGH** - Required for production

---

### 6. **Opt-In Management** ❌ **CRITICAL FOR COMPLIANCE**

**What's Missing:**
- No opt-in tracking
- No consent management
- No opt-out handling

**Impact:**
- **Compliance risk** - WhatsApp requires explicit opt-in
- Can't prove consent for regulatory purposes
- Risk of account suspension

**Recommendation:**
```go
// New model needed
type OptIn struct {
    ID            uint
    TenantID      uint
    CustomerID    uint
    PhoneNumber   string
    Platform      string // whatsapp
    OptInMethod   string // webhook, manual, template
    OptInDate     time.Time
    OptOutDate    *time.Time
    ConsentProof  string // JSON: webhook payload, manual entry, etc.
    CreatedAt     time.Time
}
```

**Priority:** 🔴 **HIGH** - Required for compliance

---

### 7. **Conversation State in Redis** ⚠️ **PARTIAL**

**What's Missing:**
- We have Redis but not using for conversation state
- No session window caching
- No LLM context window caching
- No fast lookups for active conversations

**Impact:**
- Slower session window checks (DB queries)
- No context caching for LLM (repeated API calls)
- Higher database load

**Recommendation:**
- Cache active conversations in Redis
- Store session window timestamps
- Cache recent conversation history for LLM context
- TTL: 24 hours + buffer

**Priority:** 🟢 **LOW-MEDIUM** - Performance optimization

---

### 8. **Observability & Monitoring** ⚠️ **PARTIAL**

**What's Missing:**
- No webhook latency tracking
- No delivery status monitoring
- No alerts for missing statuses
- Limited metrics

**Current:**
- We have logging (`zap.Logger`)
- Basic error tracking

**Recommendation:**
- Add Prometheus metrics
- Track: webhook latency, delivery rates, AI confidence, error rates
- Alert on: missing statuses, high error rates, webhook failures
- Dashboard for operational visibility

**Priority:** 🟡 **MEDIUM** - Required for production operations

---

### 9. **Per-Tenant Configuration** ⚠️ **PARTIAL**

**What's Missing:**
- Limited per-tenant configs
- No template allowlists
- No automation rules per tenant
- No compliance rules configuration

**Current:**
- `TenantPolicy` exists but minimal
- `BusinessProfile` has some configs

**Recommendation:**
```go
type TenantConfig struct {
    TenantID              uint
    MaxMessagesPerHour    int
    AllowedTemplates      []string // Template IDs
    AutoReplyEnabled      bool
    EscalationThreshold   float64
    ComplianceRules       string // JSON
    FallbackContact       string
    AllowedContentTypes   []string
}
```

**Priority:** 🟢 **LOW** - Nice to have

---

### 10. **Database Partitioning** ❌ **NOT IMPLEMENTED**

**What's Missing:**
- No partitioning for heavy tenants
- All data in single tables

**Impact:**
- Performance degradation at scale
- Harder to manage large datasets

**Priority:** 🟢 **LOW** - Only needed at very large scale

---

## 📊 Implementation Priority Matrix

| Component | Priority | Effort | Impact | Status |
|-----------|----------|--------|--------|--------|
| Status Webhook Handling | 🔴 HIGH | Medium | Critical | ❌ Missing |
| Message Queue System | 🔴 HIGH | High | Critical | ❌ Missing |
| Opt-In Management | 🔴 HIGH | Medium | Critical | ❌ Missing |
| Session Window Tracking | 🟡 MEDIUM-HIGH | Low | High | ⚠️ Partial |
| Rate Limiting | 🟡 MEDIUM-HIGH | Medium | High | ❌ Missing |
| Template Management | 🟡 MEDIUM | Medium | Medium | ⚠️ Partial |
| Redis Conversation State | 🟢 LOW-MEDIUM | Low | Medium | ⚠️ Partial |
| Observability | 🟡 MEDIUM | Medium | Medium | ⚠️ Partial |
| Per-Tenant Configs | 🟢 LOW | Low | Low | ⚠️ Partial |
| Database Partitioning | 🟢 LOW | High | Low | ❌ Missing |

---

## 🎯 Recommended Implementation Roadmap

### Phase 1: Critical Fixes (Week 1-2)
1. ✅ **Status Webhook Handling** - Track delivery/read receipts
2. ✅ **Message Queue** - Implement Redis queue for async processing
3. ✅ **Opt-In Management** - Add consent tracking

### Phase 2: Production Readiness (Week 3-4)
4. ✅ **Session Window Tracking** - Proactive window management
5. ✅ **Rate Limiting** - Per-tenant limits
6. ✅ **Template Management** - WhatsApp template system

### Phase 3: Scale & Optimization (Week 5-6)
7. ✅ **Redis Conversation State** - Caching for performance
8. ✅ **Observability** - Metrics and alerts
9. ✅ **Per-Tenant Configs** - Enhanced configuration

---

## 🔍 Code Locations for Implementation

### Status Webhook Handling
- **File**: `internal/social/meta_webhook.go`
- **Function**: `processWhatsAppWebhook()`
- **Add**: Status processing logic after message processing

### Message Queue
- **New Package**: `internal/queue/`
- **Files**: `redis_queue.go`, `worker.go`
- **Modify**: `internal/social/meta_webhook.go` to enqueue instead of direct call

### Opt-In Management
- **Model**: `internal/storage/models.go` - Add `OptIn` struct
- **Handler**: `internal/tenantapi/handler.go` - Add opt-in endpoints
- **Logic**: `internal/social/message_handler.go` - Check opt-in before sending

### Session Window Tracking
- **File**: `internal/social/message_handler.go`
- **Function**: `sendWhatsAppReply()` - Add session check before sending
- **Cache**: Use Redis for fast lookups

### Rate Limiting
- **New Package**: `internal/ratelimit/`
- **File**: `redis_limiter.go`
- **Middleware**: Add to `internal/tenantapi/handler.go`

---

## 📝 Next Steps

1. **Immediate**: Implement status webhook handling (blocks delivery tracking)
2. **This Week**: Add message queue system (required for scale)
3. **This Month**: Complete opt-in management (compliance requirement)
4. **Next Sprint**: Session window tracking and rate limiting

---

## ✅ What We're Doing Well

1. **Architecture Alignment**: Core components match recommended architecture
2. **Provider-Owned Model**: Correctly implemented
3. **Tenant Routing**: Robust and working
4. **AI Pipeline**: Functional with good error handling
5. **Escalation**: Well-implemented Telegram-based system
6. **Security**: Proper token encryption and auth

---

## 🚨 Critical Blockers for Production

Before going to production, these **must** be fixed:

1. ❌ **Status webhook handling** - Can't track delivery
2. ❌ **Message queue** - Webhook timeout risk
3. ❌ **Opt-in management** - Compliance risk

These are **blockers** - the system won't work reliably in production without them.

