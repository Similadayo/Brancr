package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// GrowthAdvisor provides AI-powered business growth suggestions based on analytics and business profile
type GrowthAdvisor struct {
	DB       *gorm.DB
	Logger   *zap.Logger
	AIClient *ai.AIClient
}

// NewGrowthAdvisor creates a new growth advisor
func NewGrowthAdvisor(db *gorm.DB, logger *zap.Logger, aiClient *ai.AIClient) *GrowthAdvisor {
	return &GrowthAdvisor{
		DB:       db,
		Logger:   logger,
		AIClient: aiClient,
	}
}

// BusinessGrowthSuggestion represents a growth opportunity for the business
type BusinessGrowthSuggestion struct {
	ID          uint
	TenantID    uint
	Category    string // "content", "timing", "platform", "audience", "monetization"
	Title       string
	Description string
	Impact      string // "high", "medium", "low"
	Effort      string // "low", "medium", "high"
	Confidence  float64
	ActionItems []string
	ExpectedOutcome string
	CreatedAt   time.Time
}

// GenerateGrowthSuggestions analyzes analytics and business profile to suggest growth opportunities
func (g *GrowthAdvisor) GenerateGrowthSuggestions(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*BusinessGrowthSuggestion, error) {
	// Fetch business profile
	var profile storage.BusinessProfile
	if err := g.DB.Where("tenant_id = ?", tenantID).First(&profile).Error; err != nil {
		return nil, fmt.Errorf("business profile not found: %w", err)
	}
	
	// Fetch analytics summary
	var analytics []storage.PostAnalytics
	if err := g.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).Find(&analytics).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch analytics: %w", err)
	}
	
	// Fetch trend insights
	var insights []storage.TrendInsight
	if err := g.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
		tenantID, periodStart, periodEnd, "active").Find(&insights).Error; err != nil {
		g.Logger.Warn("Failed to fetch insights", zap.Error(err))
	}
	
	// Generate AI-powered suggestions
	suggestions := []*BusinessGrowthSuggestion{}
	
	// 1. Content strategy suggestions based on niche
	if contentSuggestion := g.analyzeContentStrategy(profile, analytics, insights); contentSuggestion != nil {
		suggestions = append(suggestions, contentSuggestion)
	}
	
	// 2. Platform expansion opportunities
	if platformSuggestion := g.analyzePlatformExpansion(profile, analytics); platformSuggestion != nil {
		suggestions = append(suggestions, platformSuggestion)
	}
	
	// 3. Audience growth opportunities
	if audienceSuggestion := g.analyzeAudienceGrowth(profile, analytics); audienceSuggestion != nil {
		suggestions = append(suggestions, audienceSuggestion)
	}
	
	// 4. Monetization opportunities
	if monetizationSuggestion := g.analyzeMonetization(profile, analytics); monetizationSuggestion != nil {
		suggestions = append(suggestions, monetizationSuggestion)
	}
	
	// 5. AI-generated custom suggestions
	if aiSuggestions := g.generateAIGrowthSuggestions(ctx, profile, analytics, insights); len(aiSuggestions) > 0 {
		suggestions = append(suggestions, aiSuggestions...)
	}
	
	return suggestions, nil
}

// analyzeContentStrategy suggests content improvements based on niche and performance
func (g *GrowthAdvisor) analyzeContentStrategy(profile storage.BusinessProfile, analytics []storage.PostAnalytics, insights []storage.TrendInsight) *BusinessGrowthSuggestion {
	if len(analytics) == 0 {
		return nil
	}
	
	// Find best performing content type
	avgEngagement := 0.0
	for _, a := range analytics {
		avgEngagement += a.EngagementRate
	}
	avgEngagement /= float64(len(analytics))
	
	// Niche-specific recommendations
	var title, description string
	var actionItems []string
	
	switch profile.Niche {
	case "restaurant", "food", "cafe":
		title = "Food Content Strategy"
		description = fmt.Sprintf("Your food business is averaging %.1f%% engagement. Food content performs best with behind-the-scenes videos and customer testimonials.", avgEngagement)
		actionItems = []string{
			"Create 'Day in the Life' video content showing kitchen prep",
			"Post customer reviews and testimonials as carousel posts",
			"Use Instagram Reels for quick recipe tips or menu highlights",
			"Share staff stories to build personal connection",
		}
	case "fashion", "clothing", "retail":
		title = "Fashion Content Strategy"
		description = fmt.Sprintf("Fashion brands see best results with styling content and user-generated content. Your current engagement is %.1f%%.", avgEngagement)
		actionItems = []string{
			"Create 'Outfit of the Day' (OOTD) content",
			"Encourage customers to tag you in their posts",
			"Use carousel posts to show multiple angles of products",
			"Partner with micro-influencers in your niche",
		}
	case "fitness", "wellness", "health":
		title = "Wellness Content Strategy"
		description = fmt.Sprintf("Wellness content thrives on educational and motivational content. Boost your %.1f%% engagement with transformation stories.", avgEngagement)
		actionItems = []string{
			"Share client transformation stories",
			"Create quick workout or tip videos",
			"Post motivational quotes on high-engagement days",
			"Use before/after carousel posts",
		}
	default:
		title = "Content Strategy Optimization"
		description = fmt.Sprintf("Your content is averaging %.1f%% engagement. Let's optimize based on your niche.", avgEngagement)
		actionItems = []string{
			"Analyze your top 3 performing posts and replicate their format",
			"Test different content types (video, carousel, single image)",
			"Increase posting frequency during peak engagement times",
		}
	}
	
	return &BusinessGrowthSuggestion{
		TenantID:       profile.TenantID,
		Category:       "content",
		Title:          title,
		Description:    description,
		Impact:         "high",
		Effort:         "medium",
		Confidence:     0.75,
		ActionItems:    actionItems,
		ExpectedOutcome: "15-25% increase in engagement within 30 days",
		CreatedAt:      time.Now(),
	}
}

// analyzePlatformExpansion suggests new platforms or platform optimization
func (g *GrowthAdvisor) analyzePlatformExpansion(profile storage.BusinessProfile, analytics []storage.PostAnalytics) *BusinessGrowthSuggestion {
	// Count platforms currently used
	platforms := make(map[string]int)
	for _, a := range analytics {
		platforms[a.Platform]++
	}
	
	if len(platforms) >= 3 {
		return nil // Already on multiple platforms
	}
	
	// Suggest platform expansion
	var title, description string
	var actionItems []string
	var suggestedPlatform string
	
	// Check which platforms are missing
	hasFacebook := platforms["facebook"] > 0
	
	if !hasFacebook {
		suggestedPlatform = "Facebook"
		title = "Expand to Facebook"
		description = "Facebook's older demographic and community features can complement your Instagram presence."
		actionItems = []string{
			"Create a Facebook Business Page",
			"Cross-post your Instagram content",
			"Join relevant Facebook groups in your niche",
			"Use Facebook Events for promotions",
		}
	} else {
		return nil
	}
	
	return &BusinessGrowthSuggestion{
		TenantID:       profile.TenantID,
		Category:       "platform",
		Title:          title,
		Description:    description,
		Impact:         "high",
		Effort:         "medium",
		Confidence:     0.70,
		ActionItems:    actionItems,
		ExpectedOutcome: fmt.Sprintf("Reach new audience segments on %s within 60 days", suggestedPlatform),
		CreatedAt:      time.Now(),
	}
}

// analyzeAudienceGrowth suggests audience growth strategies
func (g *GrowthAdvisor) analyzeAudienceGrowth(profile storage.BusinessProfile, analytics []storage.PostAnalytics) *BusinessGrowthSuggestion {
	if len(analytics) < 5 {
		return nil
	}
	
	// Calculate average reach
	totalReach := int64(0)
	for _, a := range analytics {
		totalReach += a.Reach
	}
	avgReach := float64(totalReach) / float64(len(analytics))
	
	title := "Audience Growth Strategy"
	description := fmt.Sprintf("Your posts are reaching an average of %.0f people. Let's expand your reach with strategic growth tactics.", avgReach)
	
	actionItems := []string{
		"Collaborate with 2-3 micro-influencers in your niche this month",
		"Run a giveaway requiring followers to tag friends",
		"Use 5-10 relevant hashtags per post (mix of popular and niche)",
		"Engage with 10-15 accounts in your niche daily",
		"Post user-generated content to build community",
	}
	
	return &BusinessGrowthSuggestion{
		TenantID:       profile.TenantID,
		Category:       "audience",
		Title:          title,
		Description:    description,
		Impact:         "high",
		Effort:         "medium",
		Confidence:     0.75,
		ActionItems:    actionItems,
		ExpectedOutcome: "20-30% increase in reach and followers within 90 days",
		CreatedAt:      time.Now(),
	}
}

// analyzeMonetization suggests monetization opportunities
func (g *GrowthAdvisor) analyzeMonetization(profile storage.BusinessProfile, analytics []storage.PostAnalytics) *BusinessGrowthSuggestion {
	if len(analytics) < 10 {
		return nil // Need more data
	}
	
	// Calculate engagement rate
	avgEngagement := 0.0
	for _, a := range analytics {
		avgEngagement += a.EngagementRate
	}
	avgEngagement /= float64(len(analytics))
	
	if avgEngagement < 3.0 {
		return nil // Engagement too low for monetization
	}
	
	title := "Monetization Opportunities"
	description := fmt.Sprintf("With %.1f%% average engagement, you have strong potential for monetization.", avgEngagement)
	
	actionItems := []string{
		"Launch a product or service promotion campaign",
		"Create affiliate partnerships with complementary brands",
		"Offer exclusive discounts to your social media followers",
		"Develop a lead magnet (free guide, checklist) to capture emails",
		"Consider sponsored content opportunities",
	}
	
	return &BusinessGrowthSuggestion{
		TenantID:       profile.TenantID,
		Category:       "monetization",
		Title:          title,
		Description:    description,
		Impact:         "high",
		Effort:         "high",
		Confidence:     0.70,
		ActionItems:    actionItems,
		ExpectedOutcome: "Generate revenue opportunities within 60-90 days",
		CreatedAt:      time.Now(),
	}
}

// generateAIGrowthSuggestions uses AI to generate custom suggestions based on business profile and analytics
func (g *GrowthAdvisor) generateAIGrowthSuggestions(ctx context.Context, profile storage.BusinessProfile, analytics []storage.PostAnalytics, insights []storage.TrendInsight) []*BusinessGrowthSuggestion {
	if g.AIClient == nil {
		return nil
	}
	
	// Build context for AI (for future AI integration)
	_ = g.buildAIContext(profile, analytics, insights)
	
	// TODO: Implement AI-powered suggestion generation
	// For now, return empty - will be implemented with proper AI integration
	
	return []*BusinessGrowthSuggestion{}
}

// Helper functions
func (g *GrowthAdvisor) buildAIContext(profile storage.BusinessProfile, analytics []storage.PostAnalytics, insights []storage.TrendInsight) string {
	return fmt.Sprintf("Business: %s in %s niche", profile.Name, profile.Niche)
}

func (g *GrowthAdvisor) formatInsights(insights []storage.TrendInsight) string {
	if len(insights) == 0 {
		return "No insights available yet."
	}
	
	result := ""
	for i, insight := range insights {
		if i >= 3 {
			break
		}
		result += fmt.Sprintf("- %s: %s\n", insight.Title, insight.Summary)
	}
	return result
}

func (g *GrowthAdvisor) calculateAvgEngagement(analytics []storage.PostAnalytics) float64 {
	if len(analytics) == 0 {
		return 0.0
	}
	total := 0.0
	for _, a := range analytics {
		total += a.EngagementRate
	}
	return total / float64(len(analytics))
}

