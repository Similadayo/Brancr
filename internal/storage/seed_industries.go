package storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SeedIndustryDefaults seeds default industry packs with tones, templates, and escalation rules
// This is idempotent - it will skip industries that already have defaults configured
func SeedIndustryDefaults(db *gorm.DB, logger *zap.Logger) error {
	// Define industry defaults
	industryDefaults := []struct {
		IndustryName       string
		DefaultTone        string
		DefaultResponse    string // IntentType string
		DefaultStyle       string
		EscalationBehavior string
		EscalationTriggers []string
		Templates          []struct {
			Name     string
			Category string
			Trigger  string
			Message  string
		}
	}{
		{
			IndustryName:       "E-commerce",
			DefaultTone:        "Friendly, sales-oriented, helpful",
			DefaultResponse:    "product_advisory",
			DefaultStyle:       "Focus on product benefits. Use casual language. Mention discounts when appropriate.",
			EscalationBehavior: "configurable",
			EscalationTriggers: []string{"refund", "complaint", "manager", "lawyer", "scam"},
			Templates: []struct {
				Name     string
				Category string
				Trigger  string
				Message  string
			}{
				{"Price Inquiry", "inquiry", "how much, price, cost", "Hi! The {product} is currently {price}. Would you like to place an order?"},
				{"Delivery Time", "inquiry", "delivery, shipping, how long", "We typically deliver within 2-3 business days in Lagos, and 3-5 days nationwide!"},
				{"Out of Stock", "support", "available, in stock", "That item is currently out of stock, but we're restocking soon! Can I notify you when it's back?"},
				{"Order Status", "support", "my order, where is, tracking", "Let me check your order status. Could you share your order number or phone number?"},
				{"Return Policy", "support", "return, exchange, refund", "We accept returns within 7 days of delivery. The item must be unused and in original packaging."},
			},
		},
		{
			IndustryName:       "Food & Beverage",
			DefaultTone:        "Warm, friendly, appetizing",
			DefaultResponse:    "product_advisory",
			DefaultStyle:       "Use food emojis. Be enthusiastic about menu items. Always mention specials.",
			EscalationBehavior: "always_on",
			EscalationTriggers: []string{"sick", "food poisoning", "refund", "complaint"},
			Templates: []struct {
				Name     string
				Category string
				Trigger  string
				Message  string
			}{
				{"Menu Inquiry", "inquiry", "menu, what do you have, options", "Here's our menu! 🍽️ We have {menu_highlights}. What catches your eye?"},
				{"Operating Hours", "inquiry", "open, hours, when", "We're open {hours}. Can't wait to serve you! 🤗"},
				{"Reservation", "inquiry", "book, reserve, table", "I'd love to help you book a table! How many guests and what date/time works for you?"},
				{"Delivery Available", "inquiry", "deliver, delivery", "Yes! We deliver within {delivery_area}. Minimum order is {min_order}. 🚗"},
				{"Special Dietary", "support", "vegetarian, vegan, halal, allergy", "We've got options for you! Let me check our {dietary} menu items..."},
			},
		},
		{
			IndustryName:       "Real Estate",
			DefaultTone:        "Professional, trustworthy, informative",
			DefaultResponse:    "product_advisory",
			DefaultStyle:       "Be detailed about property features. Use professional language. Always ask about budget and preferences.",
			EscalationBehavior: "always_on",
			EscalationTriggers: []string{"lawyer", "legal", "fraud", "scam", "complaint"},
			Templates: []struct {
				Name     string
				Category string
				Trigger  string
				Message  string
			}{
				{"Property Inquiry", "inquiry", "looking for, need a place, apartment, house", "I'd love to help you find the perfect property! Are you looking to rent or buy? And what area do you prefer?"},
				{"Price Range", "inquiry", "budget, how much, price", "We have properties ranging from {min_price} to {max_price}. What's your budget range?"},
				{"Schedule Viewing", "inquiry", "see, visit, inspection", "I can schedule a viewing for you! When are you available this week?"},
				{"Location Info", "inquiry", "where, location, area", "This property is located in {location}, which is known for {location_highlights}."},
				{"Documentation", "support", "documents, papers, requirements", "For {transaction_type}, you'll need: {required_docs}. I can guide you through the process."},
			},
		},
		{
			IndustryName:       "Fashion",
			DefaultTone:        "Trendy, visual, enthusiastic",
			DefaultResponse:    "product_advisory",
			DefaultStyle:       "Use fashion terminology. Be excited about new arrivals. Recommend styling tips.",
			EscalationBehavior: "configurable",
			EscalationTriggers: []string{"refund", "fake", "complaint", "manager"},
			Templates: []struct {
				Name     string
				Category string
				Trigger  string
				Message  string
			}{
				{"Size Guide", "inquiry", "size, fit, measurement", "For the perfect fit, here are the measurements: {size_chart}. What size do you usually wear?"},
				{"New Arrivals", "inquiry", "new, latest, just dropped", "OMG yes! 🔥 We just dropped {new_arrivals}. They're selling fast!"},
				{"Styling Tips", "inquiry", "wear with, style, match", "Great choice! That would look amazing with {styling_suggestion}! 💃"},
				{"Availability", "inquiry", "available, have, stock", "Let me check... Yes! We have that in {available_options}. Which would you like?"},
				{"Custom Order", "support", "custom, bespoke, made to order", "We do custom orders! It takes {custom_timeline} and costs {custom_price_range}."},
			},
		},
		{
			IndustryName:       "Professional Services",
			DefaultTone:        "Expert, helpful, concise",
			DefaultResponse:    "service_inquiry",
			DefaultStyle:       "Be professional but approachable. Focus on solving problems. Mention experience and credentials when relevant.",
			EscalationBehavior: "always_on",
			EscalationTriggers: []string{"complaint", "sue", "lawyer", "report"},
			Templates: []struct {
				Name     string
				Category string
				Trigger  string
				Message  string
			}{
				{"Service Inquiry", "inquiry", "services, what do you do, help with", "We specialize in {services}. How can we help you today?"},
				{"Pricing", "inquiry", "cost, price, rates, fees", "Our rates depend on the scope of work. Could you tell me more about what you need?"},
				{"Consultation", "inquiry", "consult, meeting, discuss", "I'd be happy to schedule a consultation! We offer {consultation_options}."},
				{"Turnaround Time", "inquiry", "how long, timeline, when", "Typical turnaround is {timeline}. Rush services are available for an additional fee."},
				{"Credentials", "support", "qualified, certified, experience", "We have {years} years of experience and are certified in {certifications}."},
			},
		},
	}

	for _, id := range industryDefaults {
		// Find industry by name
		var industry Industry
		if err := db.Where("name = ?", id.IndustryName).First(&industry).Error; err != nil {
			logger.Warn("Industry not found, skipping pack", zap.String("industry", id.IndustryName))
			continue
		}

		// Check if defaults already exist
		var existingDefaults IndustryDefaults
		if err := db.Where("industry_id = ?", industry.ID).First(&existingDefaults).Error; err == nil {
			logger.Debug("Industry defaults already exist, skipping", zap.String("industry", id.IndustryName))
			continue
		}

		// Create IndustryDefaults
		triggersJSON, _ := json.Marshal(id.EscalationTriggers)
		defaults := IndustryDefaults{
			ID:                 uuid.New(),
			IndustryID:         industry.ID,
			DefaultTone:        id.DefaultTone,
			DefaultResponse:    id.DefaultResponse,
			DefaultStyle:       id.DefaultStyle,
			EscalationBehavior: id.EscalationBehavior,
			EscalationTriggers: string(triggersJSON),
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		if err := db.Create(&defaults).Error; err != nil {
			logger.Error("Failed to create industry defaults", zap.String("industry", id.IndustryName), zap.Error(err))
			continue
		}

		// Create IndustryTemplates
		for i, t := range id.Templates {
			template := IndustryTemplate{
				ID:         uuid.New(),
				IndustryID: industry.ID,
				Name:       t.Name,
				Category:   t.Category,
				Trigger:    t.Trigger,
				Message:    t.Message,
				IsActive:   true,
				SortOrder:  i,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}
			if err := db.Create(&template).Error; err != nil {
				logger.Error("Failed to create industry template", zap.String("template", t.Name), zap.Error(err))
			}
		}

		logger.Info("Seeded industry pack", zap.String("industry", id.IndustryName), zap.Int("templates", len(id.Templates)))
	}

	return nil
}
