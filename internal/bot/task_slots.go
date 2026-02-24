package bot

// Task slot configurations for each conversational task type

// GetSchedulePostSlots returns the slots needed for scheduling a post
func GetSchedulePostSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:         "media",
			Required:     true,
			Prompt:       "Which photo or video should I use?",
			Alternatives: []string{"Use my latest upload", "Show me options"},
		},
		{
			Name:         "platforms",
			Required:     true,
			Prompt:       "Which platforms should I post to?",
			Alternatives: []string{"Instagram", "Facebook", "TikTok", "All connected"},
		},
		{
			Name:         "schedule_time",
			Required:     true,
			Prompt:       "When should I post it?",
			Alternatives: []string{"Now", "Tomorrow morning", "Tomorrow evening", "Pick best time"},
		},
		{
			Name:         "caption",
			Required:     false,
			Prompt:       "Want to add a caption? (or I can generate one for you)",
			Alternatives: []string{"Generate for me", "No caption"},
		},
	}
}

// GetConnectAccountSlots returns slots for connecting a social account
func GetConnectAccountSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:         "platform",
			Required:     true,
			Prompt:       "Which platform would you like to connect?",
			Alternatives: []string{"Instagram", "Facebook", "TikTok", "WhatsApp", "YouTube"},
		},
	}
}

// GetUpdateBusinessSlots returns slots for updating business info
func GetUpdateBusinessSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:         "field",
			Required:     true,
			Prompt:       "What would you like to update?",
			Alternatives: []string{"Business name", "Description", "Operating hours", "Industry"},
		},
		{
			Name:     "value",
			Required: true,
			Prompt:   "What should the new value be?",
		},
	}
}

// GetAddProductSlots returns slots for adding a product/menu item
func GetAddProductSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:     "name",
			Required: true,
			Prompt:   "What's the name of the item?",
		},
		{
			Name:     "price",
			Required: true,
			Prompt:   "What's the price?",
		},
		{
			Name:         "description",
			Required:     false,
			Prompt:       "Any description? (optional)",
			Alternatives: []string{"Skip"},
		},
	}
}

// GetAddOfferSlots returns slots for adding a promotional offer
func GetAddOfferSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:     "offer_text",
			Required: true,
			Prompt:   "What's the offer? (e.g., 'Buy 2 get 1 free' or '20% off orders above ₦5000')",
		},
		{
			Name:         "expiry",
			Required:     false,
			Prompt:       "When does it expire? (optional)",
			Alternatives: []string{"No expiry", "End of today", "End of week"},
		},
	}
}

// GetViewAnalyticsSlots returns slots for viewing analytics
func GetViewAnalyticsSlots() []TaskSlot {
	return []TaskSlot{
		{
			Name:         "period",
			Required:     false,
			Prompt:       "What time period?",
			Alternatives: []string{"Today", "This week", "This month", "Last 30 days"},
		},
		{
			Name:         "platform",
			Required:     false,
			Prompt:       "Any specific platform?",
			Alternatives: []string{"All platforms", "Instagram", "Facebook", "TikTok"},
		},
	}
}

// TaskTypeToSlots maps task types to their slot configurations
var TaskTypeToSlots = map[string]func() []TaskSlot{
	"schedule_post":    GetSchedulePostSlots,
	"connect_account":  GetConnectAccountSlots,
	"update_business":  GetUpdateBusinessSlots,
	"add_product":      GetAddProductSlots,
	"add_offer":        GetAddOfferSlots,
	"view_analytics":   GetViewAnalyticsSlots,
}

// GetSlotsForTaskType returns the appropriate slots for a task type
func GetSlotsForTaskType(taskType string) []TaskSlot {
	if slotFunc, exists := TaskTypeToSlots[taskType]; exists {
		return slotFunc()
	}
	return nil
}
