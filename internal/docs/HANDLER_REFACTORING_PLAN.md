# Handler Refactoring Plan

## Status

✅ **Completed:**
- Payment account encryption implemented
- Auth handlers extracted to `auth_handlers.go`

🔄 **In Progress:**
- Handler file splitting (8,219 lines → target: <500 lines per file)

## Current Structure

- `handler.go` - 8,219 lines (main file with Handler struct and RegisterRoutes)
- `auth_handlers.go` - Auth handlers (signup, login, logout, me)
- `payment_account_handlers.go` - Payment account management (already separate)

## Proposed Split

### Files to Create

1. **`posts_handlers.go`** (~1,500 lines)
   - `listScheduledPosts`
   - `getScheduledPost`
   - `updateScheduledPost`
   - `deleteScheduledPost`
   - `createPost`
   - `publishPost`
   - `generateCaption`
   - `getOptimalTimes`
   - `getCalendar`
   - `getCalendarDay`
   - `createCalendarEntry`
   - `updateCalendarEntry`
   - `removePostFromCalendar`
   - `updateCalendarForPost`

2. **`analytics_handlers.go`** (~800 lines)
   - `getAnalytics`
   - `getPerformanceSummary`
   - `getCampaignStats`
   - `getInstagramAccountInsights`
   - `getInstagramMediaInsights`
   - `getTikTokAnalytics`
   - `getTikTokVideoAnalytics`

3. **`conversations_handlers.go`** (~1,200 lines)
   - `listConversations`
   - `getConversation`
   - `sendReply`
   - `updateConversation`
   - `suggestReplies`
   - `listEscalations`
   - `getEscalation`
   - `approveEscalation`
   - `replyToEscalation`
   - `ignoreEscalation`
   - `resolveEscalation`
   - `getEscalationStats`

4. **`integrations_handlers.go`** (~1,500 lines)
   - `listIntegrations`
   - `getIntegration`
   - `deleteIntegration`
   - `verifyIntegration`
   - `repairIntegration`
   - `debugIntegration`
   - `subscribeInstagramPage`
   - `listSocialAccounts`
   - `fetchFacebookPageName`

5. **`media_handlers.go`** (~600 lines)
   - `listMedia`
   - `getMedia`
   - `createMedia`
   - `updateMedia`
   - `deleteMedia`
   - `uploadToCloudinary`

6. **`onboarding_handlers.go`** (~800 lines)
   - `getOnboardingStatus`
   - `updateBusinessProfile`
   - `updatePersona`
   - `updateBusinessDetails`
   - `completeOnboarding`
   - `setOnboardingIndustry`
   - `updateBusinessProfileSettings`
   - `updatePersonaSettings`
   - `updateBusinessDetailsSettings`

7. **`whatsapp_handlers.go`** (~2,000 lines)
   - `listAvailablePhoneNumbers`
   - `assignPhoneNumber`
   - `getAssignedPhoneNumber`
   - `disconnectPhoneNumber`
   - `checkWebhookStatus`
   - `resubscribeWebhook`
   - `comprehensiveDiagnostics`
   - `requestTenantPhoneNumber`
   - `verifyTenantPhoneNumber`
   - `checkTenantPhoneNumber`
   - `selectWhatsAppNumber`
   - `connectWhatsApp`
   - `getWhatsAppConnectionStatus`
   - `refreshWhatsAppStatus`
   - `connectWhatsAppRespondIO`
   - `connectWhatsAppGupshup`
   - `connectWhatsAppMetaEmbedded`
   - `handleWhatsAppExchangeCode`
   - `handleWhatsAppSession`

8. **`tiktok_handlers.go`** (~600 lines)
   - `listTikTokVideos`
   - `getTikTokVideo`
   - `getTikTokVideoStatus`
   - `deleteTikTokVideo`
   - `listTikTokComments`
   - `replyToTikTokComment`

9. **`products_handlers.go`** (~400 lines)
   - `listProducts`
   - `createProduct`
   - `getProduct`
   - `updateProduct`
   - `deleteProduct`
   - `listMenuItems`
   - `createMenuItem`
   - `getMenuItem`
   - `updateMenuItem`
   - `deleteMenuItem`
   - `listServices`
   - `createService`
   - `getService`
   - `updateService`
   - `deleteService`

10. **`templates_handlers.go`** (~400 lines)
    - `listTemplates`
    - `createTemplate`
    - `getTemplate`
    - `updateTemplate`
    - `deleteTemplate`

11. **`bulk_upload_handlers.go`** (~200 lines)
    - `listBulkUploads`
    - `getBulkUpload`
    - `createBulkUpload`
    - `updateBulkUploadStatus`
    - `cancelBulkUpload`

12. **`caption_handlers.go`** (~300 lines)
    - `generateCaptionFromKeywords`
    - `rephraseCaption`
    - `fineTuneCaption`

13. **`orders_handlers.go`** (~500 lines)
    - `listOrders`
    - `getOrder`
    - `updateOrder`
    - `confirmOrderPayment`
    - `generatePortalToken`
    - `getOrderStats`
    - `getOrderByPortalToken`
    - `getReceiptByPortalToken`
    - `listCustomerOrders`

14. **`payments_handlers.go`** (~400 lines)
    - `listPayments`
    - `getPayment`
    - `verifyPayment`
    - `generateReceipt`
    - `disputePayment`

15. **`team_handlers.go`** (~200 lines)
    - `listTeamMembers`
    - `inviteTeamMember`
    - `getTeamMember`
    - `updateTeamMember`
    - `deleteTeamMember`

16. **`intent_handlers.go`** (~300 lines)
    - `listIntentCatalog`
    - `createIntentCatalog`
    - `updateIntentCatalog`
    - `deleteIntentCatalog`

17. **`overview_handlers.go`** (~200 lines)
    - `overview`

18. **`industry_handlers.go`** (~200 lines)
    - `listIndustries`
    - `getTenantIndustry`
    - `setTenantIndustry`

### Keep in `handler.go` (~500 lines)
- Handler struct definition
- NewHandler constructor
- RegisterRoutes function
- Helper functions:
  - `getCloudinaryURL`
  - `setSessionCookie`
  - `cookieDomain`
  - `writeJSON`
  - `sendPlatformReply`
  - `sendInstagramReply`
  - `sendFacebookReply`
  - `sendWhatsAppReply`

## Migration Steps

1. ✅ Create `auth_handlers.go` and move auth handlers
2. ✅ Implement payment account encryption
3. Create each handler file and move corresponding functions
4. Remove moved functions from `handler.go`
5. Test each migration incrementally
6. Update imports if needed
7. Run full test suite

## Benefits

- **Maintainability**: Each file focuses on a single domain
- **Testability**: Easier to test handlers in isolation
- **Code Review**: Smaller files are easier to review
- **Performance**: Faster IDE navigation and search
- **Team Collaboration**: Multiple developers can work on different domains

## Estimated Time

- **Per file**: 30-60 minutes
- **Total**: 8-12 hours for complete refactoring
- **Incremental**: Can be done file-by-file over multiple PRs

## Next Steps

1. Continue with `posts_handlers.go` (highest priority - most used)
2. Then `analytics_handlers.go`
3. Then `conversations_handlers.go`
4. Continue with remaining files in priority order

