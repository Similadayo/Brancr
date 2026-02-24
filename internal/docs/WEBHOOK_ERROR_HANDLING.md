# Webhook Error Handling Enhancement Guide

## Current State Analysis

✅ **Strong Points**
- Meta webhook has comprehensive logging for verification
- Event routing logs the platform type
- Payload parsing has error logging
- Body size and preview logged for debugging

⚠️ **Gaps to Address**
1. No explicit error logging for database failures during message storage
2. Silent failures in async processing (intent detection, reply generation)
3. Missing tenant resolution error logging
4. No circuit breaker status logging
5. TikTok and Gupshup webhooks need similar logging enhancement

---

## Enhancement 1: Add Error Logging to Message Storage

**File: `internal/social/meta_webhook.go`**

In `processInstagramWebhook()` and similar functions, add detailed logging:

```go
// When storing incoming message
message := storage.Message{
    TenantID:       tenantID,
    ConversationID: conv.ID,
    CustomerID:     customer.ID,
    Platform:       "instagram",
    ExternalID:     event.MessageID,
    Content:        event.Text,
    Direction:      "inbound",
    ReceivedAt:     time.Now(),
}

if err := h.DB.Create(&message).Error; err != nil {
    // ❌ ERROR CASE: Log with full context
    h.Logger.Error(
        "❌ Failed to store inbound message",
        zap.Stringer("tenant_id", tenantID),
        zap.Stringer("conversation_id", conv.ID),
        zap.String("platform", "instagram"),
        zap.String("external_id", event.MessageID),
        zap.String("customer_name", customer.Name),
        zap.Int("message_length", len(event.Text)),
        zap.String("message_preview", truncate(event.Text, 100)),
        zap.Error(err),
        zap.Stack("stacktrace"),
    )
    // Alert: This message was received but not stored - may lose customer data
    metrics.IncrementCounter("webhook.message_storage_error", map[string]string{
        "platform": "instagram",
        "reason":   "database_error",
    })
    return fmt.Errorf("failed to store message: %w", err)
}

h.Logger.Info(
    "✅ Message stored successfully",
    zap.Stringer("tenant_id", tenantID),
    zap.Stringer("message_id", message.ID),
    zap.String("external_id", event.MessageID),
)
```

---

## Enhancement 2: Add Error Logging for Async Job Submission

**File: `internal/social/meta_webhook.go`**

When submitting async jobs (intent detection, reply generation):

```go
// After storing message, queue async processing job
job := &ProcessMessageJob{
    MessageID:      message.ID,
    TenantID:       tenantID,
    ConversationID: conv.ID,
    CustomerID:     customer.ID,
    MessageText:    event.Text,
    Platform:       "instagram",
}

if h.JobQueue != nil {
    if err := h.JobQueue.Enqueue(ctx, job, 0); err != nil {
        // ❌ WARN: Job queue is down, but don't fail webhook
        h.Logger.Warn(
            "⚠️ Failed to queue message processing job (will retry manually later)",
            zap.Stringer("message_id", message.ID),
            zap.Stringer("tenant_id", tenantID),
            zap.Error(err),
            zap.String("reason", "job_queue_error"),
        )
        // Message is still stored, just won't be auto-processed
        metrics.IncrementCounter("webhook.job_queue_error", map[string]string{
            "platform": "instagram",
            "operation": "enqueue_message_processing",
        })
    } else {
        h.Logger.Info(
            "✅ Message processing job queued",
            zap.Stringer("message_id", message.ID),
            zap.String("operation", "process_message"),
        )
    }
} else {
    h.Logger.Warn(
        "⚠️ Job queue not initialized, skipping async processing",
        zap.Stringer("tenant_id", tenantID),
    )
}
```

---

## Enhancement 3: Add Error Logging for Tenant Resolution

**File: `internal/social/meta_webhook.go`**

When finding tenant from page_id:

```go
// Resolve tenant from page_id
var account storage.SocialAccount
if err := h.DB.Where("external_id = ? AND platform = ?", pageID, "instagram").
    First(&account).Error; err != nil {
    // ❌ CRITICAL: We can't find which tenant this message belongs to
    h.Logger.Error(
        "❌ Failed to resolve tenant from page_id",
        zap.String("page_id", pageID),
        zap.String("platform", "instagram"),
        zap.Error(err),
        zap.String("error_type", errorType(err)), // "record_not_found" or "database_error"
    )
    
    if errors.Is(err, gorm.ErrRecordNotFound) {
        // ❌ ALERT: Page not connected to any tenant
        h.Logger.Warn(
            "⚠️ Received message for unconnected Instagram page",
            zap.String("page_id", pageID),
            zap.String("action", "message_dropped"),
        )
        metrics.IncrementCounter("webhook.unknown_page", map[string]string{
            "platform": "instagram",
        })
        // Still return 200 to tell Meta webhook was received
        return
    } else {
        // Database error, not a misconfiguration
        h.Logger.Error(
            "❌ Database error during tenant resolution",
            zap.String("page_id", pageID),
            zap.Error(err),
        )
        metrics.IncrementCounter("webhook.database_error", map[string]string{
            "operation": "tenant_resolution",
            "platform":  "instagram",
        })
        return
    }
}

tenantID := account.TenantID
h.Logger.Info(
    "✅ Tenant resolved",
    zap.Stringer("tenant_id", tenantID),
    zap.String("page_id", pageID),
)
```

---

## Enhancement 4: Add Circuit Breaker Status Logging

**File: `internal/social/meta_webhook.go`**

Before sending replies via API:

```go
// Check circuit breaker status before API call
if h.CircuitBreakers != nil {
    if breaker, ok := h.CircuitBreakers["instagram"]; ok {
        if breaker.IsOpen() {
            h.Logger.Warn(
                "⚠️ Circuit breaker OPEN for Instagram API",
                zap.String("reason", "API failures detected"),
                zap.String("action", "skipping_reply_generation"),
            )
            // Don't attempt reply, customer will see slow response
            metrics.IncrementCounter("webhook.circuit_breaker_open", map[string]string{
                "platform": "instagram",
            })
            return
        }
    }
}

// Proceed with API call
if err := h.MessageHandler.SendReply(ctx, tenantID, "instagram", externalID, reply); err != nil {
    h.Logger.Error(
        "❌ Failed to send reply via Instagram API",
        zap.Stringer("tenant_id", tenantID),
        zap.String("external_id", externalID),
        zap.Error(err),
        zap.String("reply_preview", truncate(reply, 100)),
    )
    metrics.IncrementCounter("webhook.api_call_error", map[string]string{
        "platform": "instagram",
        "operation": "send_reply",
    })
    // Don't fail webhook, customer will see no reply but message was processed
} else {
    h.Logger.Info(
        "✅ Reply sent successfully",
        zap.Stringer("tenant_id", tenantID),
        zap.String("external_id", externalID),
    )
}
```

---

## Enhancement 5: TikTok Webhook Error Logging

**File: `internal/social/tiktok_webhook.go`**

Add similar comprehensive logging:

```go
func (h *TikTokWebhookHandler) HandleTikTokWebhook(w http.ResponseWriter, r *http.Request) {
    h.Logger.Info(
        "📨 TikTok webhook event received",
        zap.String("method", r.Method),
        zap.String("user_agent", r.Header.Get("User-Agent")),
    )

    // Signature verification
    signature := r.Header.Get("X-TikTok-Signature")
    body, _ := io.ReadAll(r.Body)
    
    if !h.verifySignature(string(body), signature) {
        h.Logger.Warn(
            "❌ TikTok webhook signature verification failed",
            zap.String("signature_received", truncate(signature, 32)),
            zap.String("payload_size", len(body)),
        )
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    h.Logger.Info("✅ TikTok webhook signature verified")

    // Parse and process event
    var event TikTokEvent
    if err := json.Unmarshal(body, &event); err != nil {
        h.Logger.Error(
            "❌ Failed to parse TikTok webhook payload",
            zap.Error(err),
            zap.String("payload_preview", string(body[:min(200, len(body))])),
        )
        http.Error(w, "Bad Request", http.StatusBadRequest)
        return
    }

    h.Logger.Info(
        "🔍 TikTok event parsed",
        zap.String("event_type", event.Type),
        zap.String("video_id", event.VideoID),
    )

    // Process with error handling
    if err := h.processEvent(context.Background(), &event); err != nil {
        h.Logger.Error(
            "❌ Error processing TikTok event",
            zap.String("event_type", event.Type),
            zap.String("video_id", event.VideoID),
            zap.Error(err),
        )
        metrics.IncrementCounter("webhook.tiktok_processing_error", map[string]string{
            "event_type": event.Type,
        })
    }

    w.WriteHeader(http.StatusOK)
}
```

---

## Enhancement 6: Gupshup Webhook Error Logging

**File: `internal/social/gupshup_webhook.go`**

```go
func (h *GupshupWebhookHandler) HandleGupshupWebhook(w http.ResponseWriter, r *http.Request) {
    h.Logger.Info(
        "📨 Gupshup webhook event received",
        zap.String("method", r.Method),
    )

    body, _ := io.ReadAll(r.Body)
    defer r.Body.Close()

    // Verify signature (Gupshup uses HMAC-SHA256)
    signature := r.Header.Get("X-Gupshup-Signature")
    if !h.verifySignature(string(body), signature) {
        h.Logger.Warn(
            "❌ Gupshup webhook signature verification failed",
            zap.String("expected", truncate(signature, 32)),
        )
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    h.Logger.Info("✅ Gupshup webhook signature verified")

    // Check for idempotency (Gupshup may retry)
    var payload GupshupPayload
    json.Unmarshal(body, &payload)

    if payload.Timestamp > 0 && payload.MessageID != "" {
        // Use context_hash to check if we've already processed this
        hash := fmt.Sprintf("%d-%s", payload.Timestamp, payload.MessageID)
        var existing storage.MemoryItem
        if h.DB.Where("context_hash = ?", hash).First(&existing).Error == nil {
            h.Logger.Info(
                "⏭️ Skipping duplicate Gupshup webhook (already processed)",
                zap.String("message_id", payload.MessageID),
            )
            w.WriteHeader(http.StatusOK)
            return
        }
    }

    // Process message with error logging
    if err := h.processMessage(context.Background(), &payload); err != nil {
        h.Logger.Error(
            "❌ Error processing Gupshup message",
            zap.String("message_id", payload.MessageID),
            zap.Error(err),
        )
        metrics.IncrementCounter("webhook.gupshup_processing_error")
    }

    w.WriteHeader(http.StatusOK)
}
```

---

## Logging Best Practices Implemented

| Aspect | Pattern |
|--------|---------|
| **Success** | `✅ Action completed` + INFO level |
| **Warning** | `⚠️ Something unexpected but recoverable` + WARN level |
| **Error** | `❌ Operation failed` + ERROR level |
| **Context** | Always include: tenant_id, entity_id, platform, operation |
| **Debugging** | Include: payload preview, parsed values, error type |
| **Metrics** | Increment counters for dashboards/alerting |
| **Silent Failures** | Log with ERROR level + include error reason |

---

## Testing Error Logging

Add integration tests to verify logging:

```go
func TestMetaWebhookHandler_MissingPageID(t *testing.T) {
    // Should log ERROR when page_id not found in DB
    // Should increment metric "webhook.unknown_page"
    // Should return 200 to Meta
}

func TestMetaWebhookHandler_DatabaseDown(t *testing.T) {
    // Should log ERROR with "database_error"
    // Should increment metric "webhook.database_error"
    // Should return 200 to Meta (don't fail webhook)
}

func TestMetaWebhookHandler_JobQueueDown(t *testing.T) {
    // Should log WARN
    // Message should still be stored
    // Should increment metric "webhook.job_queue_error"
}
```

---

## Monitoring & Alerting

Create Prometheus alerts for webhook failures:

```yaml
# prometheus-alerts.yml
- alert: WebhookProcessingFailure
  expr: rate(webhook.message_storage_error[5m]) > 0
  for: 2m
  annotations:
    summary: "Webhook messages not being stored ({{ $value }} errors/sec)"
    action: "Check database connection and disk space"

- alert: CircuitBreakerOpen
  expr: webhook.circuit_breaker_open > 0
  for: 1m
  annotations:
    summary: "API circuit breaker open (platform degraded)"
    action: "Check platform API health and rate limits"

- alert: JobQueueBacklog
  expr: webhook.job_queue_error > 10
  for: 5m
  annotations:
    summary: "High job queue failure rate"
    action: "Check Redis connection and queue size"
```
