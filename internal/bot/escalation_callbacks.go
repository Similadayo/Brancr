package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// handleEscalationCallback processes escalation action callbacks (approve, edit, ignore)
func (h *TenantWebhookHandler) handleEscalationCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	// Parse callback: "approve:123" or "edit:123" or "ignore:123"
	// Note: arg is already extracted from "escalate:approve:123" -> "approve:123"
	parts := strings.Split(arg, ":")
	if len(parts) < 2 {
		h.Manager.Logger.Warn("invalid escalation callback format", zap.String("arg", arg))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid callback format. Please try typing 'approve <id>' or 'edit <id>' instead."))
		return
	}
	
	action := parts[0] // "approve", "edit", "ignore"
	interactionIDStr := parts[1]
	
	interactionID, err := strconv.ParseUint(interactionIDStr, 10, 32)
	if err != nil {
		h.Manager.Logger.Warn("invalid interaction ID in escalation callback", zap.String("id", interactionIDStr))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid escalation ID. Please try again."))
		return
	}
	
	ctx := context.Background()
	
	switch action {
	case "approve":
		// 🔒 SECURITY: Verify access for payment escalations
		if h.Manager.EscalationHandler != nil && h.Manager.EscalationHandler.PaymentSecurity != nil {
			// Check if this is a payment escalation
			var interaction storage.Interaction
			if err := h.Manager.DB.First(&interaction, interactionID).Error; err == nil {
				if interaction.SecurityLevel == "payment" || interaction.DetectedIntent == "payment" {
					// Verify access and log
					allowed, err := h.Manager.EscalationHandler.PaymentSecurity.VerifyPaymentAccess(tenantID, uint(interactionID), chatID)
					if !allowed || err != nil {
						h.Manager.Logger.Error("🔒 SECURITY: Unauthorized payment escalation access",
							zap.Uint("tenant_id", tenantID),
							zap.Uint64("interaction_id", interactionID),
							zap.Error(err))
						h.Manager.EscalationHandler.PaymentSecurity.LogPaymentAccess(ctx, tenantID, uint(interactionID), "approve_attempt", chatID, false, err.Error())
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Unauthorized: Payment escalations require owner verification."))
						return
					}
					
					// Check rate limit
					rateOk, err := h.Manager.EscalationHandler.PaymentSecurity.CheckRateLimit(tenantID, chatID)
					if !rateOk || err != nil {
						h.Manager.Logger.Warn("🔒 SECURITY: Rate limit exceeded for payment escalation",
							zap.Uint("tenant_id", tenantID),
							zap.Error(err))
						bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Too many requests. Please wait a moment before trying again."))
						return
					}
					
					// Log authorized access
					h.Manager.EscalationHandler.PaymentSecurity.LogPaymentAccess(ctx, tenantID, uint(interactionID), "approve", chatID, true, "")
				}
			}
		}
		
		// Approve and send suggested reply
		// Access EscalationHandler through Manager
		if h.Manager.EscalationHandler != nil {
			// Send processing message
			processingMsg, err := bot.Send(tgbotapi.NewMessage(chatID, "⏳ Sending reply to customer..."))
			var processingMsgID int
			if err == nil && processingMsg.MessageID > 0 {
				processingMsgID = processingMsg.MessageID
			}
			
			sendErr := h.Manager.EscalationHandler.HandleEscalationResponse(ctx, tenantID, chatID, fmt.Sprintf("approve %d", interactionID))
			if sendErr != nil {
				h.Manager.Logger.Error("failed to approve escalation", zap.Error(sendErr), zap.Uint64("interaction_id", interactionID))
				
				// Delete processing message
				if processingMsgID > 0 {
					bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsgID))
				}
				
				// Send error message with helpful details
				errorMsg := fmt.Sprintf("❌ Couldn't send reply: %v\n\n", sendErr)
				if strings.Contains(sendErr.Error(), "INSTAGRAM MESSAGING CAPABILITY") || strings.Contains(sendErr.Error(), "Error Code 3") {
					errorMsg += "Instagram messaging requires App Review approval. Please check your Meta Developer Console.\n\n"
					errorMsg += fmt.Sprintf("You can try typing 'approve %d' again after fixing the issue.", interactionID)
				} else {
					errorMsg += fmt.Sprintf("Please try typing 'approve %d' instead, or contact support if the issue persists.", interactionID)
				}
				bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
			} else {
				// Delete processing message
				if processingMsgID > 0 {
					bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsgID))
				}
				// Success confirmation will be sent by sendApprovedReply
			}
		} else {
			h.Manager.Logger.Warn("escalation handler not configured in bot manager")
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Escalation handler not yet configured. Please use text commands: 'approve <interaction_id>' or type your reply."))
		}
		
	case "edit":
		// Prompt user to edit the reply
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✏️ Type your custom reply for interaction %d. It will be sent immediately.", interactionID)))
		// Store that we're waiting for edited reply
		// TODO: Store edit session state
		
	case "ignore":
		// Remove from pending escalations
		if h.Manager.EscalationHandler != nil {
			// Mark interaction as ignored
			bot.Send(tgbotapi.NewMessage(chatID, "👌 Ignored. You can respond later if needed."))
		}
		
	case "fixed":
		// Mark escalation as fixed - prompt for resolution message
		if h.Manager.EscalationHandler != nil {
			ctx := context.Background()
			
			// HandleMarkAsFixed will check for existing session and send prompt only once
			if err := h.Manager.EscalationHandler.HandleMarkAsFixed(ctx, tenantID, uint(interactionID), chatID, bot); err != nil {
				h.Manager.Logger.Error("failed to start fixed escalation flow", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %v", err)))
				return
			}
			// Prompt message is now sent inside HandleMarkAsFixed to prevent duplicates
		}
		
	default:
		h.Manager.Logger.Warn("unknown escalation action", zap.String("action", action))
	}
}

// handlePlatformSelectionCallback processes platform selection for posts
func (h *TenantWebhookHandler) handlePlatformSelectionCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	sess, ok := tenantSessions[chatID]
	if !ok || sess.Step != 4 {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Post session expired. Please start over."))
		return
	}
	
	parts := strings.Split(arg, ":")
	if len(parts) < 2 {
		return
	}
	
	action := parts[1]
	
	switch action {
	case "toggle":
		if len(parts) < 4 {
			return
		}
		platform := parts[2]
		shouldSelect := parts[3] == "true"
		
		// Toggle platform in session
		found := false
		var newPlatforms []string
		for _, p := range sess.Platforms {
			if p == platform {
				found = true
				if shouldSelect {
					newPlatforms = append(newPlatforms, p)
				}
			} else {
				newPlatforms = append(newPlatforms, p)
			}
		}
		if !found && shouldSelect {
			newPlatforms = append(newPlatforms, platform)
		}
		sess.Platforms = newPlatforms
		
		// Refresh the platform selection UI
		h.Manager.showPlatformSelection(bot, chatID, sess)
		
	case "all":
		// Select all available platforms
		sess.Platforms = []string{"instagram", "facebook", "whatsapp", "tiktok", "youtube"}
		h.Manager.showPlatformSelection(bot, chatID, sess)
		
	case "confirm":
		// Confirm selection and move to next step
		if len(sess.Platforms) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Please select at least one platform."))
			return
		}
		
		// Check if TikTok is selected - if so, ask for TikTok posting options
		hasTikTok := false
		for _, p := range sess.Platforms {
			if p == "tiktok" {
				hasTikTok = true
				break
			}
		}
		
		if hasTikTok && !sess.TikTokOptionsAsked {
			// Show TikTok options UI
			h.Manager.showTikTokPostingOptions(bot, chatID, sess)
			return
		}
		
		sess.Step = 5
		bot.Send(tgbotapi.NewMessage(chatID, "When should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
	}
}

