package domain

type MerchantConfig struct {
	MerchantID string

	Currency               string
	MaxNegotiationAttempts int
	MaxDiscountPercent     int
	AllowedPaymentMethods  []string
	TonePreset             string // e.g. "professional", "casual", "empathetic"

	// Identity / Knowledge
	BusinessName string
	Industry     string
	Description  string
}
