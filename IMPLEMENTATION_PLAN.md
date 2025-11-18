# Implementation Plan - WhatsApp Production Readiness

## 🎯 Goal
Fix critical blockers and implement high-priority features to make WhatsApp messaging production-ready.

---

## Phase 1: Critical Fixes (This Week) 🔴

### 1. Status Webhook Handling (Day 1-2)
**Why:** Currently can't track message delivery status (one tick vs two ticks issue)

**Tasks:**
- [ ] Add `Statuses` field parsing to WhatsApp webhook handler
- [ ] Create `MessageStatus` model to track delivery/read receipts
- [ ] Update `Interaction` model to link to message status
- [ ] Process status webhooks: `sent` → `delivered` → `read`
- [ ] Add endpoint to query message status: `GET /api/tenant/messages/{id}/status`
- [ ] Update diagnostics to show delivery rates

**Files to Modify:**
- `internal/social/meta_webhook.go` - Add status processing
- `internal/storage/models.go` - Add `MessageStatus` model
- `internal/tenantapi/handler.go` - Add status query endpoint

**Success Criteria:**
- Status webhooks are processed and stored
- Can query delivery status for any sent message
- Diagnostics show delivery rates

---

### 2. Message Queue System (Day 2-3)
**Why:** Webhook handler blocks on AI processing, risk of timeouts

**Tasks:**
- [ ] Create `internal/queue/` package
- [ ] Implement Redis-based queue (`redis_queue.go`)
- [ ] Create worker pool (`worker.go`)
- [ ] Modify webhook handler to enqueue instead of direct call
- [ ] Add retry logic with exponential backoff
- [ ] Add dead letter queue for failed messages
- [ ] Add queue monitoring endpoint

**Files to Create:**
- `internal/queue/redis_queue.go` - Queue implementation
- `internal/queue/worker.go` - Worker pool
- `internal/queue/message_job.go` - Job structure

**Files to Modify:**
- `internal/social/meta_webhook.go` - Enqueue messages
- `internal/core/app.go` - Initialize queue workers

**Success Criteria:**
- Webhook returns 200 OK immediately (< 1s)
- Messages processed asynchronously
- Failed messages retried automatically
- Queue metrics available

---

### 3. Opt-In Management (Day 3-4)
**Why:** Compliance requirement - WhatsApp requires explicit consent

**Tasks:**
- [ ] Create `OptIn` model in `internal/storage/models.go`
- [ ] Add opt-in tracking when customer first messages
- [ ] Add opt-out handling (when customer sends stop/opt-out)
- [ ] Check opt-in before sending messages
- [ ] Add opt-in endpoints: `GET/POST /api/tenant/opt-ins`
- [ ] Add opt-in status to diagnostics
- [ ] Store consent proof (webhook payload)

**Files to Create/Modify:**
- `internal/storage/models.go` - Add `OptIn` struct
- `internal/tenantapi/handler.go` - Add opt-in endpoints
- `internal/social/message_handler.go` - Check opt-in before sending
- `internal/social/meta_webhook.go` - Track opt-in on first message

**Success Criteria:**
- All customers have opt-in records
- Can't send to opted-out customers
- Opt-in status visible in diagnostics
- Compliance-ready

---

## Phase 2: Production Readiness (Next Week) 🟡

### 4. Session Window Tracking (Day 5-6)
**Why:** Prevent sending outside 24-hour window, better UX

**Tasks:**
- [ ] Add session window check before sending
- [ ] Cache session windows in Redis (fast lookup)
- [ ] Auto-select template if outside window
- [ ] Update `Conversation.LastMessageAt` on every message
- [ ] Add session window status to diagnostics

**Files to Modify:**
- `internal/social/message_handler.go` - Add session check
- `internal/storage/conversation.go` - Update LastMessageAt
- `internal/tenantapi/handler.go` - Add session status endpoint

**Success Criteria:**
- Never send outside session window
- Automatic template fallback
- Session status visible in UI

---

### 5. Rate Limiting (Day 6-7)
**Why:** Prevent hitting Meta rate limits, fair usage

**Tasks:**
- [ ] Create `internal/ratelimit/` package
- [ ] Implement Redis-based sliding window limiter
- [ ] Add per-tenant rate limits (configurable)
- [ ] Add rate limit middleware to send endpoints
- [ ] Return 429 Too Many Requests when exceeded
- [ ] Add rate limit status to diagnostics

**Files to Create:**
- `internal/ratelimit/redis_limiter.go` - Rate limiter
- `internal/ratelimit/middleware.go` - HTTP middleware

**Files to Modify:**
- `internal/tenantapi/handler.go` - Add rate limit middleware
- `internal/social/message_handler.go` - Check rate limits

**Success Criteria:**
- Per-tenant rate limits enforced
- 429 responses when exceeded
- Rate limit metrics available

---

### 6. Template Management (Day 7-8)
**Why:** Required for business-initiated messages outside session window

**Tasks:**
- [ ] Create `WhatsAppTemplate` model
- [ ] Add template CRUD endpoints
- [ ] Integrate with Meta Template API (create/approve templates)
- [ ] Add template selection logic (category, language)
- [ ] Add template substitution engine
- [ ] Use templates when outside session window

**Files to Create:**
- `internal/social/templates.go` - Template management
- `internal/storage/models.go` - Add `WhatsAppTemplate`

**Files to Modify:**
- `internal/tenantapi/handler.go` - Add template endpoints
- `internal/social/message_handler.go` - Use templates

**Success Criteria:**
- Can create/manage templates
- Templates auto-selected when needed
- Template approval workflow working

---

## Phase 3: Optimization (Week 3) 🟢

### 7. Redis Conversation State (Day 9-10)
**Why:** Performance optimization, reduce DB load

**Tasks:**
- [ ] Cache active conversations in Redis
- [ ] Cache session windows (24h + buffer)
- [ ] Cache recent conversation history for LLM context
- [ ] Add cache invalidation on message updates
- [ ] Add cache warming for active conversations

**Files to Create:**
- `internal/cache/conversation_cache.go` - Conversation caching

**Files to Modify:**
- `internal/social/message_handler.go` - Use cache
- `internal/ai/responder.go` - Use cached context

**Success Criteria:**
- Faster session window checks (< 10ms)
- Reduced DB queries
- Better LLM context retrieval

---

### 8. Enhanced Observability (Day 10-11)
**Why:** Production monitoring and alerting

**Tasks:**
- [ ] Add Prometheus metrics
- [ ] Track: webhook latency, delivery rates, AI confidence, error rates
- [ ] Add alerts for: missing statuses, high error rates, webhook failures
- [ ] Create operational dashboard
- [ ] Add structured logging for key events

**Files to Create:**
- `internal/metrics/prometheus.go` - Metrics collection
- `internal/alerts/alert_manager.go` - Alert system

**Success Criteria:**
- Metrics dashboard available
- Alerts configured
- Operational visibility

---

## 📋 Implementation Order

```
Week 1:
Day 1-2: Status Webhook Handling
Day 2-3: Message Queue System  
Day 3-4: Opt-In Management

Week 2:
Day 5-6: Session Window Tracking
Day 6-7: Rate Limiting
Day 7-8: Template Management

Week 3:
Day 9-10: Redis Conversation State
Day 10-11: Enhanced Observability
```

---

## 🚀 Quick Start: Today's Tasks

### Task 1: Status Webhook Handling (Start Now)

**Step 1:** Add MessageStatus model
```go
// internal/storage/models.go
type MessageStatus struct {
    gorm.Model
    TenantID        uint   `gorm:"index"`
    InteractionID   uint   `gorm:"index"`
    PlatformMessageID string `gorm:"index"`
    Status          string // sent, delivered, read, failed
    Timestamp       time.Time
    ErrorCode       string
    ErrorMessage    string
}
```

**Step 2:** Update WhatsApp webhook handler
```go
// internal/social/meta_webhook.go
// In processWhatsAppWebhook(), add:
if len(change.Value.Statuses) > 0 {
    h.processWhatsAppStatuses(ctx, change.Value.Statuses, tenantID)
}
```

**Step 3:** Create status processor
```go
func (h *MetaWebhookHandler) processWhatsAppStatuses(ctx context.Context, statuses []Status, tenantID uint) {
    // Process each status update
    // Update MessageStatus table
    // Link to Interaction by PlatformMessageID
}
```

---

## ✅ Definition of Done

Each feature is considered done when:
- [ ] Code implemented and tested
- [ ] Unit tests written
- [ ] Integration tests pass
- [ ] Documentation updated
- [ ] Diagnostics endpoint updated (if applicable)
- [ ] Deployed to staging
- [ ] Verified in staging environment

---

## 🎯 Success Metrics

After Phase 1:
- ✅ 100% of status webhooks processed
- ✅ Webhook response time < 1s
- ✅ 100% opt-in compliance
- ✅ Zero webhook timeouts

After Phase 2:
- ✅ 0% messages sent outside session window
- ✅ Rate limits enforced per tenant
- ✅ Template fallback working

After Phase 3:
- ✅ Session checks < 10ms (cached)
- ✅ Operational dashboard live
- ✅ Alerts configured

---

## 🚨 Risk Mitigation

**Risk:** Breaking existing functionality
- **Mitigation:** Feature flags, gradual rollout, comprehensive testing

**Risk:** Performance degradation
- **Mitigation:** Load testing, monitoring, gradual scaling

**Risk:** Compliance issues
- **Mitigation:** Legal review, opt-in audit, documentation

---

## 📞 Next Steps

1. **Review this plan** - Confirm priorities and timeline
2. **Start Task 1** - Status webhook handling (most critical)
3. **Daily standup** - Track progress, adjust as needed
4. **Weekly review** - Assess completion, adjust plan

---

## 🔄 Iterative Approach

We'll implement incrementally:
- **Day 1:** Status webhook parsing (basic)
- **Day 2:** Status storage and querying
- **Day 3:** Queue system (basic)
- **Day 4:** Queue workers and retries
- And so on...

Each day delivers working, testable code.

