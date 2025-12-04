package ai

import (
	"context"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CustomerInfoExtractor extracts customer information from conversations
type CustomerInfoExtractor struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewCustomerInfoExtractor creates a new CustomerInfoExtractor
func NewCustomerInfoExtractor(db *gorm.DB, logger *zap.Logger) *CustomerInfoExtractor {
	return &CustomerInfoExtractor{
		DB:     db,
		Logger: logger,
	}
}

// ExtractCustomerInfo extracts customer details from conversation and customer model
func (e *CustomerInfoExtractor) ExtractCustomerInfo(
	ctx context.Context,
	tenantID uint,
	customerID uint,
	conversationID uint,
	platform string,
) (name, phone, email string, err error) {
	// First, try to get from Customer model
	var customer storage.Customer
	if err := e.DB.Where("id = ? AND tenant_id = ?", customerID, tenantID).
		First(&customer).Error; err == nil {
		name = customer.DisplayName
		if name == "" {
			name = customer.Username
		}
		// Phone and email are not in Customer model, will extract from conversation
	}

	// Try to extract from conversation history
	// Get recent interactions to find customer info
	var interactions []storage.Interaction
	if err := e.DB.Where("conversation_id = ? AND direction = ?", conversationID, "incoming").
		Order("created_at DESC").
		Limit(20).
		Find(&interactions).Error; err == nil {
		for _, interaction := range interactions {
			content := strings.ToLower(interaction.Content)

			// Extract phone number patterns
			if phone == "" {
				phone = e.extractPhoneNumber(content)
			}

			// Extract email patterns
			if email == "" {
				email = e.extractEmail(content)
			}

			// Extract name if mentioned
			if name == "" || name == customer.Username {
				name = e.extractName(content, customer.Username)
			}

			// If we found all, break
			if phone != "" && email != "" && name != "" && name != customer.Username {
				break
			}
		}
	}

	// Fallback: use customer display name or username
	if name == "" {
		if customer.DisplayName != "" {
			name = customer.DisplayName
		} else if customer.Username != "" {
			name = customer.Username
		} else {
			name = "Customer"
		}
	}

	return name, phone, email, nil
}

// extractPhoneNumber extracts phone number from text
func (e *CustomerInfoExtractor) extractPhoneNumber(text string) string {
	// Common patterns: +234..., 080..., 081..., etc.
	// This is a simple extraction - can be enhanced with regex
	words := strings.Fields(text)
	for _, word := range words {
		// Remove common punctuation
		word = strings.Trim(word, ".,!?;:")
		
		// Check if it looks like a phone number
		if strings.HasPrefix(word, "+") && len(word) >= 10 {
			return word
		}
		if strings.HasPrefix(word, "0") && len(word) >= 10 && len(word) <= 11 {
			return word
		}
		// Nigerian phone patterns
		if strings.HasPrefix(word, "234") && len(word) >= 13 {
			return "+" + word
		}
	}
	return ""
}

// extractEmail extracts email from text
func (e *CustomerInfoExtractor) extractEmail(text string) string {
	words := strings.Fields(text)
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if strings.Contains(word, "@") && strings.Contains(word, ".") {
			return word
		}
	}
	return ""
}

// extractName extracts name from text (simple heuristic)
func (e *CustomerInfoExtractor) extractName(text, fallback string) string {
	// Look for patterns like "my name is...", "I am...", "call me..."
	lowerText := strings.ToLower(text)
	
	patterns := []string{
		"my name is",
		"i am",
		"i'm",
		"call me",
		"this is",
	}
	
	for _, pattern := range patterns {
		if idx := strings.Index(lowerText, pattern); idx != -1 {
			// Extract text after pattern
			after := text[idx+len(pattern):]
			words := strings.Fields(after)
			if len(words) > 0 {
				// Take first 2-3 words as potential name
				name := strings.Join(words[:min(3, len(words))], " ")
				// Clean up
				name = strings.Trim(name, ".,!?;:")
				if len(name) > 2 && len(name) < 50 {
					return name
				}
			}
		}
	}
	
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

