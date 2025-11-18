# WhatsApp Webhook Troubleshooting Guide

## Problem: Messages Not Received (One Tick Only)

If you're seeing **one tick** (sent) instead of **two ticks** (delivered), and no webhook is received, this indicates:

1. ✅ Message was sent from your phone
2. ❌ Message was NOT delivered to Meta's servers
3. ❌ Webhook was NOT triggered

## Diagnostic Steps

### Step 1: Check Webhook Subscription Status

**New Endpoint Added:** `GET /api/tenant/whatsapp/webhook-status`

```bash
curl -X GET https://your-api.com/api/tenant/whatsapp/webhook-status \
  -H "Authorization: Bearer <tenant-token>"
```

**Expected Response:**
```json
{
  "assigned": true,
  "phone_number_id": "123456789",
  "phone_number": "+1234567890",
  "subscribed": true,
  "message": "Webhook is subscribed"
}
```

**If `subscribed: false`:**
- Webhook subscription failed or was never completed
- Use Step 2 to resubscribe

### Step 2: Resubscribe to Webhooks

**New Endpoint Added:** `POST /api/tenant/whatsapp/resubscribe`

```bash
curl -X POST https://your-api.com/api/tenant/whatsapp/resubscribe \
  -H "Authorization: Bearer <tenant-token>"
```

This will:
- Attempt to subscribe the phone number to webhooks
- Retry up to 5 times with exponential backoff
- Log success/failure in server logs

**Wait 10-30 seconds**, then check status again with Step 1.

### Step 3: Verify Meta Webhook Configuration

Go to **Meta Developer Console** → Your WhatsApp App → Configuration → Webhooks:

1. **Callback URL:** Must be `https://your-domain.com/api/webhooks/meta`
2. **Verify Token:** Must match `META_WEBHOOK_VERIFY_TOKEN` environment variable
3. **Subscription Fields:** Must include `messages`
4. **Status:** Should show "✅ Verified" (green checkmark)

**To verify webhook:**
- Click "Verify and Save" in Meta Console
- Check server logs for verification request
- Should see: `✅ Meta webhook verified successfully`

### Step 4: Check Server Logs

Look for these log entries:

**Webhook Subscription:**
```
WhatsApp phone number subscribed
phone_number_id: <id>
response: <response>
```

**Webhook Receipt:**
```
📨 Meta webhook event received (POST)
📱 Processing WhatsApp webhook
📞 Processing WhatsApp change
phone_number_id: <id>
```

**If you don't see subscription logs:**
- Check `META_SYSTEM_USER_TOKEN` is set
- Check phone number assignment succeeded
- Check network connectivity to Meta API

**If you see subscription logs but no receipt:**
- Webhook URL might not be reachable from Meta
- Check firewall/security groups
- Verify HTTPS certificate is valid
- Check Meta Console shows webhook as "Verified"

### Step 5: Test Webhook Endpoint

**Test Endpoint:** `GET /api/webhooks/meta/test`

```bash
curl https://your-api.com/api/webhooks/meta/test
```

**Expected Response:**
```json
{
  "status": "ok",
  "endpoint": "/api/webhooks/meta",
  "message": "Meta webhook endpoint is reachable"
}
```

If this fails, your server might not be publicly accessible.

## Common Issues & Solutions

### Issue 1: Webhook Not Subscribed

**Symptoms:**
- `subscribed: false` in status check
- No subscription logs in server

**Solutions:**
1. Ensure `META_SYSTEM_USER_TOKEN` is set correctly
2. Call resubscribe endpoint
3. Check phone number is assigned (`status = "assigned"`)
4. Verify token has `whatsapp_business_messaging` permission

### Issue 2: Webhook URL Not Verified

**Symptoms:**
- Meta Console shows "Not Verified"
- Verification requests fail

**Solutions:**
1. Ensure `META_WEBHOOK_VERIFY_TOKEN` matches Meta Console
2. Check server is publicly accessible
3. Verify HTTPS certificate is valid
4. Check server logs for verification requests

### Issue 3: Messages Sent But Not Received

**Symptoms:**
- One tick (sent) but not two ticks (delivered)
- No webhook events in logs

**Possible Causes:**
1. **Phone number not verified:** Display name must be approved
2. **Webhook not subscribed:** Use diagnostic endpoints
3. **App not subscribed to WABA:** Need WABA-level subscription
4. **24-hour window expired:** Can only send template messages

**Solutions:**
1. Check phone number verification status in Meta Console
2. Ensure webhook is subscribed (Step 1-2)
3. Verify app is subscribed to WABA (admin task)
4. Use template messages for outbound if window expired

### Issue 4: Webhook Subscription Fails Silently

**Symptoms:**
- Subscription attempt logs show errors
- Status check returns `subscribed: false`

**Check Logs For:**
```
whatsapp phone subscription returned non-200
status: <status_code>
body: <error_message>
```

**Common Error Codes:**
- **401:** Invalid access token
- **403:** Token doesn't have required permissions
- **404:** Phone number ID not found
- **400:** Invalid request format

**Solutions:**
1. Verify `META_SYSTEM_USER_TOKEN` is valid
2. Check token has `whatsapp_business_messaging` permission
3. Verify phone number ID exists in Meta
4. Check token hasn't expired

## Verification Checklist

- [ ] Phone number is assigned (`GET /api/tenant/whatsapp/current`)
- [ ] Webhook is subscribed (`GET /api/tenant/whatsapp/webhook-status`)
- [ ] Meta webhook URL is verified (Meta Console shows ✅)
- [ ] `META_WEBHOOK_VERIFY_TOKEN` matches Meta Console
- [ ] `META_SYSTEM_USER_TOKEN` is set and valid
- [ ] Server is publicly accessible (test endpoint works)
- [ ] Phone number display name is approved
- [ ] App has `whatsapp_business_messaging` permission

## Next Steps After Fixing

1. **Resubscribe:** `POST /api/tenant/whatsapp/resubscribe`
2. **Wait 30 seconds** for subscription to complete
3. **Check status:** `GET /api/tenant/whatsapp/webhook-status`
4. **Send test message** from WhatsApp
5. **Check logs** for webhook receipt
6. **Verify conversation** is created in database

## Additional Debugging

### Check Phone Number Details in Meta

Use Meta Graph API Explorer:
```
GET /v18.0/{phone-number-id}?fields=id,display_phone_number,verified_name,quality_rating,code_verification_status
```

### Check App Subscription to WABA

```
GET /v18.0/{waba-id}/subscribed_apps
```

Should show your app ID with `messages` field subscribed.

### Manual Webhook Test

Use Meta's webhook testing tool:
1. Go to Meta Developer Console → Webhooks
2. Click "Test" button
3. Select "messages" event
4. Check server logs for test event

