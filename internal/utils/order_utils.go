package utils

import (
	"fmt"
	"math/rand"
	"time"
)

// GenerateOrderNumber generates a unique order number
// Format: ORD-{YEAR}-{SEQUENTIAL}
// Example: ORD-2024-001234
func GenerateOrderNumber(tenantID uint, sequenceNumber int) string {
	year := time.Now().Year()
	return fmt.Sprintf("ORD-%d-%06d", year, sequenceNumber)
}

// GeneratePaymentReference generates a unique payment reference code
// Format: BRNCR-{TENANT_ID}-{DATE}-{RANDOM}
// Example: BRNCR-123-20241204-ABC123
func GeneratePaymentReference(tenantID uint) string {
	date := time.Now().Format("20060102") // YYYYMMDD format
	random := generateRandomString(6)
	return fmt.Sprintf("BRNCR-%d-%s-%s", tenantID, date, random)
}

// generateRandomString generates a random alphanumeric string
func generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// GenerateReceiptID generates a unique receipt ID
// Format: RCPT-{YEAR}-{RANDOM}
// Example: RCPT-2024-ABC123
func GenerateReceiptID() string {
	year := time.Now().Year()
	random := generateRandomString(6)
	return fmt.Sprintf("RCPT-%d-%s", year, random)
}

// GeneratePortalToken generates a unique token for customer portal access
// Format: {RANDOM_32_CHARS}
func GeneratePortalToken() string {
	return generateRandomString(32)
}

