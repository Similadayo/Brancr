# Frontend Requirements - Week 3: Payment System Foundation

## ✅ Status: Backend Complete - Frontend Integration Ready

## Overview
Week 3 implementation added **automatic order creation** when customers commit to purchase. Orders are now created automatically in the background when the AI detects customer commitment. The frontend needs to display these orders and provide real-time updates.

**✅ All backend APIs are implemented and working!**

---

## 🎯 Key Changes from Week 2

### What's New:
1. **Automatic Order Creation** - Orders are now created automatically when customers commit
2. **Order Numbers** - Every order has a unique order number (ORD-2024-001234)
3. **Payment References** - Every order has a payment reference (BRNCR-123-20241204-ABC123)
4. **Real-time Order Creation** - Orders appear immediately after customer commits

### What Frontend Needs to Handle:
- Display automatically created orders
- Show order numbers and payment references prominently
- Real-time order notifications
- Order creation indicators
- Payment reference display for verification

---

## 🔴 Critical Frontend Updates

### 1. **Order Display Enhancements** (Required)

The existing Orders Dashboard needs updates to show automatically created orders.

#### Updates Needed:

**Orders List Page** (`/orders`)
- ✅ Already implemented in Week 2
- **NEW**: Add "Auto-created" badge for orders created automatically
- **NEW**: Show order creation source (e.g., "Created from WhatsApp conversation")
- **NEW**: Highlight new orders (created in last 5 minutes) with "New" badge
- **NEW**: Show payment reference prominently in order card

**Order Detail Page** (`/orders/{id}`)
- ✅ Already implemented in Week 2
- **NEW**: Display "Auto-created" indicator if order was created automatically
- **NEW**: Show conversation link (if available)
- **NEW**: Display payment reference prominently at top
- **NEW**: Show order creation timestamp
- **NEW**: Display customer info (extracted automatically)

#### UI Components to Add:

**Order Card Enhancement:**
```jsx
<OrderCard>
  <OrderHeader>
    <OrderNumber>ORD-2024-001234</OrderNumber>
    {order.isAutoCreated && <Badge>Auto-created</Badge>}
    {isNewOrder && <Badge variant="success">New</Badge>}
  </OrderHeader>
  <PaymentReference>
    Payment Ref: {order.payment_reference}
  </PaymentReference>
  <OrderInfo>
    Customer: {order.customer_name}
    Amount: {order.total_amount} {order.currency}
    Status: {order.status}
  </OrderInfo>
</OrderCard>
```

**Order Detail Enhancement:**
```jsx
<OrderDetail>
  <PaymentReferenceBox>
    <h3>Payment Reference</h3>
    <Code>{order.payment_reference}</Code>
    <CopyButton />
    <Info>Share this reference with customer for payment verification</Info>
  </PaymentReferenceBox>
  
  {order.isAutoCreated && (
    <AutoCreatedInfo>
      <Icon>🤖</Icon>
      <Text>This order was created automatically when customer committed to purchase</Text>
      <Link>View Conversation</Link>
    </AutoCreatedInfo>
  )}
</OrderDetail>
```

---

### 2. **Real-time Order Notifications** (Recommended)

Show notifications when new orders are created automatically.

#### Implementation Options:

**Option A: WebSocket/SSE Updates**
- Connect to WebSocket endpoint for real-time order updates
- Show toast notification when new order is created
- Update orders list automatically

**Option B: Polling**
- Poll `/api/tenant/orders?status=pending&limit=1` every 30 seconds
- Compare with last known order ID
- Show notification if new order found

**Option C: Manual Refresh Button**
- Add "Refresh" button to orders page
- Show notification count badge if new orders exist

#### Notification Component:
```jsx
<OrderNotification>
  <Icon>🛒</Icon>
  <Message>
    New order created: {order.order_number}
    <br />
    Customer: {order.customer_name}
    <br />
    Amount: {order.total_amount} {order.currency}
  </Message>
  <Actions>
    <Button onClick={viewOrder}>View Order</Button>
    <Button onClick={dismiss}>Dismiss</Button>
  </Actions>
</OrderNotification>
```

---

### 3. **Payment Reference Display** (Critical)

Payment references are now automatically generated and must be displayed prominently.

#### Where to Show Payment References:

1. **Order List** - Show in order card (truncated, full on hover)
2. **Order Detail** - Prominent display at top
3. **Payment Confirmation Modal** - Show when confirming payment
4. **Order Statistics** - Show in pending orders section

#### Payment Reference Component:
```jsx
<PaymentReferenceDisplay>
  <Label>Payment Reference</Label>
  <Code>{order.payment_reference}</Code>
  <CopyButton onClick={copyToClipboard} />
  <Info>
    Customer should include this reference when making payment
  </Info>
</PaymentReferenceDisplay>
```

#### Copy to Clipboard Feature:
- One-click copy payment reference
- Show "Copied!" feedback
- Useful for sharing with customers

---

### 4. **Order Creation Source Indicator** (Nice-to-Have)

Show where/how the order was created.

#### Display Options:

**Badge/Indicator:**
- "Auto-created" badge for automatic orders
- "Manual" badge for manually created orders (if supported)
- Platform icon (WhatsApp, Instagram, etc.)

**Info Tooltip:**
- Hover to see: "Created from WhatsApp conversation"
- Show conversation link if available
- Show creation timestamp

---

## 📋 API Endpoints (Already Implemented - Ready to Use)

All these endpoints are already working from Week 2:

### Order Management:
```
✅ GET    /api/tenant/orders                    - List orders (supports ?status=, ?platform=)
✅ GET    /api/tenant/orders/{id}                - Get order details
✅ PUT    /api/tenant/orders/{id}                - Update order
✅ PUT    /api/tenant/orders/{id}/confirm-payment - Confirm payment
✅ GET    /api/tenant/orders/stats               - Order statistics
```

### Response Format (Updated):
```json
{
  "id": 1,
  "order_number": "ORD-2024-001234",
  "payment_reference": "BRNCR-123-20241204-ABC123",
  "customer_name": "Jane Smith",
  "customer_phone": "+2348012345678",
  "customer_email": "jane@example.com",
  "total_amount": 50000,
  "currency": "NGN",
  "status": "pending",
  "platform": "whatsapp",
  "items": [...],
  "created_at": "2024-12-04T10:30:00Z",
  "is_auto_created": true,  // NEW: Indicates automatic creation
  "conversation_id": 123    // NEW: Link to conversation (if available)
}
```

---

## 🎨 UI/UX Recommendations

### 1. **Order List Updates**

**Visual Hierarchy:**
- Order number: Large, bold, prominent
- Payment reference: Medium, monospace font, copy button
- Status badge: Color-coded (pending=orange, confirmed=green, etc.)
- Auto-created badge: Subtle, informative

**New Order Highlighting:**
- Fade-in animation for new orders
- Subtle border or background color
- "New" badge that auto-removes after 5 minutes

### 2. **Payment Reference Display**

**Design:**
- Monospace font for readability
- Large, easy to copy
- Copy button with visual feedback
- QR code option (future enhancement)

**Accessibility:**
- Screen reader friendly
- Keyboard navigation for copy
- Clear labels

### 3. **Real-time Updates**

**Options:**
1. **Toast Notifications** (Recommended)
   - Non-intrusive
   - Auto-dismiss after 5 seconds
   - Click to view order

2. **Badge Counter**
   - Show count of new orders
   - Update orders list when clicked

3. **Sound Notification** (Optional)
   - Subtle sound for new orders
   - User preference setting

---

## 🔧 Technical Implementation

### 1. **Detecting New Orders**

**Method 1: Timestamp Comparison**
```javascript
// Get orders created in last 5 minutes
const recentOrders = orders.filter(order => {
  const createdAt = new Date(order.created_at);
  const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000);
  return createdAt > fiveMinutesAgo;
});
```

**Method 2: Last Order ID Tracking**
```javascript
// Store last seen order ID
const lastOrderId = localStorage.getItem('lastOrderId');
const newOrders = orders.filter(order => order.id > lastOrderId);
```

### 2. **Real-time Updates**

**Polling Implementation:**
```javascript
// Poll every 30 seconds
useEffect(() => {
  const interval = setInterval(() => {
    fetchOrders();
  }, 30000);
  return () => clearInterval(interval);
}, []);
```

**WebSocket Implementation (if available):**
```javascript
const ws = new WebSocket('wss://api.brancr.com/ws/orders');
ws.onmessage = (event) => {
  const newOrder = JSON.parse(event.data);
  showNotification(newOrder);
  updateOrdersList(newOrder);
};
```

### 3. **Copy to Clipboard**

```javascript
const copyPaymentReference = async (reference) => {
  try {
    await navigator.clipboard.writeText(reference);
    showToast('Payment reference copied!');
  } catch (err) {
    console.error('Failed to copy:', err);
  }
};
```

---

## 📝 Component Checklist

### Required Components:
- [ ] Enhanced OrderCard with payment reference
- [ ] PaymentReferenceDisplay component
- [ ] CopyToClipboard button
- [ ] Auto-created badge/indicator
- [ ] New order badge
- [ ] Order notification toast

### Optional Components:
- [ ] Real-time order updates (WebSocket/SSE)
- [ ] Order creation animation
- [ ] Conversation link component
- [ ] Payment reference QR code
- [ ] Order creation source tooltip

---

## 🎯 User Stories

### As a Tenant:
1. **I want to see new orders immediately** when customers commit to purchase
   - ✅ Orders appear in dashboard automatically
   - ⏳ Notification when new order created (frontend needed)

2. **I want to easily copy payment references** to share with customers
   - ⏳ One-click copy button (frontend needed)

3. **I want to know which orders were auto-created** vs manual
   - ⏳ Auto-created badge (frontend needed)

4. **I want to see payment references prominently** for easy verification
   - ⏳ Prominent display in order cards and detail page (frontend needed)

---

## 🚀 Implementation Priority

### Phase 1 (Critical - Do First):
1. ✅ Display payment reference in order cards
2. ✅ Copy to clipboard functionality
3. ✅ Auto-created indicator/badge
4. ✅ New order highlighting

### Phase 2 (Important):
5. ⏳ Real-time order notifications
6. ⏳ Order creation source display
7. ⏳ Conversation link (if available)

### Phase 3 (Nice-to-Have):
8. ⏳ WebSocket real-time updates
9. ⏳ Order creation animations
10. ⏳ Payment reference QR code

---

## 📊 Example API Responses

### List Orders (with new fields):
```json
GET /api/tenant/orders

Response:
{
  "orders": [
    {
      "id": 1,
      "order_number": "ORD-2024-001234",
      "payment_reference": "BRNCR-123-20241204-ABC123",
      "customer_name": "Jane Smith",
      "customer_phone": "+2348012345678",
      "total_amount": 50000,
      "currency": "NGN",
      "status": "pending",
      "platform": "whatsapp",
      "items": [
        {
          "name": "Product Name",
          "quantity": 2,
          "unit_price": 25000,
          "total_price": 50000
        }
      ],
      "created_at": "2024-12-04T10:30:00Z"
    }
  ],
  "count": 1,
  "total": 1
}
```

**Note:** The API doesn't currently return `is_auto_created` or `conversation_id` fields. These can be:
1. Added to backend response (recommended)
2. Inferred on frontend (order created within last 5 minutes = likely auto-created)

---

## 🔄 Integration Notes

### Order Creation Flow:
1. Customer commits → AI detects commitment
2. Order created automatically (backend)
3. Payment instructions sent with order number & reference
4. Order appears in tenant dashboard
5. Tenant can view, confirm payment, update status

### Frontend Should:
- Poll or use WebSocket to detect new orders
- Show notification when order created
- Display payment reference prominently
- Make it easy to copy payment reference
- Show order creation source

---

## ✅ Testing Checklist

### Frontend Testing:
- [ ] Orders list displays payment references
- [ ] Copy to clipboard works
- [ ] Auto-created badge shows correctly
- [ ] New order highlighting works
- [ ] Real-time notifications appear (if implemented)
- [ ] Order detail page shows all info
- [ ] Payment reference is prominent
- [ ] Mobile responsive design

### Integration Testing:
- [ ] New orders appear in list after customer commits
- [ ] Payment reference matches what customer received
- [ ] Order number is unique and sequential
- [ ] Customer info is correctly extracted and displayed

---

## 🎨 Design Mockups (Conceptual)

### Order Card:
```
┌─────────────────────────────────────┐
│ ORD-2024-001234  [Auto-created] [New]│
├─────────────────────────────────────┤
│ Payment Ref: BRNCR-123-20241204-ABC123 [📋]│
│ Customer: Jane Smith                │
│ Amount: ₦50,000                     │
│ Status: [Pending]                   │
│ Platform: WhatsApp                  │
└─────────────────────────────────────┘
```

### Order Detail:
```
┌─────────────────────────────────────┐
│ Order Details                       │
├─────────────────────────────────────┤
│ Payment Reference                   │
│ ┌─────────────────────────────────┐ │
│ │ BRNCR-123-20241204-ABC123      │ │
│ │                    [Copy]       │ │
│ └─────────────────────────────────┘ │
│                                      │
│ 🤖 Auto-created from WhatsApp        │
│    conversation                      │
│                                      │
│ Customer: Jane Smith                 │
│ Phone: +2348012345678                │
│ ...                                  │
└─────────────────────────────────────┘
```

---

## 📝 Summary

### What's Already Working (Backend):
- ✅ Automatic order creation
- ✅ Order number generation
- ✅ Payment reference generation
- ✅ Customer info extraction
- ✅ Order APIs (from Week 2)

### What Frontend Needs to Add:
1. **Payment Reference Display** - Show prominently in orders
2. **Copy to Clipboard** - Easy sharing with customers
3. **Auto-created Indicator** - Show which orders were auto-created
4. **New Order Notifications** - Alert when orders are created
5. **Real-time Updates** - Poll or WebSocket for new orders

### Priority:
- **High**: Payment reference display, copy functionality
- **Medium**: Auto-created badge, new order highlighting
- **Low**: Real-time WebSocket, animations

---

## 🚀 Next Steps

1. **Update OrderCard component** to show payment reference
2. **Add CopyToClipboard component** for payment references
3. **Add Auto-created badge** to order cards
4. **Implement order polling** for real-time updates
5. **Add notification system** for new orders
6. **Test end-to-end flow**: Customer commits → Order created → Frontend shows order

Once these are done, tenants will have full visibility into automatically created orders and can easily manage payments using the payment references!

