package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// showProductsMenu displays the products management menu
func (h *TenantWebhookHandler) showProductsMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	ctx := context.Background()

	// Get tenant industry to determine what type of items they manage
	var tenant storage.Tenant
	h.Manager.DB.First(&tenant, tenantID)

	industryService := h.Manager.IndustrySelectionHandler.IndustryService
	capabilities, err := industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Could not load industry capabilities."))
		return
	}

	text := "📦 *Product Management*\n\n"
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	if capabilities.HasProducts {
		var productCount int64
		h.Manager.DB.Model(&storage.Product{}).Where("tenant_id = ?", tenantID).Count(&productCount)
		text += fmt.Sprintf("📦 Products: %d\n", productCount)
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("➕ Add Product", "products:add:product"),
			tgbotapi.NewInlineKeyboardButtonData("📋 List Products", "products:list:product"),
		})
	}

	if capabilities.HasMenu {
		var menuCount int64
		h.Manager.DB.Model(&storage.MenuItem{}).Where("tenant_id = ?", tenantID).Count(&menuCount)
		text += fmt.Sprintf("🍽️ Menu Items: %d\n", menuCount)
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("➕ Add Menu Item", "products:add:menu"),
			tgbotapi.NewInlineKeyboardButtonData("📋 List Menu Items", "products:list:menu"),
		})
	}

	if capabilities.HasServices {
		var serviceCount int64
		h.Manager.DB.Model(&storage.Service{}).Where("tenant_id = ?", tenantID).Count(&serviceCount)
		text += fmt.Sprintf("💼 Services: %d\n", serviceCount)
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("➕ Add Service", "products:add:service"),
			tgbotapi.NewInlineKeyboardButtonData("📋 List Services", "products:list:service"),
		})
	}

	text += "\nSelect an option:"

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "menu:main"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// listProducts displays products, menu items, or services
func (h *TenantWebhookHandler) listProducts(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID, itemType string) {
	text := fmt.Sprintf("📋 *%s*\n\n", strings.Title(strings.ReplaceAll(itemType, "_", " ")))
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	switch itemType {
	case "product":
		var products []storage.Product
		h.Manager.DB.Where("tenant_id = ?", tenantID).
			Order("created_at DESC").
			Limit(10).
			Find(&products)

		if len(products) == 0 {
			text = "No products found. Add your first product!"
			keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("➕ Add Product", "products:add:product"),
			})
		} else {
			for i, p := range products {
				status := "✅ Active"
				if !p.IsActive {
					status = "⏸️ Inactive"
				}
				text += fmt.Sprintf(
					"%d. *%s*\n"+
						"   💰 %s %.2f\n"+
						"   📂 %s\n"+
						"   %s\n\n",
					i+1, p.Name,
					p.Currency, p.Price,
					p.Category,
					status)

				keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("View %s", p.Name),
						fmt.Sprintf("product:view:%d:%s", p.ID, itemType),
					),
				})
			}
		}

	case "menu":
		var items []storage.MenuItem
		h.Manager.DB.Where("tenant_id = ?", tenantID).
			Order("created_at DESC").
			Limit(10).
			Find(&items)

		if len(items) == 0 {
			text = "No menu items found. Add your first menu item!"
			keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("➕ Add Menu Item", "products:add:menu"),
			})
		} else {
			for i, item := range items {
				status := "✅ Available"
				if item.Availability == "unavailable" {
					status = "❌ Unavailable"
				}
				text += fmt.Sprintf(
					"%d. *%s*\n"+
						"   💰 %s %.2f\n"+
						"   📂 %s\n"+
						"   %s\n\n",
					i+1, item.Name,
					item.Currency, item.Price,
					item.Category,
					status)

				keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("View %s", item.Name),
						fmt.Sprintf("product:view:%d:%s", item.ID, itemType),
					),
				})
			}
		}

	case "service":
		var services []storage.Service
		h.Manager.DB.Where("tenant_id = ?", tenantID).
			Order("created_at DESC").
			Limit(10).
			Find(&services)

		if len(services) == 0 {
			text = "No services found. Add your first service!"
			keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("➕ Add Service", "products:add:service"),
			})
		} else {
			for i, s := range services {
				status := "✅ Active"
				if !s.IsActive {
					status = "⏸️ Inactive"
				}

				// Parse pricing
				var pricing map[string]interface{}
				if s.Pricing != "" {
					json.Unmarshal([]byte(s.Pricing), &pricing)
				}

				priceText := "Price varies"
				if pricing != nil {
					if pType, ok := pricing["type"].(string); ok {
						if pType == "fixed" {
							if amount, ok := pricing["amount"].(float64); ok {
								priceText = fmt.Sprintf("Fixed: NGN %.2f", amount)
							}
						} else if pType == "hourly" {
							if rate, ok := pricing["rate"].(float64); ok {
								priceText = fmt.Sprintf("Hourly: NGN %.2f/hr", rate)
							}
						}
					}
				}

				text += fmt.Sprintf(
					"%d. *%s*\n"+
						"   %s\n"+
						"   📂 %s\n"+
						"   %s\n\n",
					i+1, s.Name,
					priceText,
					s.Category,
					status)

				keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("View %s", s.Name),
						fmt.Sprintf("product:view:%d:%s", s.ID, itemType),
					),
				})
			}
		}
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "products:menu"),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// viewProductDetails shows detailed information about a product/menu/service
func (h *TenantWebhookHandler) viewProductDetails(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID, itemID uuid.UUID, itemType string) {
	text := ""
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	switch itemType {
	case "product":
		var product storage.Product
		if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&product).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Product not found."))
			return
		}

		text = fmt.Sprintf(
			"📦 *Product Details*\n\n"+
				"*Name:* %s\n"+
				"*Price:* %s %.2f\n"+
				"*Category:* %s\n"+
				"*Status:* %s\n"+
				"*Description:*\n%s\n",
			product.Name,
			product.Currency, product.Price,
			product.Category,
			func() string {
				if product.IsActive {
					return "✅ Active"
				}
				return "⏸️ Inactive"
			}(),
			product.Description)

		if product.SKU != "" {
			text += fmt.Sprintf("\n*SKU:* %s\n", product.SKU)
		}

		if product.StockCount >= 0 {
			text += fmt.Sprintf("*Stock:* %d\n", product.StockCount)
		}

		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", fmt.Sprintf("product:edit:%d:%s", product.ID, itemType)),
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Delete", fmt.Sprintf("product:delete:%d:%s", product.ID, itemType)),
		})

	case "menu":
		var item storage.MenuItem
		if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&item).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Menu item not found."))
			return
		}

		text = fmt.Sprintf(
			"🍽️ *Menu Item Details*\n\n"+
				"*Name:* %s\n"+
				"*Price:* %s %.2f\n"+
				"*Category:* %s\n"+
				"*Availability:* %s\n"+
				"*Description:*\n%s\n",
			item.Name,
			item.Currency, item.Price,
			item.Category,
			strings.Title(item.Availability),
			item.Description)

		if item.PreparationTime > 0 {
			text += fmt.Sprintf("\n*Prep Time:* %d minutes\n", item.PreparationTime)
		}

		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", fmt.Sprintf("product:edit:%d:%s", item.ID, itemType)),
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Delete", fmt.Sprintf("product:delete:%d:%s", item.ID, itemType)),
		})

	case "service":
		var service storage.Service
		if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&service).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Service not found."))
			return
		}

		var pricing map[string]interface{}
		if service.Pricing != "" {
			json.Unmarshal([]byte(service.Pricing), &pricing)
		}

		priceText := "Price varies"
		if pricing != nil {
			if pType, ok := pricing["type"].(string); ok {
				if pType == "fixed" {
					if amount, ok := pricing["amount"].(float64); ok {
						priceText = fmt.Sprintf("Fixed: NGN %.2f", amount)
					}
				} else if pType == "hourly" {
					if rate, ok := pricing["rate"].(float64); ok {
						priceText = fmt.Sprintf("Hourly: NGN %.2f/hr", rate)
					}
				}
			}
		}

		text = fmt.Sprintf(
			"💼 *Service Details*\n\n"+
				"*Name:* %s\n"+
				"*Pricing:* %s\n"+
				"*Category:* %s\n"+
				"*Duration:* %s\n"+
				"*Status:* %s\n"+
				"*Description:*\n%s\n",
			service.Name,
			priceText,
			service.Category,
			service.Duration,
			func() string {
				if service.IsActive {
					return "✅ Active"
				}
				return "⏸️ Inactive"
			}(),
			service.Description)

		if service.Deliverables != "" {
			text += fmt.Sprintf("\n*Deliverables:*\n%s\n", service.Deliverables)
		}

		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", fmt.Sprintf("product:edit:%d:%s", service.ID, itemType)),
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Delete", fmt.Sprintf("product:delete:%d:%s", service.ID, itemType)),
		})
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", fmt.Sprintf("products:list:%s", itemType)),
	})

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// handleProductCallback processes product-related callbacks
func (h *TenantWebhookHandler) handleProductCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, action string, arg string) {
	switch action {
	case "menu":
		h.showProductsMenu(bot, chatID, tenantID)
	case "list":
		// arg format: "product", "menu", "service"
		h.listProducts(bot, chatID, tenantID, arg)
	case "add":
		// arg format: "product", "menu", "service"
		bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("➕ To add a new %s, please use the dashboard:\n\n"+
				"https://www.brancr.com/dashboard/products\n\n"+
				"Or use the API endpoints for programmatic access.",
				arg)))
	case "view":
		// arg format: "<item_id>:<item_type>"
		parts := strings.Split(arg, ":")
		if len(parts) != 2 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid item ID."))
			return
		}
		itemID, err := uuid.Parse(parts[0])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid item ID."))
			return
		}
		h.viewProductDetails(bot, chatID, tenantID, itemID, parts[1])
	case "edit":
		// arg format: "<item_id>:<item_type>"
		bot.Send(tgbotapi.NewMessage(chatID,
			"✏️ To edit items, please use the dashboard:\n\n"+
				"https://www.brancr.com/dashboard/products"))
	case "delete":
		// arg format: "<item_id>:<item_type>"
		parts := strings.Split(arg, ":")
		if len(parts) != 2 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid item ID."))
			return
		}
		itemID, err := uuid.Parse(parts[0])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid item ID."))
			return
		}
		h.deleteProduct(bot, chatID, tenantID, itemID, parts[1])
	}
}

// deleteProduct deletes a product/menu/service
func (h *TenantWebhookHandler) deleteProduct(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID, itemID uuid.UUID, itemType string) {
	var name string
	var err error

	switch itemType {
	case "product":
		var product storage.Product
		if err = h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&product).Error; err == nil {
			name = product.Name
			h.Manager.DB.Delete(&product)
		}
	case "menu":
		var item storage.MenuItem
		if err = h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&item).Error; err == nil {
			name = item.Name
			h.Manager.DB.Delete(&item)
		}
	case "service":
		var service storage.Service
		if err = h.Manager.DB.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&service).Error; err == nil {
			name = service.Name
			h.Manager.DB.Delete(&service)
		}
	}

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Item not found or could not be deleted."))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑️ Deleted: %s", name)))
	h.listProducts(bot, chatID, tenantID, itemType)
}
