package order

import (
	"strings"
	"testing"
)

func TestAbbreviateCompanyName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Acme Foods Ltd", want: "AF"},
		{name: "Blue River Studios", want: "BRS"},
		{name: "Zenvia", want: "ZENV"},
		{name: "The Art Company", want: "ART"},
	}

	for _, tc := range tests {
		got := abbreviateCompanyName(tc.name)
		if got != tc.want {
			t.Fatalf("abbreviateCompanyName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGenerateFriendlyCode(t *testing.T) {
	code := generateFriendlyCode(6)
	if len(code) != 6 {
		t.Fatalf("expected length 6, got %d (%q)", len(code), code)
	}
	for _, bad := range []string{"0", "O", "1", "I", "L"} {
		if strings.Contains(code, bad) {
			t.Fatalf("code contains confusing character %q: %q", bad, code)
		}
	}
}
