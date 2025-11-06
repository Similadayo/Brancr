package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ContextAwareResponseGenerator creates context-aware responses using conversation history
type ContextAwareResponseGenerator struct {
	Client *AIClient
	DB     *gorm.DB
	Memory *MemoryService
}

// NewContextAwareGenerator creates a new context-aware generator
func NewContextAwareGenerator(client *AIClient, db *gorm.DB, memory *MemoryService) *ContextAwareResponseGenerator {
	return &ContextAwareResponseGenerator{
		Client: client,
		DB:     db,
		Memory: memory,
	}
}

// GenerateWithFullContext generates a response with full business and conversation context
func (g *ContextAwareResponseGenerator) GenerateWithFullContext(
	ctx context.Context,
	tenantID uint,
	userMsg string,
	isOwner bool,
	customerID uint,
) (string, error) {
	// Load all context
	persona := GetPersona(g.DB, tenantID)
	var biz storage.BusinessProfile
	_ = g.DB.First(&biz, "tenant_id = ?", tenantID).Error

	// Get conversation history
	var history []Message
	if isOwner {
		history = g.Memory.GetRecentTenantConversationHistory(ctx, tenantID, 15)
	} else {
		history = g.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30)
	}

	// Get business stats for context
	stats := g.getBusinessStats(ctx, tenantID)

	// Build rich context prompt
	systemPrompt := g.buildContextPrompt(persona, biz, stats, isOwner, len(history))

	// Build messages
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add conversation history (last 10 exchanges for context)
	maxHistory := 10
	if len(history) > maxHistory*2 {
		history = history[len(history)-maxHistory*2:]
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMsg})

	// Generate response with retry on moderation error
	reply, err := g.Client.Generate(ctx, messages)
	if err != nil {
		// If moderation error, try with sanitized prompt
		if IsModerationError(err) {
			// Retry with a simpler, safer prompt
			safePrompt := GenerateSafePrompt(persona, biz, isOwner)
			messages[0].Content = SanitizePrompt(safePrompt)
			
			reply, err = g.Client.Generate(ctx, messages)
			if err != nil {
				// If still fails, use fallback
				return GenerateFallbackResponse(ctx, tenantID, userMsg, isOwner), nil
			}
		} else {
			return "", fmt.Errorf("AI generation failed: %w", err)
		}
	}

	reply = strings.TrimSpace(reply)

	// Save to memory
	if isOwner {
		g.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	} else {
		g.Memory.SaveCustomerConversation(ctx, tenantID, customerID, userMsg, reply)
	}

	return reply, nil
}

// buildContextPrompt creates a rich context-aware prompt
func (g *ContextAwareResponseGenerator) buildContextPrompt(
	persona storage.Persona,
	biz storage.BusinessProfile,
	stats BusinessStats,
	isOwner bool,
	historyCount int,
) string {
	var prompt strings.Builder

	if isOwner {
		prompt.WriteString(fmt.Sprintf(`You're %s, helping %s manage their social media.

About the business: %s (they're in %s). Your tone should be %s.

You've been chatting with them - you have %d recent conversations to remember.

What's happening:
- They have %d posts scheduled
- %d posts went out recently
- %d posts failed (you can help with these)

What you can help with:
- Scheduling posts (they might say "schedule this for tomorrow" or "post to Instagram")
- Connecting accounts ("connect Instagram" or "link my Facebook")
- Checking stats ("show me my stats" or "how am I doing")
- Managing content ("what posts are scheduled?" or "what's coming up?")

Talk like a real person helping a friend. Use casual language, contractions, and be genuinely helpful. Don't sound like a robot or customer service rep. Just be helpful and friendly.`,
			persona.BotName, biz.Name,
			biz.Name, biz.Niche, persona.Tone,
			historyCount,
			stats.ScheduledPosts, stats.RecentPosts, stats.FailedPosts))
	} else {
		prompt.WriteString(fmt.Sprintf(`You're %s from %s.

About the business: %s (they do %s). Your tone should be %s.

You're chatting with a customer - you've talked %d times before, so remember what you've discussed.

Talk like a real person who works at this business. Be genuine, friendly, and helpful. Use natural language, contractions, and sound human. Don't be overly formal or robotic. Just be yourself - helpful and personable.`,
			persona.BotName, biz.Name,
			biz.Name, biz.Niche, persona.Tone,
			historyCount))
	}

	return prompt.String()
}

// BusinessStats represents current business statistics
type BusinessStats struct {
	ScheduledPosts int
	RecentPosts    int
	FailedPosts    int
	ConnectedAccounts int
}

// getBusinessStats retrieves current business statistics
func (g *ContextAwareResponseGenerator) getBusinessStats(ctx context.Context, tenantID uint) BusinessStats {
	stats := BusinessStats{}

	// Count scheduled posts
	var scheduledCount int64
	g.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []string{"scheduled", "posting"}).
		Count(&scheduledCount)
	stats.ScheduledPosts = int(scheduledCount)

	// Count recent posts (last 7 days)
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	var recentCount int64
	g.DB.Model(&storage.PostLog{}).
		Where("tenant_id = ? AND created_at > ?", tenantID, sevenDaysAgo).
		Count(&recentCount)
	stats.RecentPosts = int(recentCount)

	// Count failed posts (last 7 days)
	var failedCount int64
	g.DB.Model(&storage.PostLog{}).
		Where("tenant_id = ? AND created_at > ? AND status = ?", tenantID, sevenDaysAgo, "failed").
		Count(&failedCount)
	stats.FailedPosts = int(failedCount)

	// Count connected accounts
	var accountsCount int64
	g.DB.Model(&storage.SocialAccount{}).
		Where("tenant_id = ? AND connected = ?", tenantID, true).
		Count(&accountsCount)
	stats.ConnectedAccounts = int(accountsCount)

	return stats
}

