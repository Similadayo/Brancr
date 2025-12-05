# API Endpoints Status - Week 4 & 5

## ✅ Implemented Endpoints

### Customer Portal (Public - Token-Based Access)

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/api/portal/order?token={token}` | `getOrderByPortalToken` | ✅ Implemented |
| GET | `/api/portal/receipt?token={token}` | `getReceiptByPortalToken` | ✅ Implemented |
| GET | `/api/portal/orders?token={token}` | `listCustomerOrders` | ✅ Implemented |

**Location**: `internal/tenantapi/customer_portal_handlers.go`

---

### Order Management (Tenant - Authenticated)

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/api/tenant/orders` | `listOrders` | ✅ Implemented |
| GET | `/api/tenant/orders/{id}` | `getOrder` | ✅ Implemented |
| PUT | `/api/tenant/orders/{id}` | `updateOrder` | ✅ Implemented |
| PUT | `/api/tenant/orders/{id}/confirm-payment` | `confirmOrderPayment` | ✅ Implemented |
| GET | `/api/tenant/orders/{orderId}/portal-token` | `generatePortalToken` | ✅ Implemented |
| GET | `/api/tenant/orders/stats` | `getOrderStats` | ✅ Implemented |

**Location**: `internal/tenantapi/order_handlers.go`

**Features**:
- ✅ Order status updates trigger notifications
- ✅ Order status updates trigger webhooks
- ✅ Portal token generation

---

### Payment Management (Tenant - Authenticated)

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/api/tenant/payments` | `listPayments` | ✅ Implemented |
| GET | `/api/tenant/payments/{id}` | `getPayment` | ✅ Implemented |
| PUT | `/api/tenant/payments/{id}/verify` | `verifyPayment` | ✅ Implemented |
| POST | `/api/tenant/payments/{id}/generate-receipt` | `generateReceipt` | ✅ Implemented |
| PUT | `/api/tenant/payments/{id}/dispute` | `disputePayment` | ✅ Implemented |

**Location**: `internal/tenantapi/payment_handlers.go` and `internal/tenantapi/receipt_handlers.go`

**Features**:
- ✅ Receipt generation (HTML + PDF)
- ✅ Cloudinary PDF upload
- ✅ Email receipt sending
- ✅ Portal token generation

---

### Payment Account Management (Tenant - Authenticated)

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/api/tenant/payment-accounts` | `listPaymentAccounts` | ✅ Implemented |
| POST | `/api/tenant/payment-accounts` | `createPaymentAccount` | ✅ Implemented |
| GET | `/api/tenant/payment-accounts/{id}` | `getPaymentAccount` | ✅ Implemented |
| PUT | `/api/tenant/payment-accounts/{id}` | `updatePaymentAccount` | ✅ Implemented |
| DELETE | `/api/tenant/payment-accounts/{id}` | `deletePaymentAccount` | ✅ Implemented |
| PUT | `/api/tenant/payment-accounts/{id}/set-default` | `setDefaultPaymentAccount` | ✅ Implemented |

**Location**: `internal/tenantapi/payment_account_handlers.go`

---

## ❌ Missing Endpoints

### Notification Settings (Tenant - Authenticated)

| Method | Endpoint | Purpose | Priority |
|--------|----------|---------|----------|
| GET | `/api/tenant/settings/notifications` | Get notification settings | 🔴 High |
| PUT | `/api/tenant/settings/notifications` | Update notification settings | 🔴 High |
| POST | `/api/tenant/settings/notifications/test` | Test email/webhook | 🔴 High |

**Why These Are Needed**:
- Frontend needs to allow tenants to configure:
  - Email notification preferences (order status changes, payment confirmations, receipt generated)
  - Webhook URL and events
  - Enable/disable notifications
- Frontend needs to test notification delivery

**Current State**:
- ✅ Backend services exist (`OrderNotificationService`, `WebhookService`)
- ✅ Notifications are sent automatically on order status changes
- ❌ No API to configure notification settings
- ❌ No database model to store tenant notification preferences
- ❌ No way to disable/enable notifications per tenant

---

## 📋 Implementation Plan for Missing Endpoints

### Step 1: Database Model

Create a `TenantNotificationSettings` model:

```go
type TenantNotificationSettings struct {
    gorm.Model
    TenantID uint `gorm:"uniqueIndex"`
    
    // Email notifications
    EmailNotificationsEnabled bool `gorm:"default:true"`
    EmailOrderStatusChanges    bool `gorm:"default:true"`
    EmailPaymentConfirmations  bool `gorm:"default:true"`
    EmailReceiptGenerated      bool `gorm:"default:true"`
    
    // Webhook configuration
    WebhookEnabled bool   `gorm:"default:false"`
    WebhookURL     string `gorm:"type:text"`
    WebhookEvents  string `gorm:"type:jsonb"` // JSON array of event types
    WebhookSecret  string `gorm:"size:255"`   // For webhook signing (future)
    
    // Metadata
    LastTestAt     *time.Time
    LastTestStatus string `gorm:"size:50"` // success, failed
}
```

### Step 2: Repository

Create `internal/storage/repositories/notification_settings_repository.go`:

```go
type NotificationSettingsRepository struct {
    db *gorm.DB
}

func (r *NotificationSettingsRepository) GetByTenantID(ctx context.Context, tenantID uint) (*storage.TenantNotificationSettings, error)
func (r *NotificationSettingsRepository) CreateOrUpdate(ctx context.Context, settings *storage.TenantNotificationSettings) error
```

### Step 3: Handlers

Create `internal/tenantapi/notification_settings_handlers.go`:

```go
// GET /api/tenant/settings/notifications
func (h *Handler) getNotificationSettings(w http.ResponseWriter, r *http.Request)

// PUT /api/tenant/settings/notifications
func (h *Handler) updateNotificationSettings(w http.ResponseWriter, r *http.Request)

// POST /api/tenant/settings/notifications/test
func (h *Handler) testNotifications(w http.ResponseWriter, r *http.Request)
```

### Step 4: Integration

- Update `OrderNotificationService` to check tenant settings before sending
- Update `WebhookService` to check tenant settings and use configured webhook URL
- Add routes to `internal/tenantapi/handler.go`

---

## 🔍 Current Notification Behavior

### What Works Now:

1. **Order Status Updates** (`PUT /api/tenant/orders/{id}`):
   - ✅ Automatically sends email notification (if email service configured)
   - ✅ Automatically sends webhook (if webhook service configured)
   - ❌ No way to disable per tenant
   - ❌ Uses system-wide webhook URL (if configured)

2. **Payment Confirmation** (`PUT /api/tenant/orders/{id}/confirm-payment`):
   - ✅ Automatically generates receipt
   - ✅ Automatically sends receipt email (if email service configured)
   - ❌ No way to disable per tenant

3. **Receipt Generation** (`POST /api/tenant/payments/{id}/generate-receipt`):
   - ✅ Automatically sends receipt email (if email service configured)
   - ❌ No way to disable per tenant

### What's Missing:

- ❌ Tenant-specific notification preferences
- ❌ Tenant-specific webhook URLs
- ❌ Ability to enable/disable notifications per event type
- ❌ Test notification endpoint
- ❌ Notification delivery history/logs

---

## 📊 Summary

### ✅ Complete (Ready for Frontend):
- Customer Portal (all 3 endpoints)
- Order Management (all 6 endpoints)
- Payment Management (all 5 endpoints)
- Payment Account Management (all 6 endpoints)
- Receipt Generation (with PDF and email)
- Portal Token Generation

### ❌ Missing (Required for Full Frontend):
- Notification Settings API (3 endpoints)
- Database model for notification settings
- Repository for notification settings
- Integration with existing notification services

### 🎯 Recommendation:

**Priority 1**: Implement notification settings endpoints before frontend development, as the frontend requirements document includes a full "Notification Settings" section that depends on these endpoints.

**Estimated Implementation Time**: 2-3 hours
- Database model + migration: 30 min
- Repository: 30 min
- Handlers: 1 hour
- Integration: 1 hour
- Testing: 30 min

---

## 🚀 Quick Start for Missing Endpoints

If you want me to implement the missing notification settings endpoints, I can:

1. Create the database model
2. Create the repository
3. Create the API handlers
4. Integrate with existing notification services
5. Add routes to the router

This will complete the API surface for Week 4 & 5 frontend requirements.

