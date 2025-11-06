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

// 🧠 GenerateTenantAssistantReply — Human-like, Contextual, Brand-Aware
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

	// Compose a natural, concise system prompt
	systemPrompt := fmt.Sprintf(`
You're %s from %s.

About the business: %s
Your tone: %s

Talk like a real person. Be casual, friendly, and natural. Use contractions and everyday language. Don't sound like a customer service bot.
`, persona.BotName,
		biz.Name,
		biz.Description,
		persona.Tone,
	)

	// Build messages with conversation history
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	
	// Add conversation history
	messages = append(messages, conversationHistory...)
	
	// Add current user message
	messages = append(messages, Message{Role: "user", Content: userMsg})
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
	moodHint := fmt.Sprintf("User emotion: %s", emotion.Label)

	systemPrompt := fmt.Sprintf(`
You're %s.

Context: %s
Tone: %s
%s
Previous conversation: %s

Talk like a real person. Be casual and natural. Use everyday language.
`, persona.BotName, intent.Intent, persona.Tone, moodHint, contextText)

	reply, err := r.Client.Generate(ctx, []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	})
	if err != nil {
		return "", err
	}

	reply = strings.TrimSpace(reply)
	r.Memory.SaveConversation(ctx, tenantID, userMsg, reply)
	return reply, nil
}

// 🗣️ Adaptive conversational handler
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

// 🔧 GenerateTenantOwnerReply — For business owner interactions (management, help, insights)
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

	// Owner-focused system prompt
	systemPrompt := fmt.Sprintf(`
You're %s, helping %s manage their business.

Important: You're talking to the BUSINESS OWNER, not a customer. Don't sell them anything - just help them run things.

About the business: %s

You can help with:
- Connecting accounts (they might say "connect instagram" or "link my Facebook")
- Scheduling posts (they can send photos/videos and schedule them)
- Checking things out ("verify instagram" or "show me my stats")
- General help

Talk like a real person helping a friend. Be casual, friendly, and genuinely helpful. Use natural language and contractions. Don't sound formal or robotic.
`,
		persona.BotName, biz.Name,
		biz.Description,
	)

	// Build messages with conversation history
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	
	// Add conversation history
	messages = append(messages, conversationHistory...)
	
	// Add current user message
	messages = append(messages, Message{Role: "user", Content: userMsg})

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

// 👥 GenerateTenantAssistantReplyForCustomer — For customer interactions (brand representation)
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

	// 🧩 4️⃣ Customer-facing system prompt with platform-specific formatting
	platformFormat := getPlatformFormatting(cust.Channel)
	
	systemPrompt := fmt.Sprintf(`
You're %s from %s.

About the business: %s
Your tone: %s

You're chatting with a customer. Talk like a real person who works here - be genuine, friendly, and helpful. Use natural language, contractions, and sound human. Don't be overly formal.
%s
`,
		persona.BotName, biz.Name,
		biz.Description, persona.Tone,
		platformFormat,
	)

	// 🗣️ 5️⃣ Build messages with conversation history
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	
	// Add conversation history
	messages = append(messages, conversationHistory...)
	
	// Add current user message
	messages = append(messages, Message{Role: "user", Content: userMsg})

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
