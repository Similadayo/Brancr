# Billing System Deployment Checklist

## ✅ Pre-Deployment Verification

### Backend Compilation
- ✅ All Go packages compile without errors
- ✅ Type conversions fixed (uint64 → uint)
- ✅ All imports resolved
- ✅ Services properly initialized

### Frontend Build
- ✅ TypeScript compilation successful
- ✅ All unused imports removed
- ✅ Accessibility issues fixed (aria-label on select elements)
- ✅ All routes registered

### Database
- ✅ Models added to AutoMigrate
- ✅ Default plan templates seeded on startup

## 📋 Environment Variables Required

Add these to your `.env` or Railway environment:

```bash
# Paystack Configuration (optional - billing works without it)
PAYSTACK_SECRET_KEY=sk_test_...
PAYSTACK_PUBLIC_KEY=pk_test_...
```

## 🚀 Deployment Steps

1. **Database Migration**
   - AutoMigrate will create new tables on startup:
     - `plan_templates`
     - `tenant_plans`
     - `usage_events`
     - `usage_aggregations`

2. **Default Plans Seeding**
   - Trial, Starter, Pro plans are automatically created on first startup
   - Check logs for: `✅ All default plan templates seeded successfully`

3. **Service Initialization**
   - Billing services initialize after AlertService
   - Check logs for: `✅ Billing services initialized`
   - Paystack service only initializes if keys are configured

## 🔍 Post-Deployment Verification

1. **Check Admin Dashboard**
   - Navigate to `/admin/app/plans` - should show 3 default plans
   - Navigate to `/admin/app/tenants/:id` - should show billing section

2. **Test Plan Assignment**
   - Go to tenant detail page
   - Assign a plan from the billing section
   - Verify plan appears and quotas are set

3. **Test Usage Tracking**
   - Send an AI reply (should track usage)
   - Publish a post (should track usage)
   - Check tenant billing page for usage stats

4. **Test Paystack Webhook** (if configured)
   - Webhook endpoint: `/admin/api/paystack/webhook`
   - Configure in Paystack dashboard

## ⚠️ Known Issues & Notes

1. **Accessibility Warnings**
   - Some linter warnings about select elements may appear but are false positives
   - All select elements have proper aria-label attributes

2. **Usage Tracking**
   - Usage events are recorded asynchronously
   - Aggregation happens in background (may have slight delay)

3. **Plan Template Updates**
   - Editing a plan template only affects NEW tenant assignments
   - Existing tenant plans are NOT automatically updated (by design)

## 📝 API Endpoints Added

### Plan Templates
- `GET /admin/api/plans/templates` - List all templates
- `POST /admin/api/plans/templates` - Create template
- `PUT /admin/api/plans/templates/{id}` - Update template
- `POST /admin/api/plans/templates/{id}/deprecate` - Deprecate template

### Tenant Plans
- `POST /admin/api/tenants/{tenant_id}/plans/assign` - Assign plan
- `GET /admin/api/tenants/{tenant_id}/billing` - Get billing info
- `PUT /admin/api/tenants/{tenant_id}/plans/quotas` - Override quotas
- `PUT /admin/api/tenants/{tenant_id}/plans/price` - Override price
- `POST /admin/api/tenants/{tenant_id}/plans/pause` - Pause plan
- `POST /admin/api/tenants/{tenant_id}/plans/resume` - Resume plan

### Paystack
- `POST /admin/api/paystack/webhook` - Webhook handler

## 🎯 Next Steps After Deployment

1. **Assign Plans to Existing Tenants**
   - Use admin dashboard to assign appropriate plans
   - Or use API directly

2. **Configure Paystack** (if using)
   - Add webhook URL to Paystack dashboard
   - Test payment flow

3. **Monitor Usage**
   - Check tenant billing pages regularly
   - Review usage aggregations

4. **Adjust Plan Templates**
   - Update quotas/prices as needed
   - Create new plans if required

