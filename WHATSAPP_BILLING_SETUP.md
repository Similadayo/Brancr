# WhatsApp Billing & Subscription Setup Guide

This document explains how to handle WhatsApp subscriptions and configure Brancr as the payment processor for tenant WhatsApp accounts.

## 🔔 Important: Billing Setup Required for Ownership Claims

**Critical Discovery:** Meta may require payment method/billing to be set up **BEFORE** claiming business/WABA ownership. Without billing configured, ownership claims may fail with permission errors.

The updated flow is:
1. **Set up billing FIRST** (with Brancr's payment method)
2. **Then claim business ownership**
3. **Then claim WABA ownership**
4. **Then subscribe to webhooks**

This is why the billing setup now happens synchronously before claiming ownership.

---

## 🔔 Subscription Handling

### Automatic Subscription (Preferred)

After embedded signup completes, Brancr automatically attempts to:
1. Subscribe the WABA to webhooks (`/api/webhooks/meta`)
2. Subscribe the phone number to webhooks

However, **subscriptions may fail** if:
- Business/WABA claiming fails (permission issues)
- System user token lacks required permissions
- WABA doesn't exist or isn't accessible

### Manual Subscription (Fallback)

If automatic subscription fails, tenants must manually subscribe in Meta Business Manager:

#### Step 1: Subscribe WABA to Webhooks
1. Go to [Meta Business Manager](https://business.facebook.com)
2. Navigate to **Business Settings** → **WhatsApp Accounts** → Select WABA
3. Go to **Webhooks** section
4. Click **Subscribe** or **Edit Subscription**
5. Add webhook URL: `https://api.brancr.com/api/webhooks/meta`
6. Add verify token: `YOUR_WEBHOOK_VERIFY_TOKEN` (from `.env`)
7. Subscribe to fields:
   - `messages`
   - `message_status` (optional)
   - `message_template_status` (optional)

#### Step 2: Subscribe Phone Number to Webhooks
1. In the same WABA settings
2. Go to **Phone Numbers** → Select phone number
3. Scroll to **Webhooks** section
4. Ensure the webhook URL is set: `https://api.brancr.com/api/webhooks/meta`
5. Verify token matches: `YOUR_WEBHOOK_VERIFY_TOKEN`
6. Subscribe to `messages` field

#### Step 3: Verify Subscription
- Test by sending a message to the WhatsApp number
- Check backend logs for incoming webhook events
- Verify messages appear in dashboard/Telegram bot

---

## 💳 Payment Processing Setup

### Overview: Brancr as Payment Processor

Instead of Meta billing tenants directly, Brancr handles billing:
1. **Tenants pay Brancr** (via Brancr's payment system)
2. **Brancr pays Meta** for tenant usage (via Meta's reseller/payment API)
3. **Meta bills Brancr** instead of individual tenants

This provides:
- Unified billing for all Brancr services
- Better tenant experience (one bill, not multiple)
- Brancr can add markup/margin
- Easier subscription management

### Prerequisites

1. **Meta Business Partner Account**
   - Apply to become a Meta Business Partner
   - Complete Meta's partner verification
   - Gain access to Reseller APIs

2. **Payment Method on Meta Business Account**
   - Add payment method to **your** Meta Business account (not tenant's)
   - This will be used to pay for all tenant WhatsApp usage

3. **Reseller/Payment Processing API Access**
   - Request access to Meta's Payment Processing API
   - This allows you to set up billing on behalf of tenants

### Configuration Steps

#### Step 1: Configure Meta App for Payment Processing

1. **In Meta App Dashboard:**
   - Go to **App Settings** → **WhatsApp** → **Embedded Signup**
   - Enable **Payment Processing** or **Reseller Mode**
   - Configure billing settings:
     - Set **Payment Processor**: "Brancr" or your business name
     - Set **Billing Account**: Your Meta Business ID (not tenant's)

2. **Update Embedded Signup Configuration:**
   - In **Embedded Signup Builder** → Select your config
   - Go to **Payment Settings**
   - Enable **"Skip payment method collection"** or **"Use business payment method"**
   - Set payment method to your Meta Business account

#### Step 2: Update Embedded Signup Flow in Frontend

Update the frontend to skip payment step or use Brancr's payment:

```javascript
// In your WhatsApp embedded signup component
FB.login((response) => {
  if (response.authResponse && response.event === 'WA_EMBEDDED_SIGNUP') {
    handleWhatsAppSignup(response);
  }
}, {
  config_id: 'YOUR_CONFIG_ID',
  extras: {
    request_phone_number: true,
    // Skip payment method - Brancr handles billing
    skip_payment_method: true,
    // OR use Brancr's payment method
    // payment_method_id: 'YOUR_BRANCR_PAYMENT_METHOD_ID'
  },
  redirect_uri: 'https://www.brancr.com/app/integrations'
});
```

**Note:** The exact parameter name may vary based on Meta's API. Check Meta's Embedded Signup documentation for the latest parameter names.

#### Step 3: Handle Payment in Embedded Signup Callback

The embedded signup payload should not require payment method. After signup:

1. **Save WhatsApp Account** (already done in `SaveWhatsAppAccount`)
2. **Link Payment Method** (if needed):
   ```go
   // POST /{waba_id}/payment_method
   // Set your payment method as the payment method for tenant's WABA
   ```

#### Step 4: Set Up Recurring Billing

After tenant signs up, you need to:

1. **Monitor WhatsApp Usage** via Meta API:
   ```bash
   GET /{waba_id}/analytics
   ```

2. **Calculate Tenant Charges**:
   - Track message usage per tenant
   - Apply pricing (Brancr's pricing model)
   - Bill tenant via your payment processor

3. **Pay Meta**:
   - Meta will bill your Business account automatically
   - Monitor billing in Meta Business Manager

### Alternative: Manual Payment Method Collection

If you can't skip payment in embedded signup:

1. **Collect payment method** during embedded signup (current flow)
2. **After signup, transfer payment method** to your Business account:
   ```bash
   # This requires Meta's Payment Processing API
   POST /{waba_id}/transfer_payment_method
   {
     "target_business_id": "YOUR_BRANCR_BUSINESS_ID"
   }
   ```

3. **Set your payment method** as primary:
   ```bash
   POST /{waba_id}/payment_method
   {
     "payment_method_id": "YOUR_PAYMENT_METHOD_ID"
   }
   ```

---

## 🔧 Backend Implementation

### Environment Variables

Add to `.env`:

```env
# Meta Payment Processing
META_PAYMENT_METHOD_ID=your_payment_method_id
META_BILLING_ACCOUNT_ID=your_business_id
META_SKIP_PAYMENT_COLLECTION=true  # If supported by Meta API
```

### Update Embedded Signup Handler

Modify `internal/tenantapi/whatsapp_signup.go` to handle payment:

```go
// After successful account creation
if err := h.setupWhatsAppBilling(ctx, wabaID, tenantID); err != nil {
    h.Logger.Warn("whatsapp billing setup failed", zap.Error(err))
    // Non-fatal - tenant can set up billing manually
}
```

### Add Billing Setup Function

```go
func (h *Handler) setupWhatsAppBilling(ctx context.Context, wabaID string, tenantID uint) error {
    if h.Config.MetaSystemUserToken == "" || h.Config.MetaPaymentMethodID == "" {
        return fmt.Errorf("billing not configured")
    }

    // Set payment method for WABA
    endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/payment_method", wabaID)
    body := map[string]string{
        "payment_method_id": h.Config.MetaPaymentMethodID,
    }
    
    // Make API call with system user token
    // ...
    
    return nil
}
```

---

## 📋 Manual Subscription Instructions for Tenants

Create a UI component/email with these instructions:

### For Tenants: Manual WhatsApp Webhook Subscription

If automatic subscription fails, follow these steps:

1. **Access Meta Business Manager**
   - Visit https://business.facebook.com
   - Log in with the Facebook account linked to your WhatsApp Business account

2. **Navigate to WhatsApp Settings**
   - Go to **Business Settings** (gear icon)
   - Click **WhatsApp Accounts** in the left sidebar
   - Select your WhatsApp Business Account (WABA)

3. **Subscribe to Webhooks**
   - Click on **Webhooks** tab
   - Click **Subscribe** or **Edit**
   - Enter:
     - **Webhook URL**: `https://api.brancr.com/api/webhooks/meta`
     - **Verify Token**: `brancr-2025-token` (or contact support for your token)
   - Select fields: `messages`
   - Click **Verify and Save**

4. **Subscribe Phone Number**
   - In the same WABA settings, go to **Phone Numbers**
   - Click on your phone number
   - Scroll to **Webhooks** section
   - Ensure webhook URL is set: `https://api.brancr.com/api/webhooks/meta`
   - Subscribe to `messages` field

5. **Test Connection**
   - Send a test message to your WhatsApp number
   - Check your Brancr dashboard for the message
   - If messages don't appear, contact support

---

## 🚨 Troubleshooting

### Subscription Failures

**Error:** `Object with ID does not exist or cannot be loaded`
- **Cause:** WABA doesn't exist or wrong ID
- **Fix:** Verify WABA ID in embedded signup payload

**Error:** `Missing permissions`
- **Cause:** System user token lacks required permissions
- **Fix:** Regenerate system user token with required permissions:
  - `whatsapp_business_messaging`
  - `whatsapp_business_management`
  - `business_management`

**Error:** `Subscription already exists`
- **Cause:** Already subscribed
- **Fix:** No action needed - subscription is active

### Payment Setup Failures

**Payment method still required in embedded signup:**
- **Cause:** Meta API may not support skipping payment in embedded signup
- **Fix:** Collect payment method, then transfer to Brancr's account afterward

**Unable to set payment method:**
- **Cause:** Payment Processing API access not granted
- **Fix:** Apply for Meta Business Partner status and Payment Processing API access

---

## 📚 References

- [Meta Embedded Signup Documentation](https://developers.facebook.com/docs/whatsapp/embedded-signup)
- [Meta Payment Processing API](https://developers.facebook.com/docs/graph-api/reference/payment-method)
- [Meta Business Partner Program](https://www.facebook.com/business/partner-directory)
- [Meta Webhook Subscription Guide](https://developers.facebook.com/docs/graph-api/webhooks)

---

## ✅ Checklist

### For Automatic Subscriptions:
- [ ] System user token configured (`META_SYSTEM_USER_TOKEN`)
- [ ] System user has required permissions
- [ ] Business ID configured (`META_BUSINESS_ID`)
- [ ] Webhook verify token configured (`META_WEBHOOK_VERIFY_TOKEN`)
- [ ] Webhook URL is publicly accessible

### For Payment Processing:
- [ ] Meta Business Partner account approved
- [ ] Payment Processing API access granted
- [ ] Payment method added to Meta Business account
- [ ] Embedded signup configured to skip/use Brancr payment
- [ ] Billing monitoring setup (track usage per tenant)
- [ ] Tenant billing system integrated

### For Manual Fallback:
- [ ] Documentation provided to tenants
- [ ] Support team trained on manual subscription steps
- [ ] UI component showing manual instructions (when auto fails)

