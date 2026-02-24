package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ContextAwareResponseGenerator creates context-aware responses using conversation history
type ContextAwareResponseGenerator struct {
	Client       *AIClient
	DB           *gorm.DB
	Memory       *MemoryService
	QuotaService *quota.QuotaService // Optional: for cost tracking
}

// NewContextAwareGenerator creates a new context-aware generator
func NewContextAwareGenerator(client *AIClient, db *gorm.DB, memory *MemoryService) *ContextAwareResponseGenerator {
	return NewContextAwareGeneratorWithQuota(client, db, memory, nil)
}

// NewContextAwareGeneratorWithQuota creates a context-aware generator with optional quota service for cost tracking
func NewContextAwareGeneratorWithQuota(client *AIClient, db *gorm.DB, memory *MemoryService, qs *quota.QuotaService) *ContextAwareResponseGenerator {
	return &ContextAwareResponseGenerator{
		Client:       client,
		DB:           db,
		Memory:       memory,
		QuotaService: qs, // Optional: for cost tracking
	}
}

// GenerateWithFullContext generates a response with full business and conversation context
func (g *ContextAwareResponseGenerator) GenerateWithFullContext(
	ctx context.Context,
	tenantID uuid.UUID,
	userMsg string,
	isOwner bool,
	customerID uuid.UUID,
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

You are responding to %s. Write like you're having a real conversation - natural, warm, and genuinely helpful.

CONVERSATION CONTEXT:
- Previous messages: {{history}}
- %s just said: "%s"

WRITING STYLE - Write Like a Real Human:
1. **Answer directly first** - Get to the point immediately
2. **Vary sentence length** - Mix short responses with longer explanations when needed
3. **Use natural transitions** - "So...", "Actually...", "Here's the thing..."
4. **Show emotional awareness** - Match their energy and emotional state authentically
5. **Use fragments when natural** - "Sounds good!", "On it!", "No worries!"
6. **Avoid corporate speak** - Don't say "I understand your concern" - say "I get it" or "That's frustrating"
7. **Be contextually aware** - Understand nuance, humor, and subtext
8. **Don't sound robotic** - Vary your responses, don't use templates

NIGERIAN ENGLISH AWARENESS (if customer interaction):
- Understand Nigerian English patterns but respond in simple, clear English
- Use customer's name when available ("Good morning John" or "Hi Sarah")
- Use "sir/ma" respectfully when gender is clear
- Understand phrases: "How far?" = "How are you?", "Well done" = thanks, "I am coming" = "I'm on my way"
- "Sorry" often means empathy/acknowledgment, not just apology
- NEVER use Pidgin words (sha, abi, na so, dey, etc.) - always proper English
- Keep vocabulary simple and everyday

TASK: Reply naturally to their message. Address what they said directly. Be helpful, warm, and authentic. Use conversation history for context.`, intro, speaker, speaker, userMsg)

	messages = append(messages, Message{Role: "user", Content: task})

	// Generate response with retry on moderation error
	// Use GenerateWithUsage if quota service is available for cost tracking
	var reply string
	var err error
	var tokenUsage *TokenUsage

	if g.QuotaService != nil {
		result, genErr := g.Client.GenerateWithUsage(ctx, messages)
		if genErr != nil {
			// If moderation error, try with sanitized prompt
			if IsModerationError(genErr) {
				// Retry with a simpler, safer prompt
				safePrompt := GenerateSafePrompt(persona, biz, isOwner)
				messages[0].Content = SanitizePrompt(safePrompt)

				result, genErr = g.Client.GenerateWithUsage(ctx, messages)
				if genErr != nil {
					// If still fails, try dynamic fallback generator
					fallbackGen := NewDynamicFallbackGenerator(g.Client, g.DB, nil)
					fallbackReply, fallbackErr := fallbackGen.GenerateFallbackResponse(ctx, tenantID, userMsg, isOwner)
					if fallbackErr == nil && fallbackReply != "" {
						return fallbackReply, nil
					}
					// All AI attempts failed - return error for escalation
					return "", fmt.Errorf("AI generation failed after all retries: %w", genErr)
				}
			} else {
				return "", fmt.Errorf("AI generation failed: %w", genErr)
			}
		}
		reply = result.Content
		if result.TokenUsage != nil {
			tokenUsage = result.TokenUsage
		}
	} else {
		// No quota service, use regular Generate
		reply, err = g.Client.Generate(ctx, messages)
		if err != nil {
			// If moderation error, try with sanitized prompt
			if IsModerationError(err) {
				// Retry with a simpler, safer prompt
				safePrompt := GenerateSafePrompt(persona, biz, isOwner)
				messages[0].Content = SanitizePrompt(safePrompt)

				reply, err = g.Client.Generate(ctx, messages)
				if err != nil {
					// If still fails, try dynamic fallback generator
					fallbackGen := NewDynamicFallbackGenerator(g.Client, g.DB, nil)
					fallbackReply, fallbackErr := fallbackGen.GenerateFallbackResponse(ctx, tenantID, userMsg, isOwner)
					if fallbackErr == nil && fallbackReply != "" {
						return fallbackReply, nil
					}
					// All AI attempts failed - return error for escalation
					return "", fmt.Errorf("AI generation failed after all retries: %w", err)
				}
			} else {
				return "", fmt.Errorf("AI generation failed: %w", err)
			}
		}
	}

	// Track cost after successful generation
	if g.QuotaService != nil && tokenUsage != nil {
		model := g.Client.Model
		if model == "" {
			model = "default"
		}
		quotaTokenUsage := quota.TokenUsageFromAI(tokenUsage.PromptTokens, tokenUsage.CompletionTokens, tokenUsage.TotalTokens)
		if costErr := g.QuotaService.IncrementAICallCost(ctx, tenantID, model, quotaTokenUsage); costErr != nil {
			// Log but don't fail - cost tracking is optional
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
// buildContextPrompt constructs context-aware prompt
// Template: #9 ContextAwareResponseGenerator (full context response generation)
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

Use these details only when they help the owner decide what to do next. Keep suggestions pragmatic.
For simple acknowledgments (e.g., "Thanks", "Okay"), just reply naturally/briefly without a full report.`,
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
func (g *ContextAwareResponseGenerator) getBusinessStats(ctx context.Context, tenantID uuid.UUID) BusinessStats {
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
