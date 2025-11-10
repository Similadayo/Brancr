package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// CaptionOptimizer enhances social media captions with AI
type CaptionOptimizer struct {
	Client *AIClient
	DB     *gorm.DB
}

// OptimizeCaptionRequest contains the context for caption optimization
type OptimizeCaptionRequest struct {
	OriginalCaption string
	Platform        string // instagram, facebook, tiktok, youtube, whatsapp
	MediaType       string // image, video, carousel
	TenantID        uint
}

// OptimizeCaptionResult contains the optimized caption and metadata
type OptimizeCaptionResult struct {
	OptimizedCaption string
	Hashtags         []string
	SuggestedTime    string
	Improvements     string
}

// OptimizeCaption uses AI to enhance a caption based on brand voice and platform best practices
func (o *CaptionOptimizer) OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error) {
	// Load tenant's brand profile and persona
	var persona storage.Persona
	if err := o.DB.First(&persona, "tenant_id = ?", req.TenantID).Error; err != nil {
		persona.Tone = "friendly"
	}

	var biz storage.BusinessProfile
	if err := o.DB.First(&biz, "tenant_id = ?", req.TenantID).Error; err != nil {
		biz.Description = ""
	}

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(req.Platform)

	// Build layered prompt
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	taskPrompt := fmt.Sprintf(`%s

Task: Write a social media caption for %s.

Context:
- Platform: %s
- Media type: %s
- Original caption: %q

Guidance:
%s

Rules:
1. Reflect the brand tone and vibe described above.
2. Keep the caption to one or two natural sentences.
3. Use emojis only if they fit the brand personality. Include no more than three relevant hashtags.
4. Sound human and conversational—never robotic or overly promotional.
5. End with a brand-flavored hook or light invitation to engage.

Return only the finished caption with hashtags at the end.`,
		intro,
		strings.ToUpper(req.Platform),
		strings.ToUpper(req.Platform),
		strings.ToLower(req.MediaType),
		req.OriginalCaption,
		platformGuide,
	)

	messages := append(base, Message{Role: "user", Content: taskPrompt})

	optimized, err := o.Client.Generate(ctx, messages)
	if err != nil {
		// If optimization fails, return original caption
		return &OptimizeCaptionResult{
			OptimizedCaption: req.OriginalCaption,
			Hashtags:         []string{},
			SuggestedTime:    "now",
			Improvements:     "AI optimization unavailable",
		}, nil
	}

	// Extract hashtags from optimized caption
	hashtags := extractHashtags(optimized)

	return &OptimizeCaptionResult{
		OptimizedCaption: strings.TrimSpace(optimized),
		Hashtags:         hashtags,
		SuggestedTime:    suggestPostingTime(req.Platform),
		Improvements:     "AI-optimized",
	}, nil
}

// getPlatformCaptionGuidance returns platform-specific caption guidelines
func getPlatformCaptionGuidance(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return `Instagram best practices:
- Strong hook in the first line to stop scrolling
- Use emojis naturally (1-2 per sentence, max)
- Include 5-10 relevant hashtags at the end
- Keep main caption under 125 chars for optimal engagement (can extend to 2200)
- Use line breaks for readability
- Ask questions to encourage comments`
	case "facebook":
		return `Facebook best practices:
- Conversational, friendly tone
- Ask questions to drive engagement
- Use 1-3 hashtags (less is more on Facebook)
- Keep it concise (under 250 chars performs best)
- Include call-to-action when appropriate
- Use line breaks for readability`
	case "tiktok":
		return `TikTok best practices:
- Short and punchy (under 150 chars)
- Use trending hashtags (3-5)
- Hook viewers immediately
- Keep it conversational and authentic
- Use emojis sparingly (1-2 max)
- Match trending audio/video energy`
	case "youtube":
		return `YouTube best practices:
- Descriptive and keyword-rich
- Include timestamps if applicable (0:00 Introduction)
- Use 1-3 hashtags in the description
- Clear structure with line breaks
- Include call-to-action (subscribe, like, comment)
- Keep it informative and helpful`
	case "whatsapp":
		return `WhatsApp best practices:
- Brief and personal (under 1000 chars)
- Minimal formatting (no excessive emojis)
- Direct and clear message
- Use simple line breaks
- Keep it conversational`
	default:
		return `Keep it engaging, on-brand, and platform-appropriate.`
	}
}

// extractHashtags pulls hashtags from text
func extractHashtags(text string) []string {
	var hashtags []string
	words := strings.Fields(text)
	for _, word := range words {
		if strings.HasPrefix(word, "#") {
			hashtags = append(hashtags, word)
		}
	}
	return hashtags
}

// suggestPostingTime returns optimal posting time hint based on platform
func suggestPostingTime(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return "Best times: 11AM-1PM or 7PM-9PM weekdays"
	case "facebook":
		return "Best times: 1PM-3PM weekdays"
	case "tiktok":
		return "Best times: 6AM-10AM or 7PM-11PM"
	case "youtube":
		return "Best times: 2PM-4PM weekdays, 9AM-11AM weekends"
	default:
		return "Post when your audience is most active"
	}
}

