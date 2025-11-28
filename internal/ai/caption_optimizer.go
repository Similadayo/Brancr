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
	ImageURLs       []string // Optional: URLs of images to analyze
}

// OptimizeCaptionResult contains the optimized caption and metadata
type OptimizeCaptionResult struct {
	OptimizedCaption string
	Hashtags         []string
	SuggestedTime    string
	Improvements     string
}

// OptimizeCaption uses AI to enhance a caption based on brand voice, platform best practices, and image analysis
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

	// Load business details for context
	var menuItems []storage.BusinessMenuItem
	o.DB.Where("tenant_id = ?", req.TenantID).Find(&menuItems)

	var faqs []storage.BusinessFAQ
	o.DB.Where("tenant_id = ?", req.TenantID).Limit(5).Find(&faqs) // Limit to 5 most relevant

	// Analyze image if URLs provided
	var imageAnalysis string
	if len(req.ImageURLs) > 0 && req.MediaType == "image" || req.MediaType == "carousel" {
		// Use first image for analysis (for carousels, analyze the first image)
		if analysis, err := o.Client.AnalyzeImage(ctx, req.ImageURLs[0]); err == nil {
			imageAnalysis = analysis
		}
	}

	// Build business context string
	businessContext := buildBusinessContext(biz, menuItems, faqs)

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(req.Platform)

	// Build layered prompt
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	// Build task prompt with image analysis and business context
	taskPrompt := buildCaptionPrompt(intro, req, platformGuide, imageAnalysis, businessContext)

	// If we have images, use vision API
	var optimized string
	var err error

	if len(req.ImageURLs) > 0 && (req.MediaType == "image" || req.MediaType == "carousel") {
		// Use vision API to generate caption based on image + context
		systemPrompt := buildSystemPromptFromBase(base, persona, biz)

		optimized, err = o.Client.GenerateWithVision(
			ctx,
			systemPrompt,
			taskPrompt,
			req.ImageURLs,
		)
	} else {
		// Use regular text generation
		messages := append(base, Message{Role: "user", Content: taskPrompt})
		optimized, err = o.Client.Generate(ctx, messages)
	}

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

	improvements := "AI-optimized"
	if imageAnalysis != "" {
		improvements = "AI-optimized with image analysis"
	}

	return &OptimizeCaptionResult{
		OptimizedCaption: strings.TrimSpace(optimized),
		Hashtags:         hashtags,
		SuggestedTime:    suggestPostingTime(req.Platform),
		Improvements:     improvements,
	}, nil
}

// buildBusinessContext creates a context string from business details
func buildBusinessContext(biz storage.BusinessProfile, menuItems []storage.BusinessMenuItem, faqs []storage.BusinessFAQ) string {
	var context strings.Builder

	// Business info
	if biz.Name != "" {
		context.WriteString(fmt.Sprintf("Business: %s\n", biz.Name))
	}
	if biz.Industry != "" {
		context.WriteString(fmt.Sprintf("Industry: %s\n", biz.Industry))
	}
	if biz.Description != "" {
		context.WriteString(fmt.Sprintf("About: %s\n", biz.Description))
	}
	if biz.Keywords != "" {
		context.WriteString(fmt.Sprintf("Keywords: %s\n", biz.Keywords))
	}

	// Menu items (if applicable - e.g., restaurant)
	if len(menuItems) > 0 {
		context.WriteString("\nProducts/Services:\n")
		for i, item := range menuItems {
			if i >= 5 { // Limit to 5 items
				break
			}
			context.WriteString(fmt.Sprintf("- %s", item.Name))
			if item.Category != "" {
				context.WriteString(fmt.Sprintf(" (%s)", item.Category))
			}
			if item.Price != "" {
				context.WriteString(fmt.Sprintf(" - %s", item.Price))
			}
			context.WriteString("\n")
		}
	}

	return context.String()
}

// buildCaptionPrompt constructs the caption generation prompt
func buildCaptionPrompt(intro string, req OptimizeCaptionRequest, platformGuide string, imageAnalysis string, businessContext string) string {
	var prompt strings.Builder

	prompt.WriteString(intro)
	prompt.WriteString("\n\n")
	prompt.WriteString(fmt.Sprintf("Task: Write a social media caption for %s.\n\n", strings.ToUpper(req.Platform)))

	// Add image analysis if available
	if imageAnalysis != "" {
		prompt.WriteString("Image Analysis:\n")
		prompt.WriteString(imageAnalysis)
		prompt.WriteString("\n\n")
		prompt.WriteString("Important: Reference what's actually in the image in your caption. Make it relevant to what you see.\n\n")
	}

	// Add business context
	if businessContext != "" {
		prompt.WriteString("Business Context:\n")
		prompt.WriteString(businessContext)
		prompt.WriteString("\n\n")
		prompt.WriteString("Important: Connect the image content to the business's products/services when relevant.\n\n")
	}

	prompt.WriteString("Context:\n")
	prompt.WriteString(fmt.Sprintf("- Platform: %s\n", strings.ToUpper(req.Platform)))
	prompt.WriteString(fmt.Sprintf("- Media type: %s\n", strings.ToLower(req.MediaType)))
	if req.OriginalCaption != "" {
		prompt.WriteString(fmt.Sprintf("- Original caption hint: %q\n", req.OriginalCaption))
	}

	prompt.WriteString("\n")
	prompt.WriteString("Guidance:\n")
	prompt.WriteString(platformGuide)

	prompt.WriteString("\n\n")
	prompt.WriteString("Rules:\n")
	prompt.WriteString("1. Reflect the brand tone and vibe described above.\n")
	prompt.WriteString("2. Keep the caption to one or two natural sentences.\n")
	prompt.WriteString("3. Reference what's actually in the image when relevant.\n")
	prompt.WriteString("4. Connect image content to business offerings when appropriate.\n")
	prompt.WriteString("5. Use emojis only if they fit the brand personality. Include no more than three relevant hashtags.\n")
	prompt.WriteString("6. Sound human and conversational—never robotic or overly promotional.\n")
	prompt.WriteString("7. End with a brand-flavored hook or light invitation to engage.\n\n")
	prompt.WriteString("Return only the finished caption with hashtags at the end.")

	return prompt.String()
}

// buildSystemPromptFromBase converts base messages to a system prompt string
func buildSystemPromptFromBase(base []Message, persona storage.Persona, biz storage.BusinessProfile) string {
	var prompt strings.Builder
	for _, msg := range base {
		if msg.Role == "system" {
			if str, ok := msg.Content.(string); ok {
				prompt.WriteString(str)
				prompt.WriteString("\n")
			}
		}
	}
	if prompt.Len() == 0 {
		// Fallback to basic persona/business info
		prompt.WriteString(fmt.Sprintf("You are writing captions for %s", biz.Name))
		if persona.BotName != "" {
			prompt.WriteString(fmt.Sprintf(" in the voice of %s", persona.BotName))
		}
	}
	return prompt.String()
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
