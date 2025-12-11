package utils

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// GenerateOrderNumber generates a unique order number
// Format: ORD-{YEAR}-{SEQUENTIAL}
// Example: ORD-2024-001234
func GenerateOrderNumber(tenantID uint, sequenceNumber int) string {
	year := time.Now().Year()
	return fmt.Sprintf("ORD-%d-%06d", year, sequenceNumber)
}

// GeneratePaymentReference generates a unique payment reference code that starts with the tenant name
// and includes a hint of the ordered item.
// Format: {TENANT}-{ITEM}-{DATE}-{RANDOM}
// Example: ACME-CAKE-20241204-ABC1
func GeneratePaymentReference(tenantName string, tenantID uint, itemLabel string) string {
	date := time.Now().Format("20060102") // YYYYMMDD format
	random := generateRandomString(4)

	tenantSlug := refSlug(tenantName, 10)
	if tenantSlug == "" {
		tenantSlug = fmt.Sprintf("TENANT%d", tenantID)
	}

	itemSlug := refSlug(itemLabel, 10)
	if itemSlug == "" {
		itemSlug = "ORDER"
	}

	return fmt.Sprintf("%s-%s-%s-%s", tenantSlug, itemSlug, date, random)
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

// refSlug uppercases, removes spaces/non-alphanumerics, and truncates to maxLen.
func refSlug(val string, maxLen int) string {
	val = strings.ToUpper(strings.TrimSpace(val))
	slug := strings.Builder{}
	for _, r := range val {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			slug.WriteRune(r)
		}
		if slug.Len() >= maxLen {
			break
		}
	}
	return slug.String()
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
