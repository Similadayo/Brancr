package utils

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		input    string
		region   string
		expected string
		ok       bool
	}{
		{"+14155552671", "US", "+14155552671", true},
		{"4155552671", "US", "+14155552671", true},
		{"+2347039841392", "NG", "+2347039841392", true},
		{"07039841392", "NG", "+2347039841392", true},
		{"invalid", "US", "", false},
	}

	for _, c := range cases {
		got, err := NormalizePhone(c.input, c.region)
		if c.ok && err != nil {
			t.Fatalf("expected ok for %s, got err %v", c.input, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("expected error for %s, got %s", c.input, got)
		}
		if c.ok && got != c.expected {
			t.Fatalf("expected %s, got %s", c.expected, got)
		}
	}
}
