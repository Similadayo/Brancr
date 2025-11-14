# Frontend Development Tasks

This document outlines all frontend tasks needed to complete the Brancr dashboard based on the backend implementation.

## 📋 Table of Contents
1. [Authentication & Onboarding](#1-authentication--onboarding)
2. [Dashboard Overview](#2-dashboard-overview)
3. [Social Integrations](#3-social-integrations)
4. [Scheduled Posts](#4-scheduled-posts)
5. [Settings & Profile](#5-settings--profile)
6. [UI/UX Components](#6-uiux-components)
7. [API Integration](#7-api-integration)
8. [Error Handling & Validation](#8-error-handling--validation)

---

## 1. Authentication & Onboarding

### 1.1 Signup Page
- [ ] **Create signup form** (`/signup`)
  - Fields: Name, Email, Password, Company Name, Phone (optional)
  - Email validation (case-insensitive)
  - Password strength indicator
  - Terms of service checkbox
  - Submit to `POST /api/auth/signup`
  - Handle errors (email already exists, validation errors)
  - Redirect to onboarding or dashboard after success

### 1.2 Login Page
- [ ] **Create login form** (`/login`)
  - Fields: Email, Password
  - "Remember me" option
  - "Forgot password" link (future)
  - Submit to `POST /api/auth/login`
  - Handle invalid credentials
  - Store session cookie (`brancr_tenant_session`)
  - Redirect to dashboard after success
  - Ensure credentials are sent with `credentials: 'include'`

### 1.3 Logout
- [ ] **Logout functionality**
  - Call `POST /api/auth/logout`
  - Clear session cookie
  - Redirect to login page

### 1.4 Protected Routes
- [ ] **Implement route guards**
  - Check for valid session cookie
  - Redirect to login if unauthorized
  - Handle 401 responses globally

### 1.5 Onboarding Flow (Post-Signup)
- [ ] **Welcome screen** (`/onboarding/welcome`)
  - Brief introduction to Brancr
  - "Get Started" button
- [ ] **Connect Telegram Bot** (`/onboarding/telegram`)
  - Instructions: "Link your Telegram bot via @BrancrAdmin bot"
  - Display instructions:
    1. Open Telegram and message @BrancrAdmin
    2. Type `/start`
    3. Provide your email (same as signup)
    4. Provide your bot token from @BotFather
  - "Continue" button (can be skipped for now)
- [ ] **Connect Social Accounts** (`/onboarding/integrations`)
  - Quick connect buttons for Instagram, Facebook, WhatsApp, TikTok, YouTube
  - Can be skipped and done later
- [ ] **Onboarding completion**
  - Redirect to dashboard

---

## 2. Dashboard Overview

### 2.1 Main Dashboard (`/dashboard` or `/`)
- [ ] **Fetch tenant data** (`GET /api/auth/me`)
  - Display tenant name, email, plan type
  - Display integration summary (total, connected, platforms)
  - Display scheduled posts stats (total, posted)
- [ ] **Dashboard Cards**
  - [ ] **Stats Overview**
    - Total integrations (connected/total)
    - Scheduled posts count
    - Posted posts count
    - Active conversations (if applicable)
  - [ ] **Quick Actions**
    - Connect new integration
    - Create new post
    - View analytics
  - [ ] **Recent Activity**
    - Latest scheduled posts
    - Recent integration changes
    - Post results/notifications

### 2.2 Overview API Integration (`GET /api/tenant/overview`)
- [ ] **Display overview data**
  - Conversations count
  - Scheduled posts count
  - Real-time stats updates

---

## 3. Social Integrations

### 3.1 Integrations List Page (`/integrations`)
- [ ] **Fetch integrations** (`GET /api/tenant/integrations`)
  - Display all platforms: Instagram, Facebook, WhatsApp, TikTok, YouTube
  - Show connection status (Connected/Not Connected)
  - Show username/handle for connected accounts
  - Show last updated timestamp
  - Show expiration date (if applicable)

### 3.2 Integration Cards
- [ ] **Platform Card Component**
  - Platform icon/logo
  - Platform name
  - Connection status badge
  - Username (if connected)
  - Actions: Connect, Disconnect, Verify, Repair
  - Last updated info

### 3.3 Connect Integration Flow

#### 3.3.1 Instagram
- [ ] **OAuth Flow**
  - "Connect Instagram" button
  - Redirect to Meta OAuth URL (backend generates)
  - Handle OAuth callback
  - Display success/error messages

#### 3.3.2 Facebook
- [ ] **OAuth Flow**
  - "Connect Facebook" button
  - Redirect to Meta OAuth URL
  - Handle OAuth callback
  - Display success/error messages

#### 3.3.3 WhatsApp (Embedded Signup)
- [ ] **Embedded Signup Flow**
  - "Connect WhatsApp" button
  - Integrate Meta JavaScript SDK:
    ```html
    <script src="https://connect.facebook.net/en_US/sdk.js"></script>
    ```
  - Initialize Facebook SDK:
    ```javascript
    FB.init({
      appId: 'YOUR_APP_ID',
      cookie: true,
      xfbml: true,
      version: 'v18.0'
    });
    ```
  - Launch embedded signup:
    ```javascript
    FB.login((response) => {
      if (response.authResponse && response.event === 'WA_EMBEDDED_SIGNUP') {
        // Handle WA_EMBEDDED_SIGNUP payload
        handleWhatsAppSignup(response);
      }
    }, {
      config_id: 'YOUR_CONFIG_ID',
      extras: {
        request_phone_number: true,
        request_verification_code: true
      }
    });
    ```
  - Listen for `WA_EMBEDDED_SIGNUP` message events:
    ```javascript
    window.addEventListener('message', (event) => {
      if (event.data && event.data.type === 'WA_EMBEDDED_SIGNUP') {
        // Send to backend
        sendToBackend(event.data);
      }
    });
    ```
  - Send session payload to `POST /api/internal/meta/whatsapp/session`
  - Exchange code with `POST /api/internal/meta/whatsapp/code`
  - Display connection status updates
  - Handle errors (domain not whitelisted, redirect URI mismatch, etc.)

#### 3.3.4 TikTok
- [ ] **OAuth Flow**
  - "Connect TikTok" button
  - Redirect to TikTok OAuth URL
  - Handle OAuth callback
  - Display success/error messages

#### 3.3.5 YouTube
- [ ] **OAuth Flow**
  - "Connect YouTube" button
  - Redirect to YouTube OAuth URL
  - Handle OAuth callback
  - Display success/error messages

### 3.4 Integration Actions

#### 3.4.1 View Integration Details
- [ ] **Integration Detail Modal/Page** (`GET /api/tenant/integrations/{platform}`)
  - Show full integration details
  - Platform name
  - Connection status
  - Username/handle
  - External ID (page ID, account ID, etc.)
  - Mode (oauth2, manual, sandbox)
  - Created/Updated timestamps
  - Expiration date (if applicable)

#### 3.4.2 Verify Integration
- [ ] **Verify Button** (`POST /api/tenant/integrations/{platform}/verify`)
  - Show loading state
  - Call verification endpoint
  - Display verification result message
  - Update integration status in UI
  - Handle errors

#### 3.4.3 Disconnect Integration
- [ ] **Disconnect Button** (`DELETE /api/tenant/integrations/{platform}`)
  - Confirm dialog ("Are you sure you want to disconnect?")
  - Call delete endpoint
  - Update UI to show disconnected status
  - Handle errors

#### 3.4.4 Repair Integration (Future)
- [ ] **Repair Button** (`POST /api/tenant/integrations/{platform}/repair`)
  - Placeholder for future repair functionality
  - Show repair status

---

## 4. Scheduled Posts

### 4.1 Scheduled Posts List (`/posts` or `/scheduled-posts`)
- [ ] **Fetch scheduled posts** (`GET /api/tenant/scheduled-posts`)
  - Display list of all scheduled posts
  - Show: Name, Caption (truncated), Status, Scheduled Date, Platforms, Media count
  - Sort by scheduled date (upcoming first)
  - Filter by status (scheduled, posting, posted, failed, cancelled)
  - Pagination (if needed)

### 4.2 Post Card Component
- [ ] **Post Card**
  - Thumbnail preview (first media item)
  - Post name
  - Caption preview (truncated)
  - Status badge (color-coded)
  - Platforms (icons)
  - Scheduled date/time
  - Actions: View Details, Cancel, Edit (future)

### 4.3 Create New Post (Future - Backend not implemented yet)
- [ ] **Post Creation Form**
  - Post name
  - Caption editor (with hashtag suggestions)
  - Media upload (images/videos)
  - Platform selection (checkboxes)
  - Schedule date/time picker
  - Submit to backend (endpoint TBD)

### 4.4 View Post Details
- [ ] **Post Detail Modal/Page** (`GET /api/tenant/scheduled-posts/{id}`)
  - Full post details
  - Name, Caption, Status
  - Scheduled date/time
  - Platforms list
  - Media assets (IDs - need to fetch full URLs from backend)
  - Attempts count
  - Last error (if failed)
  - Posted timestamp (if posted)
  - Created/Updated timestamps

### 4.5 Cancel Post
- [ ] **Cancel Post** (`DELETE /api/tenant/scheduled-posts/{id}`)
  - Confirm dialog ("Are you sure you want to cancel this post?")
  - Only allow cancellation if status is not "posted"
  - Call delete endpoint
  - Update UI (remove from list or mark as cancelled)
  - Handle errors (already posted, not found)

### 4.6 Post Status Indicators
- [ ] **Status Badges**
  - `scheduled` - Blue/Gray
  - `posting` - Yellow/Orange (in progress)
  - `posted` - Green (success)
  - `failed` - Red (error)
  - `cancelled` - Gray (cancelled)
  - Show error message on hover if failed

---

## 5. Settings & Profile

### 5.1 Profile Settings (`/settings/profile`)
- [ ] **Display tenant profile** (from `GET /api/auth/me`)
  - Company Name (editable)
  - Email (read-only or editable with verification)
  - Owner Name (editable)
  - Owner Phone (editable)
  - Plan Type (read-only, badge)
  - Status (read-only, badge)
  - Update profile (endpoint TBD or use existing signup/login logic)

### 5.2 Account Settings (`/settings/account`)
- [ ] **Change Password** (endpoint TBD)
  - Current password
  - New password
  - Confirm new password
- [ ] **Delete Account** (endpoint TBD)
  - Danger zone
  - Confirmation dialog

### 5.3 Telegram Bot Settings
- [ ] **Telegram Bot Info**
  - Display bot username (if linked)
  - "Link Telegram Bot" button (redirects to onboarding instructions)
  - Status indicator (linked/not linked)

---

## 6. UI/UX Components

### 6.1 Layout Components
- [ ] **Main Layout**
  - Header with tenant name and logo
  - Navigation sidebar
  - User menu (profile, settings, logout)
  - Breadcrumbs (if needed)
- [ ] **Navigation Sidebar**
  - Dashboard
  - Integrations
  - Scheduled Posts
  - Analytics (future)
  - Settings
- [ ] **Responsive Design**
  - Mobile-friendly navigation
  - Collapsible sidebar
  - Mobile menu

### 6.2 Common Components
- [ ] **Loading Spinner**
  - Global loading state
  - Button loading states
  - Skeleton loaders for lists
- [ ] **Toast Notifications**
  - Success messages
  - Error messages
  - Info messages
  - Auto-dismiss after 5 seconds
- [ ] **Modal/Dialog Component**
  - Confirm dialogs
  - Form modals
  - Detail view modals
- [ ] **Empty States**
  - No integrations connected
  - No scheduled posts
  - No results found
- [ ] **Error Boundaries**
  - Catch React errors
  - Display friendly error message
  - Log errors to monitoring service

### 6.3 Platform Icons
- [ ] **Social Platform Icons**
  - Instagram icon
  - Facebook icon
  - WhatsApp icon
  - TikTok icon
  - YouTube icon
  - Use consistent icon library (e.g., React Icons, Font Awesome)

---

## 7. API Integration

### 7.1 API Client Setup
- [ ] **Create API client/utility**
  - Base URL: `https://api.brancr.com` (or `http://localhost:8080` for dev)
  - Handle CORS with credentials
  - Automatic cookie handling
  - Request/response interceptors
  - Error handling

### 7.2 API Endpoints Integration
- [ ] **Auth Endpoints**
  - `POST /api/auth/signup`
  - `POST /api/auth/login`
  - `POST /api/auth/logout`
  - `GET /api/auth/me`
- [ ] **Integration Endpoints**
  - `GET /api/tenant/integrations`
  - `GET /api/tenant/integrations/{platform}`
  - `DELETE /api/tenant/integrations/{platform}`
  - `POST /api/tenant/integrations/{platform}/verify`
  - `POST /api/tenant/integrations/{platform}/repair`
- [ ] **Scheduled Posts Endpoints**
  - `GET /api/tenant/scheduled-posts`
  - `GET /api/tenant/scheduled-posts/{id}`
  - `DELETE /api/tenant/scheduled-posts/{id}`
- [ ] **WhatsApp Endpoints**
  - `POST /api/internal/meta/whatsapp/session`
  - `POST /api/internal/meta/whatsapp/code`
- [ ] **Overview Endpoint**
  - `GET /api/tenant/overview`

### 7.3 Request Configuration
- [ ] **All API requests must include:**
  ```javascript
  {
    credentials: 'include', // Critical for cookies
    headers: {
      'Content-Type': 'application/json'
    }
  }
  ```
- [ ] **Handle response types**
  - JSON responses
  - Error responses (status codes)
  - Cookie handling (automatic with credentials: 'include')

---

## 8. Error Handling & Validation

### 8.1 Form Validation
- [ ] **Signup Form**
  - Email format validation
  - Password strength (min 8 chars, special chars, etc.)
  - Required fields
  - Email uniqueness check
- [ ] **Login Form**
  - Email format validation
  - Required fields
- [ ] **Integration Forms**
  - Platform-specific validation
  - Token format validation (if manual)

### 8.2 API Error Handling
- [ ] **Handle HTTP Errors**
  - 400 Bad Request (validation errors)
  - 401 Unauthorized (session expired, redirect to login)
  - 403 Forbidden (insufficient permissions)
  - 404 Not Found
  - 409 Conflict (email already exists)
  - 500 Internal Server Error (show friendly message)
  - 502 Bad Gateway (service unavailable)
- [ ] **Display Error Messages**
  - User-friendly error messages
  - Field-level errors for forms
  - Global error notifications

### 8.3 Network Error Handling
- [ ] **Handle Network Failures**
  - Timeout handling
  - Retry logic (for critical operations)
  - Offline detection
  - Service unavailable messages

---

## 9. State Management

### 9.1 Authentication State
- [ ] **Auth Context/Store**
  - Current user/tenant data
  - Login/logout actions
  - Session persistence
  - Auto-refresh session (if implemented)

### 9.2 Integrations State
- [ ] **Integrations Store**
  - List of integrations
  - Selected integration
  - Loading states
  - Error states

### 9.3 Posts State
- [ ] **Scheduled Posts Store**
  - List of scheduled posts
  - Selected post
  - Filters
  - Loading states

---

## 10. Environment Configuration

### 10.1 Environment Variables
- [ ] **Setup environment variables**
  - `NEXT_PUBLIC_API_URL` or `REACT_APP_API_URL` = `https://api.brancr.com`
  - `NEXT_PUBLIC_META_APP_ID` or `REACT_APP_META_APP_ID` = Meta App ID
  - `NEXT_PUBLIC_META_CONFIG_ID` or `REACT_APP_META_CONFIG_ID` = WhatsApp Config ID
  - Development vs Production configs

---

## 11. Testing (Recommended)

### 11.1 Unit Tests
- [ ] **Component tests**
  - Form components
  - API client functions
  - Utility functions

### 11.2 Integration Tests
- [ ] **API integration tests**
  - Mock API responses
  - Test error scenarios

### 11.3 E2E Tests (Optional)
- [ ] **Critical user flows**
  - Signup → Login → Dashboard
  - Connect Integration
  - View Scheduled Posts

---

## 12. Performance & Optimization

### 12.1 Performance
- [ ] **Code splitting**
  - Lazy load routes
  - Dynamic imports for heavy components
- [ ] **Image optimization**
  - Optimize integration icons
  - Post thumbnails
- [ ] **API optimization**
  - Debounce search inputs
  - Cache API responses (where appropriate)
  - Pagination for large lists

### 12.2 Accessibility
- [ ] **ARIA labels**
  - Buttons, links, forms
  - Screen reader support
- [ ] **Keyboard navigation**
  - Tab order
  - Keyboard shortcuts
- [ ] **Color contrast**
  - WCAG AA compliance

---

## Priority Order

### Phase 1: Core Functionality (MVP)
1. Authentication (Signup, Login, Logout)
2. Dashboard Overview
3. Integrations List & Connect (Instagram, Facebook, WhatsApp)
4. Scheduled Posts List & View Details

### Phase 2: Enhanced Features
5. Integration Details & Actions (Verify, Disconnect)
6. Cancel Scheduled Posts
7. Settings & Profile
8. TikTok & YouTube Integration

### Phase 3: Polish & Advanced
9. Post Creation UI (when backend is ready)
10. Analytics Dashboard
11. Advanced error handling
12. Performance optimization

---

## Notes

- **Critical:** All API requests must include `credentials: 'include'` for cookie-based authentication
- **WhatsApp:** Requires Meta JavaScript SDK integration for embedded signup
- **OAuth Flows:** Backend generates OAuth URLs, frontend redirects and handles callbacks
- **Error Messages:** Backend returns user-friendly error messages, display them clearly
- **Session Management:** Session cookie is set automatically, ensure it's sent with all requests

