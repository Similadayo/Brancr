package billing

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SeedDefaultPlans creates the required plan templates (Trial, Starter, Pro)
func SeedDefaultPlans(ctx context.Context, db *gorm.DB, logger *zap.Logger) error {
	billingService := NewBillingService(db, logger)

	// Check if plans already exist
	var count int64
	if err := db.Model(&storage.PlanTemplate{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check existing plans: %w", err)
	}

	if count > 0 {
		logger.Info("plan templates already exist, skipping seed")
		return nil
	}

	// Trial Plan
	trialPlan := &storage.PlanTemplate{
		Name:                      "Trial",
		DefaultAIReplyQuota:       300,  // Low quota for cost safety
		DefaultWhatsAppMessageQuota: 500, // Low quota
		DefaultPostQuota:          10,   // Limited posts
		DefaultStorageLimitMB:     500,  // 500MB storage
		DefaultAPICallsLimit:      1000, // API calls
		DefaultPlatformsLimit:     2,    // Limited platforms
		AINegotiationEnabled:      false,
		EscalationBehavior:        "always_on",
		OveragesAllowed:           false,
		DefaultMonthlyPrice:       0, // Free
		Status:                    "active",
		Description:               "Free trial plan for onboarding and learning. AI pauses at limit, escalation-only mode.",
	}

	if _, err := billingService.CreatePlanTemplate(ctx, trialPlan); err != nil {
		return fmt.Errorf("failed to create Trial plan: %w", err)
	}
	logger.Info("✅ Created Trial plan template")

	// Starter Plan (Default Paid Plan)
	starterPlan := &storage.PlanTemplate{
		Name:                      "Starter",
		DefaultAIReplyQuota:       2000, // Medium quota
		DefaultWhatsAppMessageQuota: 5000, // Medium quota
		DefaultPostQuota:          100,  // Full posting
		DefaultStorageLimitMB:     2000, // 2GB storage
		DefaultAPICallsLimit:      5000, // More API calls
		DefaultPlatformsLimit:     5,    // Standard platforms
		AINegotiationEnabled:      true,
		EscalationBehavior:        "configurable",
		OveragesAllowed:           false,
		DefaultMonthlyPrice:       5000, // Admin-set price (₦5000 default)
		Status:                    "active",
		Description:               "Default paid plan for most users. Full features with configurable escalation.",
	}

	if _, err := billingService.CreatePlanTemplate(ctx, starterPlan); err != nil {
		return fmt.Errorf("failed to create Starter plan: %w", err)
	}
	logger.Info("✅ Created Starter plan template")

	// Pro Plan (Admin-Only)
	proPlan := &storage.PlanTemplate{
		Name:                      "Pro",
		DefaultAIReplyQuota:       10000, // High quota
		DefaultWhatsAppMessageQuota: 20000, // High quota
		DefaultPostQuota:          500,  // High posting
		DefaultStorageLimitMB:     10000, // 10GB storage
		DefaultAPICallsLimit:      20000, // High API calls
		DefaultPlatformsLimit:     10,    // More platforms
		AINegotiationEnabled:      true,
		EscalationBehavior:        "advanced",
		OveragesAllowed:           true, // Overages allowed (manual approval)
		DefaultMonthlyPrice:       25000, // Admin-set price (₦25000 default)
		Status:                    "active",
		Description:               "Power user plan with high quotas and advanced features. Overages allowed with approval.",
	}

	if _, err := billingService.CreatePlanTemplate(ctx, proPlan); err != nil {
		return fmt.Errorf("failed to create Pro plan: %w", err)
	}
	logger.Info("✅ Created Pro plan template")

	logger.Info("✅ All default plan templates seeded successfully")
	return nil
}

