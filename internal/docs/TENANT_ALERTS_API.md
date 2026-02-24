# Tenant Alerts API Reference

Complete API reference for tenant alerts endpoints.

## Base URL
All endpoints are prefixed with `/api/tenant`

## Authentication
All endpoints require tenant authentication (session cookie or JWT token).

## Endpoints

### GET /api/tenant/alerts

Get list of alerts for the authenticated tenant.

**Query Parameters:**
- `limit` (integer, optional, default: 50, max: 100) - Number of alerts to return
- `offset` (integer, optional, default: 0) - Pagination offset
- `unread` (string, optional, "true") - Filter to show only unread alerts

**Response:**
```json
[
  {
    "id": 1,
    "tenant_id": 123,
    "type": "billing",
    "severity": "warning",
    "title": "Subscription Expiring Soon",
    "message": "Your subscription will expire in 7 days. Please renew to continue using all features.",
    "action_url": "https://app.brancr.com/billing/renew",
    "action_label": "Renew Subscription",
    "sent_email": true,
    "sent_telegram": true,
    "sent_in_app": true,
    "email_sent_at": "2024-01-15T10:30:00Z",
    "telegram_sent_at": "2024-01-15T10:30:05Z",
    "in_app_created_at": "2024-01-15T10:30:00Z",
    "created_at": "2024-01-15T10:30:00Z",
    "read_at": null,
    "metadata": "{\"expiration_days\":7,\"expires_at\":\"2024-01-22T00:00:00Z\"}"
  }
]
```

**Example:**
```bash
curl -X GET "https://api.brancr.com/api/tenant/alerts?unread=true&limit=20" \
  -H "Cookie: session=..."
```

### GET /api/tenant/alerts/counts

Get total and unread alert counts.

**Response:**
```json
{
  "total": 15,
  "unread": 3
}
```

**Example:**
```bash
curl -X GET "https://api.brancr.com/api/tenant/alerts/counts" \
  -H "Cookie: session=..."
```

### POST /api/tenant/alerts/{id}/read

Mark a specific alert as read.

**Path Parameters:**
- `id` (integer, required) - Alert ID

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X POST "https://api.brancr.com/api/tenant/alerts/123/read" \
  -H "Cookie: session=..."
```

### POST /api/tenant/alerts/read-all

Mark all alerts as read for the authenticated tenant.

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X POST "https://api.brancr.com/api/tenant/alerts/read-all" \
  -H "Cookie: session=..."
```

## Data Models

### TenantAlert

```typescript
interface TenantAlert {
  id: number
  tenant_id: number
  type: 'system' | 'billing' | 'quota' | 'security' | 'maintenance' | 'feature'
  severity: 'info' | 'warning' | 'critical'
  title: string
  message: string
  action_url?: string
  action_label?: string
  sent_email: boolean
  sent_telegram: boolean
  sent_in_app: boolean
  email_sent_at?: string
  telegram_sent_at?: string
  in_app_created_at?: string
  created_at: string
  read_at?: string | null
  metadata?: string // JSON string with additional data
}
```

## Error Responses

All endpoints may return:

- `401 Unauthorized` - Not authenticated
- `500 Internal Server Error` - Server error

**Error Response Format:**
```json
{
  "error": "error message"
}
```

