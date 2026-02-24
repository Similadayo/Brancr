package executor

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func formatPrice(amount int64, currency string) string {
	// Simple assumption: Amount is major units?
	// Usually price is stored as major units (e.g. 3500) if int64, OR minor units (cents).
	// Prompt says: "Input: Price: 3500, Currency: NGN ... Output: ₦3,500".
	// This implies 3500 is major units (3500 Naira).

	p := message.NewPrinter(language.English)
	formatted := p.Sprintf("%d", amount)

	switch currency {
	case "NGN":
		return fmt.Sprintf("₦%s", formatted)
	case "USD":
		return fmt.Sprintf("$%s", formatted)
	default:
		return formatted
	}
}

// formatWithCommas is handled by message.Printer
