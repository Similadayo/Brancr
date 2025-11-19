# Frontend Integration Guide: Automated WhatsApp Onboarding

## Overview

The backend now supports **fully automated WhatsApp number onboarding** for Method 1 (Provider-Owned WABA). This guide explains what the frontend needs to implement.

---

## 🚀 New Automated Flow

### **For Numbers in WABA Pool (Pre-provisioned)**

When a tenant's phone number already exists in your WABA pool, the onboarding is **fully automated** - no admin intervention needed!

---

## 📋 Frontend Implementation

### **Step 1: Request Phone Number**

**Endpoint:** `POST /api/tenant/whatsapp/request-number`

**Request:**
```json
{
  "phone_number": "+2348164155954",
  "display_name": "My Business" // Optional, defaults to tenant name
}
```

**Response (Automated - Number in Pool):**
```json
{
  "success": true,
  "request_id": 123,
  "phone_number_id": "123456789",
  "phone_number": "+2348164155954",
  "message": "✅ Verification code sent to +2348164155954! Please check your phone and enter the code using the verify-number endpoint.",
  "next_step": "verify_number",
  "automated": true
}
```

**Response (Manual - Number NOT in Pool):**
```json
{
  "success": true,
  "request_id": 124,
  "phone_number": "+2348164155954",
  "message": "Phone number request submitted. Admin will add your number to Meta Business Manager. Once added, you'll receive a verification code.",
  "automated": false,
  "fallback": "admin_request"
}
```

**Frontend Action:**
- If `automated: true` → Show "Code sent! Check your phone" message + show code input form
- If `automated: false` → Show "Request submitted. Admin will process it" message

---

### **Step 2: Verify Phone Number**

**Endpoint:** `POST /api/tenant/whatsapp/verify-number`

**Request:**
```json
{
  "request_id": 123,
  "verification_code": "123456"
}
```

**Response (Automated - Success):**
```json
{
  "success": true,
  "phone_number_id": "123456789",
  "phone_number": "+2348164155954",
  "message": "✅ Success! Your WhatsApp number +2348164155954 is now connected and ready to receive messages.",
  "automated": true
}
```

**Response (Manual - Code Saved):**
```json
{
  "success": true,
  "message": "Verification code received. Admin will verify it with Meta and complete the setup. You'll be notified once your number is ready.",
  "automated": false
}
```

**Frontend Action:**
- If `automated: true` → Show success message + redirect to WhatsApp dashboard
- If `automated: false` → Show "Waiting for admin approval" message

---

## 🎨 UI Flow Recommendations

### **Option 1: Single-Page Flow (Recommended)**

```
┌─────────────────────────────────────┐
│  Connect WhatsApp Number            │
├─────────────────────────────────────┤
│                                     │
│  Phone Number:                      │
│  [+234] [816 415 5954]             │
│                                     │
│  Display Name (optional):           │
│  [My Business Name]                │
│                                     │
│  [Connect Number]                   │
│                                     │
└─────────────────────────────────────┘
         ↓
┌─────────────────────────────────────┐
│  ✅ Code Sent!                      │
├─────────────────────────────────────┤
│                                     │
│  We've sent a verification code    │
│  to +2348164155954                  │
│                                     │
│  Enter Code:                        │
│  [1] [2] [3] [4] [5] [6]           │
│                                     │
│  [Verify & Connect]                 │
│                                     │
│  Didn't receive code?               │
│  [Resend Code]                      │
│                                     │
└─────────────────────────────────────┘
         ↓
┌─────────────────────────────────────┐
│  🎉 Connected!                     │
├─────────────────────────────────────┤
│                                     │
│  Your WhatsApp number is ready!    │
│  +2348164155954                     │
│                                     │
│  [Go to Dashboard]                  │
│                                     │
└─────────────────────────────────────┘
```

### **Option 2: Multi-Step Wizard**

```
Step 1: Enter Phone Number
  → POST /api/tenant/whatsapp/request-number
  → Show loading spinner
  → If automated: Move to Step 2
  → If manual: Show "Admin will process" message

Step 2: Enter Verification Code
  → POST /api/tenant/whatsapp/verify-number
  → Show loading spinner
  → If automated: Show success + redirect
  → If manual: Show "Waiting for admin" message
```

---

## 🔄 State Management

### **Recommended State Structure:**

```typescript
interface WhatsAppOnboardingState {
  step: 'phone' | 'code' | 'success' | 'pending';
  phoneNumber: string;
  displayName: string;
  requestId: number | null;
  phoneNumberId: string | null;
  isAutomated: boolean;
  error: string | null;
}
```

### **State Transitions:**

```
phone → (request-number) → code (if automated) OR pending (if manual)
code → (verify-number) → success (if automated) OR pending (if manual)
pending → (poll status) → success (when admin completes)
```

---

## 📡 API Endpoints Summary

| Endpoint | Method | Purpose | Returns |
|----------|--------|---------|---------|
| `/api/tenant/whatsapp/request-number` | POST | Request phone number connection | `{automated: boolean, request_id, ...}` |
| `/api/tenant/whatsapp/verify-number` | POST | Verify with code | `{success, automated, ...}` |
| `/api/tenant/whatsapp/check-number` | POST | Check if number exists in pool | `{found, ready, ...}` |
| `/api/tenant/whatsapp/numbers` | GET | List available numbers | `{numbers: [...]}` |
| `/api/tenant/whatsapp/numbers/{id}/assign` | POST | Assign number to tenant | `{success, ...}` |

---

## 🎯 Key Frontend Changes Needed

### **1. Update Request Flow**

**Before:**
- Always showed "Admin will process" message
- Required manual admin intervention

**After:**
- Check `automated` flag in response
- If `automated: true` → Show code input immediately
- If `automated: false` → Show admin request message

### **2. Update Verification Flow**

**Before:**
- Just saved code, waited for admin

**After:**
- If `automated: true` → Complete onboarding immediately
- If `automated: false` → Save code, wait for admin

### **3. Add Status Polling (Optional)**

For manual requests, you can poll the request status:

```typescript
// Poll every 5 seconds
const pollStatus = async (requestId: number) => {
  const response = await fetch(`/api/tenant/whatsapp/requests/${requestId}`);
  const data = await response.json();
  
  if (data.status === 'verified') {
    // Show success, redirect
  }
};
```

---

## 🐛 Error Handling

### **Common Errors:**

1. **Number Already Assigned**
```json
{
  "error": "number_already_assigned",
  "message": "This phone number is already assigned to your account."
}
```

2. **Invalid Verification Code**
```json
{
  "error": "verification_failed",
  "message": "Invalid verification code: ..."
}
```

3. **Number Not Found in Pool**
```json
{
  "found": false,
  "message": "Phone number not found in our system..."
}
```

---

## ✅ Success Criteria

The frontend should:

1. ✅ Handle automated flow (show code input immediately)
2. ✅ Handle manual flow (show admin request message)
3. ✅ Show appropriate success messages
4. ✅ Handle errors gracefully
5. ✅ Redirect to WhatsApp dashboard after success
6. ✅ Support resending verification codes (if needed)

---

## 📝 Example Implementation (React/TypeScript)

```typescript
const WhatsAppOnboarding = () => {
  const [step, setStep] = useState<'phone' | 'code' | 'success'>('phone');
  const [phoneNumber, setPhoneNumber] = useState('');
  const [code, setCode] = useState('');
  const [requestId, setRequestId] = useState<number | null>(null);
  const [isAutomated, setIsAutomated] = useState(false);
  const [loading, setLoading] = useState(false);

  const handleRequestNumber = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/tenant/whatsapp/request-number', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone_number: phoneNumber }),
      });
      const data = await res.json();
      
      if (data.automated) {
        setIsAutomated(true);
        setRequestId(data.request_id);
        setStep('code'); // Move to code input
      } else {
        // Manual flow - show admin message
        setStep('pending');
      }
    } catch (error) {
      // Handle error
    } finally {
      setLoading(false);
    }
  };

  const handleVerifyCode = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/tenant/whatsapp/verify-number', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          request_id: requestId,
          verification_code: code,
        }),
      });
      const data = await res.json();
      
      if (data.automated && data.success) {
        setStep('success'); // Show success, redirect
      }
    } catch (error) {
      // Handle error
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {step === 'phone' && (
        <PhoneInputForm
          phoneNumber={phoneNumber}
          onChange={setPhoneNumber}
          onSubmit={handleRequestNumber}
          loading={loading}
        />
      )}
      
      {step === 'code' && (
        <CodeInputForm
          code={code}
          onChange={setCode}
          onSubmit={handleVerifyCode}
          loading={loading}
          phoneNumber={phoneNumber}
        />
      )}
      
      {step === 'success' && (
        <SuccessMessage phoneNumber={phoneNumber} />
      )}
    </div>
  );
};
```

---

## 🎉 Summary

**What Changed:**
- Backend now supports fully automated onboarding for numbers in WABA pool
- Frontend needs to check `automated` flag and handle accordingly

**What Frontend Needs:**
1. Update request flow to handle automated vs manual
2. Show code input immediately for automated flow
3. Complete onboarding automatically when code verified
4. Handle both automated and manual flows gracefully

**Result:**
- Seamless self-serve onboarding for tenants
- No admin intervention needed for pre-provisioned numbers
- Scales infinitely like Interakt, Respond.io, Gupshup


