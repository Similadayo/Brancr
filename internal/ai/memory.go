package ai

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// MemoryService manages semantic + contextual long-term memory
type MemoryService struct {
	DB      *gorm.DB
	Encoder *EmbeddingClient
}

// SaveConversation stores a user→AI message pair as a semantic memory (tenant owner conversation)
func (m *MemoryService) SaveConversation(ctx context.Context, tenantID uint, userMsg, aiReply string) error {
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
		CustomerID:  0, // 0 means this is tenant owner conversation
		Text:        combined,
		Embedding:   vec,
		Category:    "conversation",
		ContextHash: hash, // Unique hash with timestamp
		CreatedAt:   now,
	}

	// Save each conversation message (each with unique hash)
	if err := m.DB.WithContext(ctx).Create(&entry).Error; err != nil {
		fmt.Printf("❌ Failed to save conversation for tenant %d: %v\n", tenantID, err)
		return fmt.Errorf("failed to store memory: %w", err)
	}
	
	fmt.Printf("💾 Saved conversation for tenant %d (user: %.50s...)\n", tenantID, userMsg)
	return nil
}

// SaveFact stores long-term business or persona knowledge (e.g. description, tone)
func (m *MemoryService) SaveFact(ctx context.Context, tenantID uint, content, category string) error {
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
func (m *MemoryService) SearchContext(ctx context.Context, tenantID uint, query string, limit int) []string {
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
func (m *MemoryService) StoreContext(ctx context.Context, tenantID uint, userMsg, aiReply string) {
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
func (m *MemoryService) SaveCustomerConversation(ctx context.Context, tenantID, customerID uint, userMsg, aiReply string) {
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
		fmt.Printf("💾 Saved customer %d conversation (user: %.50s...)\n", customerID, userMsg)
	}
}

// SaveCustomerContext saves a text + embedding for a specific customer
func (m *MemoryService) SaveCustomerContext(ctx context.Context, tenantID, customerID uint, text string, category string) error {
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

// SearchCustomerContext searches memories for a given customer within a tenant
func (m *MemoryService) SearchCustomerContext(ctx context.Context, tenantID, customerID uint, query string, limit int) []string {
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
func (m *MemoryService) GetRecentConversationHistory(ctx context.Context, tenantID, customerID uint, limit int) []Message {
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
// If conversationID > 0, filters by conversation for more precise history
func (m *MemoryService) GetRecentCustomerConversationWithTimeLimit(ctx context.Context, tenantID, customerID uint, days int) []Message {
	return m.GetRecentCustomerConversationWithTimeLimitAndConversation(ctx, tenantID, customerID, 0, days)
}

// GetRecentCustomerConversationWithTimeLimitAndConversation retrieves customer conversations with optional conversation filter
func (m *MemoryService) GetRecentCustomerConversationWithTimeLimitAndConversation(ctx context.Context, tenantID, customerID, conversationID uint, days int) []Message {
	cutoffDate := time.Now().AddDate(0, 0, -days)
	
	// PRIMARY SOURCE: Use Interaction records (created immediately, more reliable)
	var interactions []storage.Interaction
	query := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND created_at >= ?", 
			tenantID, customerID, cutoffDate)
	
	// If conversationID is provided, filter by it for more precise history
	if conversationID > 0 {
		query = query.Where("conversation_id = ?", conversationID)
	}
	
	result := query.
		Order("created_at ASC"). // Chronological order (oldest first)
		Limit(400). // Max 400 messages (200 pairs) to avoid token overflow
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
		
		fmt.Printf("📚 Retrieved %d interactions for customer %d (last %d days), converted to %d messages\n", 
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

	fmt.Printf("📚 Retrieved %d memory items for customer %d (last %d days)\n", len(items), customerID, days)

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
func (m *MemoryService) GetRecentTenantConversationHistory(ctx context.Context, tenantID uint, limit int) []Message {
	return m.GetAllTenantConversationHistory(ctx, tenantID)
}

// GetAllTenantConversationHistory retrieves ALL tenant conversation history (no time limit)
func (m *MemoryService) GetAllTenantConversationHistory(ctx context.Context, tenantID uint) []Message {
	var items []storage.MemoryItem
	result := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = 0 AND category = ?", tenantID, "conversation").
		Order("created_at DESC").
		Limit(500). // Cap at 500 exchanges (1000 messages) to avoid AI token overflow
		Find(&items)

	if result.Error != nil {
		fmt.Printf("⚠️ Error retrieving tenant conversation history: %v\n", result.Error)
		return []Message{}
	}

	fmt.Printf("📚 Retrieved ALL conversation items for tenant %d: %d exchanges (no time limit)\n", tenantID, len(items))

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