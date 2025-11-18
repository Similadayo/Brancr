# Meta WhatsApp Cloud API Webhook Requirements

Based on Meta's official documentation, here are the **exact requirements** to receive messages and fix the "one tick" issue.

---

## 🔴 Critical Requirements (All Must Be Met)

### 1. **WABA-Level Subscription** (REQUIRED)

**Meta Requirement:** Your app MUST be subscribed to the WhatsApp Business Account (WABA).

**API Call:**
```bash
POST https://graph.facebook.com/v21.0/{WABA_ID}/subscribed_apps
  ?access_token={SYSTEM_USER_TOKEN}
  &subscribed_fields=messages,statuses
```

**What We Have:**
- ✅ Function exists: `subscribeAppToWABA()` in `whatsapp_account.go:742`
- ⚠️ **Issue:** Only subscribes `messages`, missing `statuses`
- ⚠️ **Issue:** Not automatically called after phone number assignment

**Fix Required:**
1. Add `statuses` to subscribed fields
2. Ensure WABA subscription happens automatically
3. Verify subscription status in diagnostics

---

### 2. **Phone Number-Level Subscription** (REQUIRED)

**Meta Requirement:** Each phone number MUST be subscribed to receive webhooks.

**API Call:**
```bash
POST https://graph.facebook.com/v21.0/{PHONE_NUMBER_ID}/subscribed_apps
  ?access_token={SYSTEM_USER_TOKEN}
  &subscribed_fields=messages,statuses
```

**What We Have:**
- ✅ Function exists: `SubscribePhoneNumberToWebhooks()` in `whatsapp_account.go:456`
- ⚠️ **Issue:** Only subscribes `messages`, missing `statuses`
- ✅ Called after phone number assignment (we added this)

**Fix Required:**
1. Add `statuses` to subscribed fields
2. Ensure both `messages` AND `statuses` are subscribed

---

### 3. **Meta Dashboard Webhook Configuration** (REQUIRED)

**Meta Requirement:** Webhook must be configured in Meta Developer Console.

**Steps:**
1. Go to: Meta Developer Console → Your App → Configuration → Webhooks
2. Select: WhatsApp product
3. Set Callback URL: `https://your-domain.com/api/webhooks/meta`
4. Set Verify Token: Must match `META_WEBHOOK_VERIFY_TOKEN`
5. Subscribe to fields: `messages` AND `statuses`
6. Click "Verify and Save"
7. Status should show: ✅ "Verified"

**What We Have:**
- ✅ Webhook endpoint exists: `/api/webhooks/meta`
- ✅ Verification handler exists
- ⚠️ **Issue:** Need to verify this is configured correctly in Meta Dashboard

**Fix Required:**
1. Verify webhook URL in Meta Dashboard
2. Ensure `messages` AND `statuses` are subscribed
3. Re-verify if needed

---

### 4. **Phone Number Verification Status** (REQUIRED)

**Meta Requirement:** Phone number must be fully verified.

**Statuses:**
- `PENDING` - Not verified, webhooks won't work
- `VERIFIED` - Fully verified, webhooks will work
- `UNVERIFIED` - Verification failed

**What We Have:**
- ✅ We track `code_verification_status` in sync
- ✅ Diagnostics check this
- ⚠️ **Issue:** If status is `PENDING`, webhooks won't receive messages

**Fix Required:**
- Wait for Meta approval (can take 1-7 days)
- Check Meta Business Manager for approval status

---

### 5. **Webhook Endpoint Requirements** (REQUIRED)

**Meta Requirements:**
1. ✅ Must be HTTPS (we have this)
2. ✅ Must respond with 200 OK within 5 seconds (we do this)
3. ✅ Must handle GET (verification) and POST (events) (we do this)
4. ⚠️ **Missing:** Webhook signature verification (security best practice)

**What We Have:**
- ✅ HTTPS endpoint
- ✅ Fast response (< 1s)
- ✅ Handles GET and POST
- ❌ **Missing:** Signature verification

**Fix Required:**
- Add webhook signature verification (security)

---

## 🔍 The "One Tick" Issue Explained

**One Tick (Sent):** Message accepted by WhatsApp API but NOT delivered to recipient.

**Two Ticks (Delivered):** Message delivered to recipient's device.

**Why One Tick Happens:**
1. **Phone number not verified** - Meta won't deliver until verified
2. **Webhook not subscribed** - Can't track delivery status
3. **Recipient blocked number** - Delivery fails silently
4. **Invalid phone number format** - Not in E.164 format
5. **24-hour window expired** - Can only send templates
6. **Meta policy violation** - Account restricted

**To Fix:**
- Ensure phone number is `VERIFIED`
- Subscribe to `statuses` webhook field (to track delivery)
- Check recipient number format
- Verify within 24-hour window or use template

---

## 📋 Complete Checklist (Meta Requirements)

### Technical Setup (Code)

- [ ] **WABA Subscription**
  - [ ] Subscribe WABA to app: `POST /v21.0/{WABA_ID}/subscribed_apps`
  - [ ] Subscribe fields: `messages` AND `statuses`
  - [ ] Use system user token (not tenant token)
  - [ ] Verify subscription: `GET /v21.0/{WABA_ID}/subscribed_apps`

- [ ] **Phone Number Subscription**
  - [ ] Subscribe each phone number: `POST /v21.0/{PHONE_NUMBER_ID}/subscribed_apps`
  - [ ] Subscribe fields: `messages` AND `statuses`
  - [ ] Use system user token
  - [ ] Called automatically after assignment
  - [ ] Verify subscription: `GET /v21.0/{PHONE_NUMBER_ID}/subscribed_apps`

- [ ] **Webhook Signature Verification**
  - [ ] Verify `X-Hub-Signature-256` header
  - [ ] Use `META_APP_SECRET` for verification
  - [ ] Reject invalid signatures

### Meta Dashboard Setup

- [ ] **Webhook Configuration**
  - [ ] Go to: Meta Developer Console → Your App → Configuration → Webhooks
  - [ ] Select: WhatsApp product
  - [ ] Set Callback URL: `https://your-domain.com/api/webhooks/meta`
  - [ ] Set Verify Token: Match `META_WEBHOOK_VERIFY_TOKEN`
  - [ ] Subscribe to: `messages` field
  - [ ] Subscribe to: `statuses` field
  - [ ] Click "Verify and Save"
  - [ ] Status shows: ✅ "Verified"

- [ ] **Phone Number Verification**
  - [ ] Go to: Meta Business Manager → WhatsApp Accounts
  - [ ] Select your WABA → Phone Numbers
  - [ ] Check phone number status
  - [ ] Wait for `code_verification_status: VERIFIED`
  - [ ] Display name must be approved

- [ ] **App Status**
  - [ ] App is in "Live" mode (not Development)
  - [ ] WhatsApp product is added
  - [ ] Business verification is complete
  - [ ] App has required permissions

---

## 🔧 Code Fixes Required

### Fix 1: Add `statuses` to Subscriptions

**File:** `internal/social/whatsapp_account.go`

**Current:**
```go
// Line 481 - Phone number subscription
req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("subscribed_fields=messages"))
```

**Fix:**
```go
req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("subscribed_fields=messages,statuses"))
```

**Current:**
```go
// Line 637 - WABA subscription
data.Set("subscribed_fields", "messages")
```

**Fix:**
```go
data.Set("subscribed_fields", "messages,statuses")
```

### Fix 2: Ensure WABA Subscription on Startup

**File:** `internal/core/app.go` or initialization

**Add:**
```go
// Subscribe WABA to webhooks on startup
if cfg.BrancrWABAID != "" && cfg.MetaSystemUserToken != "" {
    go func() {
        social.SubscribeWABAToWebhooks(logger, cfg.BrancrWABAID, cfg.MetaSystemUserToken)
    }()
}
```

### Fix 3: Add Webhook Signature Verification

**File:** `internal/social/meta_webhook.go`

**Add:**
```go
func (h *MetaWebhookHandler) verifyWebhookSignature(body []byte, signature string) bool {
    appSecret := os.Getenv("META_APP_SECRET")
    if appSecret == "" {
        return false
    }
    
    mac := hmac.New(sha256.New, []byte(appSecret))
    mac.Write(body)
    expectedSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    
    return hmac.Equal([]byte(signature), []byte(expectedSignature))
}
```

---

## 🎯 Action Plan

### Step 1: Fix Code (Today)

1. **Add `statuses` to subscriptions**
   - Phone number subscription
   - WABA subscription

2. **Add WABA subscription on startup**
   - Ensure WABA is always subscribed

3. **Add signature verification**
   - Security best practice

### Step 2: Meta Dashboard (Today)

1. **Verify Webhook Configuration**
   - Check URL matches
   - Check verify token matches
   - Subscribe to `messages` AND `statuses`
   - Re-verify if needed

2. **Check Phone Number Status**
   - Verify `code_verification_status: VERIFIED`
   - Check display name approval

### Step 3: Resubscribe Everything (Today)

1. **Resubscribe WABA**
   ```bash
   POST /v21.0/{WABA_ID}/subscribed_apps?subscribed_fields=messages,statuses
   ```

2. **Resubscribe Phone Number**
   ```bash
   POST /api/tenant/whatsapp/resubscribe
   ```

3. **Verify Subscriptions**
   ```bash
   GET /api/tenant/whatsapp/diagnostics
   ```

### Step 4: Test (Today)

1. **Send test message** from WhatsApp
2. **Check webhook logs** for receipt
3. **Check delivery status** (should get statuses webhook)
4. **Verify two ticks** appear in WhatsApp

---

## ✅ Success Criteria

After fixes:
- ✅ WABA subscribed to `messages` AND `statuses`
- ✅ Phone number subscribed to `messages` AND `statuses`
- ✅ Meta Dashboard shows webhook verified
- ✅ Phone number `code_verification_status: VERIFIED`
- ✅ Test message triggers webhook
- ✅ Delivery status webhook received
- ✅ Two ticks appear in WhatsApp

---

## 📚 Meta Documentation References

1. **Webhook Setup:** https://developers.facebook.com/docs/whatsapp/cloud-api/webhooks
2. **Subscribe WABA:** https://developers.facebook.com/docs/whatsapp/solution-providers/get-started-for-solution-partners#subscribe-waba
3. **Subscribe Phone Number:** https://developers.facebook.com/docs/whatsapp/cloud-api/phone-numbers#subscribe
4. **Webhook Signature:** https://developers.facebook.com/docs/graph-api/webhooks/getting-started#verify-signature
5. **Message Statuses:** https://developers.facebook.com/docs/whatsapp/cloud-api/webhooks/components#statuses-object

---

## 🚨 Most Likely Issues

Based on Meta docs, the most common causes:

1. **Missing `statuses` subscription** - Can't track delivery
2. **WABA not subscribed** - Required for provider-owned model
3. **Phone number not verified** - Meta won't deliver until verified
4. **Webhook not verified in Dashboard** - Meta won't send events
5. **Missing signature verification** - Security issue (may cause rejections)

**Fix these first**, then test again.

