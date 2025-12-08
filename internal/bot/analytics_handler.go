package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/analytics"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// AnalyticsHandler handles analytics requests from tenants via Telegram
type AnalyticsHandler struct {
	Manager          *TenantBotManager
	AnalyticsService interface{} // *analytics.Service
	TelegramReporter *analytics.TelegramReporter
}

// NewAnalyticsHandler creates a new analytics handler for the bot
func NewAnalyticsHandler(manager *TenantBotManager, analyticsService interface{}, reporter *analytics.TelegramReporter) *AnalyticsHandler {
	return &AnalyticsHandler{
		Manager:          manager,
		AnalyticsService: analyticsService,
		TelegramReporter: reporter,
	}
}

// HandleAnalyticsRequest handles various analytics requests from tenants
func (h *AnalyticsHandler) HandleAnalyticsRequest(ctx context.Context, tenantID uint, chatID int64, text string) error {
	bot, ok := h.Manager.GetBot(tenantID)
	if !ok {
		return fmt.Errorf("bot not found for tenant %d", tenantID)
	}

	lower := strings.ToLower(strings.TrimSpace(text))

	// Send typing indicator
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Route to appropriate analytics function
	switch {
	case strings.Contains(lower, "weekly") || strings.Contains(lower, "week summary"):
		return h.sendWeeklySummary(ctx, bot, tenantID, chatID)
	case strings.Contains(lower, "insights") || strings.Contains(lower, "trends"):
		return h.sendTrendInsights(ctx, bot, tenantID, chatID)
	case strings.Contains(lower, "growth") || strings.Contains(lower, "suggestions"):
		return h.sendGrowthSuggestions(ctx, bot, tenantID, chatID)
	case strings.Contains(lower, "stats") || strings.Contains(lower, "analytics") || strings.Contains(lower, "performance"):
		return h.sendAnalyticsSummary(ctx, bot, tenantID, chatID)
	case strings.Contains(lower, "platform") && strings.Contains(lower, "performance"):
		return h.sendPlatformBreakdown(ctx, bot, tenantID, chatID)
	default:
		// Default to weekly summary
		return h.sendWeeklySummary(ctx, bot, tenantID, chatID)
	}
}

// sendWeeklySummary sends a conversational weekly analytics summary
func (h *AnalyticsHandler) sendWeeklySummary(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	if h.TelegramReporter == nil {
		// Fallback if reporter not initialized
		h.Manager.Logger.Warn("TelegramReporter not initialized, using basic stats",
			zap.Uint("tenant_id", tenantID))
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	if err := h.TelegramReporter.GenerateWeeklySummary(ctx, tenantID, bot, chatID); err != nil {
		h.Manager.Logger.Error("failed to generate weekly summary",
			zap.Error(err),
			zap.Uint("tenant_id", tenantID))
		// Fallback to basic stats on error
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	return nil
}

// sendTrendInsights sends AI-generated trend insights with production-grade formatting
func (h *AnalyticsHandler) sendTrendInsights(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Check if we have analytics data first
	var analyticsCount int64
	h.Manager.DB.Model(&storage.PostAnalytics{}).
		Where("tenant_id = ?", tenantID).
		Count(&analyticsCount)

	// If no analytics, generate insights from recent posts
	if analyticsCount == 0 {
		var postedPosts []storage.ScheduledPost
		h.Manager.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ?",
			tenantID, "posted", time.Now().AddDate(0, 0, -30)).
			Order("posted_at DESC").
			Limit(10).
			Find(&postedPosts)

		if len(postedPosts) == 0 {
			msg := "💡 *Trend Insights*\n\n"
			msg += "No insights available yet. Post more content to start seeing trends!\n\n"
			msg += "_Insights are generated automatically from your post performance. Post at least 3-5 pieces of content to see meaningful patterns._"
			
			response := tgbotapi.NewMessage(chatID, msg)
			response.ParseMode = "Markdown"
			bot.Send(response)
			return nil
		}

		msg := "💡 *Trend Insights*\n\n"
		msg += "📊 *Status:* Analytics are still being collected for your recent posts.\n\n"
		msg += fmt.Sprintf("You've published %d posts in the last 30 days. Insights will appear once analytics are fully collected.\n\n", len(postedPosts))
		msg += "_Check back in 10-15 minutes after posting to see detailed insights!_"
		
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Fetch recent insights (last 30 days)
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	var insights []storage.TrendInsight
	err := h.Manager.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
		tenantID, periodStart, periodEnd, "active").
		Order("created_at DESC").
		Limit(10).
		Find(&insights).Error

	if err != nil {
		h.Manager.Logger.Error("failed to fetch insights", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Couldn't fetch insights right now. Try again in a moment."))
		return err
	}

	// If no insights exist, try to generate them
	if len(insights) == 0 {
		// Trigger insights generation
		if h.AnalyticsService != nil {
			go func() {
				if svc, ok := h.AnalyticsService.(interface {
					GenerateInsights(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*storage.TrendInsight, error)
				}); ok {
					if generated, err := svc.GenerateInsights(ctx, tenantID, periodStart, periodEnd); err == nil && len(generated) > 0 {
						h.Manager.Logger.Info("Generated insights on demand", zap.Uint("tenant_id", tenantID), zap.Int("count", len(generated)))
					}
				}
			}()
		}

		msg := "💡 *Trend Insights*\n\n"
		msg += "Generating insights from your analytics data...\n\n"
		msg += "_Check back in a moment or ask again for 'insights'!_"
		
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Group insights by category
	insightsByCategory := make(map[string][]storage.TrendInsight)
	for _, insight := range insights {
		category := insight.Category
		if category == "" {
			category = "general"
		}
		insightsByCategory[category] = append(insightsByCategory[category], insight)
	}

	// Build formatted insights message
	msg := "💡 *Trend Insights & Recommendations*\n\n"
	msg += fmt.Sprintf("📅 *Period:* Last 30 days\n")
	msg += fmt.Sprintf("📊 *Total Insights:* %d\n\n", len(insights))
	msg += "━━━━━━━━━━━━━━━━━━\n\n"

	// Show insights by category
	categoryOrder := []string{"engagement", "scheduling", "content", "platform", "audience", "general"}
	categoryNames := map[string]string{
		"engagement": "📈 Engagement Trends",
		"scheduling": "⏰ Posting Time",
		"content":    "✍️ Content Strategy",
		"platform":   "📱 Platform Performance",
		"audience":   "👥 Audience Insights",
		"general":    "💡 General Insights",
	}

	shown := 0
	for _, cat := range categoryOrder {
		catInsights := insightsByCategory[cat]
		if len(catInsights) == 0 {
			continue
		}

		catName := categoryNames[cat]
		if catName == "" {
			catName = "📌 " + strings.Title(cat)
		}

		msg += fmt.Sprintf("*%s*\n", catName)
		msg += "━━━━━━━━━━━━━━━━━━\n"

		for i, insight := range catInsights {
			if shown >= 5 { // Limit total insights shown
				break
			}

			impactEmoji := getImpactEmojiForInsights(insight.Impact)
			msg += fmt.Sprintf("%s *%s*\n", impactEmoji, insight.Title)

			if insight.Summary != "" {
				msg += fmt.Sprintf("%s\n\n", insight.Summary)
			}

			if insight.Recommendation != "" {
				msg += fmt.Sprintf("💬 *Recommendation:*\n")
				msg += fmt.Sprintf("%s\n\n", insight.Recommendation)
			}

			if insight.Confidence > 0 {
				msg += fmt.Sprintf("📊 Confidence: %.0f%%\n", insight.Confidence*100)
			}

			if i < len(catInsights)-1 && shown < 4 {
				msg += "• • •\n\n"
			}
			shown++
		}
		msg += "\n"
		if shown >= 5 {
			break
		}
	}

	if len(insights) > 5 {
		msg += fmt.Sprintf("_Showing top 5 of %d insights. More insights available in your dashboard._\n\n", len(insights))
	}

	msg += "━━━━━━━━━━━━━━━━━━\n"
	msg += "_Insights are updated automatically as you post more content._"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	
	// Handle long messages (Telegram limit is 4096 chars)
	if len(msg) > 4000 {
		// Split into multiple messages
		parts := splitMessage(msg, 3500)
		for _, part := range parts {
			resp := tgbotapi.NewMessage(chatID, part)
			resp.ParseMode = "Markdown"
			bot.Send(resp)
		}
	} else {
		bot.Send(response)
	}

	return nil
}

// Helper functions
func getImpactEmojiForInsights(impact string) string {
	switch strings.ToLower(impact) {
	case "high":
		return "🔥"
	case "medium":
		return "📊"
	case "low":
		return "💡"
	default:
		return "📌"
	}
}

func splitMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}

	parts := []string{}
	current := ""
	lines := strings.Split(msg, "\n")

	for _, line := range lines {
		if len(current)+len(line)+1 > maxLen && current != "" {
			parts = append(parts, current)
			current = line + "\n"
		} else {
			current += line + "\n"
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// sendGrowthSuggestions sends business growth suggestions with production-grade implementation
func (h *AnalyticsHandler) sendGrowthSuggestions(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Check if analytics service is available
	if h.AnalyticsService == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Analytics service not available. Please contact support."))
		return fmt.Errorf("analytics service not initialized")
	}

	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	// Try to generate growth suggestions on demand using GrowthAdvisor
	suggestions := h.generateDefaultGrowthSuggestions(ctx, tenantID, periodStart, periodEnd)

	if len(suggestions) == 0 {
		msg := "🚀 *Growth Suggestions*\n\n"
		msg += "No suggestions available yet. Keep posting to get personalized growth recommendations!\n\n"
		msg += "_We analyze your performance and suggest improvements based on your business profile and post analytics._"
		
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Build formatted suggestions message
	msg := "🚀 *Growth Opportunities*\n\n"
	msg += fmt.Sprintf("📅 *Based on:* Last 30 days of analytics\n\n")
	msg += "━━━━━━━━━━━━━━━━━━\n\n"

	for i, suggestionMap := range suggestions {
		if i >= 5 {
			break
		}

		// Extract fields from map
		title := getStringFromMap(suggestionMap, "Title")
		description := getStringFromMap(suggestionMap, "Description")
		impact := getStringFromMap(suggestionMap, "Impact")
		effort := getStringFromMap(suggestionMap, "Effort")
		confidence := getFloatFromMap(suggestionMap, "Confidence")
		expectedOutcome := getStringFromMap(suggestionMap, "ExpectedOutcome")
		actionItems := getStringSliceFromMap(suggestionMap, "ActionItems")

		impactEmoji := getImpactEmojiForInsights(impact)
		effortEmoji := getEffortEmoji(effort)

		msg += fmt.Sprintf("%s *%s*\n", impactEmoji, title)
		
		if description != "" {
			msg += fmt.Sprintf("%s\n\n", description)
		}

		// Action items
		if len(actionItems) > 0 {
			msg += "📋 *Action Items:*\n"
			for j, item := range actionItems {
				if j >= 5 {
					break
				}
				msg += fmt.Sprintf("  • %s\n", item)
			}
			msg += "\n"
		}

		// Expected outcome
		if expectedOutcome != "" {
			msg += fmt.Sprintf("📈 *Expected Result:*\n%s\n\n", expectedOutcome)
		}

		// Impact and effort
		if impact != "" && effort != "" {
			msg += fmt.Sprintf("%s Impact: %s | %s Effort: %s\n", impactEmoji, strings.Title(impact), effortEmoji, strings.Title(effort))
		}
		
		if confidence > 0 {
			msg += fmt.Sprintf("📊 Confidence: %.0f%%\n", confidence*100)
		}

		if i < len(suggestions)-1 {
			msg += "\n━━━━━━━━━━━━━━━━━━\n\n"
		}
	}

	msg += "\n━━━━━━━━━━━━━━━━━━\n"
	msg += "_Implement these suggestions gradually and track your progress. Ask for 'insights' to see how strategies are working._"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	
	// Handle long messages
	if len(msg) > 4000 {
		parts := splitMessage(msg, 3500)
		for _, part := range parts {
			resp := tgbotapi.NewMessage(chatID, part)
			resp.ParseMode = "Markdown"
			bot.Send(resp)
		}
	} else {
		bot.Send(response)
	}

	return nil
}

// generateDefaultGrowthSuggestions creates growth suggestions based on analytics data
func (h *AnalyticsHandler) generateDefaultGrowthSuggestions(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) []map[string]interface{} {
	suggestions := []map[string]interface{}{}

	// Get analytics summary
	var analytics []storage.PostAnalytics
	if err := h.Manager.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).Find(&analytics).Error; err != nil {
		return suggestions
	}

	// Analyze posting frequency
	var postedPosts []storage.ScheduledPost
	h.Manager.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, "posted", periodStart, periodEnd).
		Find(&postedPosts)

	if len(postedPosts) < 5 {
		suggestions = append(suggestions, map[string]interface{}{
			"Title":          "Increase Posting Frequency",
			"Description":    fmt.Sprintf("You've posted %d times in the last 30 days. Consistent posting improves engagement and reach.", len(postedPosts)),
			"Impact":         "high",
			"Effort":         "medium",
			"Confidence":     0.85,
			"ActionItems":    []string{
				"Aim for 3-5 posts per week for steady growth",
				"Use scheduling to maintain consistency",
				"Create a content calendar to plan ahead",
			},
			"ExpectedOutcome": "20-30% increase in reach and follower growth within 60 days",
		})
	}

	if len(analytics) == 0 {
		return suggestions
	}

	// Analyze engagement rate
	avgEngagement := 0.0
	for _, a := range analytics {
		avgEngagement += a.EngagementRate
	}
	avgEngagement = avgEngagement / float64(len(analytics))

	if avgEngagement < 3.0 && len(analytics) >= 5 {
		suggestions = append(suggestions, map[string]interface{}{
			"Title":          "Improve Engagement Rate",
			"Description":    fmt.Sprintf("Your current engagement rate is %.2f%%. Let's boost it with strategic improvements.", avgEngagement),
			"Impact":         "high",
			"Effort":         "medium",
			"Confidence":     0.80,
			"ActionItems":    []string{
				"Post at optimal times (use insights to find best posting times)",
				"Increase engagement with questions in captions",
				"Respond to comments within the first hour",
				"Use relevant hashtags (mix of popular and niche)",
			},
			"ExpectedOutcome": "Double your engagement rate within 90 days",
		})
	}

	// Platform expansion suggestion
	platforms := make(map[string]int)
	for _, a := range analytics {
		platforms[a.Platform]++
	}

	if len(platforms) == 1 && platforms["instagram"] > 0 {
		suggestions = append(suggestions, map[string]interface{}{
			"Title":          "Expand to Multiple Platforms",
			"Description":    "You're currently only posting to Instagram. Expanding to Facebook or TikTok can help you reach new audiences.",
			"Impact":         "high",
			"Effort":         "medium",
			"Confidence":     0.75,
			"ActionItems":    []string{
				"Connect your Facebook Page to cross-post content",
				"Create a TikTok account for short-form video content",
				"Repurpose your best Instagram content for other platforms",
			},
			"ExpectedOutcome": "20-40% increase in total reach within 90 days",
		})
	}

	return suggestions
}

// Helper functions for extracting from map
func getStringFromMap(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0.0
}

func getStringSliceFromMap(m map[string]interface{}, key string) []string {
	if val, ok := m[key]; ok {
		if slice, ok := val.([]string); ok {
			return slice
		}
		if slice, ok := val.([]interface{}); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			return result
		}
	}
	return []string{}
}

func getEffortEmoji(effort string) string {
	switch strings.ToLower(effort) {
	case "low":
		return "⚡"
	case "medium":
		return "📊"
	case "high":
		return "🔨"
	default:
		return "📌"
	}
}

// sendAnalyticsSummary sends a comprehensive analytics summary
func (h *AnalyticsHandler) sendAnalyticsSummary(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	// Fetch analytics summary
	if h.AnalyticsService == nil {
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	// For now, fetch data directly from database
	return h.sendAnalyticsFromDB(ctx, bot, tenantID, chatID)
}

// sendAnalyticsFromDB fetches analytics directly from database
func (h *AnalyticsHandler) sendAnalyticsFromDB(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	// First, check if there are any posted posts (from ScheduledPost table)
	var postedPosts []storage.ScheduledPost
	h.Manager.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, "posted", periodStart, periodEnd).
		Order("posted_at DESC").
		Find(&postedPosts)

	// Fetch analytics (may be empty if not collected yet)
	var analytics []storage.PostAnalytics
	err := h.Manager.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).
		Order("posted_at DESC").
		Find(&analytics).Error

	if err != nil {
		h.Manager.Logger.Error("failed to fetch analytics", zap.Error(err))
		// Continue even if analytics fetch fails - we'll show posts anyway
	}

	// Build summary message
	msg := "📊 *Analytics Summary*\n\n"
	msg += "📅 *Period:* Last 30 days\n\n"

	// If we have posted posts but no analytics yet, trigger collection and show status
	if len(postedPosts) > 0 && len(analytics) == 0 {
		// Trigger analytics collection for existing posts in background
		if h.AnalyticsService != nil {
			go func() {
				h.Manager.Logger.Info("Triggering analytics collection for existing posts",
					zap.Uint("tenant_id", tenantID),
					zap.Int("post_count", len(postedPosts)))
				// Collect analytics for posts from last 7 days
				if svc, ok := h.AnalyticsService.(interface {
					CollectMetricsForTenant(ctx context.Context, tenantID uint, daysBack int) error
				}); ok {
					ctx := context.Background()
					if err := svc.CollectMetricsForTenant(ctx, tenantID, 7); err != nil {
						h.Manager.Logger.Warn("failed to trigger analytics collection",
							zap.Uint("tenant_id", tenantID),
							zap.Error(err))
					}
				}
			}()
		}
		
		msg += fmt.Sprintf("📈 *Posts Published:* %d\n\n", len(postedPosts))
		msg += "📊 *Analytics Status:*\n"
		msg += "Analytics are being collected. Check back in a few minutes for detailed insights!\n\n"
		msg += "_Posts published:\n"
		for i, post := range postedPosts {
			if i >= 5 { // Limit to 5 most recent
				break
			}
			var platforms []string
			if post.Platforms != "" {
				json.Unmarshal([]byte(post.Platforms), &platforms)
			}
			platformStr := "All platforms"
			if len(platforms) > 0 {
				platformStr = strings.Join(platforms, ", ")
			}
			postedTime := "Unknown"
			if post.PostedAt != nil {
				postedTime = post.PostedAt.Format("Jan 2, 3:04PM")
			}
			msg += fmt.Sprintf("• %s (%s) - %s\n", post.Name, platformStr, postedTime)
		}
		msg += "\n"
		msg += "💡 _Analytics collection happens automatically after posting. Instagram insights may take 5-10 minutes to appear._"
		msg += "\n\n"
	} else if len(analytics) > 0 {
		totalReach := int64(0)
		totalLikes := int64(0)
		totalComments := int64(0)
		totalShares := int64(0)
		avgEngagement := 0.0

		platformStats := make(map[string]struct {
			count      int
			reach      int64
			likes      int64
			comments   int64
			engagement float64
		})

		for _, a := range analytics {
			totalReach += a.Reach
			totalLikes += a.Likes
			totalComments += a.Comments
			totalShares += a.Shares
			avgEngagement += a.EngagementRate

			stats := platformStats[a.Platform]
			stats.count++
			stats.reach += a.Reach
			stats.likes += a.Likes
			stats.comments += a.Comments
			stats.engagement += a.EngagementRate
			platformStats[a.Platform] = stats
		}

		if len(analytics) > 0 {
			avgEngagement = avgEngagement / float64(len(analytics))
		}

		msg += "📈 *Performance*\n"
		msg += fmt.Sprintf("• Posts: %d\n", len(analytics))
		msg += fmt.Sprintf("• Total Reach: %d\n", totalReach)
		msg += fmt.Sprintf("• Total Likes: %d\n", totalLikes)
		msg += fmt.Sprintf("• Total Comments: %d\n", totalComments)
		msg += fmt.Sprintf("• Avg Engagement: %.1f%%\n\n", avgEngagement)

		// Platform breakdown
		if len(platformStats) > 0 {
			msg += "📱 *By Platform*\n"
			for platform, stats := range platformStats {
				avgRate := 0.0
				if stats.count > 0 {
					avgRate = stats.engagement / float64(stats.count)
				}
				msg += fmt.Sprintf("• %s: %.1f%% engagement (%d posts)\n",
					strings.Title(platform), avgRate, stats.count)
			}
			msg += "\n"
		}

		// Fetch insights
		var insights []storage.TrendInsight
		h.Manager.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
			tenantID, periodStart, periodEnd, "active").
			Order("created_at DESC").
			Limit(2).
			Find(&insights)

		if len(insights) > 0 {
			msg += "💡 *Key Insights*\n"
			for _, insight := range insights {
				msg += fmt.Sprintf("• %s\n", insight.Summary)
			}
			msg += "\n"
		}
	} else {
		// No posts at all
		msg += "No posts in the last 30 days. Start posting to see analytics!\n\n"
	}

	msg += "_Ask for 'weekly summary', 'insights', or 'growth suggestions' for more details!_"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)

	return nil
}

// sendPlatformBreakdown sends platform-specific performance breakdown
func (h *AnalyticsHandler) sendPlatformBreakdown(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	// Fetch analytics by platform
	var analytics []storage.PostAnalytics
	err := h.Manager.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).
		Find(&analytics).Error

	if err != nil {
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	msg := "📱 *Platform Performance*\n\n"

	if len(analytics) == 0 {
		msg += "No platform data available yet. Connect your accounts and start posting!\n"
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Group by platform
	platformStats := make(map[string]struct {
		count      int
		reach      int64
		likes      int64
		comments   int64
		engagement float64
	})

	for _, a := range analytics {
		stats := platformStats[a.Platform]
		stats.count++
		stats.reach += a.Reach
		stats.likes += a.Likes
		stats.comments += a.Comments
		stats.engagement += a.EngagementRate
		platformStats[a.Platform] = stats
	}

	for platform, stats := range platformStats {
		avgRate := 0.0
		if stats.count > 0 {
			avgRate = stats.engagement / float64(stats.count)
		}
		msg += fmt.Sprintf("*%s*\n", strings.Title(platform))
		msg += fmt.Sprintf("• Posts: %d\n", stats.count)
		msg += fmt.Sprintf("• Reach: %d\n", stats.reach)
		msg += fmt.Sprintf("• Likes: %d\n", stats.likes)
		msg += fmt.Sprintf("• Engagement: %.1f%%\n\n", avgRate)
	}

	msg += "_Connect more platforms to see comparisons!_"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)

	return nil
}

// sendBasicStats sends basic stats as fallback
func (h *AnalyticsHandler) sendBasicStats(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	// Count posts
	var postCount int64
	h.Manager.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status = ?", tenantID, "posted").
		Count(&postCount)

	var scheduledCount int64
	h.Manager.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status = ?", tenantID, "scheduled").
		Count(&scheduledCount)

	msg := "📊 *Quick Stats*\n\n"
	msg += fmt.Sprintf("• Posted: %d\n", postCount)
	msg += fmt.Sprintf("• Scheduled: %d\n\n", scheduledCount)
	msg += "_Full analytics coming soon! Post more content to see detailed insights._"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)

	return nil
}

// SendQuickSummary sends a lightweight analytics snapshot for the control center menu.
func (h *AnalyticsHandler) SendQuickSummary(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	if h.AnalyticsService == nil {
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	return h.sendAnalyticsFromDB(ctx, bot, tenantID, chatID)
}
