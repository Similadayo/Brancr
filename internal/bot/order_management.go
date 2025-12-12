package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

// showOrdersMenu displays the orders management menu
func (h *TenantWebhookHandler) showOrdersMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	// Get order stats
	var totalOrders, pendingOrders, completedOrders int64
	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ?", tenantID).
		Count(&totalOrders)
	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "pending").
		Count(&pendingOrders)
	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "completed").
		Count(&completedOrders)

	text := fmt.Sprintf(
		"📦 *Order Management*\n\n"+
			"📊 *Stats:*\n"+
			"• Total: %d\n"+
			"• Pending: %d\n"+
			"• Completed: %d\n\n"+
			"Select an option:",
		totalOrders, pendingOrders, completedOrders)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 All Orders", "orders:list:all"),
			tgbotapi.NewInlineKeyboardButtonData("⏳ Pending", "orders:list:pending"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎉 Completed", "orders:list:completed"),
			tgbotapi.NewInlineKeyboardButtonData("📊 Stats", "orders:stats"),
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

// listOrders displays orders based on status filter
func (h *TenantWebhookHandler) listOrders(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, status string) {
	var orders []storage.Order
	query := h.Manager.DB.Where("tenant_id = ?", tenantID)

	if status != "all" {
		query = query.Where("status = ?", status)
	}

	query.Order("created_at DESC").Limit(10).Find(&orders)

	if len(orders) == 0 {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("No %s orders found.", status))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "orders:menu"),
			),
		)
		bot.Send(msg)
		return
	}

	text := fmt.Sprintf("📦 *%s Orders* (showing last 10)\n\n", strings.Title(status))
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	for i, order := range orders {
		statusEmoji := "⏳"
		switch order.Status {
		case "confirmed":
			statusEmoji = "✅"
		case "completed":
			statusEmoji = "🎉"
		case "cancelled":
			statusEmoji = "❌"
		}

		text += fmt.Sprintf(
			"%d. %s *%s*\n"+
				"   💰 %s %.2f\n"+
				"   👤 %s\n"+
				"   📅 %s\n\n",
			i+1, statusEmoji, order.OrderNumber,
			order.Currency, order.TotalAmount,
			order.CustomerName,
			order.CreatedAt.Format("Jan 2, 3:04PM"))

		// Add button to view order details
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("View %s", order.OrderNumber),
				fmt.Sprintf("order:view:%d", order.ID),
			),
		})
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "orders:menu"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// viewOrderDetails shows detailed information about a specific order
func (h *TenantWebhookHandler) viewOrderDetails(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, orderID uint) {
	var order storage.Order
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Order not found."))
		return
	}

	// Parse order items
	var items []storage.OrderItem
	if order.Items != "" {
		json.Unmarshal([]byte(order.Items), &items)
	}

	statusEmoji := "⏳"
	switch order.Status {
	case "confirmed":
		statusEmoji = "✅"
	case "completed":
		statusEmoji = "🎉"
	case "cancelled":
		statusEmoji = "❌"
	}

	text := fmt.Sprintf(
		"📦 *Order Details*\n\n"+
			"*Order Number:* %s\n"+
			"*Status:* %s %s\n"+
			"*Payment Reference:* `%s`\n\n"+
			"*Customer:*\n"+
			"• Name: %s\n"+
			"• Phone: %s\n"+
			"• Email: %s\n"+
			"• Platform: %s\n\n"+
			"*Items:*\n",
		order.OrderNumber,
		statusEmoji, strings.Title(order.Status),
		order.PaymentReference,
		order.CustomerName,
		order.CustomerPhone,
		order.CustomerEmail,
		strings.Title(order.Platform))

	for i, item := range items {
		text += fmt.Sprintf("%d. %s - %s %.2f\n", i+1, item.Name, order.Currency, item.TotalPrice)
	}

	text += fmt.Sprintf(
		"\n*Total:* %s %.2f\n"+
			"*Created:* %s\n",
		order.Currency, order.TotalAmount,
		order.CreatedAt.Format("Jan 2, 2006 3:04PM"))

	if order.Notes != "" {
		text += fmt.Sprintf("\n*Notes:* %s\n", order.Notes)
	}

	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Add action buttons based on status
	if order.Status == "pending" {
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm Payment", fmt.Sprintf("order:confirm:%d", order.ID)),
			tgbotapi.NewInlineKeyboardButtonData("📝 Add Notes", fmt.Sprintf("order:notes:%d", order.ID)),
		})
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to Orders", "orders:list:all"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// showOrderStats displays order statistics
func (h *TenantWebhookHandler) showOrderStats(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var stats struct {
		TotalOrders       int64
		PendingOrders     int64
		CompletedOrders   int64
		TotalRevenue      float64
		PendingRevenue    float64
		AverageOrderValue float64
	}

	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ?", tenantID).
		Count(&stats.TotalOrders)

	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "pending").
		Count(&stats.PendingOrders)

	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "completed").
		Count(&stats.CompletedOrders)

	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "completed").
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&stats.TotalRevenue)

	h.Manager.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", tenantID, "pending").
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&stats.PendingRevenue)

	if stats.TotalOrders > 0 {
		stats.AverageOrderValue = stats.TotalRevenue / float64(stats.TotalOrders)
	}

	text := fmt.Sprintf(
		"📊 *Order Statistics*\n\n"+
			"*Overview:*\n"+
			"• Total Orders: %d\n"+
			"• Pending: %d\n"+
			"• Completed: %d\n\n"+
			"*Revenue:*\n"+
			"• Total: NGN %.2f\n"+
			"• Pending: NGN %.2f\n"+
			"• Average Order: NGN %.2f\n",
		stats.TotalOrders,
		stats.PendingOrders,
		stats.CompletedOrders,
		stats.TotalRevenue,
		stats.PendingRevenue,
		stats.AverageOrderValue)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "orders:menu"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// handleOrderCallback processes order-related callbacks
func (h *TenantWebhookHandler) handleOrderCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, action string, arg string) {
	switch action {
	case "menu":
		h.showOrdersMenu(bot, chatID, tenantID)
	case "list":
		// arg format: "all", "pending", "completed" ("confirmed" accepted for backward compatibility)
		if arg == "confirmed" {
			arg = "completed"
		}
		h.listOrders(bot, chatID, tenantID, arg)
	case "view":
		// arg is order ID
		orderID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid order ID."))
			return
		}
		h.viewOrderDetails(bot, chatID, tenantID, uint(orderID))
	case "confirm":
		// arg is order ID - confirm payment
		orderID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid order ID."))
			return
		}
		h.confirmOrderPayment(bot, chatID, tenantID, uint(orderID))
	case "complete":
		// arg is order ID - mark as complete
		orderID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid order ID."))
			return
		}
		h.markOrderComplete(bot, chatID, tenantID, uint(orderID))
	case "stats":
		h.showOrderStats(bot, chatID, tenantID)
	case "notes":
		// arg is order ID - prompt for notes
		orderID, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid order ID."))
			return
		}
		h.promptOrderNotes(bot, chatID, tenantID, uint(orderID))
	}
}

// confirmOrderPayment confirms payment for an order
func (h *TenantWebhookHandler) confirmOrderPayment(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, orderID uint) {
	var order storage.Order
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Order not found."))
		return
	}

	// Check if payment already exists
	var payment storage.Payment
	if err := h.Manager.DB.Where("order_id = ?", orderID).First(&payment).Error; err == nil {
		// Payment exists, update it
		now := time.Now()
		payment.Status = "verified"
		payment.VerificationStatus = "tenant_verified"
		payment.VerifiedAt = &now
		h.Manager.DB.Save(&payment)
	} else {
		// Create new payment
		now := time.Now()
		payment = storage.Payment{
			TenantID:           tenantID,
			OrderID:            orderID,
			CustomerID:         order.CustomerID,
			PaymentReference:   order.PaymentReference,
			Amount:             order.TotalAmount,
			Currency:           order.Currency,
			PaymentMethod:      "manual",
			Status:             "verified",
			VerificationStatus: "tenant_verified",
			VerifiedAt:         &now,
		}
		h.Manager.DB.Create(&payment)
	}

	// Update order status
	now := time.Now()
	order.Status = "completed"
	order.ConfirmedAt = &now
	order.CompletedAt = &now
	h.Manager.DB.Save(&order)

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎉 Order %s marked as completed (payment verified)", order.OrderNumber)))
	h.viewOrderDetails(bot, chatID, tenantID, orderID)
}

// markOrderComplete marks an order as completed
func (h *TenantWebhookHandler) markOrderComplete(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, orderID uint) {
	var order storage.Order
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Order not found."))
		return
	}

	now := time.Now()
	order.Status = "completed"
	order.CompletedAt = &now
	h.Manager.DB.Save(&order)

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎉 Order %s marked as completed!", order.OrderNumber)))
	h.viewOrderDetails(bot, chatID, tenantID, orderID)
}

// generateOrderReceipt generates a receipt for an order
func (h *TenantWebhookHandler) generateOrderReceipt(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, orderID uint) {
	var order storage.Order
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Order not found."))
		return
	}

	// Find payment for this order
	var payment storage.Payment
	if err := h.Manager.DB.Where("order_id = ?", orderID).First(&payment).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ No payment found for this order. Please confirm payment first."))
		return
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}

	// Ensure portal token exists for link fallback
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
		_ = h.Manager.DB.Save(&payment).Error
	}

	// Ensure we have a receipt URL (generate if missing)
	if payment.ReceiptURL == "" || payment.ReceiptID == "" {
		cloudinaryURL := os.Getenv("CLOUDINARY_URL")
		receiptService := receipt.NewReceiptService(h.Manager.DB, h.Manager.Logger, cloudinaryURL, publicURL, nil)
		if generated, err := receiptService.GenerateReceipt(context.Background(), payment.ID, payment.TenantID); err == nil {
			payment = *generated
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
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Could not generate a receipt link for this order."))
		return
	}

	text := fmt.Sprintf(
		"📄 *Receipt*\n\n"+
			"Order: *%s*\n"+
			"Payment: *%s*\n"+
			"Amount: %s %.2f\n\n"+
			"🔗 %s",
		order.OrderNumber,
		payment.PaymentReference,
		payment.Currency,
		payment.Amount,
		receiptLink,
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

// promptOrderNotes prompts user to add notes to an order
func (h *TenantWebhookHandler) promptOrderNotes(bot *tgbotapi.BotAPI, chatID int64, tenantID uint, orderID uint) {
	// Store order ID in session for notes input
	// This would require session management - simplified for now
	bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("📝 To add notes to order, use the format:\n\n"+
			"`notes %d Your notes here`\n\n"+
			"Or visit the dashboard to manage orders.",
			orderID)))
}
