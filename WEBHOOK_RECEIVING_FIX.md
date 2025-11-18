# Fix: Webhook Not Receiving Messages

## 🎯 The Real Problem

You're **not receiving messages** in your webhook. The features I proposed (status handling, queue, opt-in) assume messages are already arriving - they won't fix this.

## 🔍 Root Cause Analysis

To receive WhatsApp messages, **ALL** of these must be true:

1. ✅ **Webhook URL configured in Meta** - Must be verified
2. ✅ **Phone number subscribed to webhooks** - Must have `messages` field
3. ✅ **WABA subscribed to webhooks** - Required for provider-owned model
4. ✅ **Phone number verified** - Display name approved, code verified
5. ✅ **Webhook endpoint accessible** - Public HTTPS, responds quickly
6. ✅ **Tenant routing works** - Can resolve tenant from phone_number_id

## 🚨 Immediate Diagnostic Steps

### Step 1: Run Comprehensive Diagnostics

```bash
GET /api/tenant/whatsapp/diagnostics
```

This will show:
- ✅ Phone number assigned
- ✅ System token configured
- ✅ Phone number webhook subscription status
- ✅ WABA webhook subscription status
- ✅ Meta API status check
- ✅ Social account configuration

**Look for:**
- `phone_webhook_subscription.subscribed: false` ← **CRITICAL**
- `waba_webhook_subscription.subscribed: false` ← **CRITICAL**
- `meta_status_check.code_verification_status: "PENDING"` ← **BLOCKER**

### Step 2: Check Webhook Subscription Status

```bash
GET /api/tenant/whatsapp/webhook-status
```

**If `subscribed: false`:**
- Webhook subscription failed
- Use resubscribe endpoint

### Step 3: Verify Meta Webhook Configuration

**In Meta Developer Console:**
1. Go to: Your WhatsApp App → Configuration → Webhooks
2. Check:
   - ✅ Callback URL: `https://your-domain.com/api/webhooks/meta`
   - ✅ Verify Token: Matches `META_WEBHOOK_VERIFY_TOKEN`
   - ✅ Status: Shows "✅ Verified" (green checkmark)
   - ✅ Subscription Fields: Includes `messages` and `statuses`

**If not verified:**
- Click "Verify and Save"
- Check server logs for verification request
- Should see: `✅ Meta webhook verified successfully`

### Step 4: Check Server Logs

**Look for:**
```
📨 Meta webhook event received (POST)
📱 Processing WhatsApp webhook
📞 Processing WhatsApp change
phone_number_id: <id>
```

**If you DON'T see these logs:**
- Meta is not sending webhooks
- Webhook subscription issue
- Webhook URL not accessible

**If you see these logs but no messages:**
- Tenant resolution failing
- Check: `⚠️ Could not resolve tenant from WhatsApp webhook`

## 🔧 Fixes Based on Diagnostic Results

### Fix 1: Webhook Not Subscribed

**Symptom:** `phone_webhook_subscription.subscribed: false`

**Solution:**
```bash
POST /api/tenant/whatsapp/resubscribe
```

**Wait 30 seconds**, then check again:
```bash
GET /api/tenant/whatsapp/webhook-status
```

**If still false:**
- Check `META_SYSTEM_USER_TOKEN` is valid
- Check token has `whatsapp_business_messaging` permission
- Check phone number ID exists in Meta
- Check server logs for subscription errors

### Fix 2: WABA Not Subscribed

**Symptom:** `waba_webhook_subscription.subscribed: false`

**Solution:**
This requires admin action. Need to subscribe WABA via Meta API:

```bash
POST https://graph.facebook.com/v18.0/{waba-id}/subscribed_apps
  ?access_token={system-token}
  &subscribed_fields=messages,statuses
```

**Or use Meta Developer Console:**
1. Go to: WhatsApp → Configuration → Webhooks
2. Ensure WABA is selected
3. Subscribe to `messages` and `statuses`

### Fix 3: Webhook URL Not Verified

**Symptom:** Meta Console shows "Not Verified"

**Solution:**
1. Ensure `META_WEBHOOK_VERIFY_TOKEN` matches Meta Console
2. Ensure server is publicly accessible
3. Ensure HTTPS certificate is valid
4. Click "Verify and Save" in Meta Console
5. Check server logs for verification request

### Fix 4: Phone Number Not Verified

**Symptom:** `code_verification_status: "PENDING"`

**Solution:**
- Wait for Meta approval (can take 1-7 days)
- Check Meta Business Manager for approval status
- Ensure display name matches business verification

### Fix 5: Tenant Resolution Failing

**Symptom:** Logs show `⚠️ Could not resolve tenant from WhatsApp webhook`

**Solution:**
- Check `SocialAccount` exists for tenant
- Check `ExternalID` matches `phone_number_id`
- Check `Connected = true`
- Check `Mode = "provider_owned"`

## 🎯 Action Plan: Fix Webhook Receiving

### Today's Tasks (Priority Order)

1. **Run Diagnostics** (5 min)
   ```bash
   GET /api/tenant/whatsapp/diagnostics
   ```
   - Identify which check is failing

2. **Fix Webhook Subscription** (10 min)
   - If `phone_webhook_subscription.subscribed: false`
   - Call resubscribe endpoint
   - Wait and verify

3. **Fix WABA Subscription** (15 min)
   - If `waba_webhook_subscription.subscribed: false`
   - Subscribe via Meta API or Console
   - Verify subscription

4. **Verify Meta Webhook Config** (10 min)
   - Check Meta Developer Console
   - Verify webhook URL and token
   - Re-verify if needed

5. **Test Message Flow** (10 min)
   - Send test message from WhatsApp
   - Check server logs for webhook receipt
   - Verify tenant resolution

### Expected Outcome

After fixes:
- ✅ Diagnostics show all green
- ✅ Webhook subscription: `subscribed: true`
- ✅ WABA subscription: `subscribed: true`
- ✅ Test message triggers webhook
- ✅ Logs show message processing

## 🚨 Critical: What Won't Fix This

These features **won't help** until webhooks are receiving:
- ❌ Status webhook handling (assumes messages arriving)
- ❌ Message queue (assumes messages arriving)
- ❌ Opt-in management (assumes messages arriving)
- ❌ Session window tracking (assumes messages arriving)

**Fix webhook receiving FIRST**, then implement those features.

## 📋 Checklist: Webhook Receiving

- [ ] Run diagnostics endpoint
- [ ] Phone number webhook subscribed: `true`
- [ ] WABA webhook subscribed: `true`
- [ ] Meta webhook URL verified in Console
- [ ] Verify token matches environment variable
- [ ] Phone number verification status: `VERIFIED`
- [ ] Social account exists and connected
- [ ] Test message sent from WhatsApp
- [ ] Webhook received in server logs
- [ ] Tenant resolved successfully
- [ ] Message processed

## 🔄 If Still Not Working

1. **Check Meta Webhook Test Tool**
   - Meta Developer Console → Webhooks → Test
   - Send test event
   - Check server logs

2. **Check Network/Firewall**
   - Ensure server is publicly accessible
   - Check firewall allows Meta IPs
   - Test webhook endpoint: `GET /api/webhooks/meta/test`

3. **Check Meta App Status**
   - Ensure app is in "Live" mode (not Development)
   - Ensure WhatsApp product is added
   - Ensure business verification complete

4. **Contact Meta Support**
   - Provide: WABA ID, Phone Number ID, Webhook URL
   - Ask them to check webhook subscription status
   - Request webhook delivery logs

## ✅ Success Criteria

You'll know it's working when:
1. ✅ Diagnostics endpoint shows all green
2. ✅ Test message from WhatsApp triggers webhook
3. ✅ Server logs show: `💬 Processing WhatsApp message`
4. ✅ Conversation created in database
5. ✅ Auto-reply sent successfully

---

## 🎯 Next Steps

1. **Run diagnostics NOW** - Identify the blocker
2. **Fix the blocker** - Based on diagnostic results
3. **Test message flow** - Verify webhook receiving
4. **THEN** implement status handling, queue, etc.

**The webhook receiving issue must be fixed FIRST before any other features will help.**

