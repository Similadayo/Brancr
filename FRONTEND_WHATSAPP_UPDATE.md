# Frontend WhatsApp Connection Update

## Overview
Update the frontend to include `channel_id: "440617"` in the request body when connecting WhatsApp via Respond.io.

## Required Changes

### API Endpoint
```
POST /api/tenant/whatsapp/connect
```

### Request Body Format

**With Manual Channel ID (Recommended for now):**
```json
{
  "phone_number": "+2347039841392",
  "provider": "respondio",
  "channel_id": "440617"
}
```

**Without Manual Channel ID (will fail if API returns 0 channels):**
```json
{
  "phone_number": "+2347039841392",
  "provider": "respondio"
}
```

## Frontend Code Examples

### React/Next.js Example

```tsx
import { useState } from 'react';

function WhatsAppConnect() {
  const [phoneNumber, setPhoneNumber] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);

  const handleConnect = async () => {
    setLoading(true);
    setError('');
    setSuccess(false);

    try {
      const response = await fetch('/api/tenant/whatsapp/connect', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${getTenantToken()}`, // Your auth token
        },
        body: JSON.stringify({
          phone_number: phoneNumber,
          provider: 'respondio',
          channel_id: '440617', // Manual channel ID override
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.message || 'Connection failed');
      }

      setSuccess(true);
      // Refresh page or update UI to show connected status
      window.location.reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to connect');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <input
        type="tel"
        placeholder="+2347039841392"
        value={phoneNumber}
        onChange={(e) => setPhoneNumber(e.target.value)}
        disabled={loading}
      />
      <button onClick={handleConnect} disabled={loading || !phoneNumber}>
        {loading ? 'Connecting...' : 'Connect WhatsApp'}
      </button>
      {error && <div className="error">{error}</div>}
      {success && <div className="success">Connected successfully!</div>}
    </div>
  );
}
```

### Vue.js Example

```vue
<template>
  <div>
    <input
      v-model="phoneNumber"
      type="tel"
      placeholder="+2347039841392"
      :disabled="loading"
    />
    <button @click="connectWhatsApp" :disabled="loading || !phoneNumber">
      {{ loading ? 'Connecting...' : 'Connect WhatsApp' }}
    </button>
    <div v-if="error" class="error">{{ error }}</div>
    <div v-if="success" class="success">Connected successfully!</div>
  </div>
</template>

<script>
export default {
  data() {
    return {
      phoneNumber: '',
      loading: false,
      error: '',
      success: false,
    };
  },
  methods: {
    async connectWhatsApp() {
      this.loading = true;
      this.error = '';
      this.success = false;

      try {
        const response = await fetch('/api/tenant/whatsapp/connect', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${this.getTenantToken()}`,
          },
          body: JSON.stringify({
            phone_number: this.phoneNumber,
            provider: 'respondio',
            channel_id: '440617', // Manual channel ID override
          }),
        });

        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.message || 'Connection failed');
        }

        this.success = true;
        // Refresh page or update UI
        this.$router.go(0);
      } catch (err) {
        this.error = err.message || 'Failed to connect';
      } finally {
        this.loading = false;
      }
    },
    getTenantToken() {
      // Return your tenant auth token
      return localStorage.getItem('tenant_token');
    },
  },
};
</script>
```

### Vanilla JavaScript Example

```javascript
async function connectWhatsApp(phoneNumber) {
  try {
    const response = await fetch('/api/tenant/whatsapp/connect', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${getTenantToken()}`,
      },
      body: JSON.stringify({
        phone_number: phoneNumber,
        provider: 'respondio',
        channel_id: '440617', // Manual channel ID override
      }),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.message || 'Connection failed');
    }

    console.log('✅ WhatsApp connected:', data);
    // Refresh page or update UI
    window.location.reload();
  } catch (error) {
    console.error('❌ Connection failed:', error);
    alert(`Failed to connect: ${error.message}`);
  }
}

// Usage
const phoneInput = document.getElementById('phone-input');
document.getElementById('connect-btn').addEventListener('click', () => {
  connectWhatsApp(phoneInput.value);
});
```

## Response Format

### Success Response (200 OK)
```json
{
  "success": true,
  "message": "WhatsApp connected successfully",
  "account": {
    "external_id": "440617",
    "phone_number": "+2347039841392",
    "mode": "respondio"
  }
}
```

### Error Response (400 Bad Request)
```json
{
  "error": "no_whatsapp_channel",
  "message": "No WhatsApp channel found in Respond.io workspace...",
  "workaround": "Since channel ID 440617 exists in your dashboard, you can manually specify it by adding 'channel_id': '440617' to your request body.",
  "example_request": {
    "phone_number": "+2347039841392",
    "provider": "respondio",
    "channel_id": "440617"
  }
}
```

## Important Notes

1. **Always include `channel_id: "440617"`** - This bypasses the API channel listing issue where 0 channels are returned
2. **Phone number format** - Must include country code (e.g., `+2347039841392`)
3. **Provider** - Use `"respondio"` for Respond.io integration
4. **Authentication** - Include tenant authentication token in Authorization header

## Testing

Use the provided test script:
```bash
chmod +x test_whatsapp_connection.sh
./test_whatsapp_connection.sh
```

Or test with curl:
```bash
curl -X POST "http://your-api-url/api/tenant/whatsapp/connect" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_TOKEN" \
  -d '{
    "phone_number": "+2347039841392",
    "provider": "respondio",
    "channel_id": "440617"
  }'
```

