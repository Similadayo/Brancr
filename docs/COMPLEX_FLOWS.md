# Brancr Complex Flow Documentation

## 1. Post Scheduling Flow

### Overview
The post scheduling system is the core of Brancr's social media automation. It manages the complete lifecycle of a scheduled post from creation to execution.

### File: `internal/social/scheduler.go`

#### Key Components

**Scheduler.Run()** - Main polling loop
- Runs in a goroutine (started in `app.Run()`)
- Polls database every `pollInterval` (default: 30 seconds)
- Finds posts where `status = "scheduled" AND scheduled_at <= NOW()`
- Processes posts in batches of 50 (prevents memory bloat for high-volume businesses)

**Scheduler.processDuePosts()** - Batch processor
- Uses `FindInBatches` pattern to handle thousands of posts efficiently
- **Optimistic Locking**: Updates post status to "posting" atomically
  - Only processes posts where status transition succeeds (`rowsAffected == 1`)
  - Prevents duplicate execution if multiple schedulers run (failover scenario)
- Submits jobs to worker pool (if available) or fallback goroutines

**Scheduler.executePost()** - Single post execution
- **Step 1: Caption Processing** (depends on caption_mode)
  - Mode 0 (Empty): If no caption, generate one via AI
  - Mode 1 (Use As-Is): Use original caption unchanged
  - Mode 2 (Enhance): Optimize caption with AI (platform-specific hashtags, tone matching)
  - Mode 3 (Replace): Generate completely new caption
  - Quota check: Verify tenant hasn't exceeded daily AI call limit
  
- **Step 2: Media Validation**
  - Validate media URLs are accessible (HEAD request to verify)
  - Download and analyze for platform compliance
  - Extract image dimensions for platform requirements

- **Step 3: Platform Dispatch**
  - Detect platform (instagram, facebook, tiktok, youtube, whatsapp)
  - Call appropriate platform handler (e.g., `postToInstagram()`)
  - Pass caption_mode to preserve user intent
  
- **Step 4: Status Update**
  - On success: `status = "posted"`, `posted_at = NOW()`, `platform_post_id = <returned_id>`
  - On failure: `status = "failed"`, `error_message = <error_text>`, `retry_count++`
  - If retry_count < max (3), mark for retry in next poll cycle

- **Step 5: Analytics Trigger** (optional)
  - Call `AnalyticsService.CollectPostMetrics()` to begin metric collection
  - Analytics runs asynchronously (doesn't block post execution)

### Error Handling

**Circuit Breaker Pattern** (in `CircuitBreakers`)
- Each platform has a circuit breaker: `CircuitBreakers["instagram"]`, etc.
- If platform API fails 5x in 5 minutes, breaker opens
- Subsequent requests fail fast instead of waiting (prevents cascading failures)

**Retry Logic**
- Posts with `retry_count < 3` remain in "scheduled" status
- Next poll cycle will retry automatically
- Posts with `retry_count >= 3` move to "failed"
- Manual retry available via UI endpoint `/api/tenant/scheduled-posts/{id}/retry`

### Example Flow

```
1. User schedules post for 2025-12-11 15:00 UTC
   - POST /api/tenant/posts
   - Stored: ScheduledPost { status: "scheduled", scheduled_at: 2025-12-11 15:00 }

2. Scheduler runs at 15:00:30 (30sec polling)
   - Finds post with scheduled_at <= NOW()
   - Updates status to "posting" (claims ownership)
   - Submits to worker pool

3. Worker executes post
   - Reads caption_mode = "enhance"
   - Calls AIClient.OptimizeCaption() (uses 1 API call quota)
   - Calls postToInstagram() with optimized caption
   - Instagram returns media_id = "abc123"

4. Success case
   - Updates: status = "posted", platform_post_id = "abc123", posted_at = NOW()
   - Triggers: AnalyticsService.CollectPostMetrics(tenant_id, post_id)

5. Failure case (e.g., Instagram token expired)
   - Updates: status = "scheduled", retry_count++, error_message = "invalid_token"
   - Opens circuit breaker if 5th failure
   - Waits for next poll cycle (30 seconds)
```

---

## 2. Intent Detection & Routing Flow

### Overview
When a customer message arrives, the system must understand intent (inquiry, order, complaint, refund, etc.) and route to appropriate handler (chatbot reply, escalation, manual review, etc.).

### File: `internal/ai/intent_detector.go`

#### Key Components

**IntentDetector.DetectIntent()** - AI-powered intent analysis
```
Input: Message text, tenant context, platform
Output: DetectedIntent { intent, tone, confidence, requires_escalation, reasoning }
```

**Intent Types**
- `inquiry` - Question about products/services
- `order` - Purchase intent or order placement
- `payment` - Pricing/payment information request
- `complaint` - Product/service issue
- `refund` - Return/refund request
- `delivery_issue` - Shipping/logistics problem
- `casual` - General greeting or chat
- Custom intents from tenant's intent catalog

**Tone Detection**
- `positive` - Happy, satisfied customer
- `neutral` - Matter-of-fact
- `negative` - Unhappy, disappointed
- `urgent` - Needs immediate attention

**Escalation Logic** (requires_escalation = true when)
- Confidence < 0.25 (too uncertain for automated response)
- Intent is complaint/refund/delivery AND tone is negative/urgent (human judgment needed)
- Exception: "What's your refund policy?" → escalation = false (routine question)

#### Fallback Detection
When AI API is unavailable, uses keyword-based detection:
```go
if strings.Contains(lower, "refund") && strings.Contains(lower, "want") {
    intent = "refund"
    requires_escalation = true
}
```

### File: `internal/bot/intent_router.go`

#### Routing Decision Matrix

```
Intent=inquiry, confidence>0.7, requires_escalation=false
→ Chatbot AutoReply

Intent=order, confidence>0.8
→ Prompt for order details (product selection, quantity, payment)

Intent=complaint, requires_escalation=true
→ Escalate to human agent

Intent=refund, requires_escalation=true, tone=negative
→ Escalate to support team with "VIP" flag

Intent=casual
→ Friendly chitchat via chatbot

Intent=inquiry, confidence<0.6
→ Escalate with message "Please clarify what you need"
```

### Example Flow

```
1. Customer sends message on WhatsApp
   "Your product broke after 1 day. I want my money back!"

2. Message stored in Conversation table
   - platform = "whatsapp"
   - content = "Your product broke..."
   - direction = "inbound"

3. IntentDetector.DetectIntent() called
   - Loads tenant's business profile and custom intents
   - Sends to OpenAI: "Analyze this customer message..."
   - Response: {
       intent: "refund",
       tone: "negative",
       confidence: 0.95,
       requires_escalation: true,
       reasoning: "Customer demanding refund for broken product"
     }

4. IntentRouter routes message
   - escalation_required = true
   - Calls EscalationHandler.EscalateMessage(tenant_id, conversation_id)
   - Notifies human agents in Telegram
   - Sends auto-response: "We're sorry to hear that. A team member will assist you shortly."

5. Human agent responds
   - Initiates refund process
   - Sends response via MessageHandler.SendReply()
   - Message stored in database for memory/analytics
```

---

## 3. Webhook Processing Flow

### Overview
Meta, TikTok, and Gupshup send webhooks for incoming messages, message status updates, and account events. Brancr must validate, parse, and process these asynchronously.

### File: `internal/social/meta_webhook.go`

#### Security & Validation

**Step 1: Signature Verification**
```go
// Meta signs all webhooks with HMAC-SHA256
// Header: X-Hub-Signature-256 = sha256=<signature>
// Secret: META_WEBHOOK_VERIFY_TOKEN

payload_signature := ComputeSHA256(body, META_WEBHOOK_VERIFY_TOKEN)
if payload_signature != header_signature {
    return 401 Unauthorized
}
```

**Step 2: Event Parsing**
- Meta sends batch events: `{ "entry": [{ "messaging": [...] }] }`
- Each event can contain: message, delivery confirmation, read receipt, postback, etc.

#### Message Processing Pipeline

```
1. Receive webhook
   ↓
2. Validate signature (abort if invalid)
   ↓
3. Parse event batch
   ↓
4. For each message event:
   a. Extract: sender_id, recipient_id, text, timestamp
   b. Lookup tenant by page_id (Meta stores this in SocialAccount.PageID)
   c. Find or create Conversation entry
   d. Create Message record (direction="inbound")
   ↓
5. Trigger processing (non-blocking):
   a. Intent detection (async job in queue)
   b. Chatbot reply generation (async job in queue)
   c. Reply sending (async job in queue)
   ↓
6. Return 200 OK to Meta (don't block on processing)
```

#### Error Handling Patterns

**What to do if...**

| Scenario | Action | Logging |
|----------|--------|---------|
| Invalid signature | Return 401, drop event | WARN "signature_mismatch" |
| Unknown page_id | Return 200, log, skip processing | WARN "unknown_page_id" |
| DB insert fails | Return 500 (Meta will retry), increment retry counter | ERROR "db_write_failed" |
| Intent detection times out | Store message as "pending_intent", retry later | WARN "intent_timeout" |
| AI API is down (circuit breaker open) | Skip intent detection, use fallback | ERROR "circuit_breaker_open" |

### File: `internal/social/tiktok_webhook.go`

**TikTok-specific quirks:**
- Signature: `X-TikTok-Signature` = HMAC-SHA256(body, client_secret)
- Event types: `video.create`, `comment.create`, `message_send`, `share` (different from Meta)
- Rate limit: 100 calls/second (implement backoff)
- Retry policy: TikTok retries 3x with exponential backoff (40s, 5m, 10m)

### File: `internal/social/gupshup_webhook.go`

**Gupshup (WhatsApp provider):**
- Signature: `X-Gupshup-Signature` = HMAC-SHA256(body, api_key)
- Event types: `message_send`, `message_failed`, `message_delivered`, `message_read`
- Idempotency: Each event has `timestamp` + `message_id`; use `ContextHash` in DB to prevent duplicates

---

## 4. Escalation & Human Handoff Flow

### File: `internal/bot/escalation_handler.go`

#### When Escalation Happens

Automatically escalated to human team when:
1. **Intent Uncertainty** - confidence < 0.3
2. **Sensitive Issues** - complaint/refund/delivery with negative/urgent tone
3. **Manual Request** - customer asks "talk to human" / "need support"
4. **Policy Violations** - message contains flagged content (profanity, threats)
5. **Max Retries** - bot couldn't generate valid response 3x

#### Escalation Flow

```
1. EscalationHandler.EscalateMessage(tenant_id, conversation_id) called
   ↓
2. Create Escalation record
   - status = "open"
   - priority = "high" if tone=negative, else "normal"
   - reason = "intent_uncertain" | "complaint" | "manual_request" | etc.
   - assigned_to = NULL (unassigned)
   - created_at = NOW()
   ↓
3. Send Telegram notification to tenant owner
   - Message: "New escalation: [customer name] - [reason]"
   - Inline button: "View in Dashboard"
   - If priority=high, use 🔴 emoji
   ↓
4. Queue async job to:
   a. Store full conversation context in memory (SearchContext)
   b. Generate summary of conversation for agent
   c. Suggest response using AI (if available)
   ↓
5. Agent can respond in two ways:
   a. Via Telegram (replies to message)
   b. Via Dashboard (UI form)
   ↓
6. When agent responds:
   - Send message to customer via platform API
   - Create Message record (direction="outbound", source="human")
   - Update Escalation.status = "resolved"
   - Update Escalation.resolved_at = NOW()
```

#### Error Handling

**What if Telegram notification fails?**
- Log error but don't fail escalation
- Create escalation record anyway
- Escalation visible in dashboard

**What if agent doesn't respond for 24 hours?**
- Reminder notification sent at 12h and 23h
- Escalation moves to "stale" status
- Reassign to another agent automatically

---

## 5. Quota & Rate Limiting

### File: `internal/quota/quota.go`

#### Quota Limits (per plan, daily reset)

| Limit | Free | Pro | Enterprise |
|-------|------|-----|------------|
| AI Calls/day | 20 | 500 | Unlimited |
| Posts/day | 10 | 100 | Unlimited |
| Storage (MB) | 100 | 5000 | 50GB |
| API Calls/hour | 100 | 1000 | 10000 |

#### Quota Check Points

1. **AI Call Quota** - Before caption optimization
   ```go
   QuotaService.CheckAICallQuota(ctx, tenant_id)
   // If quota_remaining == 0:
   // Return error "quota_exceeded"
   // Fallback to non-AI caption (use as-is)
   ```

2. **Post Quota** - Before scheduling post
   ```go
   PostsCreatedToday := CountPostsBy(tenant_id, DateStart, DateEnd)
   if PostsCreatedToday >= quota_limit {
       return error "daily_post_limit_reached"
   }
   ```

3. **Storage Quota** - Before media upload
   ```go
   TotalStorageUsed := SumMediaSizeBy(tenant_id)
   if TotalStorageUsed + NewMediaSize > quota_limit_mb {
       return error "storage_quota_exceeded"
   }
   ```

#### Quota Reset

- Runs on first request after midnight (per tenant's timezone)
- Conditional reset: Only reset if `NOW() - last_reset_at > 24h`
- Transactional: Updates both usage counters and reset timestamp atomically

---

## Summary: Data Flow Diagram

```
Customer Message (WhatsApp/Instagram)
        ↓
    Webhook (/api/webhooks/meta)
        ↓
    Signature Verification (HMAC-SHA256)
        ↓
    Parse Event & Find Tenant
        ↓
    Store Message in DB
        ↓
    Async Queue Job: Process Message
        ├─ Intent Detection (AIClient.DetectIntent)
        │   └─ If requires_escalation → Escalation Handler
        │   └─ Else → Chatbot Reply Generator
        │
        ├─ Generate Reply (AIClient.Generate + memory context)
        │   └─ Uses conversation history (MemoryService.SearchContext)
        │   └─ Applies business profile context
        │   └─ Checks quota (QuotaService)
        │
        └─ Send Reply via Platform
            └─ MessageHandler.SendReply
            └─ Store sent message in DB
            └─ Update conversation.last_message_at

Scheduled Post (Webhook or Dashboard)
        ↓
    Create ScheduledPost (status="scheduled")
        ↓
    Scheduler polls every 30s for due posts
        ↓
    Claim post with optimistic locking (status="posting")
        ↓
    Process Caption (depending on caption_mode)
        ├─ Mode 0 (Empty): Generate via AI
        ├─ Mode 1 (Use As-Is): Use original
        ├─ Mode 2 (Enhance): Optimize with AI
        └─ Mode 3 (Replace): New caption from AI
        ↓
    Validate Media URLs
        ↓
    Submit to Platform API
        ├─ Instagram: Use media endpoint
        ├─ TikTok: Use video.upload + video.publish
        ├─ Facebook: Use timeline endpoint
        └─ YouTube: Use video.insert
        ↓
    On Success: status="posted", platform_post_id=<ID>
        ├─ Trigger analytics collection (async)
        └─ Cache result for quick lookup
        ↓
    On Failure: status="failed", retry_count++
        └─ If retry_count < 3: Will retry next cycle
        └─ If retry_count >= 3: Manual intervention needed
```
