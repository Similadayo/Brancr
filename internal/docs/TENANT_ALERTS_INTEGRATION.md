# Tenant Alerts Integration Guide

This guide explains how to integrate admin-sent alerts into your tenant dashboard.

## Overview

When admins send alerts from the admin dashboard, they are:
1. **Stored in the database** as `TenantAlert` records
2. **Sent via Email** (if tenant has email configured)
3. **Sent via Telegram** (if tenant has Telegram bot configured)
4. **Available via API** for display in tenant dashboard

## API Endpoints

### Get Alerts
```
GET /api/tenant/alerts
```

**Query Parameters:**
- `limit` (optional, default: 50, max: 100) - Number of alerts to return
- `offset` (optional, default: 0) - Pagination offset
- `unread` (optional, "true") - Filter to show only unread alerts

**Response:**
```json
[
  {
    "id": 1,
    "tenant_id": 123,
    "type": "billing",
    "severity": "warning",
    "title": "Subscription Expiring Soon",
    "message": "Your subscription will expire in 7 days...",
    "action_url": "https://example.com/renew",
    "action_label": "Renew Subscription",
    "sent_email": true,
    "sent_telegram": true,
    "sent_in_app": true,
    "created_at": "2024-01-15T10:30:00Z",
    "read_at": null
  }
]
```

### Get Alert Counts
```
GET /api/tenant/alerts/counts
```

**Response:**
```json
{
  "total": 15,
  "unread": 3
}
```

### Mark Alert as Read
```
POST /api/tenant/alerts/{id}/read
```

**Response:**
```json
{
  "success": true
}
```

### Mark All Alerts as Read
```
POST /api/tenant/alerts/read-all
```

**Response:**
```json
{
  "success": true
}
```

## Frontend Integration Examples

### React/TypeScript Example

```typescript
// types/alert.ts
export interface TenantAlert {
  id: number
  tenant_id: number
  type: string
  severity: 'info' | 'warning' | 'critical'
  title: string
  message: string
  action_url?: string
  action_label?: string
  sent_email: boolean
  sent_telegram: boolean
  sent_in_app: boolean
  created_at: string
  read_at?: string | null
}

// api/alerts.ts
const API_BASE = '/api/tenant'

export async function getAlerts(unreadOnly = false, limit = 50, offset = 0): Promise<TenantAlert[]> {
  const params = new URLSearchParams()
  if (unreadOnly) params.append('unread', 'true')
  params.append('limit', limit.toString())
  params.append('offset', offset.toString())
  
  const response = await fetch(`${API_BASE}/alerts?${params.toString()}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  })
  
  if (!response.ok) {
    throw new Error('Failed to fetch alerts')
  }
  
  return response.json()
}

export async function getAlertCounts(): Promise<{ total: number; unread: number }> {
  const response = await fetch(`${API_BASE}/alerts/counts`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  })
  
  if (!response.ok) {
    throw new Error('Failed to fetch alert counts')
  }
  
  return response.json()
}

export async function markAlertRead(alertId: number): Promise<void> {
  const response = await fetch(`${API_BASE}/alerts/${alertId}/read`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  })
  
  if (!response.ok) {
    throw new Error('Failed to mark alert as read')
  }
}

export async function markAllAlertsRead(): Promise<void> {
  const response = await fetch(`${API_BASE}/alerts/read-all`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  })
  
  if (!response.ok) {
    throw new Error('Failed to mark all alerts as read')
  }
}
```

### React Component Example

```tsx
// components/AlertsList.tsx
import { useState, useEffect } from 'react'
import { getAlerts, markAlertRead, markAllAlertsRead, getAlertCounts } from '@/api/alerts'
import type { TenantAlert } from '@/types/alert'
import { Bell, Check, AlertCircle, AlertTriangle, Info, X } from 'lucide-react'

export function AlertsList() {
  const [alerts, setAlerts] = useState<TenantAlert[]>([])
  const [unreadCount, setUnreadCount] = useState(0)
  const [loading, setLoading] = useState(true)
  const [showUnreadOnly, setShowUnreadOnly] = useState(false)

  useEffect(() => {
    loadAlerts()
    loadCounts()
  }, [showUnreadOnly])

  const loadAlerts = async () => {
    try {
      setLoading(true)
      const data = await getAlerts(showUnreadOnly)
      setAlerts(data)
    } catch (error) {
      console.error('Failed to load alerts:', error)
    } finally {
      setLoading(false)
    }
  }

  const loadCounts = async () => {
    try {
      const { unread } = await getAlertCounts()
      setUnreadCount(unread)
    } catch (error) {
      console.error('Failed to load counts:', error)
    }
  }

  const handleMarkRead = async (alertId: number) => {
    try {
      await markAlertRead(alertId)
      setAlerts(alerts.map(a => 
        a.id === alertId ? { ...a, read_at: new Date().toISOString() } : a
      ))
      loadCounts()
    } catch (error) {
      console.error('Failed to mark alert as read:', error)
    }
  }

  const handleMarkAllRead = async () => {
    try {
      await markAllAlertsRead()
      setAlerts(alerts.map(a => ({ ...a, read_at: new Date().toISOString() })))
      loadCounts()
    } catch (error) {
      console.error('Failed to mark all as read:', error)
    }
  }

  const getSeverityIcon = (severity: string) => {
    switch (severity) {
      case 'critical':
        return <AlertCircle className="h-5 w-5 text-red-500" />
      case 'warning':
        return <AlertTriangle className="h-5 w-5 text-yellow-500" />
      default:
        return <Info className="h-5 w-5 text-blue-500" />
    }
  }

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'border-red-200 bg-red-50'
      case 'warning':
        return 'border-yellow-200 bg-yellow-50'
      default:
        return 'border-blue-200 bg-blue-50'
    }
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bell className="h-6 w-6" />
          <h2 className="text-2xl font-bold">Alerts</h2>
          {unreadCount > 0 && (
            <span className="px-2 py-1 text-xs font-semibold text-white bg-red-500 rounded-full">
              {unreadCount} unread
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowUnreadOnly(!showUnreadOnly)}
            className="px-4 py-2 text-sm border rounded hover:bg-gray-50"
          >
            {showUnreadOnly ? 'Show All' : 'Show Unread Only'}
          </button>
          {unreadCount > 0 && (
            <button
              onClick={handleMarkAllRead}
              className="px-4 py-2 text-sm bg-blue-500 text-white rounded hover:bg-blue-600"
            >
              Mark All Read
            </button>
          )}
        </div>
      </div>

      {/* Alerts List */}
      {loading ? (
        <div className="text-center py-8">Loading alerts...</div>
      ) : alerts.length === 0 ? (
        <div className="text-center py-8 text-gray-500">
          No alerts {showUnreadOnly ? 'unread' : ''}
        </div>
      ) : (
        <div className="space-y-3">
          {alerts.map((alert) => (
            <div
              key={alert.id}
              className={`p-4 border rounded-lg ${getSeverityColor(alert.severity)} ${
                !alert.read_at ? 'border-l-4' : ''
              }`}
            >
              <div className="flex items-start justify-between">
                <div className="flex items-start gap-3 flex-1">
                  {getSeverityIcon(alert.severity)}
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <h3 className="font-semibold">{alert.title}</h3>
                      {!alert.read_at && (
                        <span className="px-2 py-0.5 text-xs font-medium text-white bg-blue-500 rounded">
                          New
                        </span>
                      )}
                      <span className="px-2 py-0.5 text-xs border rounded">
                        {alert.type}
                      </span>
                    </div>
                    <p className="text-sm text-gray-700 mb-2">{alert.message}</p>
                    <div className="flex items-center gap-4 text-xs text-gray-500">
                      <span>{new Date(alert.created_at).toLocaleString()}</span>
                      {alert.sent_email && <span>✓ Email</span>}
                      {alert.sent_telegram && <span>✓ Telegram</span>}
                    </div>
                    {alert.action_url && alert.action_label && (
                      <a
                        href={alert.action_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-block mt-2 px-4 py-2 text-sm bg-blue-500 text-white rounded hover:bg-blue-600"
                      >
                        {alert.action_label}
                      </a>
                    )}
                  </div>
                </div>
                {!alert.read_at && (
                  <button
                    onClick={() => handleMarkRead(alert.id)}
                    className="p-2 text-gray-400 hover:text-gray-600"
                    title="Mark as read"
                  >
                    <Check className="h-5 w-5" />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
```

### Alert Badge Component (for Navigation)

```tsx
// components/AlertBadge.tsx
import { useEffect, useState } from 'react'
import { getAlertCounts } from '@/api/alerts'
import { Bell } from 'lucide-react'

export function AlertBadge() {
  const [unreadCount, setUnreadCount] = useState(0)

  useEffect(() => {
    loadCounts()
    // Poll for new alerts every 30 seconds
    const interval = setInterval(loadCounts, 30000)
    return () => clearInterval(interval)
  }, [])

  const loadCounts = async () => {
    try {
      const { unread } = await getAlertCounts()
      setUnreadCount(unread)
    } catch (error) {
      console.error('Failed to load alert counts:', error)
    }
  }

  if (unreadCount === 0) {
    return (
      <button className="relative p-2 text-gray-600 hover:text-gray-900">
        <Bell className="h-6 w-6" />
      </button>
    )
  }

  return (
    <button className="relative p-2 text-gray-600 hover:text-gray-900">
      <Bell className="h-6 w-6" />
      <span className="absolute top-0 right-0 flex h-5 w-5 items-center justify-center rounded-full bg-red-500 text-xs font-bold text-white">
        {unreadCount > 9 ? '9+' : unreadCount}
      </span>
    </button>
  )
}
```

## Integration Steps

1. **Add API functions** to your tenant dashboard API client
2. **Create AlertsList component** (or use the example above)
3. **Add AlertBadge** to your navigation/header
4. **Add alerts route** to your router (e.g., `/app/alerts`)
5. **Poll for updates** periodically (every 30-60 seconds) or use WebSockets if available

## Alert Types

- `system` - System notifications
- `billing` - Billing/subscription alerts
- `quota` - Quota/usage warnings
- `security` - Security alerts
- `maintenance` - Maintenance notifications
- `feature` - Feature announcements

## Severity Levels

- `info` - Informational (blue)
- `warning` - Warning (yellow)
- `critical` - Critical/urgent (red)

## Best Practices

1. **Show unread count** in navigation/header
2. **Auto-refresh** alerts every 30-60 seconds
3. **Mark as read** when user views the alert
4. **Highlight unread** alerts visually
5. **Support action buttons** if `action_url` is provided
6. **Show delivery status** (email/telegram sent indicators)

