package intelshared

import "testing"

func TestNormalizeSymbolRemovesInteriorWhitespaceAndUppercases(t *testing.T) {
	got := NormalizeSymbol(" brk\tb \n")
	if got != "BRKB" {
		t.Fatalf("NormalizeSymbol = %q, want BRKB", got)
	}
}

func TestNYReturnsMarketClockLocation(t *testing.T) {
	if NY().String() != "America/New_York" {
		t.Fatalf("NY location = %q", NY())
	}
}
