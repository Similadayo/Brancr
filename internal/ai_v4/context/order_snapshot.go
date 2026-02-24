package context

type OrderSnapshot struct {
	HasActiveOrder bool        `json:"has_active_order"`
	Items          []OrderItem `json:"items"`
	TotalPrice     int64       `json:"total_price"` // Calculated ONLY in Go
	Currency       string      `json:"currency"`
	Stage          string      `json:"stage"` // "collecting" | "clarifying" | "confirming" | "awaiting_delivery_choice" | "awaiting_delivery_address" | "awaiting_pickup_time" | "collecting_requirements" | "payment_ready" | "awaiting_payment_method" | "payment_sent" | "fulfilled" | "cancelled"

	// Hard SKU lock state (v4 authoritative guard against cross-SKU drift).
	ActiveItemID           string `json:"active_item_id,omitempty"`
	ActiveVariantID        string `json:"active_variant_id,omitempty"`
	PendingSwitchItemID    string `json:"pending_switch_item_id,omitempty"`
	PendingSwitchVariantID string `json:"pending_switch_variant_id,omitempty"`
	AgreedUnitPrice        int64  `json:"agreed_unit_price,omitempty"`
	SwitchConfirmed        bool   `json:"switch_confirmed,omitempty"`

	// Order tracking
	OrderID string `json:"order_id,omitempty"` // DB order ID after saving

	// Delivery/Pickup details
	DeliveryMethod   string `json:"delivery_method,omitempty"`    // "delivery" | "pickup" | ""
	DeliveryAddress  string `json:"delivery_address,omitempty"`   // Customer's delivery address
	DeliveryFee      int64  `json:"delivery_fee,omitempty"`       // Calculated delivery fee
	DeliveryAreaName string `json:"delivery_area_name,omitempty"` // Matched area name
	PickupTime       string `json:"pickup_time,omitempty"`        // Requested pickup time
	RecipientName    string `json:"recipient_name,omitempty"`     // Customer-provided recipient name
	RecipientPhone   string `json:"recipient_phone,omitempty"`    // Customer-provided recipient phone
	DeliveryNotes    string `json:"delivery_notes,omitempty"`     // Customer-provided delivery notes/instructions
	LastPromptKey    string `json:"last_prompt_key,omitempty"`    // Used to avoid repeated question loops
}

type OrderItem struct {
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
}
