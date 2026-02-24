package tenantapi

import (
	"testing"
)

func TestDefaultParser_SimpleLines(t *testing.T) {
	p := NewDefaultParser()
	in := `Jollof Rice - ₦3,500
Fried Rice - 4000
Grilled Chicken - ₦6,500
Delivery available`
	items, err := p.Parse(in)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}
	if items[0].Name != "Jollof Rice" {
		t.Fatalf("expected first name Jollof Rice, got %s", items[0].Name)
	}
	if items[0].Price == nil || *items[0].Price != 3500 {
		t.Fatalf("expected first price 3500, got %v", items[0].Price)
	}
	if items[3].Flags["price_missing"] != true {
		t.Fatalf("expected last item flagged price_missing")
	}
}

func TestDefaultParser_NoPriceLines(t *testing.T) {
	p := NewDefaultParser()
	in := `Special Offer
Call for price`
	items, err := p.Parse(in)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Flags["price_missing"] != true {
		t.Fatalf("expected price_missing flag on first item")
	}
}

func TestDefaultParser_CurrencyAndRange(t *testing.T) {
	p := NewDefaultParser()
	in := `Apple - $12.50
Banana - 3,000-4,000
Sushi - £15.00`
	items, err := p.Parse(in)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Currency != "USD" {
		t.Fatalf("expected USD for Apple, got %s", items[0].Currency)
	}
	if items[2].Currency != "GBP" {
		t.Fatalf("expected GBP for Sushi, got %s", items[2].Currency)
	}
}
