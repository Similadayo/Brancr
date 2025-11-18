# WhatsApp Diagnostics Implementation

Based on comprehensive analysis of common WhatsApp Cloud API issues, we've implemented a full diagnostic and troubleshooting system.

## What Was Implemented

### 1. Enhanced Message Sending with Error Detection

**File:** `internal/social/message_handler.go`

- **Improved error parsing:** Now detects errors even when Meta returns 200 OK with errors in response body
- **Specific error code handling:**
  - `131047`: 24-hour messaging window expired
  - `131026`: Invalid recipient phone number format
  - `131031`: Phone number not registered or blocked
- **Message ID tracking:** Logs message ID for tracking delivery status via webhooks
- **Better logging:** Includes detailed error information for debugging

### 2. Phone Number Status Check

**File:** `internal/social/whatsapp_account.go`

**New Function:** `GetPhoneNumberStatus(logger, phoneID, accessToken)`

Fetches detailed status from Meta API including:
- `verified_name`: Display name approval status
- `quality_rating`: Meta's quality rating
- `code_verification_status`: Verification status (PENDING, VERIFIED, etc.)
- `status`: Phone number status

### 3. WABA-Level Subscription Check

**File:** `internal/social/whatsapp_account.go`

**New Function:** `CheckWABASubscription(logger, wabaID, accessToken)`

Checks if the app is subscribed to WABA-level webhooks, which is critical for provider-owned model. Returns:
- Subscription status (true/false)
- List of subscribed fields (`messages`, `statuses`, etc.)

### 4. Comprehensive Diagnostic Endpoint

**File:** `internal/tenantapi/handler.go`

**New Endpoint:** `GET /api/tenant/whatsapp/diagnostics`

Performs comprehensive health check covering all common issues:

#### Checks Performed:

1. **Phone Number Assignment**
   - Verifies number is assigned to tenant
   - Shows phone number details

2. **System Token Configuration**
   - Verifies `META_SYSTEM_USER_TOKEN` is set
   - Critical for provider-owned model

3. **Meta API Status Check**
   - Fetches live status from Meta API
   - Checks verification status
   - Validates display name approval

4. **Phone Number Webhook Subscription**
   - Verifies phone number is subscribed to webhooks
   - Critical for receiving inbound messages

5. **WABA-Level Subscription**
   - Checks WABA subscription (required for provider-owned)
   - Shows subscribed fields

6. **Social Account Configuration**
   - Verifies account exists and is connected
   - Checks mode is `provider_owned`
   - Validates external_id for webhook routing

7. **Overall Health Assessment**
   - Provides `healthy`/`unhealthy` status
   - Lists all errors and warnings
   - Indicates if system is `ready` for messaging

### 5. Quick Diagnostic Endpoints

**File:** `internal/tenantapi/handler.go`

**New Endpoints:**
- `GET /api/tenant/whatsapp/webhook-status` - Quick webhook subscription check
- `POST /api/tenant/whatsapp/resubscribe` - Manually trigger webhook resubscription

## Usage

### Run Comprehensive Diagnostics

```bash
GET /api/tenant/whatsapp/diagnostics
```

**Response Example:**
```json
{
  "timestamp": "2025-01-XX...",
  "tenant_id": 1,
  "assigned": true,
  "phone_number_id": "123456789",
  "phone_number": "+1234567890",
  "verified_name": "My Business",
  "quality_rating": "GREEN",
  "system_token_configured": true,
  "meta_status_check": {
    "success": true,
    "verified_name": "My Business",
    "quality_rating": "GREEN",
    "code_verification_status": "VERIFIED",
    "status": "CONNECTED"
  },
  "phone_webhook_subscription": {
    "subscribed": true
  },
  "waba_webhook_subscription": {
    "subscribed": true,
    "subscribed_fields": ["messages", "statuses"]
  },
  "social_account": {
    "exists": true,
    "connected": true,
    "mode": "provider_owned",
    "page_id": "123456789",
    "external_id": "123456789"
  },
  "health": {
    "status": "healthy",
    "errors": [],
    "warnings": [],
    "ready": true
  }
}
```

### Check Webhook Status

```bash
GET /api/tenant/whatsapp/webhook-status
```

### Resubscribe to Webhooks

```bash
POST /api/tenant/whatsapp/resubscribe
```

## Common Issues Detected

The diagnostics endpoint will identify:

### Critical Errors (blocks messaging):
- ❌ Phone number not assigned
- ❌ System token not configured
- ❌ Phone number not subscribed to webhooks
- ❌ Social account not found or not connected

### Warnings (may cause issues):
- ⚠️ Phone number verification status not VERIFIED
- ⚠️ WABA not subscribed to webhooks
- ⚠️ Account mode incorrect (not provider_owned)

## Next Steps After Diagnostics

1. **If webhook not subscribed:**
   - Call `POST /api/tenant/whatsapp/resubscribe`
   - Wait 30 seconds
   - Check status again

2. **If WABA not subscribed:**
   - Admin needs to subscribe WABA via Meta API
   - Use `POST /v18.0/{waba-id}/subscribed_apps` with `subscribed_fields=messages`

3. **If verification status is PENDING:**
   - Wait for Meta approval (can take 1-7 days)
   - Check Meta Business Manager for approval status

4. **If errors in message sending:**
   - Check logs for specific error codes
   - Verify recipient number format (E.164)
   - Ensure within 24-hour messaging window or use template

## Integration with Troubleshooting Guide

This implementation addresses all issues mentioned in the analysis:

✅ **Message status tracking** - Enhanced error detection and logging  
✅ **Webhook subscription verification** - Phone number and WABA level  
✅ **Phone number activation check** - Live status from Meta API  
✅ **Business verification status** - Via Meta API status check  
✅ **Provider-owned model support** - WABA subscription checks  
✅ **Comprehensive diagnostics** - Single endpoint for all checks  

## Testing

After implementing, test with:

1. **Healthy setup:**
   ```bash
   curl -H "Authorization: Bearer <token>" \
     https://your-api.com/api/tenant/whatsapp/diagnostics
   ```
   Should show `"ready": true` and no errors

2. **Send test message:**
   - Check logs for message ID
   - Verify webhook receives delivery status
   - Check for two ticks (delivered) in WhatsApp

3. **If issues persist:**
   - Review diagnostic output
   - Check specific error messages
   - Follow remediation steps above

