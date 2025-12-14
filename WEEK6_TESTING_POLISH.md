# Week 6: Testing & Polish

## ✅ Status: ReceiptService Fixed - Ready for Testing

---

## 🎯 Testing Objectives

1. **End-to-End Order Flow** - Complete customer journey from inquiry to receipt
2. **Receipt Generation** - HTML, PDF, Cloudinary upload, email delivery
3. **Customer Portal** - Token-based access, order viewing, receipt download
4. **Database Performance** - Index verification, query optimization
5. **Security** - Authentication, authorization, input validation
6. **Error Handling** - Edge cases and error scenarios
7. **Integration Testing** - All services working together

---

## 📋 Testing Checklist

### 1. End-to-End Order Flow Testing

#### Test Case 1.1: Complete Sales Conversation Flow
- [ ] Customer sends inquiry message
- [ ] AI responds with product/service information
- [ ] Customer shows interest
- [ ] AI engages in sales conversation
- [ ] Customer commits to purchase
- [ ] Order is automatically created
- [ ] Payment instructions are generated
- [ ] Portal token is generated
- [ ] Customer receives payment instructions with portal link

**Test Steps:**
1. Send test message via WhatsApp/Telegram
2. Verify AI response includes product information
3. Continue conversation to show purchase intent
4. Verify order creation in database
5. Verify payment reference is generated
6. Verify portal token is created
7. Verify payment instructions include portal URL

**Expected Results:**
- Order created with unique order number
- Payment reference generated
- Portal token generated and linked to order
- Payment instructions include portal URL

---

#### Test Case 1.2: Payment Confirmation Flow
- [ ] Tenant confirms payment manually
- [ ] Receipt is auto-generated
- [ ] PDF is created and uploaded to Cloudinary
- [ ] Email receipt is sent to customer
- [ ] Portal link is included in email

**Test Steps:**
1. Confirm payment via API: `PUT /api/tenant/orders/{id}/confirm-payment`
2. Verify receipt generation
3. Check Cloudinary for PDF upload
4. Verify email was sent
5. Check email content includes portal link

**Expected Results:**
- Receipt ID generated
- Receipt URL points to Cloudinary PDF
- Email sent to customer email
- Email contains receipt link and portal link

---

### 2. Receipt Generation Testing

#### Test Case 2.1: HTML Receipt Generation
- [ ] Receipt HTML is generated correctly
- [ ] All order details are included
- [ ] Business information is correct
- [ ] Customer information is correct
- [ ] Items are displayed correctly
- [ ] Totals are calculated correctly

**Test Steps:**
1. Generate receipt for confirmed payment
2. Verify receipt HTML structure
3. Check all fields are populated
4. Verify calculations are correct

**Expected Results:**
- Valid HTML structure
- All required fields present
- Correct formatting and styling
- Accurate calculations

---

#### Test Case 2.2: PDF Conversion
- [ ] HTML is converted to PDF
- [ ] PDF is valid and readable
- [ ] PDF contains all receipt information
- [ ] PDF is print-friendly

**Test Steps:**
1. Generate receipt with Cloudinary configured
2. Verify PDF conversion succeeds
3. Download and open PDF
4. Verify content is readable
5. Test printing PDF

**Expected Results:**
- PDF file is created
- PDF is valid and can be opened
- All content is visible
- Print layout is correct

---

#### Test Case 2.3: Cloudinary Upload
- [ ] PDF is uploaded to Cloudinary
- [ ] Cloudinary URL is stored in database
- [ ] PDF is accessible via URL
- [ ] PDF can be downloaded

**Test Steps:**
1. Generate receipt with Cloudinary configured
2. Verify upload to Cloudinary
3. Check database for Cloudinary URL
4. Access PDF via URL
5. Download and verify PDF

**Expected Results:**
- Upload succeeds
- URL stored in `payment.receipt_url`
- PDF accessible via URL
- Download works correctly

---

#### Test Case 2.4: Email Receipt Delivery
- [ ] Email is sent when receipt is generated
- [ ] Email contains receipt link
- [ ] Email contains portal link
- [ ] Email formatting is correct
- [ ] Email is delivered successfully

**Test Steps:**
1. Generate receipt with email service configured
2. Check email service logs
3. Verify email was sent
4. Check email inbox
5. Verify email content

**Expected Results:**
- Email sent successfully
- Email contains receipt URL
- Email contains portal URL
- Email is well-formatted
- Email delivered to customer

---

### 3. Customer Portal Testing

#### Test Case 3.1: Portal Token Generation
- [ ] Portal token is generated correctly
- [ ] Token is unique
- [ ] Token expiration is set
- [ ] Portal URL is generated correctly

**Test Steps:**
1. Generate portal token for order
2. Verify token is created
3. Check token expiration date
4. Verify portal URL format

**Expected Results:**
- Token is generated
- Token is unique
- Expiration date is set (90 days default)
- URL format: `{publicURL}/portal/order?token={token}`

---

#### Test Case 3.2: Portal Access with Valid Token
- [ ] Portal page loads with valid token
- [ ] Order details are displayed
- [ ] Payment information is shown
- [ ] Business information is displayed
- [ ] Receipt link is available (if receipt exists)

**Test Steps:**
1. Generate portal token
2. Access portal URL
3. Verify page loads
4. Check all information is displayed
5. Verify receipt link (if available)

**Expected Results:**
- Page loads successfully
- All order details visible
- Payment reference displayed
- Business info shown
- Receipt link works (if receipt exists)

---

#### Test Case 3.3: Portal Access with Invalid Token
- [ ] Invalid token returns error
- [ ] Error message is clear
- [ ] Expired token returns error
- [ ] Error message indicates expiration

**Test Steps:**
1. Access portal with invalid token
2. Access portal with expired token
3. Verify error responses
4. Check error messages

**Expected Results:**
- 401 Unauthorized for invalid token
- 401 Unauthorized for expired token
- Clear error messages
- User-friendly error text

---

#### Test Case 3.4: Receipt View via Portal
- [ ] Receipt can be viewed via portal token
- [ ] Receipt HTML is displayed correctly
- [ ] PDF receipt can be downloaded
- [ ] Receipt information is accurate

**Test Steps:**
1. Generate receipt for order
2. Access receipt via portal token
3. Verify receipt displays correctly
4. Test PDF download (if available)

**Expected Results:**
- Receipt page loads
- Receipt content is correct
- PDF download works (if PDF exists)
- All information is accurate

---

#### Test Case 3.5: Customer Order History
- [ ] Order history page loads
- [ ] All customer orders are listed
- [ ] Orders are sorted correctly
- [ ] Order details are accessible

**Test Steps:**
1. Create multiple orders for customer
2. Access order history via portal token
3. Verify all orders are listed
4. Check sorting (newest first)
5. Test viewing individual orders

**Expected Results:**
- All orders listed
- Correct sorting
- Order details accessible
- Navigation works correctly

---

### 4. Database Performance Testing

#### Test Case 4.1: Index Verification
- [ ] All critical indexes exist
- [ ] Indexes are used in queries
- [ ] Query performance is acceptable

**Test Steps:**
1. Check database indexes
2. Run EXPLAIN on critical queries
3. Verify indexes are used
4. Measure query performance

**Critical Indexes to Verify:**
- `orders.tenant_id`
- `orders.customer_id`
- `orders.status`
- `orders.created_at`
- `payments.tenant_id`
- `payments.order_id`
- `payments.payment_reference`
- `payments.portal_token`
- `payments.token_expires_at`

**Expected Results:**
- All indexes exist
- Queries use indexes
- Query time < 100ms for indexed queries

---

#### Test Case 4.2: Query Performance
- [ ] Order listing is fast
- [ ] Order details retrieval is fast
- [ ] Payment queries are optimized
- [ ] Portal token lookup is fast

**Test Steps:**
1. Test order listing with 1000+ orders
2. Test order details retrieval
3. Test payment queries
4. Test portal token lookup
5. Measure response times

**Expected Results:**
- Order listing < 200ms
- Order details < 50ms
- Payment queries < 50ms
- Portal lookup < 50ms

---

### 5. Security Testing

#### Test Case 5.1: Authentication
- [ ] Tenant authentication works
- [ ] Invalid tokens are rejected
- [ ] Expired sessions are handled
- [ ] Unauthorized access is blocked

**Test Steps:**
1. Test valid authentication
2. Test invalid token
3. Test expired session
4. Test unauthorized endpoint access

**Expected Results:**
- Valid auth succeeds
- Invalid token returns 401
- Expired session returns 401
- Unauthorized access blocked

---

#### Test Case 5.2: Authorization
- [ ] Tenants can only access their own data
- [ ] Cross-tenant access is blocked
- [ ] Portal tokens are tenant-specific
- [ ] Order access is restricted

**Test Steps:**
1. Try accessing another tenant's order
2. Try using another tenant's portal token
3. Verify access is denied
4. Check error responses

**Expected Results:**
- Cross-tenant access blocked
- 403 or 404 errors returned
- Clear error messages
- No data leakage

---

#### Test Case 5.3: Input Validation
- [ ] Invalid input is rejected
- [ ] SQL injection attempts are blocked
- [ ] XSS attempts are sanitized
- [ ] Malformed requests are handled

**Test Steps:**
1. Test SQL injection in order ID
2. Test XSS in order notes
3. Test malformed JSON
4. Test invalid status values
5. Test negative amounts

**Expected Results:**
- SQL injection blocked
- XSS sanitized
- Malformed requests rejected
- Invalid values rejected
- Clear error messages

---

#### Test Case 5.4: Payment Reference Security
- [ ] Payment references are unique
- [ ] References cannot be guessed
- [ ] References are properly validated
- [ ] Duplicate references are prevented

**Test Steps:**
1. Create multiple orders
2. Verify payment references are unique
3. Test payment reference format
4. Try to guess payment reference
5. Test duplicate reference handling

**Expected Results:**
- All references unique
- References are random/unpredictable
- Format validation works
- Duplicates prevented

---

### 6. Error Handling Testing

#### Test Case 6.1: Order Not Found
- [ ] Invalid order ID returns 404
- [ ] Error message is clear
- [ ] Cross-tenant order access returns 404

**Test Steps:**
1. Request non-existent order
2. Request another tenant's order
3. Verify error responses

**Expected Results:**
- 404 Not Found
- Clear error message
- No data leakage

---

#### Test Case 6.2: Payment Not Found
- [ ] Invalid payment ID returns 404
- [ ] Error message is clear
- [ ] Payment verification errors are handled

**Test Steps:**
1. Request non-existent payment
2. Verify error response
3. Test payment verification errors

**Expected Results:**
- 404 Not Found
- Clear error message
- Proper error handling

---

#### Test Case 6.3: Receipt Generation Errors
- [ ] Missing order data is handled
- [ ] Cloudinary upload failures are handled
- [ ] PDF conversion failures are handled
- [ ] Email sending failures are handled

**Test Steps:**
1. Test receipt generation with missing data
2. Test with Cloudinary unavailable
3. Test with PDF conversion failure
4. Test with email service unavailable

**Expected Results:**
- Errors are logged
- Graceful fallbacks (HTML if PDF fails)
- Receipt generation doesn't fail completely
- User-friendly error messages

---

#### Test Case 6.4: Portal Token Errors
- [ ] Invalid token returns error
- [ ] Expired token returns error
- [ ] Missing token returns error
- [ ] Error messages are clear

**Test Steps:**
1. Access portal with invalid token
2. Access portal with expired token
3. Access portal without token
4. Verify error responses

**Expected Results:**
- 401 Unauthorized
- Clear error messages
- User-friendly error text

---

### 7. Integration Testing

#### Test Case 7.1: Order Creation Integration
- [ ] Order creation triggers all services
- [ ] Portal token is generated
- [ ] Payment reference is created
- [ ] All services work together

**Test Steps:**
1. Create order via sales conversation
2. Verify all services are called
3. Check database for all records
4. Verify integration points

**Expected Results:**
- Order created
- Payment reference generated
- Portal token generated
- All services integrated

---

#### Test Case 7.2: Receipt Generation Integration
- [ ] Receipt generation uses all services
- [ ] PDF conversion works
- [ ] Cloudinary upload works
- [ ] Email sending works

**Test Steps:**
1. Generate receipt
2. Verify PDF conversion
3. Verify Cloudinary upload
4. Verify email sending

**Expected Results:**
- All services work together
- PDF created and uploaded
- Email sent successfully
- All integration points work

---

#### Test Case 7.3: Notification Integration
- [ ] Order status changes trigger notifications
- [ ] Email notifications are sent
- [ ] Webhook notifications are sent
- [ ] Notifications are configurable

**Test Steps:**
1. Update order status
2. Verify email notification
3. Verify webhook notification
4. Test notification settings

**Expected Results:**
- Notifications triggered
- Email sent (if configured)
- Webhook sent (if configured)
- Settings respected

---

## 🔍 Code Review Checklist

### Database
- [ ] All indexes are created
- [ ] Foreign keys are properly set
- [ ] Unique constraints are in place
- [ ] Data types are appropriate
- [ ] Migrations are tested

### API Endpoints
- [ ] All endpoints return proper status codes
- [ ] Error responses are consistent
- [ ] Request validation is in place
- [ ] Response formats are consistent
- [ ] Authentication is required where needed

### Business Logic
- [ ] Order creation logic is correct
- [ ] Payment reference generation is secure
- [ ] Portal token generation is secure
- [ ] Receipt generation handles all cases
- [ ] Email sending is optional and graceful

### Security
- [ ] Authentication is enforced
- [ ] Authorization is checked
- [ ] Input validation is in place
- [ ] SQL injection protection
- [ ] XSS protection
- [ ] CSRF protection (if needed)

### Error Handling
- [ ] All errors are logged
- [ ] User-friendly error messages
- [ ] Graceful degradation
- [ ] No sensitive data in errors

---

## 🚀 Performance Optimization

### Database
- [ ] Add missing indexes
- [ ] Optimize slow queries
- [ ] Add query result caching
- [ ] Implement pagination where needed

### API
- [ ] Add response caching
- [ ] Implement rate limiting
- [ ] Optimize response sizes
- [ ] Add compression

### Background Jobs
- [ ] Receipt generation is async (if needed)
- [ ] Email sending is async
- [ ] Webhook delivery is async
- [ ] Job retry logic is in place

---

## 📝 Documentation

### API Documentation
- [ ] All endpoints documented
- [ ] Request/response examples
- [ ] Error codes documented
- [ ] Authentication documented

### Deployment Documentation
- [ ] Environment variables documented
- [ ] Database setup documented
- [ ] Deployment steps documented
- [ ] Troubleshooting guide

### User Documentation
- [ ] Customer portal usage guide
- [ ] Receipt access instructions
- [ ] Order tracking guide

---

## 🐛 Known Issues & Fixes

### Fixed Issues
- ✅ ReceiptService initialization - Now properly accepts CloudinaryURL, PublicURL, and EmailService
- ✅ updateOrder handler - Now allows updating both status and notes together
- ✅ OAuth endpoints - All endpoints verified and working
- ✅ Environment variables - All documented in .env.example

### Potential Issues to Watch
- ⚠️ PDF conversion requires chromedp or wkhtmltopdf
- ⚠️ Cloudinary upload requires valid credentials
- ⚠️ Email sending requires SMTP configuration
- ⚠️ Portal token expiration needs testing

---

## ✅ Testing Execution Plan

### Phase 1: Unit Testing (Day 1-2)
1. Test individual service methods
2. Test repository methods
3. Test utility functions
4. Fix any unit test failures

### Phase 2: Integration Testing (Day 3-4)
1. Test order creation flow
2. Test receipt generation flow
3. Test customer portal flow
4. Test notification flow

### Phase 3: End-to-End Testing (Day 5)
1. Complete customer journey test
2. Multi-tenant isolation test
3. Error scenario testing
4. Performance testing

### Phase 4: Security & Polish (Day 6-7)
1. Security audit
2. Performance optimization
3. Documentation completion
4. Final bug fixes

---

## 📊 Success Criteria

### Functional
- ✅ All features work as expected
- ✅ All error cases are handled
- ✅ All integrations work correctly
- ✅ User experience is smooth

### Performance
- ✅ API response times < 200ms (p95)
- ✅ Database queries < 100ms (p95)
- ✅ Receipt generation < 5s
- ✅ Email delivery < 30s

### Security
- ✅ No SQL injection vulnerabilities
- ✅ No XSS vulnerabilities
- ✅ Proper authentication/authorization
- ✅ Secure token generation

### Reliability
- ✅ Graceful error handling
- ✅ No data loss
- ✅ Proper logging
- ✅ Monitoring in place

---

## 🎯 Next Steps

1. **Start Testing** - Begin with end-to-end order flow
2. **Fix Issues** - Address any bugs found during testing
3. **Optimize** - Improve performance where needed
4. **Document** - Complete API and deployment docs
5. **Deploy** - Prepare for production deployment

---

## 📌 Notes

- All testing should be done in a staging environment
- Use test data that mimics production scenarios
- Document all bugs found and fixes applied
- Keep test results for future reference

---

# Brancr Admin Dashboard: Phased Delivery Checklist

## Phase 1: Foundation & Access Control
- [x] Align backend RBAC to new roles (SupportAdmin, PlatformAdmin, BillingAdmin, ComplianceAdmin, SuperAdmin)
- [x] Add `/admin/api/me` endpoint (returns admin email and role)
- [x] Show admin identity in SPA shell
- [x] Robust static asset serving for SPA
- [x] Audit logging for all privileged actions

## Phase 2: Core Dashboard Sections
- [ ] Overview: System KPIs, live stats, event feed
- [ ] Tenants: Metadata-first tenant list, search, quota, status, escalation
- [ ] Onboarding: Onboarding state, errors, escalations, audit trail
- [ ] Integrations: Health/status of all integrations, error surfacing
- [ ] Automation: Scheduled jobs, workflow state, error handling
- [ ] Billing: Usage, quota, payment status, escalation triggers
- [ ] Audit Logs: Full audit log browser, filter by action/admin
- [ ] Settings: Admin user management, role assignment, password reset

## Phase 3: Escalation & Compliance Logic
- [ ] Escalation flows: Support → Platform → Super, with audit trail
- [ ] Compliance role: Read-only access to audit logs, system state
- [ ] Action-level RBAC: Restrict sensitive actions to correct roles
- [ ] Real-time escalation notifications (SSE or WebSocket)

## Phase 4: Polish & Productionization
- [ ] End-to-end tests (backend + SPA)
- [ ] Accessibility & responsive design
- [ ] Documentation: Admin guide, escalation SOPs
- [ ] Production readiness: Monitoring, error reporting, backup/restore

---

**Legend:**
- [x] = Complete
- [ ] = Pending

**Notes:**
- Every privileged action must be audit-logged.
- All dashboard sections must be metadata/system-state-first, not tenant-content-first.
- Escalation and compliance logic must be strictly enforced and visible in the UI.

