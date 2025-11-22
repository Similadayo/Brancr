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

	base := BuildBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	intro := BuildIntroTone(persona, biz)
	task := fmt.Sprintf(`%s

Task: Reply to the latest message from the business team. Stay human, warm, and efficient. Use the conversation history above for context.

Latest message: %s`, intro, userMsg)

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
	taskBuilder.WriteString("\n\nTask: Reply to the latest message using the intent and emotion cues provided. Stay natural and on-brand.\n")
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

	base := BuildBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	intro := BuildIntroTone(persona, biz)
	task := fmt.Sprintf(`%s

Task: Reply to the business owner. Help them run operations—think connecting accounts, scheduling posts, checking stats, or answering workflow questions. Stay human, candid, and proactive.

Owner message: %s`, intro, userMsg)

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

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	intro := BuildIntroTone(persona, biz)
	platformFormat := getPlatformFormatting(cust.Channel)
	task := fmt.Sprintf(`%s

Task: Reply to %s on %s. Be human, warm, and helpful. Use the conversation history above to stay consistent.

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
