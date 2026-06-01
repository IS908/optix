package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/IS908/optix/pkg/model"
)

func TestRenderOptionChainText(t *testing.T) {
	chain := &model.OptionChain{
		Underlying:      "AAPL",
		UnderlyingPrice: 190.12,
		Expirations: []model.OptionChainExpiry{
			{
				Expiration: "20260619",
				Calls: []model.OptionQuote{
					{Strike: 190, Bid: 4.5, Ask: 4.8, Last: 4.6, Volume: 120, OpenInterest: 345},
				},
				Puts: []model.OptionQuote{
					{Strike: 190, Bid: 3.9, Ask: 4.2, Last: 4.0, Volume: 98, OpenInterest: 210},
				},
			},
		},
	}

	var out bytes.Buffer
	renderOptionChainText(&out, chain, "yfinance")

	got := out.String()
	for _, want := range []string{
		"Option chain for AAPL",
		"Source: yfinance",
		"Underlying: $190.12",
		"Expiry: 2026-06-19",
		"CallBid",
		"PutOI",
		"190.00",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered chain missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Not yet implemented") {
		t.Fatalf("rendered chain still contains stub text:\n%s", got)
	}
}
