package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
)

// Responder combines AI, persona, memory, and business context for lifelike conversations
type Responder struct {
	Client     *AIClient
	Memory     *MemoryService
	Persona    storage.Persona
	Classifier *Classifier
	Emotion    *EmotionAnalyzer
	Policy     *PolicyEngine
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
func (r *Responder) GenerateTenantAssistantReply(ctx context.Context, tenantID uint, userMsg string) (string, error) {
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

	// Template: #5 GenerateTenantAssistantReply (tenant-facing reply generation)
	// Enhanced with GPT-5-mini best practices for human-like responses
	intro := BuildIntroTone(persona, biz)
	task := fmt.Sprintf(`%s

You are responding as a real assistant helping the business owner. Write like you're texting a colleague - natural, helpful, and genuinely supportive.

CONVERSATION CONTEXT:
- Previous messages: {{history}}
- Business owner just said: "%s"
- Brand voice: {{brand_tone}}

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

TASK: Reply naturally, like a real person would. Address what they asked directly. Be helpful, warm, and solution-focused. Use the conversation history for context.`, intro, userMsg)

	messages = append(messages, Message{Role: "user", Content: task})
	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("AI generation failed: %w", err)
	}

	reply = strings.TrimSpace(reply)

	// Save memory (user + AI pair)
	if reply != "" {
		r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	}

	return reply, nil
}

// 🔁 Emotion & Intent aware response path
func (r *Responder) GenerateHumanlikeReply(ctx context.Context, tenantID uint, userMsg string,
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
	r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	return reply, nil
}

// GenerateConversationalReply generates a general conversational reply
// Template: #7 GenerateConversationalReply (general conversational response)
func (r *Responder) GenerateConversationalReply(ctx context.Context, tenantID uint, userMsg string) (string, error) {
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
func (r *Responder) persistEscalation(tenantID uint, userMsg string, intent *IntentResult, emotion *EmotionResult) {
	esc := storage.Escalation{
		TenantID:   tenantID,
		Message:    userMsg,
		DraftReply: fmt.Sprintf("[intent=%s][emotion=%s]", intent.Intent, emotion.Label),
		Status:     "pending",
	}
	r.Memory.DB.Create(&esc)
}

// 🔧 GenerateTenantOwnerReply - For business owner interactions (management, help, insights)
func (r *Responder) GenerateTenantOwnerReply(ctx context.Context, tenantID uint, userMsg string) (string, error) {
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

	// Save conversation
	if reply != "" {
		r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	}

	return reply, nil
}

// 👥 GenerateTenantAssistantReplyForCustomer - For customer interactions (brand representation)
func (r *Responder) GenerateTenantAssistantReplyForCustomer(
	ctx context.Context,
	tenantID uint,
	customerID uint,
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

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	intro := BuildIntroTone(persona, biz)
	platformFormat := getPlatformFormatting(cust.Channel)
	task := fmt.Sprintf(`%s

Task: Reply to %s on %s.

CRITICAL: Write like a real human customer service rep would - natural, warm, and genuinely helpful.
- Vary your sentence structure (mix short responses with longer explanations when needed)
- Use contractions and conversational language naturally
- Match their energy - if they're frustrated, be empathetic; if they're casual, be casual
- Show genuine care, not just politeness
- Don't sound like a script or corporate template
- Write like you're texting a friend who's a customer

NIGERIAN ENGLISH AWARENESS: Many customers may use Nigerian English or Pidgin patterns. 

Style: Casual professional - friendly, approachable, use SIMPLE vocabulary (avoid complex words). Follow Nigerian conversation patterns - direct, warm, natural flow.

Address customers respectfully - use their name when available (e.g., "Good morning John") or "sir/ma" when you can determine gender. If gender is unclear, use their name directly or neutral friendly language. Understand common phrases like "How far?", "Well done", "I am coming", and that "sorry" means empathy/acknowledgment, not just apology. 

CRITICAL: 
- NEVER respond in Pidgin English
- Use SIMPLE, everyday words - keep it easy to understand
- Casual professional tone - friendly, not stiff
- Nigerian conversation pattern - direct, warm, natural
- Do NOT use Pidgin words like "sha", "abi", "na so", "dey", "go come", etc.
- Use customer's name when available - more personal and respectful
- If uncertain about gender, use name directly - it's okay to not use sir/ma

Use the conversation history above to stay consistent.

Formatting notes: %s

Customer message: %s`,
		intro,
		displayName,
		strings.ToUpper(cust.Channel),
		platformFormat,
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
