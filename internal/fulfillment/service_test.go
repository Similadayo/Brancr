package fulfillment

import "testing"

func TestNormalizeItemFamilyKey_StripsCapacityAndParens(t *testing.T) {
	got := normalizeItemFamilyKey("iPhone 13 Pro Max (512GB)")
	if got != "iphone13promax" {
		t.Fatalf("expected iphone13promax, got %q", got)
	}
}

