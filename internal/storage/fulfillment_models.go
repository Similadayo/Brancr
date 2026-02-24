package storage

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Requirement defines a specific piece of information a tenant needs to collect.
// e.g., "Upload Passport", "Date of Birth", "Brand Colors".
type Requirement struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"tenant_id"`
	Label       string         `gorm:"type:text;not null" json:"label"`                        // Human readable question "What is your shoe size?"
	DataType    string         `gorm:"type:varchar(50);not null" json:"data_type"`             // text, number, date, file, choice
	AppliesTo   string         `gorm:"type:varchar(20);default:'any';index" json:"applies_to"` // any, delivery, pickup
	IsRequired  bool           `gorm:"default:true" json:"is_required"`
	Options     string         `gorm:"type:text" json:"options"`     // JSON array for 'choice' type
	Description string         `gorm:"type:text" json:"description"` // Help text for the customer
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// ItemRequirement links a Requirement to a specific Catalog Item (Service/Product).
// This allows reuse: "Brand Colors" can be linked to "Logo Design" and "Website Dev".
type ItemRequirement struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TenantID      uuid.UUID `gorm:"type:uuid;not null;index" json:"tenant_id"`
	ItemID        string    `gorm:"type:varchar(255);not null;index" json:"item_id"` // Normalized name or ID of the service/product
	RequirementID uuid.UUID `gorm:"type:uuid;not null" json:"requirement_id"`
	OrderIndex    int       `gorm:"default:0" json:"order_index"` // 0 = ask first, 1 = ask second...

	Requirement Requirement `gorm:"foreignKey:RequirementID" json:"requirement"`
}

// FulfillmentState tracks the overall progress of collecting info for a specific Order Item.
type FulfillmentState struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID `gorm:"type:uuid;not null;index" json:"tenant_id"`
	OrderID     uuid.UUID `gorm:"type:uuid;not null;index" json:"order_id"`
	OrderItemID string    `gorm:"type:varchar(255);not null" json:"order_item_id"` // Specific item in the order (e.g. "Logo Design")
	CustomerID  uuid.UUID `gorm:"type:uuid;not null;index" json:"customer_id"`

	Status string `gorm:"type:varchar(50);default:'not_started'" json:"status"` // not_started, collecting, completed
	Notes  string `gorm:"type:text" json:"notes"`                               // Internal notes

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RequirementSubmission stores the ACTUAL ANSWER provided by the customer.
type RequirementSubmission struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	FulfillmentStateID uuid.UUID `gorm:"type:uuid;not null;index" json:"fulfillment_state_id"`
	RequirementID      uuid.UUID `gorm:"type:uuid;not null;index" json:"requirement_id"`

	Value    string `gorm:"type:text" json:"value"`        // The answer (text, url, choice)
	Verified bool   `gorm:"default:false" json:"verified"` // Can be used for manual tenant review

	CreatedAt time.Time `json:"created_at"`
}
