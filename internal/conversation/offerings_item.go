package conversation

import "github.com/similadayo/brancr/internal/domain"

func formatOne(o domain.Offering) string {

	line := ""

	// Title + price (first line)
	// Example: "Banana Bread — ₦3,500"
	if o.PriceText != "" {
		line += o.Title + " — " + o.PriceText
	} else {
		line += o.Title
	}

	// Description (optional)
	if o.Description != "" {
		line += "\n" + o.Description
	}

	// Extras (optional)
	// Iterate and add each on new line
	for _, e := range o.Extras {
		line += "\n" + e
	}

	return line
}
