package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/fulfillment"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// handleEscalationCallback processes escalation action callbacks (approve, edit, ignore)
func (h *TenantWebhookHandler) handleEscalationCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, actorTelegramID int64, arg string) {
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

	interactionID, err := uuid.Parse(interactionIDStr)
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
				isPaymentEscalation := interaction.SecurityLevel == "payment" || interaction.DetectedIntent == "payment" || ai.MetadataHasEvent(interaction.Metadata, ai.EventPaymentConfirmation)
				if isPaymentEscalation {
					// Some tenants are configured to use a Telegram group chat as their "owner" destination.
					// Telegram group chat IDs are negative, while user IDs are positive.
					// For these tenants, authorize based on the chat ID (so approvals work in the group),
					// but still audit-log the actual actor Telegram user ID.
					accessKey := actorTelegramID
					var tenant storage.Tenant
					if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
						if tenant.OwnerTelegramID < 0 && tenant.OwnerTelegramID == chatID {
							accessKey = chatID
						}
					}

					// Verify access and log
					allowed, err := h.Manager.EscalationHandler.PaymentSecurity.VerifyPaymentAccess(tenantID, interactionID, accessKey)
					if !allowed || err != nil {
						h.Manager.Logger.Error("🔒 SECURITY: Unauthorized payment escalation access",
							zap.Stringer("tenant_id", tenantID),
							zap.Stringer("interaction_id", interactionID),
							zap.Error(err))
						h.Manager.EscalationHandler.PaymentSecurity.LogPaymentAccess(ctx, tenantID, interactionID, "approve_attempt", actorTelegramID, false, err.Error())
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Only the tenant owner can approve payment escalations. Please ask the owner to approve."))
						return
					}

					// Check rate limit
					rateOk, err := h.Manager.EscalationHandler.PaymentSecurity.CheckRateLimit(tenantID, actorTelegramID)
					if !rateOk || err != nil {
						h.Manager.Logger.Warn("🔒 SECURITY: Rate limit exceeded for payment escalation",
							zap.Stringer("tenant_id", tenantID),
							zap.Error(err))
						bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Too many requests. Please wait a moment before trying again."))
						return
					}

					// Log authorized access
					h.Manager.EscalationHandler.PaymentSecurity.LogPaymentAccess(ctx, tenantID, interactionID, "approve", actorTelegramID, true, "")
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
				h.Manager.Logger.Error("failed to approve escalation", zap.Error(sendErr), zap.Stringer("interaction_id", interactionID))

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
			if err := h.Manager.EscalationHandler.HandleMarkAsFixed(ctx, tenantID, interactionID, chatID, bot); err != nil {
				h.Manager.Logger.Error("failed to start fixed escalation flow", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %v", err)))
				return
			}
			// Prompt message is now sent inside HandleMarkAsFixed to prevent duplicates
		}

	case "payment_approve":
		// Payment-specific approval: Mark as resolved + send RECEIPT to customer
		if h.Manager.EscalationHandler != nil {
			ctx := context.Background()

			// Security checks (same as regular approve)
			var interaction storage.Interaction
			if err := h.Manager.DB.First(&interaction, interactionID).Error; err != nil {
				h.Manager.Logger.Error("interaction not found for payment_approve", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Escalation not found. It may have already been resolved."))
				return
			}

			if h.Manager.EscalationHandler.PaymentSecurity != nil {
				accessKey := actorTelegramID
				var tenant storage.Tenant
				if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
					if tenant.OwnerTelegramID < 0 && tenant.OwnerTelegramID == chatID {
						accessKey = chatID
					}
				}

				allowed, err := h.Manager.EscalationHandler.PaymentSecurity.VerifyPaymentAccess(tenantID, interactionID, accessKey)
				if !allowed || err != nil {
					h.Manager.Logger.Error("🔒 SECURITY: Unauthorized payment escalation access",
						zap.Stringer("tenant_id", tenantID),
						zap.Stringer("interaction_id", interactionID))
					bot.Send(tgbotapi.NewMessage(chatID, "❌ Only the tenant owner can approve payment escalations. Please ask the owner to approve."))
					return
				}
				// Log authorized access
				h.Manager.EscalationHandler.PaymentSecurity.LogPaymentAccess(ctx, tenantID, interactionID, "payment_approve", actorTelegramID, true, "")
			}

			// Send processing message
			processingMsg := tgbotapi.NewMessage(chatID, "⏳ Approving payment and sending receipt...")
			sentMsg, _ := bot.Send(processingMsg)

			// Find the order - MUST use ConversationID for exact matching
			// NEVER guess based on "most recent" or time windows
			var order storage.Order
			var receiptMessage string
			orderFound := false

			// Strategy 1: Check if PendingEscalation has an OrderID (explicit binding)
			var pendingEsc storage.PendingEscalation
			if err := h.Manager.DB.Where("interaction_id = ?", interactionID).First(&pendingEsc).Error; err == nil {
				if pendingEsc.OrderID != nil {
					if err := h.Manager.DB.First(&order, *pendingEsc.OrderID).Error; err == nil {
						orderFound = true
						h.Manager.Logger.Info("Order found via PendingEscalation.OrderID",
							zap.Stringer("order_id", order.ID),
							zap.Float64("amount", order.TotalAmount))
					}
				}
			}

			// Strategy 2: Use ConversationID binding (the CORRECT approach for new orders)
			if !orderFound && interaction.ConversationID != uuid.Nil {
				if err := h.Manager.DB.Where("tenant_id = ? AND conversation_id = ? AND status = ?",
					tenantID, interaction.ConversationID, "pending").
					First(&order).Error; err == nil {
					orderFound = true
					h.Manager.Logger.Info("Order found via ConversationID binding",
						zap.Stringer("order_id", order.ID),
						zap.Stringer("conversation_id", interaction.ConversationID),
						zap.Float64("amount", order.TotalAmount))
				}
			}

			// Strategy 3: LEGACY FALLBACK - for old orders without conversation_id
			// This is temporary during migration - once all orders have conversation_id, this can be removed
			// ONLY match orders created in the last 2 hours for this specific customer
			if !orderFound {
				if err := h.Manager.DB.Where("tenant_id = ? AND customer_id = ? AND status = ?",
					tenantID, interaction.CustomerID, "pending").
					Where("conversation_id IS NULL OR conversation_id = ?", uuid.Nil). // Only match legacy orders
					Where("created_at > ?", time.Now().Add(-2*time.Hour)).
					Order("created_at DESC").
					First(&order).Error; err == nil {
					orderFound = true
					h.Manager.Logger.Warn("⚠️ Order found via LEGACY fallback (no conversation_id) - migration pending",
						zap.Stringer("order_id", order.ID),
						zap.Float64("amount", order.TotalAmount),
						zap.Time("order_created", order.CreatedAt))
				}
			}

			// Strategy 4: DRAFT ORDER PROMOTION - if draft exists, promote it to pending
			// This implements two-phase order lifecycle: draft → pending
			if !orderFound && interaction.ConversationID != uuid.Nil {
				var draftOrder storage.Order
				if err := h.Manager.DB.Where("tenant_id = ? AND conversation_id = ? AND status = ?",
					tenantID, interaction.ConversationID, "draft").
					First(&draftOrder).Error; err == nil {
					// Promote draft to pending
					draftOrder.Status = "pending"
					if err := h.Manager.DB.Save(&draftOrder).Error; err == nil {
						order = draftOrder
						orderFound = true
						h.Manager.Logger.Info("⬆️ Draft order PROMOTED to pending on payment confirmation",
							zap.Stringer("order_id", order.ID),
							zap.Float64("amount", order.TotalAmount))

						// Ensure a pending payment record exists for this order
						var existingPayment storage.Payment
						if err := h.Manager.DB.Where("tenant_id = ? AND order_id = ?", tenantID, order.ID).First(&existingPayment).Error; err != nil {
							now := time.Now()
							payment := storage.Payment{
								TenantID:           tenantID,
								OrderID:            order.ID,
								CustomerID:         order.CustomerID,
								PaymentReference:   order.PaymentReference,
								Amount:             order.TotalAmount,
								Currency:           order.Currency,
								PaymentMethod:      "bank_transfer",
								Status:             "pending",
								VerificationStatus: "unverified",
								Metadata:           "{}",
								CreatedAt:          now,
							}
							if payment.Currency == "" {
								payment.Currency = "NGN"
							}
							if payment.PortalToken == "" {
								payment.PortalToken = utils.GeneratePortalToken()
							}
							if payment.ReceiptID == "" {
								payment.ReceiptID = utils.GenerateReceiptID()
							}
							_ = h.Manager.DB.Create(&payment).Error
						}
					}
				}
			}

			// Strategy 5: FAIL LOUDLY if still no order found
			if !orderFound {
				h.Manager.Logger.Error("⚠️ CRITICAL: No order found for payment confirmation - REFUSING TO GUESS",
					zap.Stringer("interaction_id", interactionID),
					zap.Stringer("conversation_id", interaction.ConversationID),
					zap.Stringer("customer_id", interaction.CustomerID))

				// Delete processing message
				if sentMsg.MessageID > 0 {
					bot.Request(tgbotapi.NewDeleteMessage(chatID, sentMsg.MessageID))
				}
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Cannot find the order for this payment. Please check the Orders page and confirm manually."))
				return
			}

			if orderFound {
				// Generate portal URL for receipt
				var portalURL string
				var payment storage.Payment
				if err := h.Manager.DB.Where("order_id = ?", order.ID).First(&payment).Error; err == nil {
					if payment.PortalURL != "" {
						portalURL = payment.PortalURL
					}
				}

				// Fetch tenant persona for AI-generated message
				var persona storage.Persona
				h.Manager.DB.Where("tenant_id = ?", tenantID).First(&persona)

				// Generate AI payment confirmation
				receiptMessage = h.generateAIPaymentConfirmation(ctx, tenantID, &order, persona.Tone, portalURL)

				// Update order status: tenant-approved payment means the order is CONFIRMED.
				now := time.Now()
				order.Status = "confirmed"
				if order.ConfirmedAt == nil {
					order.ConfirmedAt = &now
				}
				h.Manager.DB.Save(&order)

				// Update payment status (upsert)
				nows := time.Now()
				if err := h.Manager.DB.Where("tenant_id = ? AND order_id = ?", tenantID, order.ID).First(&payment).Error; err == nil {
					payment.Status = "verified"
					payment.VerificationStatus = "tenant_verified"
					payment.VerifiedAt = &now
					h.Manager.DB.Save(&payment)
				} else {
					p := storage.Payment{
						TenantID:           tenantID,
						OrderID:            order.ID,
						CustomerID:         order.CustomerID,
						PaymentReference:   order.PaymentReference,
						Amount:             order.TotalAmount,
						Currency:           order.Currency,
						PaymentMethod:      "bank_transfer",
						Status:             "verified",
						VerificationStatus: "tenant_verified",
						ClaimedByCustomer:  true,
						ClaimedAt:          &now,
						VerifiedAt:         &now,
						CompletedAt:        &now,
						Metadata:           "{}",
						CreatedAt:          nows,
					}
					if p.Currency == "" {
						p.Currency = "NGN"
					}
					maxRetries := 5
					for i := 0; i < maxRetries; i++ {
						if p.PortalToken == "" {
							p.PortalToken = utils.GeneratePortalToken()
						}
						if p.ReceiptID == "" {
							p.ReceiptID = utils.GenerateReceiptID()
						}
						if err := h.Manager.DB.Create(&p).Error; err != nil {
							if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
								p.PortalToken = ""
								p.ReceiptID = ""
								continue
							}
							h.Manager.Logger.Warn("failed to create payment on approval", zap.Error(err), zap.Stringer("order_id", order.ID))
							break
						}
						break
					}
				}

				h.Manager.Logger.Info("✅ Payment approved and order confirmed",
					zap.Stringer("order_id", order.ID),
					zap.Stringer("interaction_id", interactionID))
			} else {
				// No order found - generate generic AI confirmation
				var persona storage.Persona
				h.Manager.DB.Where("tenant_id = ?", tenantID).First(&persona)
				receiptMessage = h.generateAIPaymentConfirmation(ctx, tenantID, nil, persona.Tone, "")
			}

			// Send receipt directly to customer (not the suggested reply)
			if err := h.Manager.EscalationHandler.SendReceiptToCustomer(ctx, tenantID, &interaction, receiptMessage); err != nil {
				h.Manager.Logger.Error("failed to send receipt to customer", zap.Error(err))
				// Delete processing message
				if sentMsg.MessageID > 0 {
					bot.Request(tgbotapi.NewDeleteMessage(chatID, sentMsg.MessageID))
				}
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error sending receipt: %v", err)))
				return
			}

			// Update interaction status
			interaction.ResponseType = "manual"
			interaction.ResponseStatus = "sent"
			interaction.FinalReply = receiptMessage
			h.Manager.DB.Save(&interaction)

			// Reset SalesConversation stage so future messages aren't auto-escalated
			// Find the SalesConversation for this customer and reset its stage
			var salesConv storage.SalesConversation
			if err := h.Manager.DB.Where("customer_id = ? AND tenant_id = ?",
				interaction.CustomerID, tenantID).
				Order("created_at DESC").First(&salesConv).Error; err == nil {
				// Save new status
				h.Manager.DB.Save(&salesConv)

				// REQUIREMENTS (Step-by-step)
				// Initialize fulfillment tracking and send ONLY the first requirement question.
				if h.Manager.MessageHandler != nil && interaction.ConversationID != uuid.Nil {
					fs := fulfillment.NewService(h.Manager.DB, h.Manager.Logger)
					if err := fs.EnsureFulfillmentState(&order); err != nil {
						h.Manager.Logger.Warn("failed to initialize fulfillment state", zap.Error(err))
					} else {
						req, _, err := fs.GetPendingRequirementForConversation(tenantID, interaction.ConversationID)
						if err != nil {
							h.Manager.Logger.Warn("failed to get pending requirement", zap.Error(err))
						} else if req != nil {
							// Find threadID for sending
							threadID := interaction.CustomerID.String() // fallback
							var conversation storage.Conversation
							if err := h.Manager.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
								if conversation.PlatformThreadID != "" {
									threadID = conversation.PlatformThreadID
								}
							}

							// Industry-aware label: service request vs order
							label := "order"
							var tenant storage.Tenant
							if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
								var industry storage.Industry
								if err := h.Manager.DB.First(&industry, tenant.IndustryID).Error; err == nil {
									if industry.HasServices {
										label = "service request"
									}
								} else {
									var menuCount int64
									_ = h.Manager.DB.Model(&storage.MenuItem{}).Where("tenant_id = ? AND is_active = ?", tenantID, true).Count(&menuCount).Error
									var productCount int64
									_ = h.Manager.DB.Model(&storage.Product{}).Where("tenant_id = ? AND is_active = ?", tenantID, true).Count(&productCount).Error
									if menuCount == 0 && productCount == 0 {
										label = "service request"
									}
								}
							} else {
								var menuCount int64
								_ = h.Manager.DB.Model(&storage.MenuItem{}).Where("tenant_id = ? AND is_active = ?", tenantID, true).Count(&menuCount).Error
								var productCount int64
								_ = h.Manager.DB.Model(&storage.Product{}).Where("tenant_id = ? AND is_active = ?", tenantID, true).Count(&productCount).Error
								if menuCount == 0 && productCount == 0 {
									label = "service request"
								}
							}

							msg := fmt.Sprintf("To complete your %s, please send: %s.", label, req.Label)
							if strings.TrimSpace(req.Description) != "" {
								msg += " " + strings.TrimSpace(req.Description)
							}
							h.Manager.MessageHandler.SendReply(ctx, tenantID, interaction.Platform, threadID, threadID, msg)
						}
					}
				}
			}

			// Log reset
			h.Manager.Logger.Info("🔄 SalesConversation stage reset after payment approval",
				zap.Stringer("sales_conv_id", salesConv.ID),
				zap.String("old_stage", "payment_confirmation"),
				zap.String("new_stage", "completed"))

			// Also clear any pending escalations for this interaction
			h.Manager.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})

			// Delete processing message
			if sentMsg.MessageID > 0 {
				bot.Request(tgbotapi.NewDeleteMessage(chatID, sentMsg.MessageID))
			}

			// Success confirmation
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Payment confirmed! Receipt sent to customer and order marked as complete."))
		}

	case "payment_edit":
		// Payment-specific edit: Prompt for custom reply, then offer Confirm/Stall options
		if h.Manager.EscalationHandler != nil {
			// Store that we're in payment edit mode for this interaction
			// The next message from this user should be the custom reply
			editPrompt := fmt.Sprintf(
				"✏️ *Edit your reply for this payment confirmation.*\n\n"+
					"Type your message, then I'll ask if you want to *Confirm* (approve payment) or *Stall* (hold off).\n\n"+
					"_Reply to interaction: %s_", interactionID)
			msg := tgbotapi.NewMessage(chatID, editPrompt)
			msg.ParseMode = "Markdown"
			bot.Send(msg)

			// Store edit session state in pending escalation
			var esc storage.PendingEscalation
			if err := h.Manager.DB.Where("interaction_id = ?", interactionID).First(&esc).Error; err == nil {
				esc.Reason = "awaiting_payment_edit" // Use reason field to track state
				h.Manager.DB.Save(&esc)
			}
		}

	default:
		h.Manager.Logger.Warn("unknown escalation action", zap.String("action", action))
	}
}

// handlePlatformSelectionCallback processes platform selection for posts
func (h *TenantWebhookHandler) handlePlatformSelectionCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	var sess *PostSession
	var ok bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess, ok = h.Manager.SessionManager.GetPostSession(context.Background(), chatID)
	}
	if !ok || sess == nil || sess.Step != 4 {
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
		// Save session after modification
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		// Refresh the platform selection UI
		h.Manager.showPlatformSelection(bot, chatID, sess)

	case "all":
		// Select all available platforms
		sess.Platforms = []string{"instagram", "facebook", "whatsapp", "tiktok", "youtube"}
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
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
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "When should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
	}
}

// generateAIPaymentConfirmation uses AI to generate a persona-driven payment confirmation message
// If order is nil, generates a generic confirmation
func (h *TenantWebhookHandler) generateAIPaymentConfirmation(ctx context.Context, tenantID uuid.UUID, order *storage.Order, tone string, portalURL string) string {
	labelUpper := "ORDER"
	if h.Manager != nil && h.Manager.DB != nil {
		var tenant storage.Tenant
		if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
			var industry storage.Industry
			if err := h.Manager.DB.First(&industry, tenant.IndustryID).Error; err == nil {
				if industry.HasServices {
					labelUpper = "SERVICE REQUEST"
				}
			}
		}
	}

	// Build the AI prompt
	var prompt string
	if order != nil {
		prompt = fmt.Sprintf(`Generate a short, natural payment confirmation message for a customer.

TONE: %s (match this tone exactly)
%s NUMBER: #%s
AMOUNT: %s %.2f
RECEIPT LINK: %s

Rules:
- Be natural and match the tone exactly
- Include the %s number and amount
- Include receipt link naturally if provided
- Keep it SHORT (max 3-4 lines, like a WhatsApp message)
- NO robotic phrases like "I understand" or "Is there anything else"
- Sound like a real person texting, not a bot
- IMPORTANT: If this is a services business, DO NOT use the word "order". Use "%s" instead.

Generate ONLY the message text, nothing else.`,
			tone,
			labelUpper,
			order.OrderNumber,
			order.Currency,
			order.TotalAmount,
			portalURL,
			strings.ToLower(labelUpper),
			strings.ToLower(labelUpper),
		)
	} else {
		prompt = fmt.Sprintf(`Generate a short, natural payment confirmation message for a customer.

TONE: %s (match this tone exactly)

Rules:
- Be natural and match the tone exactly
- Confirm payment was received
- Keep it SHORT (max 2-3 lines, like a WhatsApp message)
- NO robotic phrases
- Sound like a real person texting
- IMPORTANT: If this is a services business, DO NOT use the word "order". Use "%s" instead.

Generate ONLY the message text, nothing else.`,
			tone,
			strings.ToLower(labelUpper),
		)
	}

	// Use AI to generate the message
	if h.Manager != nil && h.Manager.AIClient != nil {
		messages := []ai.Message{
			{Role: "system", Content: "You are a helpful assistant that generates short, natural payment confirmation messages matching the specified tone."},
			{Role: "user", Content: prompt},
		}
		response, err := h.Manager.AIClient.Generate(ctx, messages)
		if err == nil && response != "" {
			if labelUpper == "SERVICE REQUEST" {
				reOrders := regexp.MustCompile(`(?i)\borders\b`)
				reOrder := regexp.MustCompile(`(?i)\border\b`)
				response = reOrders.ReplaceAllString(response, "service requests")
				response = reOrder.ReplaceAllString(response, "service request")
			}
			h.Manager.Logger.Info("Generated AI payment confirmation",
				zap.Stringer("tenant_id", tenantID),
				zap.String("tone", tone))
			return response
		}
		h.Manager.Logger.Warn("AI generation failed for payment confirmation, using fallback",
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID))
	}

	// Fallback only if AI quota exhausted or not available
	if order != nil {
		labelLower := strings.ToLower(labelUpper)
		labelPretty := labelLower
		if len(labelLower) > 0 {
			labelPretty = strings.ToUpper(labelLower[:1]) + labelLower[1:]
		}
		msg := fmt.Sprintf("✅ Payment confirmed! %s #%s - %s %.2f",
			labelPretty, order.OrderNumber, order.Currency, order.TotalAmount)
		if portalURL != "" {
			msg += fmt.Sprintf("\n\n📱 Receipt: %s", portalURL)
		}
		return msg
	}
	return "✅ Payment confirmed! Thank you 🎉"
}
