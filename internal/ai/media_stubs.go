package ai

import (
	"context"

	"go.uber.org/zap"
)

// MediaType represents the type of media content
type MediaType string

const (
	MediaTypeUnknown        MediaType = "unknown"
	MediaTypePaymentReceipt MediaType = "payment_receipt"
	MediaTypeImage          MediaType = "image"
	MediaTypeVideo          MediaType = "video"
	MediaTypeDocument       MediaType = "document"
)

// MediaClassification represents the result of media classification
type MediaClassification struct {
	Type       MediaType
	Confidence float64
	Details    string
}

// DetectOfferInText detects if text contains an offer and returns the offer amount
// Stub implementation for V3 consolidation - returns 0.0 (no offer detected)
func DetectOfferInText(text string) float64 {
	// V3 pipeline will handle full offer detection
	// Return 0.0 to indicate no offer detected
	return 0.0
}

// ClassifyMedia classifies media content
// Stub implementation for V3 consolidation
func ClassifyMedia(ctx context.Context, mediaURL string, caption string, logger *zap.Logger) MediaClassification {
	// V3 pipeline will handle media classification
	return MediaClassification{
		Type:       MediaTypeUnknown,
		Confidence: 0.0,
		Details:    "Media classification not implemented in V3",
	}
}
