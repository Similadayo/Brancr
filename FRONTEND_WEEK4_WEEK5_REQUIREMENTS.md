# Frontend Requirements: Week 4 & 5
## Receipt Generation, Customer Portal & Integration

---

## 📋 Table of Contents

1. [Customer Portal (Public-Facing)](#customer-portal-public-facing)
2. [Receipt Management (Tenant Dashboard)](#receipt-management-tenant-dashboard)
3. [Order Status & Notifications](#order-status--notifications)
4. [Portal Token Management](#portal-token-management)
5. [Email & Webhook Configuration](#email--webhook-configuration)
6. [Integration Points](#integration-points)

---

## 1. Customer Portal (Public-Facing)

### 1.1 Portal Access Page
**Route**: `/portal/order?token={token}`

**Purpose**: Public-facing page where customers can view their order details using a portal token (no login required).

#### Features:
- **Token-based Authentication**: No login required, access via unique token
- **Order Overview**: Display order details, status, items, and payment information
- **Receipt Download**: Button to view/download receipt
- **Order History**: Link to view all customer orders
- **Responsive Design**: Mobile-friendly layout

#### UI Components:

```typescript
// Customer Portal Page
interface CustomerPortalPage {
  // Header
  businessName: string;
  businessLogo?: string;
  
  // Order Details Section
  order: {
    orderNumber: string;
    paymentReference: string;
    status: 'pending' | 'confirmed' | 'processing' | 'completed' | 'cancelled';
    totalAmount: number;
    currency: string;
    items: OrderItem[];
    createdAt: string;
    confirmedAt?: string;
    completedAt?: string;
  };
  
  // Payment Information
  payment: {
    paymentReference: string;
    amount: number;
    currency: string;
    paymentMethod: string;
    status: string;
    receiptId?: string;
    receiptUrl?: string;
  };
  
  // Business Information
  business: {
    name: string;
    email: string;
    phone: string;
    location?: string;
  };
}
```

#### Design Requirements:
- **Clean, Professional Layout**: Simple and easy to navigate
- **Status Badge**: Color-coded order status (pending=yellow, confirmed=green, etc.)
- **Receipt Button**: Prominent "Download Receipt" button (if receipt available)
- **Order Items Table**: Clear table showing items, quantities, prices
- **Payment Reference Display**: Highlighted payment reference code
- **Mobile Responsive**: Works well on phones and tablets

#### API Integration:

```typescript
// GET /api/portal/order?token={token}
const fetchOrderByToken = async (token: string) => {
  const response = await fetch(`/api/portal/order?token=${token}`);
  if (response.status === 401) {
    // Token expired - show error message
    return { error: 'token_expired' };
  }
  return response.json();
};
```

#### Error Handling:
- **Invalid Token**: Show "Invalid or expired link" message
- **Token Expired**: Show "This link has expired. Please contact the business for a new link."
- **Order Not Found**: Show appropriate error message

---

### 1.2 Receipt View Page
**Route**: `/portal/receipt?token={token}`

**Purpose**: Display receipt HTML or redirect to PDF receipt.

#### Features:
- **Receipt Display**: Show receipt HTML in a print-friendly format
- **PDF Download**: If receipt is PDF URL, provide download button
- **Print Button**: Browser print functionality
- **Back to Order**: Link back to order details

#### UI Components:

```typescript
interface ReceiptViewPage {
  receiptHtml?: string;  // If HTML receipt
  receiptUrl?: string;   // If PDF URL
  orderNumber: string;
  receiptId: string;
}
```

#### Design Requirements:
- **Print-Friendly**: Optimized for printing (A4 size)
- **Professional Layout**: Clean receipt design
- **Download Button**: For PDF receipts
- **Print Button**: Trigger browser print dialog

#### API Integration:

```typescript
// GET /api/portal/receipt?token={token}
const fetchReceipt = async (token: string) => {
  const response = await fetch(`/api/portal/receipt?token=${token}`);
  if (response.headers.get('content-type')?.includes('text/html')) {
    // HTML receipt - display directly
    const html = await response.text();
    return { type: 'html', content: html };
  }
  // PDF URL - redirect or download
  return { type: 'pdf', url: response.url };
};
```

---

### 1.3 Customer Order History
**Route**: `/portal/orders?token={token}`

**Purpose**: Show all orders for a customer (using portal token).

#### Features:
- **Order List**: Display all customer orders
- **Order Cards**: Each order as a card with key details
- **Status Indicators**: Visual status badges
- **View Details**: Click to view full order details
- **Receipt Links**: Direct links to receipts for each order

#### UI Components:

```typescript
interface CustomerOrderHistory {
  orders: Array<{
    id: number;
    orderNumber: string;
    paymentReference: string;
    status: string;
    totalAmount: number;
    currency: string;
    items: OrderItem[];
    createdAt: string;
    confirmedAt?: string;
    completedAt?: string;
  }>;
  count: number;
}
```

#### Design Requirements:
- **Order Cards**: Clean card layout for each order
- **Status Badges**: Color-coded status indicators
- **Sort Options**: Sort by date (newest first)
- **Receipt Badge**: Show if receipt is available
- **Empty State**: Friendly message if no orders

---

## 2. Receipt Management (Tenant Dashboard)

### 2.1 Receipt Generation
**Location**: Order Details Page → Receipt Section

#### Features:
- **Generate Receipt Button**: Manual receipt generation
- **Receipt Status**: Show if receipt exists
- **Receipt ID Display**: Show receipt ID when generated
- **Receipt URL**: Link to view/download receipt
- **Email Receipt**: Button to send receipt via email

#### UI Components:

```typescript
interface ReceiptSection {
  receiptId?: string;
  receiptUrl?: string;
  receiptStatus: 'not_generated' | 'generated' | 'email_sent';
  canGenerate: boolean;  // Only if payment is confirmed/verified
  orderId: number;
  paymentId: number;
}
```

#### Actions:

```typescript
// POST /api/tenant/payments/{id}/generate-receipt
const generateReceipt = async (paymentId: number) => {
  const response = await fetch(`/api/tenant/payments/${paymentId}/generate-receipt`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${token}` }
  });
  return response.json();
};
```

#### Design Requirements:
- **Generate Button**: Prominent button in order details
- **Loading State**: Show spinner while generating
- **Success Message**: Toast notification on success
- **Receipt Preview**: Thumbnail or link to view receipt
- **Email Button**: Send receipt email to customer

---

### 2.2 Receipt List View
**Location**: Payments Page → Receipts Tab

#### Features:
- **Receipt List**: All receipts for tenant
- **Filter Options**: By date, order, customer
- **Search**: Search by receipt ID, order number
- **Download**: Bulk download receipts
- **Email Status**: Show if email was sent

#### UI Components:

```typescript
interface ReceiptListItem {
  receiptId: string;
  orderNumber: string;
  customerName: string;
  amount: number;
  currency: string;
  receiptUrl: string;
  generatedAt: string;
  emailSent: boolean;
  emailSentAt?: string;
}
```

#### Design Requirements:
- **Table View**: Sortable table with receipts
- **Quick Actions**: View, Download, Email buttons
- **Status Indicators**: Email sent badge
- **Date Filters**: Filter by generation date
- **Export**: Export receipt list as CSV

---

## 3. Order Status & Notifications

### 3.1 Order Status Update UI
**Location**: Order Details Page → Status Section

#### Features:
- **Status Dropdown**: Change order status
- **Status History**: Show status change timeline
- **Notification Toggle**: Enable/disable email notifications
- **Status Badge**: Visual status indicator
- **Confirmation Dialog**: Confirm status changes

#### UI Components:

```typescript
interface OrderStatusSection {
  currentStatus: string;
  availableStatuses: string[];
  statusHistory: Array<{
    status: string;
    changedAt: string;
    changedBy?: string;
  }>;
  emailNotificationsEnabled: boolean;
  webhookEnabled: boolean;
}
```

#### Actions:

```typescript
// PUT /api/tenant/orders/{id}
const updateOrderStatus = async (orderId: number, status: string, notes?: string) => {
  const response = await fetch(`/api/tenant/orders/${orderId}`, {
    method: 'PUT',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ status, notes })
  });
  return response.json();
};
```

#### Design Requirements:
- **Status Selector**: Dropdown with available statuses
- **Status Badge**: Large, color-coded badge
- **Timeline View**: Visual timeline of status changes
- **Notification Toggle**: Switch for email notifications
- **Confirmation**: Modal to confirm status change
- **Success Toast**: Notification when status updated

---

### 3.2 Notification Settings
**Location**: Settings → Notifications

#### Features:
- **Email Notifications**: Toggle email notifications
- **Webhook Configuration**: Configure webhook URL
- **Event Selection**: Choose which events trigger notifications
- **Test Notifications**: Send test email/webhook
- **Notification History**: View sent notifications

#### UI Components:

```typescript
interface NotificationSettings {
  emailNotifications: {
    enabled: boolean;
    orderStatusChanges: boolean;
    paymentConfirmations: boolean;
    receiptGenerated: boolean;
  };
  webhook: {
    enabled: boolean;
    url: string;
    events: string[];
    secret?: string;  // For webhook signing (future)
  };
  testResults?: {
    emailSent: boolean;
    webhookSent: boolean;
    lastTestAt?: string;
  };
}
```

#### Actions:

```typescript
// PUT /api/tenant/settings/notifications
const updateNotificationSettings = async (settings: NotificationSettings) => {
  const response = await fetch('/api/tenant/settings/notifications', {
    method: 'PUT',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(settings)
  });
  return response.json();
};

// POST /api/tenant/settings/notifications/test
const testNotifications = async () => {
  const response = await fetch('/api/tenant/settings/notifications/test', {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${token}` }
  });
  return response.json();
};
```

#### Design Requirements:
- **Toggle Switches**: Enable/disable notification types
- **Webhook URL Input**: Text input for webhook URL
- **Event Checkboxes**: Select which events to send
- **Test Button**: Send test notification
- **Status Indicator**: Show if webhook URL is valid
- **History Log**: Recent notification attempts

---

## 4. Portal Token Management

### 4.1 Generate Portal Token
**Location**: Order Details Page → Share Section

#### Features:
- **Generate Token Button**: Create portal token for order
- **Token Display**: Show generated token (masked)
- **Portal URL**: Display full portal URL
- **Copy to Clipboard**: Easy copy functionality
- **Share Options**: Share via email, WhatsApp, etc.
- **Token Expiry**: Show expiration date

#### UI Components:

```typescript
interface PortalTokenSection {
  portalToken?: string;
  portalUrl?: string;
  tokenExpiresAt?: string;
  orderId: number;
  isGenerated: boolean;
}
```

#### Actions:

```typescript
// GET /api/tenant/orders/{orderId}/portal-token
const generatePortalToken = async (orderId: number) => {
  const response = await fetch(`/api/tenant/orders/${orderId}/portal-token`, {
    method: 'GET',
    headers: { 'Authorization': `Bearer ${token}` }
  });
  return response.json();
};
```

#### Design Requirements:
- **Generate Button**: Prominent "Generate Portal Link" button
- **URL Display**: Read-only input with copy button
- **QR Code**: Generate QR code for easy sharing
- **Expiry Warning**: Show when token expires
- **Share Buttons**: Email, WhatsApp, Copy link
- **Masked Token**: Show only first/last few characters

#### Share Options:

```typescript
const sharePortalLink = {
  copy: (url: string) => navigator.clipboard.writeText(url),
  email: (url: string, customerEmail: string) => {
    window.location.href = `mailto:${customerEmail}?subject=Your Order Details&body=View your order: ${url}`;
  },
  whatsapp: (url: string) => {
    window.open(`https://wa.me/?text=${encodeURIComponent(`View your order: ${url}`)}`);
  }
};
```

---

### 4.2 Portal Link in Payment Instructions
**Location**: Order Details → Payment Instructions

#### Features:
- **Portal Link Display**: Show portal link in payment instructions
- **Copy Link**: Easy copy functionality
- **QR Code**: Generate QR code for customer
- **Auto-Generated**: Link generated automatically when order is created

#### Design Requirements:
- **Prominent Display**: Highlight portal link
- **Copy Button**: One-click copy
- **QR Code**: Visual QR code for mobile scanning
- **Instructions**: "Share this link with customer for order tracking"

---

## 5. Email & Webhook Configuration

### 5.1 Email Settings
**Location**: Settings → Email Configuration

#### Features:
- **SMTP Configuration**: Configure email server
- **From Address**: Set sender email
- **Test Email**: Send test email
- **Email Templates**: Customize email templates (future)
- **Email Logs**: View sent emails

#### UI Components:

```typescript
interface EmailSettings {
  smtpHost: string;
  smtpPort: number;
  smtpUser: string;
  smtpFrom: string;
  testEmail: string;  // For testing
  isConfigured: boolean;
}
```

#### Design Requirements:
- **SMTP Form**: Input fields for SMTP configuration
- **Test Button**: Send test email
- **Status Indicator**: Show if email is configured
- **Security Note**: Warn about password storage
- **Save Button**: Save configuration

---

### 5.2 Webhook Configuration
**Location**: Settings → Webhooks

#### Features:
- **Webhook URL**: Configure webhook endpoint
- **Event Selection**: Choose events to send
- **Webhook Secret**: Set secret for signing (future)
- **Test Webhook**: Send test webhook
- **Webhook Logs**: View webhook delivery history
- **Retry Failed**: Retry failed webhooks

#### UI Components:

```typescript
interface WebhookSettings {
  url: string;
  enabled: boolean;
  events: string[];
  secret?: string;
  lastTestAt?: string;
  lastTestStatus?: 'success' | 'failed';
  deliveryHistory: Array<{
    event: string;
    status: 'success' | 'failed';
    timestamp: string;
    responseCode?: number;
  }>;
}
```

#### Design Requirements:
- **URL Input**: Text input for webhook URL
- **Event Checkboxes**: Select events
- **Test Button**: Send test webhook
- **Status Indicator**: Show webhook health
- **Delivery Log**: Table of webhook attempts
- **Retry Button**: Retry failed webhooks

---

## 6. Integration Points

### 6.1 Order Details Page Enhancements

#### New Sections to Add:

1. **Receipt Section**
   ```typescript
   <ReceiptSection
     receiptId={order.receiptId}
     receiptUrl={order.receiptUrl}
     paymentId={order.paymentId}
     onGenerate={handleGenerateReceipt}
     onEmail={handleEmailReceipt}
   />
   ```

2. **Portal Link Section**
   ```typescript
   <PortalLinkSection
     portalToken={order.portalToken}
     portalUrl={order.portalUrl}
     expiresAt={order.tokenExpiresAt}
     onGenerate={handleGenerateToken}
     onShare={handleShareLink}
   />
   ```

3. **Notification Status**
   ```typescript
   <NotificationStatus
     emailSent={order.emailSent}
     webhookSent={order.webhookSent}
     lastNotificationAt={order.lastNotificationAt}
   />
   ```

---

### 6.2 Payment Confirmation Flow

#### Enhanced Payment Confirmation:

When tenant confirms payment:
1. **Auto-Generate Receipt**: Show loading state
2. **Show Receipt**: Display receipt immediately
3. **Email Sent Badge**: Show if email was sent
4. **Portal Link**: Auto-generate and display
5. **Success Toast**: "Payment confirmed and receipt generated"

#### UI Flow:

```typescript
const handleConfirmPayment = async (orderId: number, paymentRef: string) => {
  // 1. Show loading
  setLoading(true);
  
  // 2. Confirm payment
  const result = await confirmPayment(orderId, paymentRef);
  
  // 3. Show receipt (auto-generated)
  if (result.receipt_id) {
    showReceipt(result.receipt_id);
  }
  
  // 4. Show portal link
  if (result.portal_url) {
    showPortalLink(result.portal_url);
  }
  
  // 5. Success message
  toast.success('Payment confirmed! Receipt and portal link generated.');
  
  setLoading(false);
};
```

---

### 6.3 Order Status Update Flow

#### Enhanced Status Update:

When status changes:
1. **Confirmation Dialog**: Confirm status change
2. **Notification Toggle**: Ask if should send notification
3. **Update Status**: Show loading
4. **Success Toast**: "Status updated. Notification sent."
5. **Status History**: Update timeline

#### UI Flow:

```typescript
const handleStatusUpdate = async (orderId: number, newStatus: string) => {
  // 1. Show confirmation
  const confirmed = await showConfirmDialog({
    title: 'Update Order Status',
    message: `Change status to ${newStatus}?`,
    sendNotification: true  // Checkbox
  });
  
  if (!confirmed) return;
  
  // 2. Update status
  setLoading(true);
  const result = await updateOrderStatus(orderId, newStatus);
  
  // 3. Show notification status
  if (result.notification_sent) {
    toast.success('Status updated and customer notified.');
  } else {
    toast.success('Status updated.');
  }
  
  // 4. Refresh order
  refreshOrder(orderId);
  setLoading(false);
};
```

---

## 7. Component Library

### 7.1 Reusable Components

#### ReceiptViewer Component
```typescript
interface ReceiptViewerProps {
  receiptUrl: string;
  receiptId: string;
  orderNumber: string;
  onDownload?: () => void;
  onPrint?: () => void;
  onEmail?: () => void;
}

const ReceiptViewer: React.FC<ReceiptViewerProps> = ({
  receiptUrl,
  receiptId,
  orderNumber,
  onDownload,
  onPrint,
  onEmail
}) => {
  // Display receipt (HTML or PDF)
  // Action buttons
};
```

#### PortalLinkGenerator Component
```typescript
interface PortalLinkGeneratorProps {
  orderId: number;
  portalUrl?: string;
  expiresAt?: string;
  onGenerate: () => Promise<void>;
  onShare: (url: string) => void;
}

const PortalLinkGenerator: React.FC<PortalLinkGeneratorProps> = ({
  orderId,
  portalUrl,
  expiresAt,
  onGenerate,
  onShare
}) => {
  // Generate and display portal link
  // Share options
  // QR code
};
```

#### StatusBadge Component
```typescript
interface StatusBadgeProps {
  status: string;
  size?: 'sm' | 'md' | 'lg';
}

const StatusBadge: React.FC<StatusBadgeProps> = ({ status, size = 'md' }) => {
  const colors = {
    pending: 'yellow',
    confirmed: 'green',
    processing: 'blue',
    completed: 'green',
    cancelled: 'red'
  };
  // Color-coded badge
};
```

#### NotificationToggle Component
```typescript
interface NotificationToggleProps {
  enabled: boolean;
  onToggle: (enabled: boolean) => void;
  label: string;
}

const NotificationToggle: React.FC<NotificationToggleProps> = ({
  enabled,
  onToggle,
  label
}) => {
  // Toggle switch with label
};
```

---

## 8. API Integration Summary

### 8.1 Customer Portal APIs (Public)

```typescript
// Get order by portal token
GET /api/portal/order?token={token}
Response: {
  order: OrderDetails;
  payment: PaymentDetails;
  business: BusinessInfo;
}

// Get receipt by portal token
GET /api/portal/receipt?token={token}
Response: HTML receipt or PDF URL

// List customer orders
GET /api/portal/orders?token={token}
Response: {
  orders: Order[];
  count: number;
}
```

### 8.2 Tenant APIs (Authenticated)

```typescript
// Generate portal token
GET /api/tenant/orders/{orderId}/portal-token
Response: {
  portal_token: string;
  portal_url: string;
  message: string;
}

// Generate receipt
POST /api/tenant/payments/{id}/generate-receipt
Response: {
  receipt_id: string;
  receipt_url: string;
  message: string;
}

// Update order status
PUT /api/tenant/orders/{id}
Body: {
  status?: string;
  notes?: string;
}
Response: {
  success: boolean;
  order: Order;
}

// Get notification settings
GET /api/tenant/settings/notifications
Response: NotificationSettings

// Update notification settings
PUT /api/tenant/settings/notifications
Body: NotificationSettings
Response: NotificationSettings

// Test notifications
POST /api/tenant/settings/notifications/test
Response: {
  email_sent: boolean;
  webhook_sent: boolean;
}
```

---

## 9. Design Specifications

### 9.1 Color Scheme

```css
/* Status Colors */
.status-pending { color: #f59e0b; background: #fef3c7; }
.status-confirmed { color: #10b981; background: #d1fae5; }
.status-processing { color: #3b82f6; background: #dbeafe; }
.status-completed { color: #10b981; background: #d1fae5; }
.status-cancelled { color: #ef4444; background: #fee2e2; }

/* Portal Link Colors */
.portal-link { color: #2563eb; }
.portal-link:hover { color: #1d4ed8; }

/* Receipt Colors */
.receipt-container { background: white; }
.receipt-header { background: #2563eb; color: white; }
```

### 9.2 Typography

- **Headings**: Bold, clear hierarchy
- **Body Text**: Readable, 16px base size
- **Receipt Text**: Print-friendly, serif optional
- **Portal Text**: Sans-serif, modern

### 9.3 Spacing & Layout

- **Customer Portal**: Centered, max-width 800px
- **Receipt**: Full-width, print-optimized
- **Dashboard Sections**: Card-based layout
- **Mobile**: Stack vertically, touch-friendly buttons

---

## 10. User Flows

### 10.1 Customer Receives Portal Link

1. Customer receives portal link (via WhatsApp, email, etc.)
2. Clicks link → Opens customer portal
3. Views order details
4. Clicks "View Receipt" → Receipt page
5. Downloads or prints receipt
6. Can view order history via same token

### 10.2 Tenant Generates Receipt

1. Tenant views order details
2. Confirms payment
3. Receipt auto-generates (shows loading)
4. Receipt appears in order details
5. Tenant can:
   - View receipt
   - Download receipt
   - Email receipt to customer
   - Generate portal link

### 10.3 Tenant Shares Portal Link

1. Tenant views order details
2. Clicks "Generate Portal Link"
3. Portal URL appears
4. Tenant can:
   - Copy link
   - Share via email
   - Share via WhatsApp
   - Generate QR code
5. Customer receives link and accesses portal

---

## 11. Error Handling

### 11.1 Customer Portal Errors

```typescript
// Token expired
if (error === 'token_expired') {
  showError({
    title: 'Link Expired',
    message: 'This link has expired. Please contact the business for a new link.',
    action: 'Contact Business'
  });
}

// Invalid token
if (error === 'invalid_token') {
  showError({
    title: 'Invalid Link',
    message: 'This link is invalid or has been revoked.',
    action: null
  });
}

// Receipt not available
if (error === 'receipt_not_available') {
  showWarning({
    message: 'Receipt is only available for confirmed payments.',
    action: null
  });
}
```

### 11.2 Tenant Dashboard Errors

```typescript
// Receipt generation failed
if (error === 'receipt_generation_failed') {
  showError({
    title: 'Receipt Generation Failed',
    message: 'Failed to generate receipt. Please try again.',
    action: 'Retry'
  });
}

// Email sending failed
if (error === 'email_send_failed') {
  showWarning({
    message: 'Receipt generated but email could not be sent.',
    action: 'Resend Email'
  });
}
```

---

## 12. Mobile Optimization

### 12.1 Customer Portal Mobile

- **Responsive Layout**: Stack elements vertically
- **Touch-Friendly**: Large buttons, easy taps
- **QR Code Scanning**: Easy to scan QR codes
- **Print Optimization**: Receipt prints correctly on mobile

### 12.2 Tenant Dashboard Mobile

- **Collapsible Sections**: Save space
- **Swipe Actions**: Swipe to view receipt
- **Quick Actions**: Floating action button for common actions
- **Mobile Menu**: Hamburger menu for navigation

---

## 13. Accessibility

### 13.1 Requirements

- **Keyboard Navigation**: All actions accessible via keyboard
- **Screen Reader Support**: Proper ARIA labels
- **Color Contrast**: WCAG AA compliant
- **Focus Indicators**: Clear focus states
- **Alt Text**: Images have descriptive alt text

### 13.2 Implementation

```typescript
// Accessible button
<button
  aria-label="Generate portal link for order"
  onClick={handleGenerate}
  className="btn-primary"
>
  Generate Portal Link
</button>

// Accessible status badge
<span
  role="status"
  aria-label={`Order status: ${status}`}
  className={`status-badge status-${status}`}
>
  {status}
</span>
```

---

## 14. Testing Checklist

### 14.1 Customer Portal Testing

- [ ] Portal access with valid token
- [ ] Token expiration handling
- [ ] Invalid token handling
- [ ] Receipt viewing (HTML and PDF)
- [ ] Receipt download
- [ ] Order history display
- [ ] Mobile responsiveness
- [ ] Print functionality

### 14.2 Tenant Dashboard Testing

- [ ] Receipt generation
- [ ] Receipt email sending
- [ ] Portal token generation
- [ ] Portal link sharing
- [ ] Order status updates
- [ ] Notification settings
- [ ] Webhook configuration
- [ ] Test notifications

---

## 15. Implementation Priority

### Phase 1: Core Features (Week 4)
1. Customer Portal - Order View
2. Receipt View Page
3. Receipt Generation (Tenant)
4. Portal Token Generation

### Phase 2: Enhanced Features (Week 5)
5. Email Receipt Sending
6. Order Status Notifications
7. Webhook Configuration
8. Notification Settings

### Phase 3: Polish
9. QR Code Generation
10. Advanced Sharing Options
11. Notification History
12. Email Template Customization

---

## 16. API Response Examples

### 16.1 Portal Order Response

```json
{
  "order": {
    "id": 123,
    "order_number": "ORD-2024-001234",
    "payment_reference": "BRNCR-123-20241204-ABC123",
    "status": "confirmed",
    "total_amount": 5000.00,
    "currency": "NGN",
    "items": [
      {
        "name": "Product A",
        "quantity": 2,
        "unit_price": 2500.00,
        "total_price": 5000.00
      }
    ],
    "customer_name": "John Doe",
    "customer_phone": "+2348123456789",
    "customer_email": "john@example.com",
    "platform": "whatsapp",
    "created_at": "2024-12-04T10:30:00Z",
    "confirmed_at": "2024-12-04T11:00:00Z"
  },
  "payment": {
    "id": 456,
    "payment_reference": "BRNCR-123-20241204-ABC123",
    "amount": 5000.00,
    "currency": "NGN",
    "payment_method": "bank_transfer",
    "status": "verified",
    "receipt_id": "RCPT-2024-XYZ789",
    "receipt_url": "https://res.cloudinary.com/.../receipt.pdf",
    "created_at": "2024-12-04T11:00:00Z"
  },
  "business": {
    "name": "My Business",
    "email": "business@example.com",
    "phone": "+2348123456789",
    "location": "Lagos, Nigeria"
  }
}
```

### 16.2 Portal Token Response

```json
{
  "portal_token": "ABC123XYZ789...",
  "portal_url": "https://api.brancr.com/api/portal/order?token=ABC123XYZ789...",
  "message": "Portal token generated successfully"
}
```

### 16.3 Receipt Generation Response

```json
{
  "receipt_id": "RCPT-2024-XYZ789",
  "receipt_url": "https://res.cloudinary.com/.../receipt.pdf",
  "message": "Receipt generated successfully"
}
```

---

## 17. State Management

### 17.1 Customer Portal State

```typescript
interface CustomerPortalState {
  order: OrderDetails | null;
  payment: PaymentDetails | null;
  business: BusinessInfo | null;
  loading: boolean;
  error: string | null;
  token: string;
}
```

### 17.2 Tenant Dashboard State

```typescript
interface OrderDetailsState {
  order: Order;
  receipt: {
    id: string | null;
    url: string | null;
    generating: boolean;
  };
  portalLink: {
    token: string | null;
    url: string | null;
    expiresAt: string | null;
    generating: boolean;
  };
  notifications: {
    emailSent: boolean;
    webhookSent: boolean;
  };
}
```

---

## 18. Performance Considerations

### 18.1 Optimization

- **Lazy Loading**: Load receipt PDF only when requested
- **Caching**: Cache portal order data
- **Image Optimization**: Optimize receipt images
- **Code Splitting**: Separate customer portal bundle
- **CDN**: Serve receipts from CDN (Cloudinary)

### 18.2 Loading States

```typescript
// Show loading skeleton while fetching
<SkeletonLoader type="order-details" />

// Show loading spinner for actions
<Button loading={isGenerating}>
  Generate Receipt
</Button>
```

---

## 19. Security Considerations

### 19.1 Token Security

- **HTTPS Only**: Portal links only work over HTTPS
- **Token Expiration**: Tokens expire after set period
- **No Token in Logs**: Don't log tokens
- **Rate Limiting**: Limit portal access attempts

### 19.2 Data Privacy

- **Customer Data**: Only show data for authenticated token
- **No Cross-Tenant Access**: Strict tenant isolation
- **Receipt Access**: Only accessible via valid token
- **Email Privacy**: Don't expose email addresses unnecessarily

---

## 20. Localization

### 20.1 Multi-Language Support

- **Currency Display**: Format currency correctly
- **Date Formatting**: Local date formats
- **Status Labels**: Translated status names
- **Error Messages**: Translated error messages

### 20.2 Implementation

```typescript
const t = useTranslation();

<StatusBadge status={order.status}>
  {t(`order.status.${order.status}`)}
</StatusBadge>
```

---

## 21. Analytics & Tracking

### 21.1 Customer Portal Analytics

- **Portal Access**: Track portal visits
- **Receipt Downloads**: Track receipt downloads
- **Time on Page**: Track engagement
- **Device Type**: Track mobile vs desktop

### 21.2 Tenant Dashboard Analytics

- **Receipt Generation**: Track receipt generation
- **Email Sends**: Track email delivery
- **Portal Link Shares**: Track link generation
- **Status Updates**: Track status change frequency

---

## Summary

This document covers all frontend requirements for Week 4 (Receipt Generation & Customer Portal) and Week 5 (Integration & End-to-End Flow). The implementation includes:

✅ **Customer Portal** - Public-facing order tracking
✅ **Receipt Management** - Generation, viewing, downloading
✅ **Portal Token Management** - Generation and sharing
✅ **Order Status Updates** - With notifications
✅ **Email & Webhook Configuration** - Settings UI
✅ **Integration Points** - Seamless flow from order to receipt

All components are designed to be:
- **User-Friendly**: Intuitive and easy to use
- **Responsive**: Works on all devices
- **Accessible**: WCAG compliant
- **Performant**: Fast and optimized
- **Secure**: Token-based access with expiration

