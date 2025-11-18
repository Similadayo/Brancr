# How to Register Phone Numbers

## 🎯 Quick Answer

You have **two options** to register phone numbers:

### Option 1: Use Verify Endpoint (Recommended - Automatic)
The verify endpoint **automatically calls register first**, so you don't need to register separately:

```bash
POST /admin/whatsapp/verify
{
  "phone_number_id": "123456789",
  "verification_code": "123456"
}
```

This will:
1. ✅ Register the number automatically
2. ✅ Verify with the code
3. ✅ Activate the number

### Option 2: Register Separately (For Existing Pending Numbers)
If you have existing pending numbers, you can register them first:

```bash
POST /admin/whatsapp/register
{
  "phone_number_id": "123456789"
}
```

Then verify later:
```bash
POST /admin/whatsapp/verify
{
  "phone_number_id": "123456789",
  "verification_code": "123456"
}
```

---

## 📋 Step-by-Step Guide

### For New Numbers (Recommended Flow)

1. **Add number to Meta B
usiness Manager** (manual step)
   - Go to Meta Business Manager → WhatsApp Accounts → Phone Numbers
   - Add the phone number
   - Meta will send verification code to the phone

2. **Sync numbers to Brancr:**
   ```bash
   POST /admin/whatsapp/sync
   ```
   This populates your database with numbers from Meta

3. **Register and Verify:**
   ```bash
   POST /admin/whatsapp/verify
   {
     "phone_number_id": "123456789",
     "verification_code": "123456"  # Code from Meta SMS
   }
   ```
   This automatically:
   - ✅ Registers the number
   - ✅ Verifies with code
   - ✅ Activates the number

4. **Check Status:**
   ```bash
   GET /admin/whatsapp/numbers
   ```
   Should show status as "available" (not "pending")

---

### For Existing Pending Numbers (Fix Them)

If you have numbers stuck in "Pending" status:

1. **Get the phone_number_id:**
   ```bash
   GET /admin/whatsapp/numbers
   ```
   Find the number with status "pending" and note its `phone_number_id`

2. **Register it:**
   ```bash
   POST /admin/whatsapp/register
   {
     "phone_number_id": "123456789"
   }
   ```

3. **Request verification code:**
   ```bash
   POST /admin/whatsapp/request-code
   {
     "phone_number": "+1234567890"
   }
   ```
   Meta will send code to the phone

4. **Verify with code:**
   ```bash
   POST /admin/whatsapp/verify
   {
     "phone_number_id": "123456789",
     "verification_code": "123456"
   }
   ```

5. **Sync to update status:**
   ```bash
   POST /admin/whatsapp/sync
   ```

---

## 🔧 API Endpoints Reference

### 1. Register Phone Number
```bash
POST /admin/whatsapp/register
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "phone_number_id": "123456789"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Phone number registered successfully. You can now verify it with the verification code.",
  "phone_number_id": "123456789"
}
```

### 2. Verify Phone Number (Auto-Registers)
```bash
POST /admin/whatsapp/verify
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "phone_number_id": "123456789",
  "verification_code": "123456"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Phone number verified successfully. It will appear in the pool after sync."
}
```

### 3. List All Numbers
```bash
GET /admin/whatsapp/numbers
Authorization: Bearer <admin-token>
```

**Response:**
```json
{
  "numbers": [
    {
      "id": 1,
      "phone_number_id": "123456789",
      "phone_number": "+1234567890",
      "status": "available",
      "verified_name": "My Business",
      "tenant_name": null
    }
  ],
  "total": 1
}
```

### 4. Sync Numbers from Meta
```bash
POST /admin/whatsapp/sync
Authorization: Bearer <admin-token>
```

---

## 🎯 Common Scenarios

### Scenario 1: New Number Setup

**Flow:**
1. Add number in Meta Business Manager
2. `POST /admin/whatsapp/sync` - Get number into database
3. `POST /admin/whatsapp/verify` - Register + verify (automatic)
4. Number is ready!

### Scenario 2: Fix Pending Number

**Flow:**
1. `GET /admin/whatsapp/numbers` - Find pending number
2. `POST /admin/whatsapp/register` - Register it
3. `POST /admin/whatsapp/request-code` - Get verification code
4. `POST /admin/whatsapp/verify` - Verify with code
5. `POST /admin/whatsapp/sync` - Update status

### Scenario 3: Bulk Register Multiple Numbers

**Script:**
```bash
# Get all pending numbers
curl -X GET http://localhost:8080/admin/whatsapp/numbers \
  -H "Authorization: Bearer <token>" | jq '.numbers[] | select(.status == "pending")'

# Register each one
for phone_id in $(curl ... | jq -r '.numbers[] | select(.status == "pending") | .phone_number_id'); do
  curl -X POST http://localhost:8080/admin/whatsapp/register \
    -H "Authorization: Bearer <token>" \
    -H "Content-Type: application/json" \
    -d "{\"phone_number_id\": \"$phone_id\"}"
done
```

---

## ✅ After Registration + Verification

You should see:
- ✅ Phone number status: `available` (not `pending`)
- ✅ `code_verification_status`: `VERIFIED` (in Meta)
- ✅ Messages deliver (two ticks)
- ✅ Webhooks fire correctly
- ✅ Number appears in tenant assignment list

---

## 🚨 Troubleshooting

### Error: "failed to register phone number"

**Possible causes:**
- Phone number ID doesn't exist in Meta
- System token not configured (`META_SYSTEM_USER_TOKEN`)
- Token doesn't have required permissions
- Number already registered

**Fix:**
- Check `META_SYSTEM_USER_TOKEN` is set
- Verify phone_number_id exists in Meta Dashboard
- Check token has `whatsapp_business_messaging` permission

### Error: "verification failed"

**Possible causes:**
- Code expired (codes expire after 10 minutes)
- Wrong code
- Number not registered first

**Fix:**
- Request new code: `POST /admin/whatsapp/request-code`
- Ensure register was called first
- Double-check code from SMS

### Number Still Shows "Pending"

**Fix:**
- Call `POST /admin/whatsapp/sync` to refresh status from Meta
- Check Meta Dashboard directly
- Wait a few minutes for status to update

---

## 📝 Quick Reference

**Most Common Use Case:**
```bash
# Just verify - register happens automatically
POST /admin/whatsapp/verify
{
  "phone_number_id": "123456789",
  "verification_code": "123456"
}
```

**For Existing Pending Numbers:**
```bash
# Step 1: Register
POST /admin/whatsapp/register
{"phone_number_id": "123456789"}

# Step 2: Verify
POST /admin/whatsapp/verify
{"phone_number_id": "123456789", "verification_code": "123456"}
```

That's it! The verify endpoint handles registration automatically, so you usually just need to call verify.

