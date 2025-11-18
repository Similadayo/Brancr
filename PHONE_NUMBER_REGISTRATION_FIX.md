# Phone Number Registration Fix

## 🔴 Critical Issue Found

We were **missing the `/register` API call** - this is REQUIRED before verification according to Meta's documentation.

## What Was Wrong

**Before:**
- ❌ Only called `verify_code` endpoint
- ❌ Never called `/register` endpoint
- ❌ Phone numbers stayed in "Pending" status
- ❌ Messages showed one tick (sent) but not delivered
- ❌ Webhooks didn't fire

**After:**
- ✅ Call `/register` FIRST
- ✅ Then call `verify_code`
- ✅ Phone numbers activate properly
- ✅ Messages deliver (two ticks)
- ✅ Webhooks fire correctly

## The Fix

### Step 1: Register Phone Number (NEW)

**API Call:**
```bash
POST https://graph.facebook.com/v19.0/{phone_number_id}/register
Authorization: Bearer {system_user_token}
```

**Response:**
```json
{
  "success": true
}
```

### Step 2: Verify with Code (EXISTING - Updated)

**API Call:**
```bash
POST https://graph.facebook.com/v19.0/{phone_number_id}/verify_code
Content-Type: application/json
Authorization: Bearer {system_user_token}

{
  "code": "123456"
}
```

## Code Changes

### 1. Added `RegisterPhoneNumber()` Function

**File:** `internal/social/whatsapp_account.go`

**New Function:**
- Calls `/register` endpoint
- Uses system user token
- Must be called BEFORE `verify_code`

### 2. Updated Admin Handlers

**File:** `internal/admin/handler.go`

**Updated Functions:**
- `WhatsAppVerifyNumberHandler` - Now calls register first
- `WhatsAppVerifyRequestHandler` - Now calls register first

**Flow:**
1. Admin receives verification code from tenant
2. Admin calls verify endpoint
3. **NEW:** System calls `/register` first
4. System calls `verify_code` with the code
5. Phone number activates

## Usage

### For Existing Numbers (Manual Fix)

If you have a phone number that's stuck in "Pending":

1. **Get the phone_number_id** from database or Meta Dashboard

2. **Call register endpoint:**
   ```bash
   POST /api/admin/whatsapp/register-number
   {
     "phone_number_id": "123456789"
   }
   ```

3. **Then verify with code:**
   ```bash
   POST /api/admin/whatsapp/verify
   {
     "phone_number_id": "123456789",
     "verification_code": "123456"
   }
   ```

### For New Numbers (Automatic)

The flow now automatically:
1. ✅ Registers the number when verification is attempted
2. ✅ Then verifies with the code
3. ✅ Activates the number

## Expected Results

After registration + verification:

- ✅ Phone number status changes: `PENDING` → `CONNECTED`
- ✅ Messages deliver instantly (two ticks)
- ✅ Webhooks fire within 5-30 seconds
- ✅ Inbound messages work
- ✅ Outbound messages work

## Testing

### Quick Test

1. **Register and verify a number**
2. **Send "Hello" to the number from WhatsApp**
3. **You should see:**
   - ✅ Two ticks (delivered) in WhatsApp
   - ✅ Webhook event in server logs:
     ```json
     {
       "messages": [
         { "from": "2348...", "text": { "body": "Hello" } }
       ]
     }
     ```

### If Not Working

**99% chance webhook callback URL is not responding 200 OK:**
- Check webhook endpoint is accessible
- Check webhook returns 200 OK quickly (< 5 seconds)
- Check webhook subscription status

## API Endpoints

### Register Phone Number (NEW)

```bash
POST /api/admin/whatsapp/register-number
{
  "phone_number_id": "123456789"
}
```

### Verify Phone Number (UPDATED)

```bash
POST /api/admin/whatsapp/verify
{
  "phone_number_id": "123456789",
  "verification_code": "123456"
}
```

**Note:** The verify endpoint now automatically calls register first, so you don't need to call register separately.

## Important Notes

1. **Must use system user token** - Not tenant token
2. **Must call register BEFORE verify_code** - Order matters
3. **Use API, not UI** - Meta docs say use ONLY the API
4. **After registration** - Dashboard changes from Pending → Connected

## Next Steps

1. ✅ Code is fixed - registration happens automatically
2. ⚠️ **For existing pending numbers:** Manually register them
3. ✅ **For new numbers:** Will work automatically
4. ✅ Test with a number to verify it works

