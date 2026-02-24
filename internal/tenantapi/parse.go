package tenantapi

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedItem is a candidate product/menu/service parsed from free text
type ParsedItem struct {
	Name          string              `json:"name"`
	Price         *float64            `json:"price,omitempty"`     // Single price or max price
	MinPrice      *float64            `json:"min_price,omitempty"` // Min price for ranges
	MaxPrice      *float64            `json:"max_price,omitempty"` // Max price for ranges
	Currency      string              `json:"currency,omitempty"`
	Category      string              `json:"category,omitempty"`
	Type          string              `json:"type,omitempty"`           // "product", "service", "subscription", "menu_item"
	Description   string              `json:"description,omitempty"`    // Item description or features
	Deliverables  []string            `json:"deliverables,omitempty"`   // List of specific deliverables/items included
	Duration      string              `json:"duration,omitempty"`       // Duration of service (e.g. "1 month")
	BillingPeriod string              `json:"billing_period,omitempty"` // "monthly", "yearly", "one-time", etc.
	Variants      map[string][]string `json:"variants,omitempty"`       // extracted variants e.g. {"color": ["red", "blue"], "storage": ["256GB"]}
	Flags         map[string]bool     `json:"flags,omitempty"`
	Confidence    float64             `json:"confidence"`
}

// Parser is an interface for parsing free text into structured ParsedItems
type Parser interface {
	Parse(raw string) ([]ParsedItem, error)
}

// DefaultParser is a simple rule-based parser that extracts lines of the form "Name - Price"
type DefaultParser struct {
	priceRe      *regexp.Regexp
	priceRangeRe *regexp.Regexp // For detecting price ranges like "₦150,000 - ₦250,000"
	headerRe     *regexp.Regexp // For detecting header rows
	separatorRe  *regexp.Regexp // For detecting separator lines (-----, ====, etc.)
}

func NewDefaultParser() *DefaultParser {
	// match: name - ₦3,500 or name - 3500 or name – ₦3,500
	priceRe := regexp.MustCompile(`^\s*(?P<name>.+?)\s*[–-]\s*([₦$£€]?\s?[0-9\,\.]+(?:\s*[–-]\s*[₦$£€]?\s?[0-9\,\.]+)?)\s*(?:negotiable|fixed)?\s*$`)

	// Match price ranges: ₦150,000 - ₦250,000 or 150000 - 250000
	priceRangeRe := regexp.MustCompile(`([₦$£€]?\s?[0-9\,\.]+)\s*[–-]\s*([₦$£€]?\s?[0-9\,\.]+)`)

	// Match header rows (all caps, common header words)
	headerRe := regexp.MustCompile(`(?i)^\s*(SERVICE|NAME|ITEM|PRODUCT|MENU|PRICE|COST|AMOUNT|NEGOTIATION|MODE|TYPE|CATEGORY|DESCRIPTION).*$`)

	// Match separator lines (mostly dashes, underscores, equals, or similar)
	separatorRe := regexp.MustCompile(`^[\s\-_=─━┅]+$`)

	return &DefaultParser{
		priceRe:      priceRe,
		priceRangeRe: priceRangeRe,
		headerRe:     headerRe,
		separatorRe:  separatorRe,
	}
}

// Parse extracts one item per non-empty line
func (p *DefaultParser) Parse(raw string) ([]ParsedItem, error) {
	lines := strings.Split(raw, "\n")
	var out []ParsedItem
	var currentCategory string // Track category from parent lines

	for _, l := range lines {
		line := strings.TrimSpace(l)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip separator lines (----, ====, etc.)
		if p.separatorRe.MatchString(line) {
			continue
		}

		// Skip header rows (SERVICE NAME, PRICE, etc.)
		if p.headerRe.MatchString(line) {
			continue
		}

		item := ParsedItem{Flags: map[string]bool{}, Confidence: 0.8}

		// Clean up bullet points and leading dashes
		cleanedLine := strings.TrimSpace(line)
		if strings.HasPrefix(cleanedLine, "- ") {
			cleanedLine = strings.TrimPrefix(cleanedLine, "- ")
		} else if strings.HasPrefix(cleanedLine, "• ") {
			cleanedLine = strings.TrimPrefix(cleanedLine, "• ")
		}

		// Try price regex (handles both single prices and ranges)
		if m := p.priceRe.FindStringSubmatch(cleanedLine); m != nil {
			// m[1] is name, last group is price/range
			name := strings.TrimSpace(m[1])
			priceStr := strings.TrimSpace(m[len(m)-1])

			// Check if this is a category header (no price, just a name on its own line)
			// Category headers are usually followed by indented items
			if priceStr == "" {
				currentCategory = name
				continue // Skip category headers, they'll be applied to child items
			}

			item.Name = name
			if currentCategory != "" {
				item.Category = currentCategory
			}

			// Check if price string contains a range
			if p.priceRangeRe.MatchString(priceStr) {
				// Extract both prices from range
				rangeMatch := p.priceRangeRe.FindStringSubmatch(priceStr)
				if len(rangeMatch) >= 3 {
					minPrice, minCurrency := parsePrice(rangeMatch[1])
					maxPrice, _ := parsePrice(rangeMatch[2])

					// Use max price as the main price, flag as range
					if maxPrice != nil {
						item.Price = maxPrice
						item.Currency = minCurrency
						if minPrice != nil {
							item.Flags["has_price_range"] = true
							// Store min price in a way that can be extracted later
							// Note: ParsedItem doesn't have min/max fields, so we flag it
							item.Flags["min_price_available"] = true
							// For now, we'll use the max price. Frontend can handle ranges separately
						}
					}
				}
			} else {
				// Single price
				priceVal, currency := parsePrice(priceStr)
				if priceVal != nil {
					item.Price = priceVal
					item.Currency = currency
				}
			}

			out = append(out, item)
			continue
		}

		// Fallback: try to split by tab or multiple spaces where last token is price
		// Handle tabular format where prices are right-aligned
		parts := strings.Fields(cleanedLine)
		if len(parts) >= 2 {
			// Check last few tokens for price pattern
			for i := len(parts) - 1; i >= 0 && i >= len(parts)-3; i-- {
				if v, c := parsePrice(parts[i]); v != nil {
					item.Price = v
					item.Currency = c
					item.Name = strings.TrimSpace(strings.Join(parts[:i], " "))
					if currentCategory != "" {
						item.Category = currentCategory
					}
					out = append(out, item)
					break
				}
			}
			if item.Price != nil {
				continue
			}
		}

		// Check if line might be a category/section header (no price found)
		// If it looks like a header, use it as category for next items
		if !strings.Contains(cleanedLine, "₦") && !strings.Contains(cleanedLine, "$") &&
			!strings.Contains(cleanedLine, "£") && !strings.Contains(cleanedLine, "€") {
			// Might be a category - check if it's all caps or title case
			if len(cleanedLine) < 50 && len(cleanedLine) > 0 &&
				strings.ToUpper(cleanedLine) == cleanedLine &&
				regexp.MustCompile(`^[A-Z\s]+$`).MatchString(cleanedLine) {
				currentCategory = cleanedLine
				continue
			}
		}

		// Not parsable: treat whole line as name (might be a category or unparsable item)
		item.Name = cleanedLine
		if currentCategory != "" {
			item.Category = currentCategory
		}
		item.Flags["price_missing"] = true
		item.Confidence = 0.5

		out = append(out, item)
	}
	return out, nil
}

func parsePrice(s string) (*float64, string) {
	// remove currency symbols and commas, detect currency symbol if present
	currency := ""
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ""
	}

	// Detect currency symbol (check start of string)
	currencyPrefix := ""
	if strings.HasPrefix(s, "₦") {
		currency = "NGN"
		currencyPrefix = "₦"
	} else if strings.HasPrefix(s, "$") {
		currency = "USD"
		currencyPrefix = "$"
	} else if strings.HasPrefix(s, "£") {
		currency = "GBP"
		currencyPrefix = "£"
	} else if strings.HasPrefix(s, "€") {
		currency = "EUR"
		currencyPrefix = "€"
	} else if strings.HasPrefix(strings.ToUpper(s), "NGN") {
		currency = "NGN"
		currencyPrefix = "NGN"
	} else if strings.HasPrefix(strings.ToUpper(s), "USD") {
		currency = "USD"
		currencyPrefix = "USD"
	} else if strings.HasPrefix(strings.ToUpper(s), "GBP") {
		currency = "GBP"
		currencyPrefix = "GBP"
	} else if strings.HasPrefix(strings.ToUpper(s), "EUR") {
		currency = "EUR"
		currencyPrefix = "EUR"
	}

	// Remove currency prefix for parsing
	if currencyPrefix != "" {
		s = strings.TrimPrefix(s, currencyPrefix)
		s = strings.TrimSpace(s)
	}

	// Handle suffixes (k, m) - Check BEFORE stripping characters
	multiplier := 1.0
	sLower := strings.ToLower(s)
	if strings.HasSuffix(sLower, "k") {
		multiplier = 1000.0
		s = s[:len(s)-1]
	} else if strings.HasSuffix(sLower, "m") {
		multiplier = 1000000.0
		s = s[:len(s)-1]
	}

	// strip non-digit except dot and comma (for thousands separator)
	clean := ""
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			clean += string(r)
		}
	}

	// Remove commas (thousands separator)
	clean = strings.ReplaceAll(clean, ",", "")

	if clean == "" {
		return nil, currency
	}

	val, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return nil, currency
	}

	val = val * multiplier
	return &val, currency
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return nil, currency
	}
	return &f, currency
}
