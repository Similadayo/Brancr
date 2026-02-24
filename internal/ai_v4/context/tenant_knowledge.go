package context

import (
	"time"

	"github.com/google/uuid"
)

type TenantKnowledgeSnapshot struct {
	// Identity
	TenantID     uuid.UUID `json:"tenant_id"`
	BusinessName string    `json:"business_name"`
	Industry     string    `json:"industry"`
	Niche        string    `json:"niche"`

	// Profile
	Description    string `json:"description"`
	Tone           string `json:"tone"`
	OperatingHours string `json:"operating_hours"`
	Location       string `json:"location"`
	Website        string `json:"website"`
	Keywords       string `json:"keywords"`
	KnowledgeBase  string `json:"knowledge_base"`

	// Offerings
	Products  []ProductSummary  `json:"products"`
	MenuItems []MenuItemSummary `json:"menu_items"`
	Services  []ServiceSummary  `json:"services"`

	// FAQs & Knowledge
	FAQs             []FAQEntry       `json:"faqs"`
	ServiceKnowledge []KnowledgeEntry `json:"service_knowledge"`

	// Capability flags
	HasProducts bool `json:"has_products"`
	HasMenu     bool `json:"has_menu"`
	HasServices bool `json:"has_services"`

	LastUpdated time.Time `json:"last_updated"`
}

type ProductSummary struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Price           float64 `json:"price"`
	Currency        string  `json:"currency"`
	Category        string  `json:"category"`
	NegotiationMode string  `json:"negotiation_mode"` // default, fixed, auto
	MinPrice        float64 `json:"min_price"`
	Availability    string  `json:"availability"`
	StockCount      int     `json:"stock_count"`
	Variants        string  `json:"variants"`
}

type MenuItemSummary struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Price           float64 `json:"price"`
	Currency        string  `json:"currency"`
	Category        string  `json:"category"`
	NegotiationMode string  `json:"negotiation_mode"`
	MinPrice        float64 `json:"min_price"`
	Availability    string  `json:"availability"`
}

type ServiceSummary struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Category        string  `json:"category"`
	NegotiationMode string  `json:"negotiation_mode"`
	Pricing         string  `json:"pricing"`      // Raw JSON
	Deliverables    string  `json:"deliverables"` // Crucial for service details
	Price           float64 `json:"price"`
	MinPrice        float64 `json:"min_price"`
	MaxPrice        float64 `json:"max_price"`
}

type FAQEntry struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type KnowledgeEntry struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}
