package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// showPaymentsMenu displays the payments management menu
func (h *TenantWebhookHandler) showPaymentsMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	// Get payment stats
	var totalPayments, pendingPayments, verifiedPayments int64
	var totalRevenue float64

	h.Manager.DB.Model(&storage.Payment{}).
		Where("tenant_id = ?", tenantID).
		Count(&totalPayments)

	h.Manager.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", tenantID, "pending").
		Count(&pendingPayments)

	h.Manager.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []string{"verified", "confirmed"}).
		Count(&verifiedPayments)

	h.Manager.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []string{"verified", "confirmed"}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&totalRevenue)

	text := fmt.Sprintf(
		"💳 *Payment Management*\n\n"+
			"📊 *Stats:*\n"+
			"• Total Payments: %d\n"+
			"• Pending: %d\n"+
			"• Verified: %d\n"+
			"• Total Revenue: NGN %.2f\n\n"+
			"Select an option:",
		totalPayments, pendingPayments, verifiedPayments, totalRevenue)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 All Payments", "payments:list:all"),
			tgbotapi.NewInlineKeyboardButtonData("⏳ Pending", "payments:list:pending"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Verified", "payments:list:verified"),
			tgbotapi.NewInlineKeyboardButtonData("🔍 Search", "payments:search"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// listPayments displays payments based on status filter
func (h *TenantWebhookHandler) listPayments(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, status string) {
	var payments []storage.Payment
	query := h.Manager.DB.Where("tenant_id = ?", tenantID)

	if status != "all" {
		query = query.Where("status = ?", status)
	}

	query.Order("created_at DESC").Limit(10).Find(&payments)

	if len(payments) == 0 {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("No %s payments found.", status))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "payments:menu"),
			),
		)
		bot.Send(msg)
		return
	}

	text := fmt.Sprintf("💳 *%s Payments* (showing last 10)\n\n", strings.Title(status))
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	for i, payment := range payments {
		statusEmoji := "⏳"
		switch payment.Status {
		case "verified":
			statusEmoji = "✅"
		case "confirmed":
			statusEmoji = "🎉"
		case "failed":
			statusEmoji = "❌"
		}

		text += fmt.Sprintf(
			"%d. %s *%s*\n"+
				"   💰 %s %.2f\n"+
				"   📦 Order: %s\n"+
				"   📅 %s\n\n",
			i+1, statusEmoji, payment.PaymentReference,
			payment.Currency, payment.Amount,
			func() string {
				var order storage.Order
				if err := h.Manager.DB.First(&order, payment.OrderID).Error; err == nil {
					return order.OrderNumber
				}
				return "N/A"
			}(),
			payment.CreatedAt.Format("Jan 2, 3:04PM"))

		// Add button to view payment details
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("View %s", payment.PaymentReference),
				fmt.Sprintf("payment:view:%d", payment.ID),
			),
		})
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "payments:menu"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// viewPaymentDetails shows detailed information about a specific payment
func (h *TenantWebhookHandler) viewPaymentDetails(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, paymentID uint) {
	var payment storage.Payment
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", paymentID, tenantID).First(&payment).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Payment not found."))
		return
	}

	var order storage.Order
	h.Manager.DB.First(&order, payment.OrderID)

	statusEmoji := "⏳"
	switch payment.Status {
	case "verified":
		statusEmoji = "✅"
	case "confirmed":
		statusEmoji = "🎉"
	case "failed":
		statusEmoji = "❌"
	}

	text := fmt.Sprintf(
		"💳 *Payment Details*\n\n"+
			"*Payment Reference:* `%s`\n"+
			"*Status:* %s %s\n"+
			"*Verification:* %s\n"+
			"*Amount:* %s %.2f\n"+
			"*Method:* %s\n\n"+
			"*Order:*\n"+
			"• Number: %s\n"+
			"• Status: %s\n\n"+
			"*Customer:*\n"+
			"• Name: %s\n"+
			"• Phone: %s\n\n"+
			"*Timestamps:*\n"+
			"• Created: %s\n",
		payment.PaymentReference,
		statusEmoji, strings.Title(payment.Status),
		strings.Title(payment.VerificationStatus),
		payment.Currency, payment.Amount,
		strings.Title(strings.ReplaceAll(payment.PaymentMethod, "_", " ")),
		order.OrderNumber,
		strings.Title(order.Status),
		order.CustomerName,
		order.CustomerPhone,
		payment.CreatedAt.Format("Jan 2, 2006 3:04PM"))

	if payment.VerifiedAt != nil {
		text += fmt.Sprintf("• Verified: %s\n", payment.VerifiedAt.Format("Jan 2, 2006 3:04PM"))
	}

	if payment.ReceiptURL != "" {
		text += fmt.Sprintf("\n*Receipt:* %s\n", payment.ReceiptURL)
	}

	if payment.PortalURL != "" {
		text += fmt.Sprintf("\n*Portal Link:* %s\n", payment.PortalURL)
	}

	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Add action buttons based on status
	if payment.Status == "pending" {
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✅ Verify Payment", fmt.Sprintf("payment:verify:%d", payment.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Dispute", fmt.Sprintf("payment:dispute:%d", payment.ID)),
		})
	}

	if payment.Status == "verified" && payment.ReceiptURL == "" {
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("📄 Generate Receipt", fmt.Sprintf("payment:receipt:%d", payment.ID)),
		})
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to Payments", "payments:list:all"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// verifyPayment verifies a payment
func (h *TenantWebhookHandler) verifyPayment(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, paymentID uint) {
	var payment storage.Payment
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", paymentID, tenantID).First(&payment).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Payment not found."))
		return
	}

	now := time.Now()
	payment.Status = "verified"
	payment.VerificationStatus = "tenant_verified"
	payment.VerifiedAt = &now
	h.Manager.DB.Save(&payment)

	// Update associated order status
	var order storage.Order
	if err := h.Manager.DB.First(&order, payment.OrderID).Error; err == nil {
		order.Status = "completed"
		order.ConfirmedAt = &now
		order.CompletedAt = &now
		h.Manager.DB.Save(&order)
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Payment %s verified successfully!", payment.PaymentReference)))
	h.viewPaymentDetails(bot, chatID, tenantID, paymentID)
}

// disputePayment marks a payment as disputed
func (h *TenantWebhookHandler) disputePayment(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, paymentID uint) {
	var payment storage.Payment
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", paymentID, tenantID).First(&payment).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Payment not found."))
		return
	}

	payment.VerificationStatus = "disputed"
	h.Manager.DB.Save(&payment)

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ Payment %s marked as disputed.", payment.PaymentReference)))
	h.viewPaymentDetails(bot, chatID, tenantID, paymentID)
}

// handlePaymentCallback processes payment-related callbacks
func (h *TenantWebhookHandler) handlePaymentCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, action string, arg string) {
	switch action {
	case "menu":
		h.showPaymentsMenu(bot, chatID, tenantID)
	case "list":
		// arg format: "all", "pending", "verified"
		h.listPayments(bot, chatID, tenantID, arg)
	case "view":
		// arg is payment ID
		paymentID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid payment ID."))
			return
		}
		h.viewPaymentDetails(bot, chatID, tenantID, uint(paymentID))
	case "verify":
		// arg is payment ID
		paymentID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid payment ID."))
			return
		}
		h.verifyPayment(bot, chatID, tenantID, uint(paymentID))
	case "dispute":
		// arg is payment ID
		paymentID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid payment ID."))
			return
		}
		h.disputePayment(bot, chatID, tenantID, uint(paymentID))
	case "receipt":
		// arg is payment ID
		paymentID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid payment ID."))
			return
		}
		h.generatePaymentReceipt(bot, chatID, tenantID, uint(paymentID))
	case "search":
		bot.Send(tgbotapi.NewMessage(chatID,
			"🔍 To search payments by reference, use:\n\n"+
				"`payment <reference>`\n\n"+
				"Or visit the dashboard:\n"+
				"https://www.brancr.com/dashboard/payments"))
	}
}

// generatePaymentReceipt generates a receipt for a payment
func (h *TenantWebhookHandler) generatePaymentReceipt(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, paymentID uint) {
	var payment storage.Payment
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", paymentID, tenantID).First(&payment).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Payment not found."))
		return
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}

	// Ensure portal token exists for link fallback
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
		if err := h.Manager.DB.Save(&payment).Error; err != nil {
			h.Manager.Logger.Warn("failed to save portal token", zap.Error(err), zap.Uint("payment_id", payment.ID))
		}
	}

	// Ensure we have a receipt URL (generate if missing)
	if payment.ReceiptURL == "" || payment.ReceiptID == "" {
		cloudinaryURL := os.Getenv("CLOUDINARY_URL")
		receiptService := receipt.NewReceiptService(h.Manager.DB, h.Manager.Logger, cloudinaryURL, publicURL, nil)
		if generated, err := receiptService.GenerateReceipt(context.Background(), payment.ID, payment.TenantID); err == nil {
			payment = *generated
		} else {
			h.Manager.Logger.Warn("failed to generate receipt", zap.Error(err), zap.Uint("payment_id", payment.ID))
		}
	}

	receiptLink := ""
	if strings.HasPrefix(payment.ReceiptURL, "https://") || strings.HasPrefix(payment.ReceiptURL, "http://") {
		receiptLink = payment.ReceiptURL
	} else if payment.PortalToken != "" {
		receiptLink = fmt.Sprintf("%s/portal/receipt?token=%s", publicURL, payment.PortalToken)
	} else if payment.PortalURL != "" {
		receiptLink = payment.PortalURL
	}

	if receiptLink == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Could not generate a receipt link for this payment."))
		return
	}

	text := fmt.Sprintf(
		"📄 *Receipt*\n\n"+
			"Payment: *%s*\n"+
			"Amount: %s %.2f\n\n"+
			"🔗 %s",
		payment.PaymentReference,
		payment.Currency,
		payment.Amount,
		receiptLink,
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}
