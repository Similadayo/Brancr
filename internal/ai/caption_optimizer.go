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
// Template: #13 OptimizeCaption (caption optimization/generation)
func buildCaptionPrompt(intro string, req OptimizeCaptionRequest, platformGuide string, imageAnalysis string, businessContext string) string {
	var prompt strings.Builder

	prompt.WriteString(intro)
	prompt.WriteString("\n\n")

	// Handle 3 modes:
	// 1. AI-generated: OriginalCaption is empty - generate from scratch
	// 2. Tenant-customized: OriginalCaption exists but we're not optimizing (handled by caller)
	// 3. AI-enhanced: OriginalCaption exists - optimize/enhance it
	if req.OriginalCaption == "" {
		// Mode 1: Generate caption from scratch based on tenant preferences
		prompt.WriteString(fmt.Sprintf("Task: Generate a social media caption for %s based on the business's brand voice, image content, and preferences.\n\n", strings.ToUpper(req.Platform)))
		prompt.WriteString("Generate a fresh, engaging caption that:\n")
		prompt.WriteString("1. Reflects the brand's voice and personality\n")
		prompt.WriteString("2. References the image content when relevant\n")
		prompt.WriteString("3. Is engaging and platform-appropriate\n")
		prompt.WriteString("4. Sounds like a real human wrote it (not robotic or template-like)\n\n")
	} else {
		// Mode 3: Optimize/enhance existing caption
		prompt.WriteString(fmt.Sprintf("Task: Optimize and rewrite a social media caption for %s.\n\n", strings.ToUpper(req.Platform)))
		prompt.WriteString(fmt.Sprintf("Current caption: \"%s\"\n", req.OriginalCaption))
		prompt.WriteString("Rewrite the caption to be more engaging and aligned with the brand's voice.\n\n")
	}
	if len(req.ImageURLs) > 0 {
		prompt.WriteString(fmt.Sprintf("Image URL: %s\n", req.ImageURLs[0]))
	}
	prompt.WriteString(fmt.Sprintf("Brand voice: {{brand_tone}}\n\n"))

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

	prompt.WriteString("CRITICAL - Write like a REAL HUMAN would write this caption:\n")
	prompt.WriteString("- Vary your sentence length naturally (mix short punchy lines with longer descriptive ones)\n")
	prompt.WriteString("- Use conversational language - write like you're talking to friends, not crafting marketing copy\n")
	prompt.WriteString("- Avoid overly formal or corporate phrasing\n")
	prompt.WriteString("- Don't sound like a template or robot - add personality and authenticity\n")
	prompt.WriteString("- Use contractions naturally (I'm, we're, you're, it's)\n")
	prompt.WriteString("- Let your personality shine through - don't be generic or overly polished\n")
	prompt.WriteString("- Make the caption concise, clear, and appealing for the target audience\n")
	prompt.WriteString("- Follow brand voice but keep the style lively and informal as appropriate\n")
	prompt.WriteString("- Do NOT use em-dashes in captions. Use commas, periods, or regular hyphens instead.\n\n")

	prompt.WriteString("Rules:\n")
	prompt.WriteString("1. Reflect the brand tone and vibe described above.\n")
	prompt.WriteString("2. Keep the caption to one or two natural sentences - vary the length.\n")
	prompt.WriteString("3. Reference what's actually in the image when relevant - be specific, not generic.\n")
	prompt.WriteString("4. Connect image content to business offerings when appropriate - do it naturally, not forced.\n")
	prompt.WriteString("5. Use emojis only if they fit the brand personality. Include no more than three relevant hashtags.\n")
	prompt.WriteString("6. Sound genuinely human and conversational - never robotic, overly promotional, or template-like.\n")
	prompt.WriteString("7. End with a brand-flavored hook or light invitation to engage - make it feel natural.\n\n")
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

// GenerateCaptionFromKeywords generates a caption from keywords provided by the tenant
func (o *CaptionOptimizer) GenerateCaptionFromKeywords(ctx context.Context, tenantID uint, keywords string, platform string, mediaType string, imageURLs []string) (string, error) {
	// Load tenant's brand profile and persona
	var persona storage.Persona
	if err := o.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly"
	}

	var biz storage.BusinessProfile
	if err := o.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
	}

	// Load business details for context
	var menuItems []storage.BusinessMenuItem
	o.DB.Where("tenant_id = ?", tenantID).Find(&menuItems)

	// Analyze image if URLs provided
	var imageAnalysis string
	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		if analysis, err := o.Client.AnalyzeImage(ctx, imageURLs[0]); err == nil {
			imageAnalysis = analysis
		}
	}

	// Build business context
	businessContext := buildBusinessContext(biz, menuItems, []storage.BusinessFAQ{})

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(platform)

	// Build base messages
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	// Build prompt for keyword-based generation
	var prompt strings.Builder
	prompt.WriteString(intro)
	prompt.WriteString("\n\n")
	prompt.WriteString(fmt.Sprintf("Task: Generate a social media caption for %s based on keywords provided by the business owner.\n\n", strings.ToUpper(platform)))

	prompt.WriteString(fmt.Sprintf("Keywords provided: %s\n\n", keywords))

	if imageAnalysis != "" {
		prompt.WriteString("Image Analysis:\n")
		prompt.WriteString(imageAnalysis)
		prompt.WriteString("\n\n")
		prompt.WriteString("Important: Reference what's actually in the image in your caption. Make it relevant to what you see.\n\n")
	}

	if businessContext != "" {
		prompt.WriteString("Business Context:\n")
		prompt.WriteString(businessContext)
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("Context:\n")
	prompt.WriteString(fmt.Sprintf("- Platform: %s\n", strings.ToUpper(platform)))
	prompt.WriteString(fmt.Sprintf("- Media type: %s\n", strings.ToLower(mediaType)))
	prompt.WriteString(fmt.Sprintf("- Keywords to incorporate: %s\n", keywords))

	prompt.WriteString("\n")
	prompt.WriteString("Guidance:\n")
	prompt.WriteString(platformGuide)

	prompt.WriteString("\n\n")
	prompt.WriteString("Generate a caption that:\n")
	prompt.WriteString("1. Incorporates the provided keywords naturally\n")
	prompt.WriteString("2. Reflects the brand's voice and personality\n")
	prompt.WriteString("3. References the image content when relevant\n")
	prompt.WriteString("4. Is engaging and platform-appropriate\n")
	prompt.WriteString("5. Sounds like a real human wrote it (not robotic or template-like)\n\n")

	prompt.WriteString("CRITICAL - Write like a REAL HUMAN would write this caption:\n")
	prompt.WriteString("- Vary your sentence length naturally\n")
	prompt.WriteString("- Use conversational language\n")
	prompt.WriteString("- Avoid overly formal or corporate phrasing\n")
	prompt.WriteString("- Use contractions naturally\n")
	prompt.WriteString("- Do NOT use em-dashes. Use commas, periods, or regular hyphens instead.\n\n")

	prompt.WriteString("Return only the finished caption with hashtags at the end.")

	// Use vision API if images available, otherwise text generation
	var caption string
	var err error

	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		systemPrompt := buildSystemPromptFromBase(base, persona, biz)
		caption, err = o.Client.GenerateWithVision(ctx, systemPrompt, prompt.String(), imageURLs)
	} else {
		messages := append(base, Message{Role: "user", Content: prompt.String()})
		caption, err = o.Client.Generate(ctx, messages)
	}

	if err != nil {
		return "", fmt.Errorf("failed to generate caption: %w", err)
	}

	return strings.TrimSpace(caption), nil
}

// RephraseCaption rephrases an existing caption while keeping the same meaning
func (o *CaptionOptimizer) RephraseCaption(ctx context.Context, tenantID uint, originalCaption string, platform string, mediaType string, imageURLs []string) (string, error) {
	// Load tenant's brand profile and persona
	var persona storage.Persona
	if err := o.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly"
	}

	var biz storage.BusinessProfile
	if err := o.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
	}

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(platform)

	// Build base messages
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	// Build prompt for rephrasing
	var prompt strings.Builder
	prompt.WriteString(intro)
	prompt.WriteString("\n\n")
	prompt.WriteString(fmt.Sprintf("Task: Rephrase this social media caption for %s while keeping the same meaning and message.\n\n", strings.ToUpper(platform)))

	prompt.WriteString(fmt.Sprintf("Original caption: \"%s\"\n\n", originalCaption))

	prompt.WriteString("Context:\n")
	prompt.WriteString(fmt.Sprintf("- Platform: %s\n", strings.ToUpper(platform)))
	prompt.WriteString(fmt.Sprintf("- Media type: %s\n", strings.ToLower(mediaType)))

	prompt.WriteString("\n")
	prompt.WriteString("Guidance:\n")
	prompt.WriteString(platformGuide)

	prompt.WriteString("\n\n")
	prompt.WriteString("Rephrase the caption to:\n")
	prompt.WriteString("1. Keep the same core message and meaning\n")
	prompt.WriteString("2. Use different words and phrasing\n")
	prompt.WriteString("3. Make it more engaging if possible\n")
	prompt.WriteString("4. Maintain the brand voice\n")
	prompt.WriteString("5. Sound natural and human (not robotic)\n")
	prompt.WriteString("6. Do NOT use em-dashes. Use commas, periods, or regular hyphens instead.\n\n")

	prompt.WriteString("Return only the rephrased caption with hashtags at the end.")

	messages := append(base, Message{Role: "user", Content: prompt.String()})
	caption, err := o.Client.Generate(ctx, messages)

	if err != nil {
		return "", fmt.Errorf("failed to rephrase caption: %w", err)
	}

	return strings.TrimSpace(caption), nil
}

// FineTuneCaption fine-tunes an existing caption to improve it
func (o *CaptionOptimizer) FineTuneCaption(ctx context.Context, tenantID uint, originalCaption string, platform string, mediaType string, imageURLs []string) (string, error) {
	// Load tenant's brand profile and persona
	var persona storage.Persona
	if err := o.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly"
	}

	var biz storage.BusinessProfile
	if err := o.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
	}

	// Analyze image if URLs provided
	var imageAnalysis string
	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		if analysis, err := o.Client.AnalyzeImage(ctx, imageURLs[0]); err == nil {
			imageAnalysis = analysis
		}
	}

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(platform)

	// Build base messages
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	// Build prompt for fine-tuning
	var prompt strings.Builder
	prompt.WriteString(intro)
	prompt.WriteString("\n\n")
	prompt.WriteString(fmt.Sprintf("Task: Fine-tune and improve this social media caption for %s.\n\n", strings.ToUpper(platform)))

	prompt.WriteString(fmt.Sprintf("Current caption: \"%s\"\n\n", originalCaption))

	if imageAnalysis != "" {
		prompt.WriteString("Image Analysis:\n")
		prompt.WriteString(imageAnalysis)
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("Context:\n")
	prompt.WriteString(fmt.Sprintf("- Platform: %s\n", strings.ToUpper(platform)))
	prompt.WriteString(fmt.Sprintf("- Media type: %s\n", strings.ToLower(mediaType)))

	prompt.WriteString("\n")
	prompt.WriteString("Guidance:\n")
	prompt.WriteString(platformGuide)

	prompt.WriteString("\n\n")
	prompt.WriteString("Fine-tune the caption to:\n")
	prompt.WriteString("1. Keep the same message but make it more engaging\n")
	prompt.WriteString("2. Improve clarity and flow\n")
	prompt.WriteString("3. Optimize for the platform's best practices\n")
	prompt.WriteString("4. Enhance readability and impact\n")
	prompt.WriteString("5. Maintain brand voice and personality\n")
	prompt.WriteString("6. Reference image content when relevant\n")
	prompt.WriteString("7. Sound natural and human (not robotic)\n")
	prompt.WriteString("8. Do NOT use em-dashes. Use commas, periods, or regular hyphens instead.\n\n")

	prompt.WriteString("Return only the fine-tuned caption with hashtags at the end.")

	// Use vision API if images available, otherwise text generation
	var caption string
	var err error

	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		systemPrompt := buildSystemPromptFromBase(base, persona, biz)
		caption, err = o.Client.GenerateWithVision(ctx, systemPrompt, prompt.String(), imageURLs)
	} else {
		messages := append(base, Message{Role: "user", Content: prompt.String()})
		caption, err = o.Client.Generate(ctx, messages)
	}

	if err != nil {
		return "", fmt.Errorf("failed to fine-tune caption: %w", err)
	}

	return strings.TrimSpace(caption), nil
}
