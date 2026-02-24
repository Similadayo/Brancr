package ai

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// MemoryService manages semantic + contextual long-term memory
type MemoryService struct {
	DB      *gorm.DB
	Encoder *EmbeddingClient
}

// SaveConversation stores a user→AI message pair as a semantic memory (tenant owner conversation)
func (m *MemoryService) SaveConversation(ctx context.Context, tenantID uuid.UUID, userMsg, aiReply string) error {
	if strings.TrimSpace(userMsg) == "" || strings.TrimSpace(aiReply) == "" {
		return nil
	}

	combined := fmt.Sprintf("User: %s\nAI: %s", userMsg, aiReply)

	// Skip embedding generation for conversations (we use recency, not semantic search)
	// This saves API costs and avoids credit errors
	vec := []float32{}

	// Generate unique hash by including timestamp to allow duplicate conversations
	now := time.Now()
	uniqueContent := fmt.Sprintf("%s|%d", combined, now.UnixNano())
	hash := hashContext(uniqueContent)

	entry := storage.MemoryItem{
		TenantID:    tenantID,
		CustomerID:  uuid.Nil, // uuid.Nil means this is tenant owner conversation
		Text:        combined,
		Embedding:   vec,
		Category:    "conversation",
		ContextHash: hash, // Unique hash with timestamp
		CreatedAt:   now,
	}

	// Save each conversation message (each with unique hash)
	if err := m.DB.WithContext(ctx).Create(&entry).Error; err != nil {
		fmt.Printf("❌ Failed to save conversation for tenant %s: %v\n", tenantID, err)
		return fmt.Errorf("failed to store memory: %w", err)
	}

	fmt.Printf("💾 Saved conversation for tenant %s (user: %.50s...)\n", tenantID, userMsg)
	return nil
}

// SaveFact stores long-term business or persona knowledge (e.g. description, tone)
func (m *MemoryService) SaveFact(ctx context.Context, tenantID uuid.UUID, content, category string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	hash := hashContext(content)
	vec, err := m.Encoder.GenerateEmbedding(ctx, content)
	if err != nil {
		return fmt.Errorf("embedding generation failed: %w", err)
	}

	item := storage.MemoryItem{
		TenantID:    tenantID,
		Text:        content,
		Embedding:   vec,
		Category:    category,
		ContextHash: hash,
		CreatedAt:   time.Now(),
	}

	if err := m.DB.WithContext(ctx).
		Where("context_hash = ? AND tenant_id = ?", hash, tenantID).
		FirstOrCreate(&item).Error; err != nil {
		return fmt.Errorf("failed to save fact: %w", err)
	}

	return nil
}

// SearchContext retrieves top-N similar conversation or fact memories
func (m *MemoryService) SearchContext(ctx context.Context, tenantID uuid.UUID, query string, limit int) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	queryVec, err := m.Encoder.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil
	}

	var items []storage.MemoryItem
	m.DB.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Limit(500). // broader scope for recall
		Find(&items)

	type scored struct {
		Text  string
		Score float64
	}
	var scoredItems []scored

	for _, it := range items {
		if len(it.Embedding) == len(queryVec) {
			sim := cosineSimilarity(it.Embedding, queryVec)
			if sim > 0.25 { // ignore low-similarity noise
				scoredItems = append(scoredItems, scored{Text: it.Text, Score: sim})
			}
		}
	}

	sort.Slice(scoredItems, func(i, j int) bool { return scoredItems[i].Score > scoredItems[j].Score })

	var results []string
	for i, s := range scoredItems {
		if i >= limit {
			break
		}
		results = append(results, s.Text)
	}
	return results
}

// StoreContext saves user input + reply into persistent memory
func (m *MemoryService) StoreContext(ctx context.Context, tenantID uuid.UUID, userMsg, aiReply string) {
	if err := m.SaveConversation(ctx, tenantID, userMsg, aiReply); err != nil {
		fmt.Printf("⚠️ Failed to save memory: %v\n", err)
	}
}

// cosineSimilarity returns the cosine similarity between two float32 vectors
func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// hashContext generates a SHA1 hash of a string (for deduplication)
func hashContext(s string) string {
	h := sha1.New()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(s))))
	return hex.EncodeToString(h.Sum(nil))
}

// SaveCustomerConversation stores chat memory scoped to a customer
func (m *MemoryService) SaveCustomerConversation(ctx context.Context, tenantID, customerID uuid.UUID, userMsg, aiReply string) {
	if strings.TrimSpace(userMsg) == "" || strings.TrimSpace(aiReply) == "" {
		return
	}
	text := fmt.Sprintf("User: %s\nAssistant: %s", userMsg, aiReply)

	// Skip embedding generation for conversations (we use recency/time, not semantic search)
	// This saves API costs and avoids credit errors
	vec := []float32{}

	// Generate unique hash with timestamp
	now := time.Now()
	uniqueContent := fmt.Sprintf("%s|%d|%d", text, customerID, now.UnixNano())
	hash := hashContext(uniqueContent)

	item := storage.MemoryItem{
		TenantID:    tenantID,
		CustomerID:  customerID,
		Text:        text,
		Embedding:   vec,
		Category:    "conversation",
		ContextHash: hash,
		CreatedAt:   now,
	}

	if err := m.DB.Create(&item).Error; err != nil {
		fmt.Printf("❌ Failed to save customer conversation: %v\n", err)
	} else {
		fmt.Printf("💾 Saved customer %s conversation (user: %.50s...)\n", customerID, userMsg)
	}
}

// SaveCustomerContext saves a text + embedding for a specific customer
func (m *MemoryService) SaveCustomerContext(ctx context.Context, tenantID, customerID uuid.UUID, text string, category string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	vec, err := m.Encoder.GenerateEmbedding(ctx, text)
	if err != nil {
		return fmt.Errorf("embedding generation failed: %w", err)
	}

	item := storage.MemoryItem{
		TenantID:   tenantID,
		CustomerID: customerID,
		Text:       text,
		Embedding:  vec,
		Category:   category,
		CreatedAt:  time.Now(),
	}
	return m.DB.WithContext(ctx).Create(&item).Error
}

// SaveCustomerFact stores a lightweight, tenant-scoped customer fact WITHOUT embeddings.
// This is used for durable preferences (e.g., "prefers pay on delivery") without extra AI cost.
// Facts should be confirmed by behavior (not inferred emotions).
func (m *MemoryService) SaveCustomerFact(ctx context.Context, tenantID, customerID uuid.UUID, fact string, category string) error {
	if m == nil || m.DB == nil {
		return nil
	}
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return nil
	}
	if strings.TrimSpace(category) == "" {
		category = "customer_fact"
	}

	now := time.Now()
	uniqueContent := fmt.Sprintf("%s|%s|%s|%d", fact, tenantID, customerID, now.UnixNano())
	hash := hashContext(uniqueContent)

	item := storage.MemoryItem{
		TenantID:    tenantID,
		CustomerID:  customerID,
		Text:        fact,
		Embedding:   []float32{},
		Category:    category,
		ContextHash: hash,
		CreatedAt:   now,
	}
	return m.DB.WithContext(ctx).Create(&item).Error
}

// UpsertCustomerConversationSummary stores/updates a lightweight conversation summary WITHOUT embeddings.
// This is session-level memory intended to prevent repeated questions and keep continuity.
func (m *MemoryService) UpsertCustomerConversationSummary(ctx context.Context, tenantID, customerID, conversationID uuid.UUID, summary string) error {
	if m == nil || m.DB == nil || conversationID == uuid.Nil {
		return nil
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	// Deterministic hash so we update a single row per conversation.
	hash := hashContext(fmt.Sprintf("conv_summary:%s:%s:%s", tenantID, customerID, conversationID))

	now := time.Now()
	item := storage.MemoryItem{
		TenantID:    tenantID,
		CustomerID:  customerID,
		Text:        summary,
		Embedding:   []float32{},
		Category:    "conversation_summary",
		ContextHash: hash,
		CreatedAt:   now,
	}

	// Upsert by context hash.
	var existing storage.MemoryItem
	if err := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND context_hash = ? AND category = ?",
			tenantID, customerID, hash, "conversation_summary").
		First(&existing).Error; err == nil {
		existing.Text = summary
		existing.CreatedAt = now
		return m.DB.WithContext(ctx).Save(&existing).Error
	}
	return m.DB.WithContext(ctx).Create(&item).Error
}

// SearchCustomerContext searches memories for a given customer within a tenant
func (m *MemoryService) SearchCustomerContext(ctx context.Context, tenantID, customerID uuid.UUID, query string, limit int) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	queryVec, err := m.Encoder.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil
	}

	var items []storage.MemoryItem
	m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		Order("created_at DESC").
		Limit(150).
		Find(&items)

	type scored struct {
		Text  string
		Score float64
	}
	var scoredItems []scored
	for _, it := range items {
		if len(it.Embedding) == len(queryVec) {
			sim := cosineSimilarity(it.Embedding, queryVec)
			scoredItems = append(scoredItems, scored{Text: it.Text, Score: sim})
		}
	}
	sort.Slice(scoredItems, func(i, j int) bool { return scoredItems[i].Score > scoredItems[j].Score })

	var results []string
	for i, s := range scoredItems {
		if i >= limit {
			break
		}
		results = append(results, s.Text)
	}
	return results
}

// GetRecentConversationHistory retrieves the last N conversation messages (chronological)
func (m *MemoryService) GetRecentConversationHistory(ctx context.Context, tenantID, customerID uuid.UUID, limit int) []Message {
	var items []storage.MemoryItem
	m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND category = ?", tenantID, customerID, "conversation").
		Order("created_at DESC").
		Limit(limit).
		Find(&items)

	// Reverse to get chronological order (oldest first)
	var messages []Message
	for i := len(items) - 1; i >= 0; i-- {
		text := items[i].Text
		// Parse "User: X\nAssistant: Y" format
		parts := strings.Split(text, "\nAssistant: ")
		if len(parts) == 2 {
			userMsg := strings.TrimPrefix(parts[0], "User: ")
			aiMsg := parts[1]
			messages = append(messages, Message{Role: "user", Content: userMsg})
			messages = append(messages, Message{Role: "assistant", Content: aiMsg})
		}
	}
	return messages
}

// GetRecentCustomerConversationWithTimeLimit retrieves customer conversations from the last N days
// Uses Interaction records as primary source (more reliable) with MemoryItem as fallback
// If conversationID != uuid.Nil, filters by conversation for more precise history
func (m *MemoryService) GetRecentCustomerConversationWithTimeLimit(ctx context.Context, tenantID, customerID uuid.UUID, days int) []Message {
	return m.GetRecentCustomerConversationWithTimeLimitAndConversation(ctx, tenantID, customerID, uuid.Nil, days)
}

// GetRecentCustomerConversationWithTimeLimitAndConversation retrieves customer conversations with optional conversation filter
func (m *MemoryService) GetRecentCustomerConversationWithTimeLimitAndConversation(ctx context.Context, tenantID, customerID, conversationID uuid.UUID, days int) []Message {
	cutoffDate := time.Now().AddDate(0, 0, -days)

	// PRIMARY SOURCE: Use Interaction records (created immediately, more reliable)
	var interactions []storage.Interaction
	query := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND created_at >= ?",
			tenantID, customerID, cutoffDate)

	// If conversationID is provided, filter by it for more precise history
	if conversationID != uuid.Nil {
		query = query.Where("conversation_id = ?", conversationID)
	}

	result := query.
		Order("created_at ASC"). // Chronological order (oldest first)
		Limit(400).              // Max 400 messages (200 pairs) to avoid token overflow
		Find(&interactions)

	var messages []Message

	if result.Error == nil && len(interactions) > 0 {
		// Build messages from interactions
		for _, interaction := range interactions {
			if interaction.Direction == "incoming" {
				// Customer message
				if strings.TrimSpace(interaction.Content) != "" {
					messages = append(messages, Message{Role: "user", Content: interaction.Content})
				}
			} else if interaction.Direction == "outgoing" {
				// AI reply - use FinalReply if available, otherwise Content
				reply := interaction.FinalReply
				if reply == "" {
					reply = interaction.Content
				}
				if strings.TrimSpace(reply) != "" {
					messages = append(messages, Message{Role: "assistant", Content: reply})
				}
			}
		}

		fmt.Printf("📚 Retrieved %d interactions for customer %s (last %d days), converted to %d messages\n",
			len(interactions), customerID, days, len(messages))

		// If we got messages from interactions, return them
		if len(messages) > 0 {
			return messages
		}
	}

	// FALLBACK: Use MemoryItem records (for backward compatibility)
	var items []storage.MemoryItem
	result = m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND category = ? AND created_at >= ?",
			tenantID, customerID, "conversation", cutoffDate).
		Order("created_at DESC").
		Limit(200). // Max 200 conversation pairs (400 messages) to avoid token overflow
		Find(&items)

	if result.Error != nil {
		fmt.Printf("⚠️ Error retrieving customer conversation history: %v\n", result.Error)
		return messages // Return whatever we got from interactions
	}

	fmt.Printf("📚 Retrieved %d memory items for customer %s (last %d days)\n", len(items), customerID, days)

	// Reverse to get chronological order (oldest first)
	for i := len(items) - 1; i >= 0; i-- {
		text := items[i].Text
		// Parse "User: X\nAssistant: Y" format
		parts := strings.Split(text, "\nAssistant: ")
		if len(parts) == 2 {
			userMsg := strings.TrimPrefix(parts[0], "User: ")
			aiMsg := parts[1]
			messages = append(messages, Message{Role: "user", Content: userMsg})
			messages = append(messages, Message{Role: "assistant", Content: aiMsg})
		}
	}

	fmt.Printf("📝 Converted to %d total messages for customer context (from last %d days)\n", len(messages), days)
	return messages
}

// GetRecentTenantConversationHistory retrieves tenant conversation history (for backward compatibility)
func (m *MemoryService) GetRecentTenantConversationHistory(ctx context.Context, tenantID uuid.UUID, limit int) []Message {
	return m.GetAllTenantConversationHistory(ctx, tenantID)
}

// GetAllTenantConversationHistory retrieves ALL tenant conversation history (no time limit)
func (m *MemoryService) GetAllTenantConversationHistory(ctx context.Context, tenantID uuid.UUID) []Message {
	var items []storage.MemoryItem
	result := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND category = ?", tenantID, uuid.Nil, "conversation").
		Order("created_at DESC").
		Limit(500). // Cap at 500 exchanges (1000 messages) to avoid AI token overflow
		Find(&items)

	if result.Error != nil {
		fmt.Printf("⚠️ Error retrieving tenant conversation history: %v\n", result.Error)
		return []Message{}
	}

	fmt.Printf("📚 Retrieved ALL conversation items for tenant %s: %d exchanges (no time limit)\n", tenantID, len(items))

	var messages []Message
	for i := len(items) - 1; i >= 0; i-- {
		text := items[i].Text
		parts := strings.Split(text, "\nAI: ")
		if len(parts) == 2 {
			userMsg := strings.TrimPrefix(parts[0], "User: ")
			aiMsg := parts[1]
			messages = append(messages, Message{Role: "user", Content: userMsg})
			messages = append(messages, Message{Role: "assistant", Content: aiMsg})
		}
	}

	fmt.Printf("📝 Converted to %d messages for tenant context (ALL conversations)\n", len(messages))
	return messages
}

// GetCustomerInsight retrieves the customer insight/personality summary
func (m *MemoryService) GetCustomerInsight(ctx context.Context, tenantID, customerID uuid.UUID) *storage.CustomerInsight {
	if m == nil || m.DB == nil {
		return nil
	}

	var insight storage.CustomerInsight
	if err := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		First(&insight).Error; err != nil {
		return nil // No insight found yet
	}

	return &insight
}

// UpdateCustomerInsight updates or creates a customer insight record
// The summary should be AI-generated based on conversation patterns
func (m *MemoryService) UpdateCustomerInsight(ctx context.Context, tenantID, customerID uuid.UUID, summary string, buyerType string, tags []string) error {
	if m == nil || m.DB == nil {
		return nil
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	// Convert tags to JSON string
	tagsJSON := ""
	if len(tags) > 0 {
		tagsJSON = fmt.Sprintf(`["%s"]`, strings.Join(tags, `","`))
	}

	now := time.Now()

	// Try to find existing insight
	var existing storage.CustomerInsight
	err := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		First(&existing).Error

	if err == nil {
		// Update existing
		existing.Summary = summary
		existing.BuyerType = buyerType
		existing.Tags = tagsJSON
		existing.LastUpdated = now
		return m.DB.WithContext(ctx).Save(&existing).Error
	}

	// Create new
	insight := storage.CustomerInsight{
		TenantID:    tenantID,
		CustomerID:  customerID,
		Summary:     summary,
		Tags:        tagsJSON,
		BuyerType:   buyerType,
		LastUpdated: now,
	}
	return m.DB.WithContext(ctx).Create(&insight).Error
}

// InsightExtraction represents the structure we ask the LLM to return
type InsightExtraction struct {
	Summary   string   `json:"summary"`    // One-liner: "Price-sensitive, prefers delivery"
	BuyerType string   `json:"buyer_type"` // "premium", "budget", "undecided"
	Tags      []string `json:"tags"`       // ["price_sensitive", "repeat_buyer"]
}

// GenerateInsightFromConversation uses LLM to analyze conversation and extract customer traits
// This is called asynchronously after conversations to build customer profile
func (m *MemoryService) GenerateInsightFromConversation(
	ctx context.Context,
	client AIClientInterface,
	tenantID, customerID uuid.UUID,
	recentMessages []Message,
	lastAction string,
) error {
	if m == nil || m.DB == nil || client == nil {
		return nil
	}

	// Need at least 2 messages for meaningful insight
	if len(recentMessages) < 2 {
		return nil
	}

	// Build conversation text for analysis
	var convText strings.Builder
	for _, msg := range recentMessages {
		// Cast Content to string (it's interface{} for vision support)
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}
		if msg.Role == "user" {
			convText.WriteString("Customer: " + content + "\n")
		} else {
			convText.WriteString("AI: " + content + "\n")
		}
	}

	prompt := fmt.Sprintf(`Analyze this customer conversation and extract personality insights.

Conversation:
%s

Last AI action: %s

Return JSON with:
- summary: One short sentence describing the customer (e.g., "Price-sensitive, prefers cash on delivery, repeat buyer")
- buyer_type: "premium" (doesn't negotiate, quick decisions), "budget" (negotiates, price-focused), or "undecided" (still exploring)
- tags: Array of traits like ["price_sensitive", "repeat_buyer", "prefers_delivery", "quick_decider", "needs_reassurance"]

Only include traits clearly shown in the conversation. Be concise.`, convText.String(), lastAction)

	messages := []Message{
		{Role: "user", Content: prompt},
	}

	response, err := client.Generate(ctx, messages)
	if err != nil {
		return fmt.Errorf("LLM insight extraction failed: %w", err)
	}

	// Parse response
	var extraction InsightExtraction
	if err := parseJSONResponse(response, &extraction); err != nil {
		return fmt.Errorf("failed to parse insight extraction: %w", err)
	}

	// Skip if no meaningful insight
	if extraction.Summary == "" {
		return nil
	}

	// Save to database
	return m.UpdateCustomerInsight(ctx, tenantID, customerID, extraction.Summary, extraction.BuyerType, extraction.Tags)
}

// parseJSONResponse extracts JSON from an LLM response (handles markdown code blocks)
func parseJSONResponse(response string, v interface{}) error {
	response = strings.TrimSpace(response)

	// Remove markdown code blocks if present
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		response = strings.TrimSuffix(response, "```")
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
	}

	response = strings.TrimSpace(response)

	// Try to parse as JSON
	decoder := json.NewDecoder(strings.NewReader(response))
	return decoder.Decode(v)
}
