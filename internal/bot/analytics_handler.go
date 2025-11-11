package bot

import (
	"context"
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
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	return h.TelegramReporter.GenerateWeeklySummary(ctx, tenantID, bot, chatID)
}

// sendTrendInsights sends AI-generated trend insights
func (h *AnalyticsHandler) sendTrendInsights(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	// Fetch recent insights
	var insights []storage.TrendInsight
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	err := h.Manager.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
		tenantID, periodStart, periodEnd, "active").
		Order("created_at DESC").
		Limit(5).
		Find(&insights).Error

	if err != nil {
		h.Manager.Logger.Error("failed to fetch insights", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Couldn't fetch insights right now. Try again in a moment."))
		return err
	}

	if len(insights) == 0 {
		msg := "📊 *Trend Insights*\n\nNo insights available yet. Post more content to start seeing trends!\n\n_Insights are generated automatically as you post._"
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Build insights message
	msg := "💡 *Trend Insights*\n\n"
	for i, insight := range insights {
		if i >= 5 {
			break
		}
		msg += fmt.Sprintf("*%s*\n", insight.Title)
		msg += fmt.Sprintf("%s\n\n", insight.Summary)
		msg += fmt.Sprintf("💬 *Tip:* %s\n\n", insight.Recommendation)
		msg += "---\n\n"
	}

	msg += "_View more insights anytime by asking 'show me insights' or 'trends'_"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)

	return nil
}

// sendGrowthSuggestions sends business growth suggestions
func (h *AnalyticsHandler) sendGrowthSuggestions(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uint, chatID int64) error {
	// Check if analytics service is available
	if h.AnalyticsService == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Analytics service not available. Please contact support."))
		return fmt.Errorf("analytics service not initialized")
	}

	// Check if analytics service is available
	if h.AnalyticsService == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Analytics service not available."))
		return fmt.Errorf("analytics service not available")
	}

	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)

	// Try to get growth suggestions from the service
	// Since GenerateGrowthSuggestions doesn't exist on Service, we'll fetch from DB directly
	var suggestions []*analytics.BusinessGrowthSuggestion
	err := h.Manager.DB.Where("tenant_id = ? AND created_at >= ?", tenantID, periodStart).
		Order("created_at DESC").
		Limit(5).
		Find(&suggestions).Error

	if err != nil {
		h.Manager.Logger.Error("failed to fetch growth suggestions", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Couldn't generate suggestions right now. Try again in a moment."))
		return err
	}

	suggestionsInterface := make([]interface{}, len(suggestions))
	for i, s := range suggestions {
		suggestionsInterface[i] = s
	}

	if len(suggestionsInterface) == 0 {
		msg := "🚀 *Growth Suggestions*\n\nNo suggestions available yet. Keep posting to get personalized growth recommendations!\n\n_We analyze your performance and suggest improvements based on your business profile._"
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	// Build suggestions message
	msg := "🚀 *Growth Opportunities*\n\n"
	for i, suggestionInterface := range suggestionsInterface {
		if i >= 3 {
			break
		}

		// Type assert to BusinessGrowthSuggestion
		suggestion, ok := suggestionInterface.(*analytics.BusinessGrowthSuggestion)
		if !ok {
			// Try to extract fields if type assertion fails
			continue
		}

		// Impact emoji
		impactEmoji := "📈"
		if suggestion.Impact == "high" {
			impactEmoji = "🔥"
		} else if suggestion.Impact == "medium" {
			impactEmoji = "📊"
		}

		msg += fmt.Sprintf("%s *%s*\n", impactEmoji, suggestion.Title)
		msg += fmt.Sprintf("%s\n\n", suggestion.Description)

		// Action items
		if len(suggestion.ActionItems) > 0 {
			msg += "*Action Items:*\n"
			for j, item := range suggestion.ActionItems {
				if j >= 3 {
					break
				}
				msg += fmt.Sprintf("• %s\n", item)
			}
			msg += "\n"
		}

		// Expected outcome
		if suggestion.ExpectedOutcome != "" {
			msg += fmt.Sprintf("📈 *Expected:* %s\n\n", suggestion.ExpectedOutcome)
		}

		msg += "---\n\n"
	}

	msg += "_Get more suggestions anytime by asking 'growth ideas' or 'how can I grow'_"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)

	return nil
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

	// Fetch analytics
	var analytics []storage.PostAnalytics
	err := h.Manager.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).
		Order("posted_at DESC").
		Find(&analytics).Error

	if err != nil {
		h.Manager.Logger.Error("failed to fetch analytics", zap.Error(err))
		return h.sendBasicStats(ctx, bot, tenantID, chatID)
	}

	// Build summary message
	msg := "📊 *Analytics Summary*\n\n"
	msg += "📅 *Period:* Last 30 days\n\n"

	if len(analytics) > 0 {
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
