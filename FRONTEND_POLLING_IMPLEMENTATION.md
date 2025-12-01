# Frontend Implementation: Detect WhatsApp Onboarding Completion

## Problem
Gupshup shows "Congratulations!" screen after onboarding but **doesn't always redirect automatically**. The user gets stuck on that screen.

## Solution: Poll Backend to Detect Completion

Since the redirect is unreliable, your frontend needs to **poll the backend every 5 seconds** to check if onboarding completed.

---

## Simple Implementation

### 1. When Opening Onboarding Popup

```typescript
// After calling POST /api/tenant/whatsapp/connect
const popup = window.open(onboarding_url, 'Gupshup Onboarding', 'width=800,height=600');

// Start polling immediately
startPollingStatus(popup);
```

### 2. Poll Every 5 Seconds

```typescript
let pollInterval: NodeJS.Timeout | null = null;

function startPollingStatus(popup: Window | null) {
  let attempts = 0;
  const MAX_ATTEMPTS = 60; // 5 minutes (60 * 5 seconds)
  
  // Clear any existing polling
  if (pollInterval) {
    clearInterval(pollInterval);
  }
  
  pollInterval = setInterval(async () => {
    attempts++;
    
    try {
      // Check connection status
      const response = await fetch('/api/tenant/whatsapp/connection-status', {
        credentials: 'include'
      });
      
      const data = await response.json();
      
      // ✅ SUCCESS: Status is "live" = onboarding complete!
      if (data.status === 'live' && data.connected) {
        stopPolling();
        closePopup(popup);
        showSuccessMessage(data.phone_number);
        return;
      }
      
      // Also try manual refresh every 15 seconds for faster detection
      if (attempts % 3 === 0) {
        await refreshAppDetails();
      }
      
      // Timeout after 5 minutes
      if (attempts >= MAX_ATTEMPTS) {
        stopPolling();
        showTimeoutMessage();
      }
      
    } catch (error) {
      console.error('Polling error:', error);
      // Continue polling on error
    }
  }, 5000); // Poll every 5 seconds
}

function stopPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
}

function closePopup(popup: Window | null) {
  if (popup && !popup.closed) {
    popup.close();
  }
}
```

### 3. Manual Refresh (Optional - Faster Detection)

```typescript
async function refreshAppDetails() {
  try {
    const response = await fetch('/api/tenant/whatsapp/refresh', {
      method: 'POST',
      credentials: 'include'
    });
    
    const data = await response.json();
    
    if (data.success && data.data?.status === 'live') {
      stopPolling();
      closePopup(popup);
      showSuccessMessage(data.data.phone_number);
      return true;
    }
    
    return false;
  } catch (error) {
    console.error('Refresh error:', error);
    return false;
  }
}
```

---

## API Endpoints

### 1. Check Status
```
GET /api/tenant/whatsapp/connection-status
```

**Response:**
```json
{
  "connected": true,           // ← Check this
  "status": "live",            // ← Or this
  "phone_number": "2347039841392",
  "app_id": "6241a714-417c-4e28-b417-fc537e1def5d",
  "provider": "gupshup_partner"
}
```

### 2. Manual Refresh
```
POST /api/tenant/whatsapp/refresh
```

**Response:**
```json
{
  "success": true,
  "data": {
    "status": "live",          // ← Check this
    "phone_number": "2347039841392",
    "waba_id": "1400083381531412"
  }
}
```

---

## Complete React Component Example

```tsx
import { useState, useEffect, useRef } from 'react';

export function WhatsAppConnect() {
  const [loading, setLoading] = useState(false);
  const [connected, setConnected] = useState(false);
  const [phoneNumber, setPhoneNumber] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  
  const pollingRef = useRef<NodeJS.Timeout | null>(null);
  const popupRef = useRef<Window | null>(null);

  const connectWhatsApp = async () => {
    setLoading(true);
    setError(null);
    
    try {
      const response = await fetch('/api/tenant/whatsapp/connect', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include'
      });
      
      const data = await response.json();
      
      if (!response.ok) {
        throw new Error(data.message || 'Failed to connect');
      }
      
      // Open popup
      const popup = window.open(
        data.onboarding_url,
        'Gupshup Onboarding',
        'width=800,height=600,scrollbars=yes'
      );
      
      popupRef.current = popup;
      
      // Start polling
      startPolling();
      
    } catch (err: any) {
      setLoading(false);
      setError(err.message);
    }
  };

  const startPolling = () => {
    stopPolling();
    let attempts = 0;
    
    pollingRef.current = setInterval(async () => {
      attempts++;
      
      try {
        // Check status
        const statusRes = await fetch('/api/tenant/whatsapp/connection-status', {
          credentials: 'include'
        });
        const statusData = await statusRes.json();
        
        if (statusData.status === 'live' && statusData.connected) {
          stopPolling();
          closePopup();
          setConnected(true);
          setPhoneNumber(statusData.phone_number);
          setLoading(false);
          return;
        }
        
        // Manual refresh every 15 seconds
        if (attempts % 3 === 0) {
          await refreshAppDetails();
        }
        
        // Timeout after 5 minutes
        if (attempts >= 60) {
          stopPolling();
          setLoading(false);
          setError('Onboarding is taking longer than expected. Please check status manually.');
        }
        
      } catch (err) {
        console.error('Polling error:', err);
      }
    }, 5000);
  };

  const refreshAppDetails = async () => {
    try {
      const res = await fetch('/api/tenant/whatsapp/refresh', {
        method: 'POST',
        credentials: 'include'
      });
      const data = await res.json();
      
      if (data.success && data.data?.status === 'live') {
        stopPolling();
        closePopup();
        setConnected(true);
        setPhoneNumber(data.data.phone_number);
        setLoading(false);
        return true;
      }
    } catch (err) {
      console.error('Refresh error:', err);
    }
    return false;
  };

  const stopPolling = () => {
    if (pollingRef.current) {
      clearInterval(pollingRef.current);
      pollingRef.current = null;
    }
  };

  const closePopup = () => {
    if (popupRef.current && !popupRef.current.closed) {
      popupRef.current.close();
    }
    popupRef.current = null;
  };

  useEffect(() => {
    return () => {
      stopPolling();
      closePopup();
    };
  }, []);

  return (
    <div>
      {error && <div className="error">{error}</div>}
      
      {loading && (
        <div className="loading">
          <p>Waiting for onboarding to complete... (this may take a few minutes)</p>
          <button onClick={stopPolling}>Cancel</button>
          <button onClick={refreshAppDetails}>Check Status Now</button>
        </div>
      )}
      
      {connected && (
        <div className="success">
          ✅ WhatsApp Connected!
          {phoneNumber && <p>Phone: {phoneNumber}</p>}
        </div>
      )}
      
      {!loading && !connected && (
        <button onClick={connectWhatsApp}>Connect WhatsApp</button>
      )}
    </div>
  );
}
```

---

## Status Values

- `pending_onboarding` - User needs to complete onboarding
- `pending_verification` - Waiting for verification  
- `live` - ✅ **Onboarding complete!**

---

## Key Points

1. ✅ **Poll every 5 seconds** - Check `/api/tenant/whatsapp/connection-status`
2. ✅ **Stop polling when `status === "live"`** - Onboarding is complete
3. ✅ **Close popup automatically** - User shouldn't be stuck
4. ✅ **Show success message** - Display phone number
5. ✅ **Timeout after 5 minutes** - Prevent infinite polling
6. ✅ **Manual refresh button** - Let user check status immediately

---

## Quick Test

After implementing polling, when you see the Gupshup "Congratulations!" screen:

1. **Wait 5-10 seconds** - The frontend should detect completion
2. **Popup should close automatically**
3. **Success message should appear**

If it doesn't work, click the "Check Status Now" button to manually refresh.

