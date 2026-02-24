package bot

import (
	"context"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

func (m *TenantBotManager) HandleCustomerMessage(bot *tgbotapi.BotAPI, tenantID uuid.UUID, update tgbotapi.Update) {
	msg := update.Message
	if msg == nil {
		return
	}

	ctx := context.Background()

	// Log customer inquiry (keep for audit trail)
	m.DB.Create(&storage.EventLog{
		TenantID:   tenantID,
		EventType:  "customer_chat",
		SenderName: msg.From.UserName,
		Message:    msg.Text,
	})

	// Build sender name from Telegram user info
	senderName := msg.From.UserName
	if senderName == "" {
		senderName = fmt.Sprintf("%s %s", msg.From.FirstName, msg.From.LastName)
		senderName = trimString(senderName)
	}
	if senderName == "" {
		senderName = fmt.Sprintf("User_%d", msg.From.ID)
	}

	// Use MessageHandler to properly create conversation and interactions
	// This ensures Telegram messages appear in the dashboard inbox
	if m.MessageHandler != nil {
		// Create IncomingMessage for the message handler
		// NOTE: ThreadID must be the raw chat ID (numeric string) for sendTelegramReply to work
		incomingMsg := social.IncomingMessage{
			TenantID:         tenantID,
			Platform:         "telegram",
			CustomerID:       fmt.Sprintf("%d", msg.From.ID),
			CustomerName:     senderName,
			CustomerUsername: msg.From.UserName,
			MessageID:        fmt.Sprintf("tg_%d_%d", msg.Chat.ID, msg.MessageID),
			Content:          msg.Text,
			MessageType:      "text",
			ThreadID:         fmt.Sprintf("%d", msg.Chat.ID), // Must be numeric for sendTelegramReply
			Timestamp:        time.Unix(int64(msg.Date), 0),
			Metadata: map[string]interface{}{
				"telegram_chat_id":    msg.Chat.ID,
				"telegram_message_id": msg.MessageID,
				"telegram_user_id":    msg.From.ID,
				"telegram_username":   msg.From.UserName,
				"telegram_first_name": msg.From.FirstName,
				"telegram_last_name":  msg.From.LastName,
			},
		}

		// Process through message handler - this creates conversation, interactions, and generates AI reply
		if err := m.MessageHandler.ProcessMessage(ctx, incomingMsg); err != nil {
			m.Logger.Error("failed to process telegram customer message through message handler",
				zap.Error(err),
				zap.Stringer("tenant_id", tenantID),
				zap.Int64("chat_id", msg.Chat.ID))

			// Fallback: Try to generate a dynamic response
			fallbackReply := m.generateFallbackReply(ctx, tenantID, msg.Text)
			if fallbackReply != "" {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fallbackReply))
			}
			// If empty, no message sent - escalation will handle it
		}
		// Note: Reply is sent by MessageHandler via sendTelegramReply
		return
	}

	// Fallback if MessageHandler is not available
	m.Logger.Warn("MessageHandler not available for Telegram customer message",
		zap.Stringer("tenant_id", tenantID))
	reply := m.generateFallbackReply(ctx, tenantID, msg.Text)
	if reply != "" {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, reply))
	}
}

// generateFallbackReply generates a contextual fallback response when MessageHandler is unavailable
// Uses AI to generate a dynamic response - never returns hardcoded text
func (m *TenantBotManager) generateFallbackReply(ctx context.Context, tenantID uuid.UUID, messageText string) string {
	if m.Responder != nil && m.Responder.Client != nil {
		fallbackGen := ai.NewContextualFallbackGenerator(
			m.Responder.Client,
			m.DB,
			m.Logger,
			m.Memory,
		)
		ackMsg, err := fallbackGen.GenerateProcessingResponse(ctx, tenantID, uuid.Nil, messageText)
		if err == nil && ackMsg != "" {
			return ackMsg
		}
		// If AI generation failed, try a simpler approach
		m.Logger.Warn("fallback AI generation failed, attempting error response", zap.Error(err))
		errorMsg, err := fallbackGen.GenerateErrorResponse(ctx, tenantID, uuid.Nil, messageText, "processing_error")
		if err == nil && errorMsg != "" {
			return errorMsg
		}
	}
	// Last resort: return empty string so caller knows to escalate
	// Caller should handle this by not sending any message (escalation will happen)
	m.Logger.Warn("all fallback generation failed, returning empty for escalation")
	return ""
}

// trimString removes leading/trailing whitespace
func trimString(s string) string {
	// Simple trim implementation
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
