# Frontend Enhancement Checklist

Based on the backend implementation, here's what needs to be updated/enhanced in the existing dashboard.

## 🔐 Authentication Updates

### 1. Update Login Flow
- [ ] Ensure `credentials: 'include'` is set on all API requests
- [ ] Update API base URL to `https://api.brancr.com` (or `https://brancr.onrender.com` for testing)
- [ ] Ensure cookies are being sent/received correctly
- [ ] Handle 401 responses globally (redirect to login)

### 2. Enhanced User Profile Endpoint
- [ ] Replace placeholder user data with `GET /api/auth/me` response
- [ ] Display integration stats from response:
  ```json
  {
    "integrations": {
      "total": 5,
      "connected": 3,
      "platforms": ["instagram", "facebook", "whatsapp"]
    },
    "scheduled_posts": {
      "total": 10,
      "posted": 7
    }
  }
  ```
- [ ] Update dashboard overview cards with real data from `/api/auth/me`

---

## 🔗 Integrations Page Enhancements

### 3. Replace Mock Data with Real API
- [ ] Update `GET /api/tenant/integrations` call (replace any mock data)
- [ ] Response includes: `id`, `platform`, `connected`, `username`, `external_id`, `page_id`, `mode`, `expires_at`, `created_at`, `updated_at`

### 4. Add Integration Actions
- [ ] **View Details** - Call `GET /api/tenant/integrations/{platform}`
- [ ] **Verify Connection** - Button → `POST /api/tenant/integrations/{platform}/verify`
- [ ] **Disconnect** - Button with confirmation → `DELETE /api/tenant/integrations/{platform}`
- [ ] Show verification result messages
- [ ] Update UI state after verify/disconnect actions

### 5. Platform Connection Flows

#### Instagram & Facebook
- [ ] Ensure OAuth redirects work with backend-generated URLs
- [ ] Handle OAuth callback success/errors

#### WhatsApp (CRITICAL - New Implementation)
- [ ] **Add Meta JavaScript SDK** to the page:
  ```html
  <script src="https://connect.facebook.net/en_US/sdk.js"></script>
  ```
- [ ] **Initialize Facebook SDK**:
  ```javascript
  FB.init({
    appId: 'YOUR_META_APP_ID',
    cookie: true,
    xfbml: true,
    version: 'v18.0'
  });
  ```
- [ ] **Launch Embedded Signup** when user clicks "Connect WhatsApp":
  ```javascript
  FB.login((response) => {
    if (response.authResponse && response.event === 'WA_EMBEDDED_SIGNUP') {
      handleWhatsAppSignup(response);
    }
  }, {
    config_id: 'YOUR_WHATSAPP_CONFIG_ID',
    extras: {
      request_phone_number: true
    },
    redirect_uri: 'https://www.brancr.com/app/integrations'
  });
  ```
- [ ] **Listen for WA_EMBEDDED_SIGNUP events**:
  ```javascript
  window.addEventListener('message', (event) => {
    if (event.data && event.data.type === 'WA_EMBEDDED_SIGNUP') {
      // Ignore non-JSON messages (domain check pings)
      try {
        const payload = JSON.parse(event.data);
        if (payload.type === 'WA_EMBEDDED_SIGNUP') {
          sendSessionToBackend(payload);
        }
      } catch (e) {
        // Not JSON, ignore
      }
    }
  });
  ```
- [ ] **Send session to backend**: `POST /api/internal/meta/whatsapp/session`
  - Must include `credentials: 'include'`
  - Send the full `WA_EMBEDDED_SIGNUP` payload
- [ ] **Exchange code for token**: `POST /api/internal/meta/whatsapp/code`
  - Use `response.authResponse.code` from FB.login
  - Include `redirect_uri: 'https://www.brancr.com/'` (normalized)
  - Handle the response and update UI

#### TikTok & YouTube
- [ ] Ensure OAuth flows work with backend-generated URLs

---

## 📅 Scheduled Posts Enhancements

### 6. Replace Mock Data
- [ ] Update `GET /api/tenant/scheduled-posts` call
- [ ] Parse response fields: `id`, `name`, `caption`, `status`, `scheduled_at`, `platforms[]`, `media_asset_ids[]`, `attempts`, `last_error`, `created_at`, `posted_at`

### 7. Add Post Actions
- [ ] **View Post Details** - Call `GET /api/tenant/scheduled-posts/{id}`
- [ ] **Cancel Post** - Button with confirmation → `DELETE /api/tenant/scheduled-posts/{id}`
  - Only show cancel for non-"posted" posts
  - Handle error if post already posted

### 8. Status Indicators
- [ ] Update status badges to match backend statuses:
  - `scheduled` (blue/gray)
  - `posting` (yellow/orange) 
  - `posted` (green)
  - `failed` (red) - show error message
  - `cancelled` (gray)

---

## 📊 Dashboard Overview

### 9. Real-time Stats
- [ ] Replace placeholder stats with `GET /api/auth/me` data
- [ ] Replace placeholder stats with `GET /api/tenant/overview` data
- [ ] Display:
  - Integration count (connected/total)
  - Scheduled posts count
  - Posted posts count
  - Conversations count (from overview)

---

## 🔧 Technical Fixes

### 10. API Configuration
- [ ] **Update API base URL**:
  ```javascript
  const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'https://api.brancr.com';
  ```
- [ ] **Ensure all fetch calls include credentials**:
  ```javascript
  fetch(url, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  })
  ```
- [ ] **Handle CORS properly** - ensure frontend origin is whitelisted in backend

### 11. Error Handling
- [ ] Handle 401 globally (session expired → redirect to login)
- [ ] Handle 400/409 errors with user-friendly messages
- [ ] Display backend error messages to users
- [ ] Show loading states during API calls

### 12. Cookie Handling
- [ ] Ensure session cookie `brancr_tenant_session` is being sent
- [ ] Verify cookie domain works (`.brancr.com` for subdomains)
- [ ] Test cookie persistence after page refresh

---

## 🔗 New Features to Add

### 13. Integration Details Modal/Page
- [ ] Create detail view for integration
- [ ] Show: platform, username, external_id, connection status, mode, timestamps
- [ ] Add verify/disconnect buttons

### 14. Post Details Modal/Page
- [ ] Create detail view for scheduled post
- [ ] Show: name, caption, status, platforms, media assets, scheduled date, error messages
- [ ] Add cancel button (if applicable)

### 15. Onboarding Flow Update
- [ ] Update onboarding instructions to reflect new flow:
  1. Sign up on dashboard (web)
  2. Link Telegram bot via @BrancrAdmin bot
  3. Connect social accounts on dashboard

---

## 🚨 Critical Issues to Fix

### 16. WhatsApp Embedded Signup
- [ ] **MUST implement Meta SDK integration** (currently not working without it)
- [ ] Ensure `redirect_uri` is normalized to domain root (`https://www.brancr.com/`)
- [ ] Handle `WA_EMBEDDED_SIGNUP` payload correctly
- [ ] Exchange code for token properly
- [ ] Update integration status after successful connection

### 17. API Authentication
- [ ] **Verify all API calls send credentials**
- [ ] **Verify session cookie is being received**
- [ ] **Test login persistence** across page refreshes

---

## 📝 Quick Reference: API Endpoints

```
# Auth
GET  /api/auth/me                    - Get tenant profile + stats
POST /api/auth/login                 - Login
POST /api/auth/logout                - Logout
POST /api/auth/signup                - Signup (if not using)

# Integrations
GET    /api/tenant/integrations              - List all
GET    /api/tenant/integrations/{platform}   - Get one
DELETE /api/tenant/integrations/{platform}   - Disconnect
POST   /api/tenant/integrations/{platform}/verify  - Verify
POST   /api/tenant/integrations/{platform}/repair  - Repair (placeholder)

# Scheduled Posts
GET    /api/tenant/scheduled-posts      - List all
GET    /api/tenant/scheduled-posts/{id} - Get one
DELETE /api/tenant/scheduled-posts/{id} - Cancel

# WhatsApp (Internal)
POST /api/internal/meta/whatsapp/session  - Send WA_EMBEDDED_SIGNUP payload
POST /api/internal/meta/whatsapp/code     - Exchange code for token

# Overview
GET /api/tenant/overview - Get stats
```

---

## ✅ Priority Order

1. **Fix Authentication** (credentials, cookies, API URL)
2. **Replace Mock Data** (integrations, posts, dashboard stats)
3. **Implement WhatsApp Embedded Signup** (critical new feature)
4. **Add Integration Actions** (verify, disconnect, view details)
5. **Add Post Actions** (cancel, view details)
6. **Polish & Error Handling**

---

## 🎯 Testing Checklist

After implementing:
- [ ] Login persists across page refresh
- [ ] All API calls include credentials
- [ ] Integrations list shows real data
- [ ] Can verify/disconnect integrations
- [ ] WhatsApp connection flow works end-to-end
- [ ] Scheduled posts show real data
- [ ] Can cancel scheduled posts
- [ ] Dashboard stats are accurate
- [ ] Error messages display properly
- [ ] Loading states show during API calls

