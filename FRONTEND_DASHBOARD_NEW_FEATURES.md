# Frontend Dashboard: New Features Implementation

## Overview
This document outlines the frontend implementation requirements for the new features added in Weeks 1-5:
- **Product/Menu/Service Management** (Week 1)
- **Order Management** (Week 3)
- **Payment Management** (Week 3)
- **Receipt Generation** (Week 4)
- **Customer Portal** (Week 4)

---

## 📦 1. Product/Menu/Service Management

### 1.1 Products Page (`/app/products`)

#### Route Structure
```
/app/products
  ├── /app/products (list view)
  ├── /app/products/new (create)
  ├── /app/products/[id] (view/edit)
  └── /app/products/[id]/edit (edit form)
```

#### Features

**List View (`/app/products`)**
- **Header Section:**
  - Title: "Products" (or "Menu Items" / "Services" based on industry)
  - Industry indicator badge showing current industry
  - "Add Product" button (primary)
  - Search bar with filters:
    - Search by name/SKU
    - Filter by category
    - Filter by status (Active/Inactive)
    - Filter by availability (In Stock/Out of Stock)

- **Products Grid/Table:**
  - Card view or table view toggle
  - Each product card shows:
    - Product image (first image from Images array)
    - Product name
    - Price (with currency)
    - Category badge
    - Stock count (if applicable)
    - Status indicator (Active/Inactive)
    - Quick actions: View, Edit, Delete

- **Empty State:**
  - Illustration
  - Message: "No products yet"
  - "Add Your First Product" button

**Create/Edit Form (`/app/products/new` or `/app/products/[id]/edit`)**
- **Form Fields:**
  - Name* (text input)
  - Description (textarea, markdown support)
  - Price* (number input with currency selector)
  - Currency (dropdown, default: NGN)
  - Category* (text input with autocomplete from existing categories)
  - Images (image upload, multiple images, drag & drop)
  - Variants (for products only):
    - JSON editor or structured form
    - Example: `{"size": ["S", "M", "L"], "color": ["Red", "Blue"]}`
  - Tags (tag input, comma-separated)
  - Stock Count (number input, -1 for unlimited)
  - Availability* (dropdown: in_stock, out_of_stock, low_stock)
  - SKU (text input, optional)
  - Is Active (toggle switch)

- **Form Actions:**
  - Save (creates/updates product)
  - Cancel (navigates back)
  - Delete (only on edit, with confirmation modal)

**View Details (`/app/products/[id]`)**
- **Product Information:**
  - Image gallery (carousel)
  - Name, description, price
  - Category, tags
  - Variants display (if applicable)
  - Stock information
  - SKU
  - Status
  - Created/Updated timestamps

- **Actions:**
  - Edit button
  - Delete button
  - Back to list

#### API Integration
```typescript
// API endpoints to use
GET    /api/tenant/products              // List products
POST   /api/tenant/products              // Create product
GET    /api/tenant/products/{id}          // Get product
PUT    /api/tenant/products/{id}         // Update product
DELETE /api/tenant/products/{id}         // Delete product
```

#### Components Needed
- `ProductCard.tsx` - Product card component
- `ProductForm.tsx` - Create/edit form
- `ProductDetails.tsx` - View details page
- `ProductFilters.tsx` - Filter/search component
- `ImageUploader.tsx` - Multi-image upload component
- `VariantEditor.tsx` - Variant JSON editor

---

### 1.2 Menu Items Page (`/app/menu`)

**Similar structure to Products but with menu-specific fields:**
- Preparation Time (minutes)
- Dietary Info (multi-select: vegetarian, vegan, gluten-free, etc.)
- Spice Level (dropdown: mild, medium, hot)
- Availability (available, unavailable, limited)

**API Endpoints:**
```typescript
GET    /api/tenant/menu-items
POST   /api/tenant/menu-items
GET    /api/tenant/menu-items/{id}
PUT    /api/tenant/menu-items/{id}
DELETE /api/tenant/menu-items/{id}
```

---

### 1.3 Services Page (`/app/services`)

**Similar structure but with service-specific fields:**
- Pricing Type (dropdown: fixed, hourly)
- Fixed Amount (if fixed)
- Hourly Rate (if hourly)
- Duration (text: "1 hour", "2 weeks", etc.)
- Deliverables (textarea)
- Packages (JSON editor for package options)

**API Endpoints:**
```typescript
GET    /api/tenant/services
POST   /api/tenant/services
GET    /api/tenant/services/{id}
PUT    /api/tenant/services/{id}
DELETE /api/tenant/services/{id}
```

---

### 1.4 Industry-Aware Navigation

**Conditional Menu Items:**
- Show "Products" only if `industry.hasProducts === true`
- Show "Menu Items" only if `industry.hasMenu === true`
- Show "Services" only if `industry.hasServices === true`

**Implementation:**
```typescript
// In sidebar/navigation component
const { industry } = useTenant();
const showProducts = industry?.hasProducts;
const showMenu = industry?.hasMenu;
const showServices = industry?.hasServices;
```

---

## 📋 2. Order Management

### 2.1 Orders List Page (`/app/orders`)

#### Route Structure
```
/app/orders
  ├── /app/orders (list view)
  ├── /app/orders/[id] (view details)
  └── /app/orders/[id]/edit (edit status/notes)
```

#### Features

**List View (`/app/orders`)**
- **Header Section:**
  - Title: "Orders"
  - Stats cards:
    - Total Orders
    - Pending Orders
    - Confirmed Orders
    - Total Revenue
  - Filters:
    - Status (All, Pending, Confirmed, Processing, Completed, Cancelled)
    - Date range picker
    - Customer search
    - Platform filter (WhatsApp, Instagram, Telegram, Facebook)

- **Orders Table:**
  - Columns:
    - Order Number (link to details)
    - Customer Name
    - Total Amount
    - Status (badge with color)
    - Platform (icon + name)
    - Created Date
    - Actions (View, Edit, Confirm Payment)

- **Status Badges:**
  - Pending: Yellow
  - Confirmed: Blue
  - Processing: Purple
  - Completed: Green
  - Cancelled: Red

**Order Details Page (`/app/orders/[id]`)**
- **Order Information Section:**
  - Order Number (copy button)
  - Payment Reference (copy button)
  - Status (with status change dropdown)
  - Created/Confirmed/Completed dates

- **Customer Information:**
  - Name
  - Phone (clickable link)
  - Email (clickable link)
  - Platform

- **Order Items:**
  - Table showing:
    - Item name
    - Quantity
    - Unit price
    - Total price
  - Subtotal
  - Total amount

- **Payment Information:**
  - Payment reference
  - Payment status
  - Payment method
  - Verification status
  - Receipt link (if generated)

- **Actions:**
  - Confirm Payment button (if pending)
  - Mark as Complete button (if confirmed)
  - Update Status dropdown
  - Add/Edit Notes
  - Generate Receipt button
  - View Receipt link (if exists)

**Edit Order Page (`/app/orders/[id]/edit`)**
- **Form Fields:**
  - Status (dropdown)
  - Notes (textarea)
  - Save/Cancel buttons

#### API Integration
```typescript
GET    /api/tenant/orders                    // List orders
GET    /api/tenant/orders/{id}               // Get order
PUT    /api/tenant/orders/{id}               // Update order
POST   /api/tenant/orders/{id}/confirm-payment // Confirm payment
GET    /api/tenant/orders/stats              // Get order statistics
```

#### Components Needed
- `OrdersList.tsx` - Orders table/list
- `OrderCard.tsx` - Order card component
- `OrderDetails.tsx` - Order details page
- `OrderStats.tsx` - Statistics cards
- `OrderFilters.tsx` - Filter component
- `OrderStatusBadge.tsx` - Status badge component
- `ConfirmPaymentModal.tsx` - Payment confirmation modal

---

## 💳 3. Payment Management

### 3.1 Payments Page (`/app/payments`)

#### Route Structure
```
/app/payments
  ├── /app/payments (list view)
  └── /app/payments/[id] (view details)
```

#### Features

**List View (`/app/payments`)**
- **Header Section:**
  - Title: "Payments"
  - Stats cards:
    - Total Payments
    - Pending Payments
    - Verified Payments
    - Total Revenue
    - Pending Revenue
    - Average Order Value
  - Filters:
    - Status (All, Pending, Verified, Confirmed, Failed, Refunded)
    - Verification Status (All, Unverified, Tenant Verified, Disputed)
    - Date range picker
    - Payment method filter

- **Payments Table:**
  - Columns:
    - Payment Reference (link to details)
    - Order Number (link to order)
    - Customer Name
    - Amount
    - Payment Method
    - Status (badge)
    - Verification Status (badge)
    - Created Date
    - Actions (View, Verify, Dispute)

**Payment Details Page (`/app/payments/[id]`)**
- **Payment Information:**
  - Payment Reference (copy button)
  - Status (badge)
  - Verification Status (badge)
  - Amount (with currency)
  - Payment Method
  - Created/Verified dates

- **Order Information:**
  - Order Number (link)
  - Order Status
  - Order Total

- **Customer Information:**
  - Name
  - Phone
  - Email

- **Receipt Information:**
  - Receipt ID (if generated)
  - Receipt URL (download link)
  - Portal URL (customer portal link)

- **Actions:**
  - Verify Payment button (if pending)
  - Dispute Payment button (if verified)
  - Generate Receipt button (if verified, no receipt)
  - View Receipt link (if exists)

#### API Integration
```typescript
GET    /api/tenant/payments                  // List payments
GET    /api/tenant/payments/{id}             // Get payment
PUT    /api/tenant/payments/{id}/verify      // Verify payment
PUT    /api/tenant/payments/{id}/dispute    // Dispute payment
```

#### Components Needed
- `PaymentsList.tsx` - Payments table/list
- `PaymentCard.tsx` - Payment card component
- `PaymentDetails.tsx` - Payment details page
- `PaymentStats.tsx` - Statistics cards
- `PaymentFilters.tsx` - Filter component
- `VerifyPaymentModal.tsx` - Payment verification modal
- `DisputePaymentModal.tsx` - Payment dispute modal

---

## 📄 4. Receipt Generation

### 4.1 Receipt Management

#### Features

**Receipt Generation:**
- Triggered from:
  - Order details page ("Generate Receipt" button)
  - Payment details page ("Generate Receipt" button)
  - Automatic on payment confirmation (optional)

**Receipt Display:**
- Receipt preview modal
- Download PDF button
- Share link button
- Email receipt button (if email service configured)

**Receipt Information:**
- Receipt ID
- Receipt URL (Cloudinary link)
- Portal URL (customer portal link)
- Generation timestamp

#### API Integration
```typescript
POST   /api/tenant/receipts/{paymentId}      // Generate receipt
GET    /api/tenant/receipts/{paymentId}     // Get receipt (if exists)
```

#### Components Needed
- `ReceiptPreview.tsx` - Receipt preview modal
- `ReceiptGenerator.tsx` - Receipt generation component
- `ReceiptDownload.tsx` - Download/share component

---

## 🔗 5. Customer Portal

### 5.1 Portal Access

#### Features

**Portal Token Generation:**
- Automatic on order creation
- Manual generation from order details page
- Token expiration (90 days default)

**Portal Pages (Public, no auth required):**
- `/portal/order?token={token}` - View order details
- `/portal/receipt?token={token}` - View/download receipt
- `/portal/orders?token={token}` - List customer orders

**Portal Features:**
- View order details
- View payment information
- Download receipt (PDF)
- View order history
- Track order status

#### Implementation Notes
- Public routes (no authentication)
- Token-based access
- Token validation and expiration check
- Responsive design (mobile-friendly)
- Branded with tenant's business info

#### Components Needed
- `PortalOrderView.tsx` - Public order view
- `PortalReceiptView.tsx` - Public receipt view
- `PortalOrderHistory.tsx` - Public order history
- `PortalTokenInput.tsx` - Token input component

---

## 🎨 6. UI/UX Guidelines

### 6.1 Design System

**Colors:**
- Primary: Brand color
- Success: Green (#10b981)
- Warning: Yellow (#f59e0b)
- Error: Red (#ef4444)
- Info: Blue (#3b82f6)

**Status Colors:**
- Pending: Yellow/Orange
- Confirmed: Blue
- Processing: Purple
- Completed: Green
- Cancelled: Red

**Typography:**
- Headings: Bold, larger size
- Body: Regular weight
- Labels: Medium weight, smaller size

**Spacing:**
- Consistent padding/margins
- Card spacing: 16px
- Section spacing: 24px

### 6.2 Responsive Design

**Breakpoints:**
- Mobile: < 768px
- Tablet: 768px - 1024px
- Desktop: > 1024px

**Mobile Considerations:**
- Stack cards vertically
- Collapsible filters
- Bottom sheet modals
- Touch-friendly buttons

### 6.3 Loading States

- Skeleton loaders for lists
- Spinner for actions
- Progress indicators for uploads
- Optimistic UI updates

### 6.4 Error Handling

- Error toast notifications
- Inline error messages
- Error boundaries
- Retry mechanisms

---

## 🔌 7. API Integration Details

### 7.1 API Client Updates

**Add to `lib/api.ts`:**

```typescript
// Products
export const productsApi = {
  list: (filters?) => GET('/api/tenant/products', filters),
  get: (id) => GET(`/api/tenant/products/${id}`),
  create: (data) => POST('/api/tenant/products', data),
  update: (id, data) => PUT(`/api/tenant/products/${id}`, data),
  delete: (id) => DELETE(`/api/tenant/products/${id}`),
};

// Menu Items
export const menuItemsApi = {
  list: (filters?) => GET('/api/tenant/menu-items', filters),
  get: (id) => GET(`/api/tenant/menu-items/${id}`),
  create: (data) => POST('/api/tenant/menu-items', data),
  update: (id, data) => PUT(`/api/tenant/menu-items/${id}`, data),
  delete: (id) => DELETE(`/api/tenant/menu-items/${id}`),
};

// Services
export const servicesApi = {
  list: (filters?) => GET('/api/tenant/services', filters),
  get: (id) => GET(`/api/tenant/services/${id}`),
  create: (data) => POST('/api/tenant/services', data),
  update: (id, data) => PUT(`/api/tenant/services/${id}`, data),
  delete: (id) => DELETE(`/api/tenant/services/${id}`),
};

// Orders
export const ordersApi = {
  list: (filters?) => GET('/api/tenant/orders', filters),
  get: (id) => GET(`/api/tenant/orders/${id}`),
  update: (id, data) => PUT(`/api/tenant/orders/${id}`, data),
  confirmPayment: (id) => POST(`/api/tenant/orders/${id}/confirm-payment`),
  stats: () => GET('/api/tenant/orders/stats'),
};

// Payments
export const paymentsApi = {
  list: (filters?) => GET('/api/tenant/payments', filters),
  get: (id) => GET(`/api/tenant/payments/${id}`),
  verify: (id) => PUT(`/api/tenant/payments/${id}/verify`),
  dispute: (id) => PUT(`/api/tenant/payments/${id}/dispute`),
};

// Receipts
export const receiptsApi = {
  generate: (paymentId) => POST(`/api/tenant/receipts/${paymentId}`),
  get: (paymentId) => GET(`/api/tenant/receipts/${paymentId}`),
};
```

### 7.2 Type Definitions

**Add to `types/index.ts`:**

```typescript
export interface Product {
  id: number;
  name: string;
  description: string;
  price: number;
  currency: string;
  category: string;
  images: string[];
  variants?: Record<string, string[]>;
  tags: string[];
  stockCount: number;
  availability: 'in_stock' | 'out_of_stock' | 'low_stock';
  isActive: boolean;
  sku?: string;
  createdAt: string;
  updatedAt: string;
}

export interface MenuItem {
  id: number;
  name: string;
  description: string;
  price: number;
  currency: string;
  category: string;
  images: string[];
  dietaryInfo: string[];
  preparationTime: number;
  availability: 'available' | 'unavailable' | 'limited';
  isActive: boolean;
  spiceLevel?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Service {
  id: number;
  name: string;
  description: string;
  pricing: {
    type: 'fixed' | 'hourly';
    amount?: number;
    rate?: number;
  };
  packages?: any[];
  duration: string;
  deliverables: string;
  isActive: boolean;
  category: string;
  createdAt: string;
  updatedAt: string;
}

export interface Order {
  id: number;
  orderNumber: string;
  paymentReference: string;
  customerName: string;
  customerPhone: string;
  customerEmail: string;
  totalAmount: number;
  currency: string;
  status: 'pending' | 'confirmed' | 'processing' | 'completed' | 'cancelled';
  platform: string;
  items: OrderItem[];
  paymentInstructions: string;
  notes?: string;
  createdAt: string;
  confirmedAt?: string;
  completedAt?: string;
}

export interface OrderItem {
  name: string;
  description?: string;
  quantity: number;
  unitPrice: number;
  totalPrice: number;
}

export interface Payment {
  id: number;
  orderId: number;
  customerId: number;
  paymentReference: string;
  amount: number;
  currency: string;
  paymentMethod: string;
  status: 'pending' | 'verified' | 'confirmed' | 'failed' | 'refunded';
  verificationStatus: 'unverified' | 'tenant_verified' | 'disputed' | 'auto_verified';
  receiptId?: string;
  receiptURL?: string;
  portalToken?: string;
  portalURL?: string;
  createdAt: string;
  verifiedAt?: string;
}
```

---

## 📱 8. Navigation Updates

### 8.1 Sidebar Menu

**Add new menu items:**

```typescript
// In sidebar/navigation component
const menuItems = [
  // ... existing items
  {
    label: 'Products',
    href: '/app/products',
    icon: PackageIcon,
    show: industry?.hasProducts,
  },
  {
    label: 'Menu Items',
    href: '/app/menu',
    icon: UtensilsIcon,
    show: industry?.hasMenu,
  },
  {
    label: 'Services',
    href: '/app/services',
    icon: BriefcaseIcon,
    show: industry?.hasServices,
  },
  {
    label: 'Orders',
    href: '/app/orders',
    icon: ShoppingCartIcon,
    badge: pendingOrdersCount, // Show count if > 0
  },
  {
    label: 'Payments',
    href: '/app/payments',
    icon: CreditCardIcon,
  },
  // ... rest of items
];
```

### 8.2 Dashboard Home Updates

**Add widgets to dashboard home:**
- Recent Orders widget
- Pending Payments widget
- Order Statistics chart
- Revenue Overview

---

## ✅ 9. Implementation Checklist

### Phase 1: Products/Menu/Services
- [ ] Create Products list page
- [ ] Create Product form (create/edit)
- [ ] Create Product details page
- [ ] Create Menu Items pages (similar structure)
- [ ] Create Services pages (similar structure)
- [ ] Add industry-aware navigation
- [ ] Implement image upload
- [ ] Add search and filters

### Phase 2: Orders
- [ ] Create Orders list page
- [ ] Create Order details page
- [ ] Create Order edit page
- [ ] Add order statistics
- [ ] Implement status updates
- [ ] Add payment confirmation flow
- [ ] Add filters and search

### Phase 3: Payments
- [ ] Create Payments list page
- [ ] Create Payment details page
- [ ] Implement payment verification
- [ ] Add payment statistics
- [ ] Add dispute functionality
- [ ] Add filters and search

### Phase 4: Receipts
- [ ] Add receipt generation to orders
- [ ] Add receipt generation to payments
- [ ] Create receipt preview component
- [ ] Add receipt download functionality
- [ ] Add email receipt option

### Phase 5: Customer Portal
- [ ] Create public portal routes
- [ ] Create order view page
- [ ] Create receipt view page
- [ ] Create order history page
- [ ] Add token validation
- [ ] Add mobile-responsive design

### Phase 6: Integration & Polish
- [ ] Update API client
- [ ] Add type definitions
- [ ] Update navigation
- [ ] Add dashboard widgets
- [ ] Add loading states
- [ ] Add error handling
- [ ] Add toast notifications
- [ ] Test all flows
- [ ] Mobile responsiveness
- [ ] Accessibility checks

---

## 🚀 10. Quick Start Guide

### Step 1: Set Up Routes
```typescript
// app/(tenant)/app/products/page.tsx
// app/(tenant)/app/products/new/page.tsx
// app/(tenant)/app/products/[id]/page.tsx
// app/(tenant)/app/orders/page.tsx
// app/(tenant)/app/orders/[id]/page.tsx
// app/(tenant)/app/payments/page.tsx
// app/(tenant)/app/payments/[id]/page.tsx
```

### Step 2: Create API Hooks
```typescript
// hooks/useProducts.ts
// hooks/useOrders.ts
// hooks/usePayments.ts
```

### Step 3: Create Shared Components
```typescript
// components/products/ProductCard.tsx
// components/orders/OrderCard.tsx
// components/payments/PaymentCard.tsx
```

### Step 4: Update Navigation
```typescript
// components/layout/Sidebar.tsx
// Add new menu items with industry checks
```

---

## 📝 Notes

1. **Industry Awareness**: Always check tenant's industry capabilities before showing menu items
2. **Real-time Updates**: Consider using WebSockets or polling for order/payment status updates
3. **Permissions**: Ensure proper authorization checks for all actions
4. **Error Handling**: Implement comprehensive error handling and user feedback
5. **Performance**: Use pagination, lazy loading, and caching where appropriate
6. **Accessibility**: Follow WCAG guidelines for accessibility
7. **Testing**: Write unit and integration tests for critical flows

---

## 🔗 Related Documents

- `FRONTEND_WEEK2_REQUIREMENTS.md` - Sales conversation UI
- `FRONTEND_WEEK3_REQUIREMENTS.md` - Payment system UI
- `FRONTEND_WEEK4_WEEK5_REQUIREMENTS.md` - Receipt and portal UI
- API Documentation - Backend API endpoints

---

**Last Updated**: [Current Date]
**Status**: Ready for Implementation

