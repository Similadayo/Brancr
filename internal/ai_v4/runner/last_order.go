package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

func isLastOrderRequest(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}

	phrases := []string{
		"my last order",
		"last order",
		"previous order",
		"my previous order",
		"recent order",
		"my recent order",
		"last purchase",
		"previous purchase",
		"what did i order last",
		"what was my last purchase",
	}
	for _, p := range phrases {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

func (r *FlowRunner) buildLastOrderReply(ctx context.Context, tenantID uuid.UUID, conversationID uuid.UUID) (string, error) {
	if r == nil || r.deps.DB == nil {
		return "", nil
	}

	var conv storage.Conversation
	if err := r.deps.DB.WithContext(ctx).
		Select("customer_id").
		Where("id = ? AND tenant_id = ?", conversationID, tenantID).
		First(&conv).Error; err != nil {
		return "", nil
	}
	if conv.CustomerID == uuid.Nil {
		return "I can help with that, but I can’t find your customer record yet. What would you like to order today?", nil
	}

	statuses := []string{"pending", "confirmed", "paid", "verified", "processing", "completed"}
	var ord storage.Order
	if err := r.deps.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND status IN ?", tenantID, conv.CustomerID, statuses).
		Order("created_at desc").
		First(&ord).Error; err != nil {
		return "I don’t see any previous orders yet. What would you like to order today?", nil
	}

	var items []storage.OrderItem
	_ = json.Unmarshal([]byte(ord.Items), &items)

	itemsPart := ""
	if len(items) > 0 {
		lines := make([]string, 0, len(items))
		for i, it := range items {
			if i >= 4 {
				remaining := len(items) - 4
				if remaining > 0 {
					lines = append(lines, fmt.Sprintf("…and %d more", remaining))
				}
				break
			}

			qty := it.Quantity
			if qty <= 0 {
				qty = 1
			}
			lines = append(lines, fmt.Sprintf("- %dx %s", qty, strings.TrimSpace(it.Name)))
		}
		itemsPart = strings.Join(lines, "\n")
	}

	currency := strings.TrimSpace(ord.Currency)
	if currency == "" {
		currency = "NGN"
	}

	created := ord.CreatedAt
	createdPart := ""
	if !created.IsZero() {
		createdPart = created.Format(time.RFC1123)
	}

	header := "Here’s your last order:"
	if strings.TrimSpace(ord.OrderNumber) != "" {
		header = fmt.Sprintf("Here’s your last order (%s):", strings.TrimSpace(ord.OrderNumber))
	}

	var b strings.Builder
	b.WriteString(header)
	if createdPart != "" {
		b.WriteString("\n")
		b.WriteString("Created: ")
		b.WriteString(createdPart)
	}
	if itemsPart != "" {
		b.WriteString("\n")
		b.WriteString(itemsPart)
	}

	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Total: %s%.2f", currency, ord.TotalAmount))
	if s := strings.TrimSpace(ord.Status); s != "" {
		b.WriteString("\n")
		b.WriteString("Status: ")
		b.WriteString(s)
	}

	b.WriteString("\n\nWould you like to reorder, or place a new order?")
	return b.String(), nil
}

