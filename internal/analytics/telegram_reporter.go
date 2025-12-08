package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/interfaces"
	"go.uber.org/zap"
	"gorm.io/gorm"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramReporter generates conversational analytics summaries for Telegram
type TelegramReporter struct {
	DB      *gorm.DB
	Logger  *zap.Logger
	Service *Service
}

// NewTelegramReporter creates a new Telegram reporter
func NewTelegramReporter(db *gorm.DB, logger *zap.Logger, service *Service) *TelegramReporter {
	return &TelegramReporter{
		DB:      db,
		Logger:  logger,
		Service: service,
	}
}

// GenerateWeeklySummary generates a conversational weekly analytics summary
func (r *TelegramReporter) GenerateWeeklySummary(ctx context.Context, tenantID uint, bot *tgbotapi.BotAPI, chatID int64) error {
	// Send typing indicator
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Calculate week period (last 7 days)
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7)

	r.Logger.Info("Generating weekly summary",
		zap.Uint("tenant_id", tenantID),
		zap.Time("start_date", startDate),
		zap.Time("end_date", endDate),
		zap.Bool("has_service", r.Service != nil))

	// Check if there are any posted posts first
	var postedPosts []storage.ScheduledPost
	if err := r.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, "posted", startDate, endDate).
		Order("posted_at DESC").
		Find(&postedPosts).Error; err != nil {
		r.Logger.Error("failed to fetch posted posts",
			zap.Error(err),
			zap.Uint("tenant_id", tenantID))
		// Continue - we'll try analytics anyway
	} else {
		r.Logger.Info("Found posted posts",
			zap.Uint("tenant_id", tenantID),
			zap.Int("count", len(postedPosts)))
	}

	// Get analytics summary
	if r.Service == nil {
		r.Logger.Error("analytics service is nil",
			zap.Uint("tenant_id", tenantID))
		return r.buildWeeklySummaryFromPosts(bot, chatID, tenantID, postedPosts, startDate, endDate)
	}

	summary, err := r.Service.GetTenantAnalytics(ctx, tenantID, startDate, endDate)
	if err != nil {
		r.Logger.Error("failed to get analytics summary",
			zap.Error(err),
			zap.Uint("tenant_id", tenantID),
			zap.String("error_type", fmt.Sprintf("%T", err)))
		// Fallback: build message from posts if available
		return r.buildWeeklySummaryFromPosts(bot, chatID, tenantID, postedPosts, startDate, endDate)
	}

	r.Logger.Info("Got analytics summary",
		zap.Uint("tenant_id", tenantID),
		zap.Int("total_posts", summary.TotalPosts),
		zap.Int64("total_reach", summary.TotalReach))

	// If no analytics but posts exist, trigger collection
	if summary.TotalPosts == 0 && len(postedPosts) > 0 {
		// Trigger analytics collection in background
		go func() {
			r.Logger.Info("Triggering analytics collection for weekly summary",
				zap.Uint("tenant_id", tenantID))
			if err := r.Service.CollectMetricsForTenant(ctx, tenantID, 7); err != nil {
				r.Logger.Warn("failed to collect analytics", zap.Error(err))
			}
		}()
		return r.buildWeeklySummaryFromPosts(bot, chatID, tenantID, postedPosts, startDate, endDate)
	}

	// Generate insights if not already generated (only if we have analytics)
	if summary.TotalPosts > 0 {
		insights, err := r.Service.GenerateInsights(ctx, tenantID, startDate, endDate)
		if err != nil {
			r.Logger.Warn("Failed to generate insights", zap.Error(err))
			insights = []*storage.TrendInsight{}
		}
		summary.TrendInsights = insights
	}

	// Build conversational message
	message := r.buildSummaryMessage(summary, summary.TrendInsights)

	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// buildWeeklySummaryFromPosts builds a summary from posted posts when analytics aren't available yet
func (r *TelegramReporter) buildWeeklySummaryFromPosts(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, posts []storage.ScheduledPost, startDate, endDate time.Time) error {
	if len(posts) == 0 {
		msg := "📊 *Weekly Analytics Summary*\n\n"
		msg += "📅 *Period:* Last 7 days\n\n"
		msg += "No posts this week. Start posting to see analytics! 📝\n\n"
		msg += "_Post content through your Telegram bot to start tracking performance._"
		
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return nil
	}

	msg := "📊 *Weekly Analytics Summary*\n\n"
	msg += "📅 *Period:* Last 7 days\n\n"
	msg += fmt.Sprintf("📈 *Posts Published:* %d\n\n", len(posts))

	// Group by platform
	platformCount := make(map[string]int)
	for _, post := range posts {
		var platforms []string
		json.Unmarshal([]byte(post.Platforms), &platforms)
		for _, p := range platforms {
			platformCount[p]++
		}
	}

	if len(platformCount) > 0 {
		msg += "📱 *Published On:*\n"
		for platform, count := range platformCount {
			msg += fmt.Sprintf("• %s: %d posts\n", strings.Title(platform), count)
		}
		msg += "\n"
	}

	msg += "📊 *Analytics Status:*\n"
	msg += "Analytics are being collected. Check back in 10-15 minutes for detailed insights!\n\n"
	msg += "_Instagram insights typically appear 5-10 minutes after posting. Full analytics will include reach, impressions, engagement, and more._"

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)
	return nil
}

// buildSummaryMessage builds a conversational analytics summary with production-grade formatting
func (r *TelegramReporter) buildSummaryMessage(summary *interfaces.TenantAnalyticsSummary, insights []*storage.TrendInsight) string {
	msg := "📊 *Weekly Analytics Summary*\n\n"
	msg += "📅 *Period:* Last 7 days\n\n"

	// Overall performance
	if summary == nil || summary.TotalPosts == 0 {
		msg += "No posts this week. Start posting to see analytics! 📝\n\n"
		msg += "_Post content through your Telegram bot to start tracking performance._"
		return msg
	}

	msg += "📈 *Performance Overview*\n"
	msg += fmt.Sprintf("• Posts Published: %d\n", summary.TotalPosts)
	msg += fmt.Sprintf("• Total Reach: %s\n", formatNumber(summary.TotalReach))
	msg += fmt.Sprintf("• Total Impressions: %s\n", formatNumber(summary.TotalImpressions))
	msg += fmt.Sprintf("• Total Likes: %s\n", formatNumber(summary.TotalLikes))
	msg += fmt.Sprintf("• Total Comments: %s\n", formatNumber(int64(summary.TotalComments)))
	msg += fmt.Sprintf("• Total Shares: %s\n", formatNumber(summary.TotalShares))
	msg += fmt.Sprintf("• Avg Engagement Rate: %.2f%%\n\n", summary.AvgEngagementRate)

	// Week-over-week comparison (if we have previous week data)
	if comparison := r.compareWithPreviousWeek(summary.TenantID, summary.PeriodStart); comparison != "" {
		msg += comparison + "\n"
	}

	// Platform breakdown with better formatting
	if len(summary.PlatformBreakdown) > 0 {
		msg += "📱 *Platform Performance*\n"
		for platform, stats := range summary.PlatformBreakdown {
			emoji := getPlatformEmoji(platform)
			msg += fmt.Sprintf("%s *%s:*\n", emoji, strings.Title(platform))
			msg += fmt.Sprintf("  • Posts: %d\n", stats.TotalPosts)
			msg += fmt.Sprintf("  • Reach: %s\n", formatNumber(stats.TotalReach))
			msg += fmt.Sprintf("  • Engagement: %.2f%%\n", stats.AvgEngagementRate)
			if stats.BestPostEngagement > 0 {
				msg += fmt.Sprintf("  • Best Post: %s engagements\n", formatNumber(stats.BestPostEngagement))
			}
			msg += "\n"
		}
	}

	// Top performing posts
	if len(summary.TopPosts) > 0 {
		msg += "⭐ *Top Performing Posts*\n"
		count := 0
		for _, post := range summary.TopPosts {
			if count >= 3 {
				break
			}
			platformEmoji := getPlatformEmoji(post.Platform)
			msg += fmt.Sprintf("%d. %s %s\n", count+1, platformEmoji, strings.Title(post.Platform))
			msg += fmt.Sprintf("   • Engagement: %.2f%%\n", post.EngagementRate)
			msg += fmt.Sprintf("   • Reach: %s | Likes: %s\n", formatNumber(post.Reach), formatNumber(post.Likes))
			count++
		}
		msg += "\n"
	}

	// Key insights
	if len(insights) > 0 {
		msg += "💡 *Key Insights*\n"
		count := 0
		for _, insight := range insights {
			if count >= 3 {
				break
			}
			impactEmoji := getImpactEmoji(insight.Impact)
			msg += fmt.Sprintf("%s *%s*\n", impactEmoji, insight.Title)
			msg += fmt.Sprintf("   %s\n", insight.Summary)
			if insight.Recommendation != "" {
				msg += fmt.Sprintf("   💬 *Tip:* %s\n", insight.Recommendation)
			}
			msg += "\n"
			count++
		}
	}

	// Engagement trend
	engagementInsight := r.findInsightByType(insights, "engagement_trend")
	if engagementInsight != nil {
		msg += "📈 *Engagement Trend*\n"
		msg += fmt.Sprintf("%s\n", engagementInsight.Summary)
		if engagementInsight.Recommendation != "" {
			msg += fmt.Sprintf("💬 *Recommendation:* %s\n\n", engagementInsight.Recommendation)
		}
	}

	msg += "━━━━━━━━━━━━━━━━━━\n"
	msg += "_Ask for 'insights' or 'growth suggestions' for more detailed analysis._"

	return msg
}

// Helper functions for formatting
func formatNumber(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000.0)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

func getPlatformEmoji(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return "📷"
	case "facebook":
		return "👥"
	case "tiktok":
		return "🎵"
	case "twitter", "x":
		return "🐦"
	case "youtube":
		return "📺"
	default:
		return "📱"
	}
}

func getImpactEmoji(impact string) string {
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

func (r *TelegramReporter) compareWithPreviousWeek(tenantID uint, currentStart time.Time) string {
	prevStart := currentStart.AddDate(0, 0, -7)
	prevEnd := currentStart

	var prevAnalytics []storage.PostAnalytics
	if err := r.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at < ?",
		tenantID, prevStart, prevEnd).Find(&prevAnalytics).Error; err != nil {
		return ""
	}

	if len(prevAnalytics) == 0 {
		return ""
	}

	prevTotalReach := int64(0)
	prevAvgEngagement := 0.0
	for _, a := range prevAnalytics {
		prevTotalReach += a.Reach
		prevAvgEngagement += a.EngagementRate
	}
	if len(prevAnalytics) > 0 {
		prevAvgEngagement = prevAvgEngagement / float64(len(prevAnalytics))
	}

	// This will be populated when we have current week data
	// For now, return empty - can be enhanced with actual comparison
	return ""
}

func (r *TelegramReporter) findInsightByType(insights []*storage.TrendInsight, insightType string) *storage.TrendInsight {
	for _, insight := range insights {
		if insight.Type == insightType {
			return insight
		}
	}
	return nil
}

// SendInsightAlert sends a specific insight as an alert
func (r *TelegramReporter) SendInsightAlert(ctx context.Context, tenantID uint, insight *storage.TrendInsight, bot *tgbotapi.BotAPI, chatID int64) error {
	// Get tenant to find owner chat ID
	var tenant storage.Tenant
	if err := r.DB.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	message := fmt.Sprintf("💡 *New Insight: %s*\n\n%s\n\n💬 *Recommendation:* %s",
		insight.Title, insight.Summary, insight.Recommendation)

	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send insight: %w", err)
	}

	return nil
}

