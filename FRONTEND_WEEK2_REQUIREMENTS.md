# Frontend Requirements - Week 2: Industry-Aware AI & Sales Conversation

## Overview
Week 2 implementation added automatic sales conversation tracking, negotiation, commitment detection, and payment instruction generation. Most features work automatically in the background, but tenants need UI to manage payment accounts and view orders.

---

## 🔴 Critical Frontend Requirements

### 1. **Payment Account Management** (Required for Payment Instructions)

Tenants need to add their payment account details so the AI can generate payment instructions when customers commit to purchase.

#### API Endpoints (✅ IMPLEMENTED):
```
✅ GET    /api/tenant/payment-accounts          - List all payment accounts
✅ POST   /api/tenant/payment-accounts          - Add new payment account
✅ GET    /api/tenant/payment-accounts/{id}     - Get single account
✅ PUT    /api/tenant/payment-accounts/{id}     - Update account
✅ DELETE /api/tenant/payment-accounts/{id}     - Delete account
✅ PUT    /api/tenant/payment-accounts/{id}/set-default - Set as default
```

#### UI Components Needed:

**Payment Accounts Page** (`/settings/payment-accounts`)
- List of payment accounts (bank, mobile money, cash)
- Add new account button
- Edit/Delete actions
- Set default account toggle
- Account types:
  - **Bank Transfer**: Bank name, Account number, Account name
  - **Mobile Money**: Provider (MTN, Airtel, etc.), Phone number, Account name
  - **Cash**: Just a label/note

**Add/Edit Payment Account Form**
- Account type selector (dropdown: Bank, Mobile Money, Cash)
- Conditional fields based on type:
  - Bank: Bank name, Account number, Account name
  - Mobile Money: Provider, Phone number, Account name
  - Cash: Description/note
- "Set as default" checkbox
- Active/Inactive toggle

---

### 2. **Order Management Dashboard** (Recommended)

Tenants should be able to view and manage orders created through sales conversations.

#### API Endpoints (✅ IMPLEMENTED):
```
✅ GET    /api/tenant/orders                    - List orders (with filters: ?status=, ?platform=, ?limit=, ?offset=)
✅ GET    /api/tenant/orders/{id}                - Get order details
✅ PUT    /api/tenant/orders/{id}                - Update order (status, notes)
✅ PUT    /api/tenant/orders/{id}/confirm-payment - Confirm payment received
✅ GET    /api/tenant/orders/stats               - Order statistics
```

#### UI Components Needed:

**Orders Page** (`/orders`)
- Orders list with filters:
  - Status: pending, confirmed, processing, completed, cancelled
  - Date range
  - Customer name/search
  - Platform (WhatsApp, Instagram, etc.)
- Order card showing:
  - Order number
  - Customer name & contact
  - Items (summary)
  - Total amount
  - Status badge
  - Created date
  - Platform icon

**Order Detail Page** (`/orders/{id}`)
- Order information:
  - Order number, Payment reference
  - Customer details (name, phone, email)
  - Items list with quantities and prices
  - Total amount
  - Status
  - Payment instructions sent
- Actions:
  - Confirm payment button (if status = pending)
  - Update status dropdown
  - Add notes
  - Mark as completed
- Payment verification:
  - Show payment reference code
  - "Confirm Payment Received" button
  - Payment verification history

**Order Statistics Widget**
- Total orders
- Pending payments
- Completed orders
- Revenue (total confirmed)
- Average order value

---

### 3. **Sales Conversation Analytics** (Optional but Valuable)

Show tenants insights about their sales conversations.

#### API Endpoints Needed:
```
GET    /api/tenant/sales-analytics           - Sales conversation stats
GET    /api/tenant/sales-conversations       - List sales conversations
GET    /api/tenant/sales-conversations/{id}  - Get conversation details
```

#### UI Components Needed:

**Sales Analytics Dashboard** (`/analytics/sales`)
- Metrics:
  - Total sales conversations
  - Conversion rate (committed / total)
  - Average negotiation rounds
  - Most common objections
  - Average time to commitment
- Charts:
  - Sales funnel (greeting → inquiry → negotiation → commitment)
  - Conversion by industry
  - Negotiation success rate

**Sales Conversations List** (`/sales-conversations`)
- List of sales conversations
- Show: Customer, Stage, Commitment level, Items discussed, Price, Status
- Filter by stage, commitment level, date range

---

## 🟡 Nice-to-Have Frontend Features

### 4. **Payment Verification UI**

When customers claim they've paid, tenants need to verify payments securely.

#### API Endpoints (✅ IMPLEMENTED):
```
✅ GET    /api/tenant/payments                  - List payments (with filters: ?status=, ?verification_status=, ?limit=, ?offset=)
✅ GET    /api/tenant/payments/{id}              - Get payment details
✅ PUT    /api/tenant/payments/{id}/verify       - Verify payment
✅ PUT    /api/tenant/payments/{id}/dispute      - Mark as disputed
⏳ POST   /api/tenant/payments/{id}/generate-receipt - Generate receipt (Week 4 - Receipt Generation)
```

#### UI Components:
- Payment verification page
- Payment reference code search
- Payment status indicators
- Receipt generation button

---

### 5. **Customer Portal** (Week 4 Feature - Preview)

Customers need a portal to view their orders and download receipts. This will be fully implemented in Week 4, but you can start planning:

- Token-based access (no login required)
- Order history
- Receipt download
- Order status tracking
- Payment reference lookup

---

## 📋 Implementation Priority

### Phase 1 (Critical - Do First):
1. ✅ Payment Account Management API endpoints
2. ✅ Payment Account Management UI
3. ✅ Order Management API endpoints
4. ✅ Order List & Detail UI

### Phase 2 (Important):
5. ✅ Order Statistics
6. ✅ Payment Verification UI
7. ✅ Sales Analytics (basic)

### Phase 3 (Nice-to-Have):
8. Sales Conversation Details UI
9. Advanced Analytics
10. Customer Portal (Week 4)

---

## 🔧 Technical Notes

### Payment Account Security
- Account numbers should be encrypted at rest
- Only show last 4 digits in UI (mask the rest)
- Require confirmation for deletion

### Order Number Format
- Format: `ORD-{YEAR}-{SEQUENTIAL}` (e.g., `ORD-2024-001234`)
- Generated automatically on order creation

### Payment Reference Format
- Format: `BRNCR-{TENANT_ID}-{DATE}-{RANDOM}` (e.g., `BRNCR-123-20241204-ABC123`)
- Used for secure payment verification
- Unique per order

### Status Flow
```
Order: pending → confirmed → processing → completed
Payment: pending → verified → confirmed
```

---

## 📝 API Request/Response Examples

### Add Payment Account
```json
POST /api/tenant/payment-accounts
{
  "account_type": "bank",
  "bank_name": "Access Bank",
  "account_number": "0123456789",
  "account_name": "John Doe",
  "is_default": true
}
```

### List Orders
```json
GET /api/tenant/orders?status=pending&limit=20
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
      "items": [...],
      "created_at": "2024-12-04T10:30:00Z"
    }
  ],
  "count": 1
}
```

### Confirm Payment
```json
PUT /api/tenant/orders/{id}/confirm-payment
{
  "payment_reference": "BRNCR-123-20241204-ABC123",
  "notes": "Payment confirmed via bank transfer"
}
```

---

## 🎨 UI/UX Recommendations

1. **Payment Accounts**:
   - Simple, clean form
   - Clear account type selection
   - Visual indicator for default account
   - Confirmation before deletion

2. **Orders Dashboard**:
   - Card-based layout for easy scanning
   - Color-coded status badges
   - Quick actions (confirm, view details)
   - Search and filter prominently placed

3. **Order Details**:
   - Clear information hierarchy
   - Prominent "Confirm Payment" button for pending orders
   - Payment reference code clearly displayed
   - Timeline showing order progression

4. **Mobile Responsive**:
   - All pages must work on mobile
   - Touch-friendly buttons
   - Simplified forms on small screens

---

## ✅ Checklist

### Backend (Status):
- [x] ✅ Payment Account CRUD API endpoints - **COMPLETED**
- [x] ✅ Order Management API endpoints - **COMPLETED**
- [x] ✅ Payment verification API endpoints - **COMPLETED**
- [ ] ⏳ Sales analytics API endpoints - **PENDING** (Optional, Phase 3)
- [x] ✅ Order number generation logic - **COMPLETED** (`internal/utils/order_utils.go`)
- [x] ✅ Payment reference generation logic - **COMPLETED** (`internal/utils/order_utils.go`)

### Frontend (To Implement):
- [ ] Payment Account Management page
- [ ] Add/Edit Payment Account form
- [ ] Orders list page
- [ ] Order detail page
- [ ] Order statistics widget
- [ ] Payment verification UI
- [ ] Sales analytics dashboard (optional)

---

## 🚀 Next Steps

### ✅ Backend (COMPLETED):
1. ✅ **Payment Account API endpoints** - DONE
2. ✅ **Order Management API endpoints** - DONE
3. ✅ **Payment Verification API endpoints** - DONE
4. ✅ **Order number & payment reference generation** - DONE

### 📋 Frontend (TODO):
1. **Build Payment Account Management UI** - Ready to implement
2. **Build Orders Dashboard UI** - Ready to implement
3. **Build Payment Management UI** - Ready to implement
4. **Test end-to-end flow**: Customer conversation → Order creation → Payment instructions → Payment verification

### 🔄 Integration Notes:
- Order creation happens automatically when AI detects customer commitment (handled in `internal/ai/customer_responder.go`)
- Payment instructions are automatically generated when customer commits (uses `PaymentInstructionGenerator`)
- All APIs are ready and tested - frontend can start building UI components

Once these are done, you'll have a complete sales-to-payment flow that tenants can manage through the UI!

