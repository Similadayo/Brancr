package analytics

import (
	"context"
	"fmt"
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
	// Calculate week period
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7)

	// Get analytics summary
	summary, err := r.Service.GetTenantAnalytics(ctx, tenantID, startDate, endDate)
	if err != nil {
		return fmt.Errorf("failed to get analytics: %w", err)
	}

	// Generate insights if not already generated
	insights, err := r.Service.GenerateInsights(ctx, tenantID, startDate, endDate)
	if err != nil {
		r.Logger.Warn("Failed to generate insights", zap.Error(err))
		insights = []*storage.TrendInsight{}
	}

	// Build conversational message
	message := r.buildSummaryMessage(summary, insights)

	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// buildSummaryMessage builds a conversational analytics summary
func (r *TelegramReporter) buildSummaryMessage(summary *interfaces.TenantAnalyticsSummary, insights []*storage.TrendInsight) string {
	msg := "📊 *Weekly Analytics Summary*\n\n"

	// Overall performance
	if summary.TotalPosts > 0 {
		msg += fmt.Sprintf("📈 *Performance*\n")
		msg += fmt.Sprintf("• Posts: %d\n", summary.TotalPosts)
		msg += fmt.Sprintf("• Total Reach: %d\n", summary.TotalReach)
		msg += fmt.Sprintf("• Total Likes: %d\n", summary.TotalLikes)
		msg += fmt.Sprintf("• Avg Engagement: %.1f%%\n\n", summary.AvgEngagementRate)
	} else {
		msg += "No posts this week. Start posting to see analytics! 📝\n\n"
		return msg
	}

	// Engagement change
	if len(insights) > 0 {
		for _, insight := range insights {
			if insight.Type == "engagement_trend" {
				msg += fmt.Sprintf("💡 *Trend:* %s\n", insight.Summary)
				msg += fmt.Sprintf("💬 *Tip:* %s\n\n", insight.Recommendation)
				break
			}
		}
	}

	// Platform breakdown
	if len(summary.PlatformBreakdown) > 0 {
		msg += "📱 *By Platform*\n"
		for platform, stats := range summary.PlatformBreakdown {
			msg += fmt.Sprintf("• %s: %.1f%% engagement (%d posts)\n",
				platform, stats.AvgEngagementRate, stats.TotalPosts)
		}
		msg += "\n"
	}

	// Top insights
	if len(insights) > 0 {
		msg += "🎯 *Key Insights*\n"
		count := 0
		for _, insight := range insights {
			if count >= 3 {
				break
			}
			if insight.Type != "engagement_trend" {
				msg += fmt.Sprintf("• %s\n", insight.Summary)
				count++
			}
		}
		msg += "\n"
	}

	// Best performing post
	if len(summary.TopPosts) > 0 {
		best := summary.TopPosts[0]
		msg += fmt.Sprintf("⭐ *Top Post*\n")
		msg += fmt.Sprintf("Platform: %s | Engagement: %.1f%%\n", best.Platform, best.EngagementRate)
	}

	msg += "\n_View detailed analytics in your dashboard_ 📊"

	return msg
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

