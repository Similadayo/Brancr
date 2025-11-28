package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ProactiveAssistant suggests actions and improvements based on context
type ProactiveAssistant struct {
	Client *AIClient
	DB     *gorm.DB
	Memory *MemoryService
}

// NewProactiveAssistant creates a new proactive assistant
func NewProactiveAssistant(client *AIClient, db *gorm.DB, memory *MemoryService) *ProactiveAssistant {
	return &ProactiveAssistant{
		Client: client,
		DB:     db,
		Memory: memory,
	}
}

// SuggestActions generates proactive suggestions based on current state
func (p *ProactiveAssistant) SuggestActions(ctx context.Context, tenantID uint) ([]string, error) {
	var suggestions []string

	// Check for scheduled posts without media
	var postsWithoutMedia []storage.ScheduledPost
	p.DB.Where("tenant_id = ? AND status = ?", tenantID, "scheduled").
		Find(&postsWithoutMedia)

	// Check for failed posts
	var failedPosts []storage.PostLog
	p.DB.Where("tenant_id = ? AND status = ? AND created_at > ?", tenantID, "failed", time.Now().AddDate(0, 0, -7)).
		Find(&failedPosts)

	// Check for connected accounts
	var connectedAccounts []storage.SocialAccount
	p.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).
		Find(&connectedAccounts)

	// Generate suggestions using AI
	suggestionPrompt := fmt.Sprintf(`You are a helpful business assistant. Based on this context, suggest 2-3 actionable next steps:

Business Context:
- Scheduled posts: %d
- Failed posts (last 7 days): %d
- Connected social accounts: %d

Return a JSON array of suggestions (max 3), each as a short actionable sentence:
["Suggestion 1", "Suggestion 2", "Suggestion 3"]

Be concise and actionable.`,
		len(postsWithoutMedia),
		len(failedPosts),
		len(connectedAccounts))

	messages := []Message{
		{Role: "system", Content: suggestionPrompt},
		{Role: "user", Content: "What should I do next?"},
	}

	response, err := p.Client.Generate(ctx, messages)
	if err != nil {
		// Fallback suggestions
		if len(connectedAccounts) == 0 {
			suggestions = append(suggestions, "Connect your social media accounts to start posting")
		}
		if len(postsWithoutMedia) == 0 {
			suggestions = append(suggestions, "Upload media to schedule posts")
		}
		if len(failedPosts) > 0 {
			suggestions = append(suggestions, fmt.Sprintf("Review %d failed posts and fix issues", len(failedPosts)))
		}
		return suggestions, nil
	}

	// Parse JSON suggestions
	var parsedSuggestions []string
	if err := parseJSONArray(response, &parsedSuggestions); err == nil {
		suggestions = parsedSuggestions
	}

	return suggestions, nil
}

// EnhanceResponseWithProactiveSuggestions adds proactive suggestions to responses
func (p *ProactiveAssistant) EnhanceResponseWithProactiveSuggestions(
	ctx context.Context,
	tenantID uint,
	baseResponse string,
) (string, error) {
	// Only add suggestions occasionally (not every message)
	// Check conversation history to see if we've suggested recently
	recentHistory := p.Memory.GetRecentTenantConversationHistory(ctx, tenantID, 5)

	// Check if we've suggested in last 3 messages
	hasRecentSuggestion := false
	for i := len(recentHistory) - 1; i >= 0 && i >= len(recentHistory)-3; i-- {
		if recentHistory[i].Role == "assistant" {
			content := recentHistory[i].Content
			// Handle both string and ContentPart array
			var contentStr string
			switch v := content.(type) {
			case string:
				contentStr = v
			case []ContentPart:
				// Extract text from ContentPart array
				for _, part := range v {
					if part.Type == "text" {
						contentStr += part.Text
					}
				}
			default:
				continue
			}
			if strings.Contains(contentStr, "💡") || strings.Contains(contentStr, "suggestion") {
				hasRecentSuggestion = true
				break
			}
		}
	}

	if hasRecentSuggestion {
		return baseResponse, nil
	}

	// Get suggestions
	suggestions, err := p.SuggestActions(ctx, tenantID)
	if err != nil || len(suggestions) == 0 {
		return baseResponse, nil
	}

	// Add suggestions to response (only if relevant)
	if len(suggestions) > 0 {
		baseResponse += "\n\n💡 *Proactive Tips:*\n"
		for i, suggestion := range suggestions {
			if i >= 2 { // Max 2 suggestions
				break
			}
			baseResponse += fmt.Sprintf("• %s\n", suggestion)
		}
	}

	return baseResponse, nil
}

// parseJSONArray parses a JSON array from AI response
func parseJSONArray(response string, target interface{}) error {
	// Remove markdown code blocks if present
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		response = strings.Join(lines[1:len(lines)-1], "\n")
	}
	response = strings.Trim(response, "`")

	// Try to find JSON array
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start != -1 && end != -1 && end > start {
		response = response[start : end+1]
	}

	return json.Unmarshal([]byte(response), target)
}
