package industry

import (
	"testing"
)

func TestFormatNegotiationForAI_HidesNumericRange(t *testing.T) {
	item := RetrievalResult{}
	item.Metadata = map[string]interface{}{"negotiation_mode": "negotiable", "negotiation_min_price": 600000.0, "negotiation_max_price": 750000.0}
	item.Currency = "NGN"
	s := formatNegotiationForAI(item)
	if s == "" {
		t.Fatalf("expected non-empty negotiation string")
	}
	if s == "Negotiable: 600000.00-750000.00 NGN" {
		t.Fatalf("expected numeric range to be hidden, got %s", s)
	}
}
