package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
)

// Responder combines AI, persona, memory, and business context for lifelike conversations
type Responder struct {
	Client       *AIClient
	Memory       *MemoryService
	Persona      storage.Persona
	Classifier   *Classifier
	Emotion      *EmotionAnalyzer
	Policy       *PolicyEngine
	QuotaService *quota.QuotaService // Optional: for cost tracking
}

// isSensitiveIntent checks if an intent should never be auto-replied to
func isSensitiveIntent(intent string) bool {
	return intent == "payment" ||
		intent == "account" ||
		intent == "billing" ||
		intent == "refund" ||
		intent == "complaint"
}

// getPlatformFormatting returns platform-specific formatting instructions
func getPlatformFormatting(channel string) string {
	switch strings.ToLower(channel) {
	case "telegram":
		return "Format: *bold* for emphasis, _italic_ for subtle points."
	case "whatsapp":
		return "Format: *bold* for emphasis, _italic_ for subtle points."
	case "instagram", "facebook":
		return "Format: Line breaks for clarity. Minimal emojis."
	default:
		return "Format: Plain text."
	}
}

// 🧠 GenerateTenantAssistantReply - Human-like, Contextual, Brand-Aware
func (r *Responder) GenerateTenantAssistantReply(ctx context.Context, tenantID uuid.UUID, userMsg string) (string, error) {
	if r == nil || r.Client == nil || r.Memory == nil {
		return "", fmt.Errorf("AI responder improperly initialized")
	}

	// Load persona from database (tenant-specific)
	var persona storage.Persona
	if err := r.Memory.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.BotName = "Assistant"
	}

	// Load business profile
	var biz storage.BusinessProfile
	_ = r.Memory.DB.First(&biz, "tenant_id = ?", tenantID).Error

	// Retrieve conversation history (last 10 exchanges = 20 messages)
	conversationHistory := r.Memory.GetRecentTenantConversationHistory(ctx, tenantID, 10)

	base := BuildBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Check if tenant is mentioning an offer naturally (before AI responds)
	// This makes it feel like a boss-assistant conversation
	offerDetected := r.detectAndSaveOffer(ctx, tenantID, userMsg)

	// Template: #5 GenerateTenantAssistantReply (tenant-facing reply generation)
	// Enhanced with GPT-5-mini best practices for human-like responses
	intro := BuildIntroTone(persona, biz)

	offerContext := ""
	if offerDetected {
		offerContext = "\n\nIMPORTANT: The owner just mentioned a promotional offer. Acknowledge it naturally and confirm you'll use it with customers. Be conversational, not robotic - like 'Got it! I'll let customers know about that offer' or 'Perfect! I'll suggest that when it makes sense.'"
	}

	task := fmt.Sprintf(`%s

You are responding as a real assistant helping the business owner. Write like you're texting a colleague - natural, helpful, and genuinely supportive.

CONVERSATION CONTEXT:
- Previous messages: {{history}}
- Business owner just said: "%s"
- Brand voice: {{brand_tone}}%s

WRITING STYLE - Write Like a Real Human:
1. **Answer directly first** - Get to the point, then add helpful context
2. **Vary sentence length** - Mix short responses ("Got it!") with slightly longer explanations
3. **Use natural transitions** - "So...", "Actually...", "Here's what I can do..."
4. **Show you're listening** - Reference what they said ("I hear you", "That makes sense")
5. **Match their energy** - If they're casual, be casual. If serious, be appropriately serious
6. **Use fragments when natural** - "Sounds good!", "On it!", "No problem!"
7. **Avoid corporate speak** - Don't say "I understand your request" - say "I got you" or "Let me help with that"
8. **Be specific, not generic** - Instead of "I can help", say "I can check that for you right now"

AVOID ROBOTIC PATTERNS:
- Don't start every response the same way
- Don't use phrases like "Thank you for your inquiry", "I understand your request", "Is there anything else?"
- Don't sound like a template or script
- Don't be overly formal or polished
- Each conversation should feel unique and personal

TASK: Reply naturally, like a real person would. Address what they asked directly. Be helpful, warm, and solution-focused. Use the conversation history for context.`, intro, userMsg, offerContext)

	messages = append(messages, Message{Role: "user", Content: task})

	// Use GenerateWithUsage if quota service is available for cost tracking
	var reply string
	var err error
	if r.QuotaService != nil {
		result, genErr := r.Client.GenerateWithUsage(ctx, messages)
		if genErr != nil {
			return "", fmt.Errorf("AI generation failed: %w", genErr)
		}
		reply = result.Content

		// Track cost after successful generation
		if result.TokenUsage != nil {
			model := r.Client.Model
			if model == "" {
				model = "default"
			}
			quotaTokenUsage := quota.TokenUsageFromAI(result.TokenUsage.PromptTokens, result.TokenUsage.CompletionTokens, result.TokenUsage.TotalTokens)
			if costErr := r.QuotaService.IncrementAICallCost(ctx, tenantID, model, quotaTokenUsage); costErr != nil {
				// Log but don't fail - cost tracking is optional
			}
		}
	} else {
		// No quota service, use regular Generate
		reply, err = r.Client.Generate(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("AI generation failed: %w", err)
		}
	}

	reply = strings.TrimSpace(reply)

	// Remove em-dashes from anywhere in the message
	reply = removeEmDashes(reply)

	// Save memory (user + AI pair)
	if reply != "" {
		r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	}

	return reply, nil
}

// 🔁 Emotion & Intent aware response path
func (r *Responder) GenerateHumanlikeReply(ctx context.Context, tenantID uuid.UUID, userMsg string,
	intent *IntentResult, emotion *EmotionResult) (string, error) {

	// Load persona from database (tenant-specific)
	var persona storage.Persona
	if err := r.Memory.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.BotName = "Assistant"
	}

	contexts := r.Memory.SearchContext(ctx, tenantID, userMsg, 5)
	contextText := strings.Join(contexts, "\n- ")

	var biz storage.BusinessProfile
	_ = r.Memory.DB.First(&biz, "tenant_id = ?", tenantID).Error

	base := BuildBaseMessages(persona, biz)
	intro := BuildIntroTone(persona, biz)

	taskBuilder := strings.Builder{}
	taskBuilder.WriteString(intro)
	taskBuilder.WriteString("\n\nYou are responding naturally, using the detected intent and emotion to guide your reply.\n\n")
	taskBuilder.WriteString("WRITING STYLE - Write Like a Real Human:\n")
	taskBuilder.WriteString("- Answer directly first, then add context if needed\n")
	taskBuilder.WriteString("- Vary sentence length - mix short responses (\"Got it!\") with longer explanations\n")
	taskBuilder.WriteString("- Use natural transitions (\"So...\", \"Actually...\", \"Here's what I can do...\")\n")
	taskBuilder.WriteString("- Show you're listening (\"I hear you\", \"That makes sense\", \"I get it\")\n")
	taskBuilder.WriteString("- Match the detected emotion authentically - show genuine empathy or enthusiasm\n")
	taskBuilder.WriteString("- Use contractions and everyday language naturally\n")
	taskBuilder.WriteString("- Don't sound robotic, overly formal, or too polished\n")
	taskBuilder.WriteString("- Write like you're having a real conversation\n\n")
	taskBuilder.WriteString("AVOID ROBOTIC PATTERNS:\n")
	taskBuilder.WriteString("- Don't start every response the same way\n")
	taskBuilder.WriteString("- Don't use corporate clichés (\"Thank you for contacting us\", \"I understand your concern\")\n")
	taskBuilder.WriteString("- Vary your responses - each conversation should feel unique\n\n")
	taskBuilder.WriteString(fmt.Sprintf("Detected intent: %s\n", intent.Intent))
	taskBuilder.WriteString(fmt.Sprintf("Detected emotion: %s\n", emotion.Label))
	if strings.TrimSpace(contextText) != "" {
		taskBuilder.WriteString("Relevant context:\n")
		taskBuilder.WriteString(contextText)
		taskBuilder.WriteString("\n")
	}
	taskBuilder.WriteString(fmt.Sprintf("\nUser message: %s", userMsg))

	messages := append(base, Message{Role: "user", Content: taskBuilder.String()})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	reply = strings.TrimSpace(reply)

	// Remove em-dashes from anywhere in the message
	reply = removeEmDashes(reply)

	r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	return reply, nil
}

// detectAndSaveOffer detects if tenant is mentioning an offer and saves it naturally
// Returns true if an offer was detected and saved
func (r *Responder) detectAndSaveOffer(ctx context.Context, tenantID uuid.UUID, message string) bool {
	messageLower := strings.ToLower(message)

	// Check for offer-related keywords
	offerKeywords := []string{"offer", "deal", "discount", "promo", "special", "sale", "combo", "bundle", "buy", "get", "free", "off", "%"}
	hasOfferKeyword := false
	for _, keyword := range offerKeywords {
		if strings.Contains(messageLower, keyword) {
			hasOfferKeyword = true
			break
		}
	}

	if !hasOfferKeyword {
		return false
	}

	// Check if it's actually about adding an offer (not just mentioning offers in general)
	// Look for phrases like "tell customers", "offer", "deal", "special", etc.
	isAddingOffer := strings.Contains(messageLower, "tell customer") ||
		strings.Contains(messageLower, "tell them") ||
		strings.Contains(messageLower, "say") ||
		strings.Contains(messageLower, "offer:") ||
		strings.Contains(messageLower, "promo:") ||
		strings.Contains(messageLower, "deal:") ||
		strings.Contains(messageLower, "special:") ||
		strings.Contains(messageLower, "add offer") ||
		strings.Contains(messageLower, "new offer") ||
		strings.Contains(messageLower, "promotional") ||
		(messageLower != "" && (strings.Contains(messageLower, "buy") && strings.Contains(messageLower, "get")) ||
			strings.Contains(messageLower, "% off") ||
			strings.Contains(messageLower, "percent off"))

	if !isAddingOffer {
		return false
	}

	// Extract offer text
	offerText := strings.TrimSpace(message)

	// Remove common prefixes
	prefixes := []string{"tell customers", "tell them", "offer:", "offer", "add offer", "new offer", "promo:", "deal:", "special:", "promotional"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(offerText), strings.ToLower(prefix)) {
			offerText = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(offerText), strings.ToLower(prefix)))
			offerText = strings.TrimPrefix(offerText, ":")
			offerText = strings.TrimSpace(offerText)
			// Capitalize first letter
			if len(offerText) > 0 {
				offerText = strings.ToUpper(offerText[:1]) + offerText[1:]
			}
			break
		}
	}

	if offerText == "" || len(offerText) < 3 {
		return false
	}

	// Save offer
	offer := storage.TenantOffer{
		TenantID:    tenantID,
		Title:       offerText,
		Description: "",
		IsActive:    true,
	}

	if err := r.Memory.DB.Create(&offer).Error; err != nil {
		// Log but don't fail - offer might already exist or there's a DB issue
		return false
	}

	return true
}

// GenerateConversationalReply generates a general conversational reply
// Template: #7 GenerateConversationalReply (general conversational response)
func (r *Responder) GenerateConversationalReply(ctx context.Context, tenantID uuid.UUID, userMsg string) (string, error) {
	intentRes, _ := r.Classifier.Classify(ctx, tenantID, userMsg)
	emotionRes, _ := r.Emotion.Analyze(ctx, tenantID, userMsg)
	decision, _ := r.Policy.Evaluate(ctx, tenantID, intentRes, emotionRes)

	switch decision.Action {
	case "ask_clarify":
		return fmt.Sprintf("Just to confirm, did you mean %s?", intentRes.Intent), nil
	case "escalate":
		r.persistEscalation(tenantID, userMsg, intentRes, emotionRes)
		return "Got it, I'll have someone from the team look at this.", nil
	case "auto_reply":
		return r.GenerateHumanlikeReply(ctx, tenantID, userMsg, intentRes, emotionRes)
	default:
		return r.GenerateTenantAssistantReply(ctx, tenantID, userMsg)
	}
}

// 🔒 Persist unresolved or complex requests for human follow-up
func (r *Responder) persistEscalation(tenantID uuid.UUID, userMsg string, intent *IntentResult, emotion *EmotionResult) {
	esc := storage.Escalation{
		TenantID:   tenantID,
		Message:    userMsg,
		DraftReply: fmt.Sprintf("[intent=%s][emotion=%s]", intent.Intent, emotion.Label),
		Status:     "pending",
	}
	r.Memory.DB.Create(&esc)
}

// 🔧 GenerateTenantOwnerReply - For business owner interactions (management, help, insights)
func (r *Responder) GenerateTenantOwnerReply(ctx context.Context, tenantID uuid.UUID, userMsg string) (string, error) {
	if r == nil || r.Client == nil || r.Memory == nil {
		return "", fmt.Errorf("AI responder improperly initialized")
	}

	// Load persona from database (tenant-specific)
	var persona storage.Persona
	if err := r.Memory.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.BotName = "Assistant"
	}

	var biz storage.BusinessProfile
	_ = r.Memory.DB.First(&biz, "tenant_id = ?", tenantID).Error

	// Retrieve ALL tenant conversation history (no time limit, capped at 500 exchanges to avoid token overflow)
	conversationHistory := r.Memory.GetAllTenantConversationHistory(ctx, tenantID)

	base := BuildBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Template: #6 GenerateTenantOwnerReply (owner reply generation)
	// Enhanced with GPT-5-mini best practices for human-like responses
	intro := BuildIntroTone(persona, biz)
	task := fmt.Sprintf(`%s

You are {{owner_name}}, the owner of {{brand_name}}. You're replying to your assistant who just said: "{{assistant_message}}".

CONVERSATION CONTEXT:
- Previous messages: {{history}}
- You (the owner) want to say: "%s"

WRITING STYLE - Write Like a Real Business Owner:
1. **Be natural and authentic** - Write like you're texting a helpful colleague
2. **Show appreciation** - If they helped, acknowledge it naturally ("Thanks!", "Perfect!", "That works!")
3. **Be concise** - Business owners are busy - get to the point
4. **Use natural language** - Contractions, casual phrases, fragments when appropriate
5. **Match the tone** - If assistant is casual, be casual. If formal, be slightly more formal but still natural
6. **Be specific** - Instead of "Thanks", say "Thanks for checking that" or "Appreciate the help"

AVOID ROBOTIC PATTERNS:
- Don't sound overly formal or corporate
- Don't use phrases like "I appreciate your assistance", "Thank you for your support"
- Don't sound like a script - sound like a real person
- Vary your responses - each message should feel authentic

TASK: Write your reply as the business owner. Be appreciative, concise, and natural. Use the conversation history for context.`, intro, userMsg)

	messages = append(messages, Message{Role: "user", Content: task})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("AI generation failed: %w", err)
	}
	reply = strings.TrimSpace(reply)

	// Remove em-dashes from anywhere in the message
	reply = removeEmDashes(reply)

	// Save conversation
	if reply != "" {
		r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	}

	return reply, nil
}

// 👥 GenerateTenantAssistantReplyForCustomer - For customer interactions (brand representation)
func (r *Responder) GenerateTenantAssistantReplyForCustomer(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	userMsg string,
) (string, error) {
	if r == nil || r.Client == nil || r.Memory == nil {
		return "", fmt.Errorf("AI responder improperly initialized")
	}

	// 🧩 1️⃣ Load persona from database (tenant-specific)
	var persona storage.Persona
	if err := r.Memory.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.BotName = "Assistant"
	}

	var biz storage.BusinessProfile
	_ = r.Memory.DB.First(&biz, "tenant_id = ?", tenantID).Error

	// 🧩 2️⃣ Fetch customer info
	var cust storage.Customer
	_ = r.Memory.DB.First(&cust, "id = ?", customerID).Error
	displayName := cust.DisplayName
	if displayName == "" {
		displayName = "there"
	}

	// 🧠 3️⃣ Retrieve conversation history (last month for customers)
	conversationHistory := r.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30)

	// 🧠 4️⃣ Retrieve Knowledge Base Context (Menu, Policies, etc.)
	contexts := r.Memory.SearchContext(ctx, tenantID, userMsg, 5)
	contextText := strings.Join(contexts, "\n- ")

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	intro := BuildIntroTone(persona, biz)

	// Determine industry context for terminology (Menu vs Products vs Services)
	industryContext := "You are a business assistant."
	industry := strings.ToLower(fallback(biz.Industry, biz.Niche))

	if strings.Contains(industry, "food") || strings.Contains(industry, "restaurant") || strings.Contains(industry, "bakery") || strings.Contains(industry, "kitchen") {
		industryContext = "You are working for a Food & Beverage business. Refer to items as the 'Menu'. Be helpful like a server or front-desk staff."
	} else if strings.Contains(industry, "fashion") || strings.Contains(industry, "retail") || strings.Contains(industry, "store") || strings.Contains(industry, "shop") || strings.Contains(industry, "ecommerce") {
		industryContext = "You are working for a Retail/E-commerce business. Refer to items as 'Products' or 'Stock'. Be helpful like a sales associate."
	} else if strings.Contains(industry, "service") || strings.Contains(industry, "consulting") || strings.Contains(industry, "agency") || strings.Contains(industry, "tech") {
		industryContext = "You are working for a Service-based business. Refer to offerings as 'Services' or 'Packages'. Be professional and consultative."
	}

	task := fmt.Sprintf(`%s

Task: Reply to %s on %s.

CONTEXT: %s

CRITICAL INSTRUCTIONS:
1. **Knowledge Base**: Use the "Relevant Business Context" below to answer questions. It contains the menu, price list, and policies.
   - If the user asks for a menu, list the items found in the context.
   - If the user asks for a price, use the exact price from the context.
   - If the context does NOT contain the answer, say "I'll need to check on that" or escalate. Do NOT hallucinate products or prices.
2. **Orders**: Do NOT assume an order is placed unless the user explicitly says "I want to buy", "order this", etc.
   - If the user asks "Do you have cake?", answer "Yes we do" (if in context), do NOT say "Order placed".
   - Do NOT say "Order received" or "Total is..." unless the user confirmed the purchase.
3. **Tone**: Natural, warm, helpful. No robotic phrases.
	- Mirror the customer's language. Do NOT introduce slang, idioms, or Pidgin unless the customer already used similar language.

Relevant Business Context:
%s

Customer message: %s`,
		intro,
		displayName,
		strings.ToUpper(cust.Channel),
		industryContext,
		contextText,
		userMsg,
	)

	messages = append(messages, Message{Role: "user", Content: task})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("AI generation failed: %w", err)
	}
	reply = strings.TrimSpace(reply)

	// 🧩 6️⃣ Save conversation memory (per customer)
	if reply != "" {
		r.Memory.SaveCustomerConversation(ctx, tenantID, customerID, userMsg, reply)
	}

	return reply, nil
}
