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
	contextPrompt := g.buildContextPrompt(persona, biz, stats, isOwner, len(history))

	// Build messages
	base := BuildBaseMessages(persona, biz)
	messages := append([]Message{}, base...)
	if strings.TrimSpace(contextPrompt) != "" {
		messages = append(messages, Message{Role: "system", Content: contextPrompt})
	}

	// Add conversation history (last 10 exchanges for context)
	maxHistory := 10
	if len(history) > maxHistory*2 {
		history = history[len(history)-maxHistory*2:]
	}
	messages = append(messages, history...)

	intro := BuildIntroTone(persona, biz)
	speaker := "the customer"
	if isOwner {
		speaker = "the business owner"
	}
	task := fmt.Sprintf(`%s

Task: Reply to the latest message from %s. Stay human, emotionally aware, and on-brand. Use the conversation history above for context.

Latest message: %s`, intro, speaker, userMsg)

	messages = append(messages, Message{Role: "user", Content: task})

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
		prompt.WriteString(fmt.Sprintf(`Owner context:
- Recent conversations remembered: %d
- Scheduled posts awaiting publish: %d
- Posts published in the last week: %d
- Posts needing attention (failed in last week): %d
- Connected social accounts: %d

Use these details only when they help the owner decide what to do next. Keep suggestions pragmatic.`,
			historyCount,
			stats.ScheduledPosts, stats.RecentPosts, stats.FailedPosts, stats.ConnectedAccounts))
	} else {
		prompt.WriteString(fmt.Sprintf(`Customer context:
- Previous touchpoints with this customer: %d
- Tone to mirror: %s
- Industry focus: %s

Use their history to stay consistent and caring.`, historyCount, persona.Tone, fallback(biz.Niche, biz.Industry)))
	}

	return prompt.String()
}

// BusinessStats represents current business statistics
type BusinessStats struct {
	ScheduledPosts    int
	RecentPosts       int
	FailedPosts       int
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
