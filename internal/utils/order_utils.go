package utils

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateOrderNumber generates a unique order number
// Format: ORD-{YEAR}-{SEQUENTIAL}
// Example: ORD-2024-001234
func GenerateOrderNumber(tenantID uuid.UUID, sequenceNumber int) string {
	year := time.Now().Year()
	return fmt.Sprintf("ORD-%d-%06d", year, sequenceNumber)
}

// GeneratePaymentReference generates a unique payment reference code
// Format: {TENANT_SHORT}-{RANDOM4} for easy readability
// Example: BANANA-A1B2
func GeneratePaymentReference(tenantName string, tenantID uuid.UUID, itemLabel string) string {
	random := generateRandomString(4)

	tenantSlug := refSlug(tenantName, 8) // Max 8 chars for tenant name
	if tenantSlug == "" {
		// Fallback: use first 6 chars of tenant ID
		tenantSlug = strings.ToUpper(tenantID.String()[:6])
	}

	return fmt.Sprintf("%s-%s", tenantSlug, random)
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

var ngnAmountRegex = regexp.MustCompile(`(?i)(?:\bngn\b|\bnaira\b|₦)?\s*([0-9]{1,3}(?:[,0-9]*)(?:\.[0-9]+)?)`)

// ExtractLikelyAmountNGN attempts to extract a likely NGN amount from free text.
// It prioritizes values explicitly marked with NGN/naira/₦ and ignores phone-like numbers when unmarked.
func ExtractLikelyAmountNGN(text string) float64 {
	text = strings.ToLower(text)

	matches := ngnAmountRegex.FindAllStringSubmatch(text, -1)
	var best float64
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		num := strings.ReplaceAll(m[1], ",", "")
		full := m[0]
		// Ignore phone-like numbers (typically 10+ digits) unless currency is explicitly present.
		if len(num) >= 10 {
			if !strings.Contains(full, "ngn") && !strings.Contains(full, "naira") && !strings.Contains(full, "₦") {
				continue
			}
		}
		val, err := strconv.ParseFloat(num, 64)
		if err != nil {
			continue
		}
		// Ignore tiny numbers that are likely option selections (e.g. "go for 1") unless explicitly currency-marked.
		hasCurrency := strings.Contains(full, "ngn") || strings.Contains(full, "naira") || strings.Contains(full, "₦")
		if !hasCurrency && val > 0 && val < 50 {
			continue
		}
		if val > best {
			best = val
		}
	}

	return best
}
