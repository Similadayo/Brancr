package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// CaptionOptimizer enhances social media captions with AI
type CaptionOptimizer struct {
	Client AIClientInterface
	DB     *gorm.DB
}

// OptimizeCaptionRequest contains the context for caption optimization
type OptimizeCaptionRequest struct {
	OriginalCaption string
	Platform        string // instagram, facebook, tiktok, youtube, whatsapp
	MediaType       string // image, video, carousel
	TenantID        uuid.UUID
	ImageURLs       []string // Optional: URLs of images to analyze
	Goal            string   // promote, announce, educate, engage, scratch (post goal)
}

// OptimizeCaptionResult contains the optimized caption and metadata
type OptimizeCaptionResult struct {
	OptimizedCaption string
	Hashtags         []string
	SuggestedTime    string
	Improvements     string
	TokenUsage       *TokenUsage // Token usage for cost tracking
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
	contextItems := o.fetchContextItems(req.TenantID)

	var faqs []storage.BusinessFAQ
	o.DB.Where("tenant_id = ?", req.TenantID).Limit(5).Find(&faqs) // Limit to 5 most relevant

	// Analyze image if URLs provided
	var imageAnalysis string
	if len(req.ImageURLs) > 0 && (req.MediaType == "image" || req.MediaType == "carousel") {
		// Use first image for analysis (for carousels, analyze the first image)
		if analysis, err := o.Client.AnalyzeImage(ctx, req.ImageURLs[0]); err == nil {
			imageAnalysis = analysis
		}
	}

	// Build business context string
	businessContext := buildBusinessContext(biz, contextItems, faqs)

	// Get platform-specific guidance
	platformGuide := getPlatformCaptionGuidance(req.Platform)

	// Build layered prompt
	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	// Build task prompt with image analysis and business context
	taskPrompt := buildCaptionPrompt(intro, req, platformGuide, imageAnalysis, businessContext)

	// If we have images, use vision API
	var optimized string
	var tokenUsage *TokenUsage
	var err error

	if len(req.ImageURLs) > 0 && (req.MediaType == "image" || req.MediaType == "carousel") {
		// Use vision API to generate caption based on image + context
		// For carousels, limit to first image to avoid API limits and improve performance
		imagesToProcess := req.ImageURLs
		if req.MediaType == "carousel" && len(req.ImageURLs) > 1 {
			// For carousels, use only the first image for caption generation
			// The caption should represent the overall carousel content
			imagesToProcess = req.ImageURLs[:1]
		}

		systemPrompt := buildSystemPromptFromBase(base, persona, biz)

		optimized, tokenUsage, err = o.Client.GenerateWithVisionWithUsage(
			ctx,
			systemPrompt,
			taskPrompt,
			imagesToProcess,
		)
	} else {
		// Use regular text generation with usage tracking
		messages := append(base, Message{Role: "user", Content: taskPrompt})
		var result *GenerateResult
		result, err = o.Client.GenerateWithUsage(ctx, messages)
		if err == nil {
			optimized = result.Content
			tokenUsage = result.TokenUsage
		}
	}

	if err != nil {
		// If optimization fails, return error (don't silently return empty caption)
		// Only return original caption if it exists (for optimization requests)
		if req.OriginalCaption != "" {
			// Optimization request - return original caption as fallback
			return &OptimizeCaptionResult{
				OptimizedCaption: req.OriginalCaption,
				Hashtags:         []string{},
				SuggestedTime:    "now",
				Improvements:     "AI optimization unavailable",
			}, nil
		}
		// Generation request (no original caption) - return error
		return nil, fmt.Errorf("failed to generate caption: %w", err)
	}

	// Ensure we actually got a caption
	if strings.TrimSpace(optimized) == "" {
		return nil, fmt.Errorf("AI generated empty caption")
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
		TokenUsage:       tokenUsage,
	}, nil
}

// buildBusinessContext creates a context string from business details
func buildBusinessContext(biz storage.BusinessProfile, items []string, faqs []storage.BusinessFAQ) string {
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
	if len(items) > 0 {
		context.WriteString("\nProducts/Services:\n")
		for _, item := range items {
			context.WriteString(fmt.Sprintf("- %s\n", item))
		}
	}

	return context.String()
}

// fetchContextItems fetches relevant products/services based on tenant industry
func (o *CaptionOptimizer) fetchContextItems(tenantID uuid.UUID) []string {
	var items []string

	// Get tenant to get industry ID
	var tenant storage.Tenant
	if err := o.DB.First(&tenant, tenantID).Error; err != nil {
		return items
	}

	var industry storage.Industry
	if tenant.IndustryID != uuid.Nil {
		o.DB.First(&industry, tenant.IndustryID)
	} else {
		return items // No industry, no specific items
	}

	// Based on industry, fetch items (Limit to 5 for context window efficiency)
	if industry.HasProducts {
		var products []storage.Product
		o.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).Limit(5).Find(&products)
		for _, p := range products {
			items = append(items, fmt.Sprintf("%s (%s) - %s %.2f", p.Name, p.Category, p.Currency, p.Price))
		}
	}

	if industry.HasMenu {
		var menu []storage.MenuItem
		o.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).Limit(5).Find(&menu)
		for _, m := range menu {
			items = append(items, fmt.Sprintf("%s (%s) - %s %.2f", m.Name, m.Category, m.Currency, m.Price))
		}
	}

	if industry.HasServices {
		var services []storage.Service
		o.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).Limit(5).Find(&services)
		for _, s := range services {
			items = append(items, fmt.Sprintf("%s (%s)", s.Name, s.Category))
		}
	}

	return items
}

// getGoalGuidance returns goal-specific guidance for caption generation
// FEATURE 10: Goal-based caption generation
func getGoalGuidance(goal string) string {
	switch strings.ToLower(goal) {
	case "promote":
		return `POST GOAL: Promote Product/Service
- Focus on driving sales and conversions
- Mention price or value proposition if known
- End with a strong CTA like 'DM to order', 'Link in bio', 'Shop now'
- Highlight benefits and features
- Create desire and urgency`
	case "announce":
		return `POST GOAL: Make an Announcement
- Create excitement and urgency
- Use attention-grabbing language: 'Now!', 'Just dropped!', 'Don't miss out!'
- Make it feel like breaking news
- Encourage sharing and engagement
- Keep it concise but impactful`
	case "educate":
		return `POST GOAL: Educate Audience
- Provide genuine value first
- Share tips, insights, or how-to information
- Position the brand as an expert
- Soft sell only at the very end (if at all)
- Use clear, accessible language`
	case "engage":
		return `POST GOAL: Drive Engagement
- Ask questions to spark conversation
- Encourage comments and tagging: 'Tag a friend!', 'What do you think?'
- Use interactive language: 'Drop a 🔥 if you agree'
- Create polls or debates
- Make it fun and interactive`
	default: // "scratch" or empty
		return `POST GOAL: General
- Follow brand voice naturally
- Keep it authentic and engaging
- Match the content to what's in the image`
	}
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
		prompt.WriteString(fmt.Sprintf("Task: Generate a social media caption for %s.\n\n", strings.ToUpper(req.Platform)))
		prompt.WriteString("GOAL: Write a fresh, engaging caption that sells the brand/product while using the image as a visual hook.\n")
		prompt.WriteString("Do NOT simply describe the image (e.g., 'Here is a plate of food'). Instead, tell a story or sell the experience (e.g., 'Craving this? Come taste the magic').\n\n")
	} else {
		// Mode 3: Optimize/enhance existing caption
		prompt.WriteString(fmt.Sprintf("Task: Optimize and rewrite a social media caption for %s.\n\n", strings.ToUpper(req.Platform)))
		prompt.WriteString(fmt.Sprintf("Current caption: \"%s\"\n", req.OriginalCaption))
		prompt.WriteString("Rewrite the caption to be more engaging and aligned with the brand's voice.\n\n")
	}

	// 1. Context: Business (PRIORITY)
	if businessContext != "" {
		prompt.WriteString("BUSINESS CONTEXT (Use this to ground the caption in the brand's identity):\n")
		prompt.WriteString(businessContext)
		prompt.WriteString("\n\n")
	}

	// 2. Context: Image (SUPPORTING)
	if imageAnalysis != "" {
		prompt.WriteString("IMAGE CONTENT (Use this only as a visual hook):\n")
		prompt.WriteString(imageAnalysis)
		prompt.WriteString("\n\n")
	} else if len(req.ImageURLs) > 0 {
		prompt.WriteString(fmt.Sprintf("Image URL: %s\n", req.ImageURLs[0]))
	}

	prompt.WriteString(fmt.Sprintf("Brand voice: {{brand_tone}}\n\n"))

	prompt.WriteString("Context:\n")
	prompt.WriteString(fmt.Sprintf("- Platform: %s\n", strings.ToUpper(req.Platform)))
	prompt.WriteString(fmt.Sprintf("- Media type: %s\n", strings.ToLower(req.MediaType)))
	if req.OriginalCaption != "" {
		prompt.WriteString(fmt.Sprintf("- Original caption hint: %q\n", req.OriginalCaption))
	}

	prompt.WriteString("\n")
	prompt.WriteString("Guidance:\n")
	prompt.WriteString(platformGuide)

	// FEATURE 10: Goal-based caption generation
	if req.Goal != "" {
		prompt.WriteString("\n\n")
		prompt.WriteString(getGoalGuidance(req.Goal))
	}

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
	prompt.WriteString("3. Use the image as inspiration, but focus on the BRAND MESSAGE.\n")
	prompt.WriteString("4. Connect image content to business offerings. If the image shows food, sell the taste. If it shows a product, sell the benefit.\n")
	prompt.WriteString("5. Use emojis only if they fit the brand personality. Include no more than three relevant hashtags.\n")
	prompt.WriteString("6. Sound genuinely human and conversational - never robotic, overly promotional, or template-like.\n")
	prompt.WriteString("7. End with a brand-flavored hook or light invitation to engage - make it feel natural.\n\n")
	prompt.WriteString("HASHTAG STRATEGY (CRITICAL):\n")
	prompt.WriteString("You MUST include 15-20 highly relevant, trending hashtags at the end.\n")
	prompt.WriteString("Mix these categories for maximum reach:\n")
	prompt.WriteString("1. Niche/Industry Tags (specific to this business type)\n")
	prompt.WriteString("2. Trending Business Tags: #SmallBusiness #EntrepreneurLife #BusinessGrowth #ShopSmall #SupportSmallBusiness #BehindTheScenes\n")
	prompt.WriteString("3. Content Tags: #Trending #Viral #Inspiration #fyp\n")
	prompt.WriteString("4. Location/Community Tags: #ShopLocal #LocalBusiness\n")
	prompt.WriteString("Ensure all hashtags fit the tenant's business and image context.\n\n")

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
// Returns the generated caption and token usage for cost tracking
func (o *CaptionOptimizer) GenerateCaptionFromKeywords(ctx context.Context, tenantID uuid.UUID, keywords string, platform string, mediaType string, imageURLs []string) (string, *TokenUsage, error) {
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
	contextItems := o.fetchContextItems(tenantID)

	// Analyze image if URLs provided
	var imageAnalysis string
	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		if analysis, err := o.Client.AnalyzeImage(ctx, imageURLs[0]); err == nil {
			imageAnalysis = analysis
		}
	}

	// Build business context
	businessContext := buildBusinessContext(biz, contextItems, []storage.BusinessFAQ{})

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

	prompt.WriteString("HASHTAG STRATEGY (CRITICAL):\n")
	prompt.WriteString("You MUST include 15-20 highly relevant, trending hashtags at the end.\n")
	prompt.WriteString("Mix these categories for maximum reach:\n")
	prompt.WriteString("1. Niche/Industry Tags (specific to this business type)\n")
	prompt.WriteString("2. Trending Business Tags: #SmallBusiness #EntrepreneurLife #BusinessGrowth #ShopSmall #SupportSmallBusiness #BehindTheScenes\n")
	prompt.WriteString("3. Content Tags: #Trending #Viral #Inspiration #fyp\n")
	prompt.WriteString("4. Location/Community Tags: #ShopLocal #LocalBusiness\n")
	prompt.WriteString("Ensure all hashtags fit the tenant's business and request context.\n\n")

	prompt.WriteString("Return only the finished caption with hashtags at the end.")

	// Use vision API if images available, otherwise text generation
	var caption string
	var tokenUsage *TokenUsage
	var err error

	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		// For carousels, limit to first image to avoid API limits
		imagesToProcess := imageURLs
		if mediaType == "carousel" && len(imageURLs) > 1 {
			imagesToProcess = imageURLs[:1]
		}

		systemPrompt := buildSystemPromptFromBase(base, persona, biz)
		caption, tokenUsage, err = o.Client.GenerateWithVisionWithUsage(ctx, systemPrompt, prompt.String(), imagesToProcess)
	} else {
		messages := append(base, Message{Role: "user", Content: prompt.String()})
		var result *GenerateResult
		result, err = o.Client.GenerateWithUsage(ctx, messages)
		if err == nil {
			caption = result.Content
			tokenUsage = result.TokenUsage
		}
	}

	return strings.TrimSpace(caption), tokenUsage, nil
}

// RephraseCaption rephrases an existing caption while keeping the same meaning
// Returns the rephrased caption and token usage for cost tracking
func (o *CaptionOptimizer) RephraseCaption(ctx context.Context, tenantID uuid.UUID, originalCaption string, platform string, mediaType string, imageURLs []string) (string, *TokenUsage, error) {
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

	prompt.WriteString("HASHTAG STRATEGY (CRITICAL):\n")
	prompt.WriteString("You MUST include 15-20 highly relevant, trending hashtags at the end.\n")
	prompt.WriteString("Mix these categories for maximum reach:\n")
	prompt.WriteString("1. Niche/Industry Tags (specific to this business type)\n")
	prompt.WriteString("2. Trending Business Tags: #SmallBusiness #EntrepreneurLife #BusinessGrowth #ShopSmall #SupportSmallBusiness #BehindTheScenes\n")
	prompt.WriteString("3. Content Tags: #Trending #Viral #Inspiration #fyp\n")
	prompt.WriteString("4. Location/Community Tags: #ShopLocal #LocalBusiness\n")
	prompt.WriteString("Ensure all hashtags fit the tenant's business.\n\n")

	prompt.WriteString("Return only the rephrased caption with hashtags at the end.")

	messages := append(base, Message{Role: "user", Content: prompt.String()})
	var result *GenerateResult
	result, err := o.Client.GenerateWithUsage(ctx, messages)
	if err != nil {
		return "", nil, fmt.Errorf("failed to rephrase caption: %w", err)
	}

	return strings.TrimSpace(result.Content), result.TokenUsage, nil
}

// FineTuneCaption fine-tunes an existing caption to improve it
// Returns the fine-tuned caption and token usage for cost tracking
func (o *CaptionOptimizer) FineTuneCaption(ctx context.Context, tenantID uuid.UUID, originalCaption string, platform string, mediaType string, imageURLs []string) (string, *TokenUsage, error) {
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

	prompt.WriteString("HASHTAG STRATEGY (CRITICAL):\n")
	prompt.WriteString("You MUST include 15-20 highly relevant, trending hashtags at the end.\n")
	prompt.WriteString("Mix these categories for maximum reach:\n")
	prompt.WriteString("1. Niche/Industry Tags (specific to this business type)\n")
	prompt.WriteString("2. Trending Business Tags: #SmallBusiness #EntrepreneurLife #BusinessGrowth #ShopSmall #SupportSmallBusiness #BehindTheScenes\n")
	prompt.WriteString("3. Content Tags: #Trending #Viral #Inspiration #fyp\n")
	prompt.WriteString("4. Location/Community Tags: #ShopLocal #LocalBusiness\n")
	prompt.WriteString("Ensure all hashtags fit the tenant's business.\n\n")

	prompt.WriteString("Return only the fine-tuned caption with hashtags at the end.")

	// Use vision API if images available, otherwise text generation
	var caption string
	var tokenUsage *TokenUsage
	var err error

	if len(imageURLs) > 0 && (mediaType == "image" || mediaType == "carousel") {
		// For carousels, limit to first image to avoid API limits
		imagesToProcess := imageURLs
		if mediaType == "carousel" && len(imageURLs) > 1 {
			imagesToProcess = imageURLs[:1]
		}

		systemPrompt := buildSystemPromptFromBase(base, persona, biz)
		caption, tokenUsage, err = o.Client.GenerateWithVisionWithUsage(ctx, systemPrompt, prompt.String(), imagesToProcess)
	} else {
		messages := append(base, Message{Role: "user", Content: prompt.String()})
		var result *GenerateResult
		result, err = o.Client.GenerateWithUsage(ctx, messages)
		if err == nil {
			caption = result.Content
			tokenUsage = result.TokenUsage
		}
	}

	if err != nil {
		return "", nil, fmt.Errorf("failed to fine-tune caption: %w", err)
	}

	return strings.TrimSpace(caption), tokenUsage, nil
}
