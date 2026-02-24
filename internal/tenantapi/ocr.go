package tenantapi

import (
	"context"
	"errors"
)

// OCRService provides an interface for performing OCR on images
type OCRService interface {
	ExtractText(ctx context.Context, imageBytes []byte) (string, error)
}

// simple stub implementation used in tests / local dev
type NoopOCR struct{}

func (n NoopOCR) ExtractText(ctx context.Context, imageBytes []byte) (string, error) {
	return "", errors.New("ocr not configured")
}
