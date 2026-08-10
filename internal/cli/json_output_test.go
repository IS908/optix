package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	"github.com/IS908/optix/pkg/model"
)

func TestAgentJSONFormatFlags(t *testing.T) {
	tests := map[string]func() string{
		"quote": func() string {
			v, _ := newQuoteCmd().Flags().GetString("format")
			return v
		},
		"analyze": func() string {
			v, _ := newAnalyzeCmd().Flags().GetString("format")
			return v
		},
		"option-quote": func() string {
			v, _ := newOptionQuoteCmd().Flags().GetString("format")
			return v
		},
		"chain": func() string {
			v, _ := newChainCmd().Flags().GetString("format")
			return v
		},
		"dashboard": func() string {
			v, _ := newDashboardCmd().Flags().GetString("format")
			return v
		},
		"positions": func() string {
			v, _ := newPositionsCmd().Flags().GetString("format")
			return v
		},
		"trades": func() string {
			v, _ := newTradesCmd().Flags().GetString("format")
			return v
		},
	}

	for name, getDefault := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getDefault(); got != "text" {
				t.Fatalf("default --format = %q, want text", got)
			}
		})
	}
}

func TestAgentJSONRenderQuote(t *testing.T) {
	q := &model.StockQuote{
		Symbol:        "AAPL",
		Last:          201.23,
		Bid:           201.1,
		Ask:           201.4,
		Volume:        123456,
		Change:        1.2,
		ChangePct:     0.6,
		Timestamp:     time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC),
		MarketSession: model.SessionRegular,
	}

	var buf bytes.Buffer
	if err := renderQuoteJSON(&buf, q, "ibkr"); err != nil {
		t.Fatalf("renderQuoteJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["symbol"] != "AAPL" {
		t.Fatalf("symbol = %v, want AAPL", got["symbol"])
	}
	if got["market_session"] != "regular" {
		t.Fatalf("market_session = %v, want regular", got["market_session"])
	}
	if got["session_label"] == "" {
		t.Fatalf("session_label missing")
	}
	if got["source"] != "ibkr" {
		t.Fatalf("source = %v, want ibkr", got["source"])
	}
}

func TestAgentJSONRenderOptionChainUsesStringOptionTypes(t *testing.T) {
	chain := &model.OptionChain{
		Underlying:      "AAPL",
		UnderlyingPrice: 201.23,
		Expirations: []model.OptionChainExpiry{{
			Expiration:   "20260619",
			DaysToExpiry: 18,
			Calls: []model.OptionQuote{{
				Underlying:        "AAPL",
				Expiration:        "20260619",
				Strike:            200,
				OptionType:        model.OptionTypeCall,
				Bid:               3.1,
				Ask:               3.3,
				OpenInterest:      1234,
				ImpliedVolatility: 0.25,
			}},
			Puts: []model.OptionQuote{{
				Underlying:   "AAPL",
				Expiration:   "20260619",
				Strike:       200,
				OptionType:   model.OptionTypePut,
				Bid:          2.1,
				Ask:          2.3,
				OpenInterest: 987,
			}},
		}},
	}

	var buf bytes.Buffer
	if err := renderOptionChainJSON(&buf, chain, "ibkr"); err != nil {
		t.Fatalf("renderOptionChainJSON: %v", err)
	}

	var got struct {
		Expirations []struct {
			Calls []struct {
				OptionType string `json:"option_type"`
			} `json:"calls"`
			Puts []struct {
				OptionType string `json:"option_type"`
			} `json:"puts"`
		} `json:"expirations"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.Expirations[0].Calls[0].OptionType != "CALL" {
		t.Fatalf("call option_type = %q, want CALL", got.Expirations[0].Calls[0].OptionType)
	}
	if got.Expirations[0].Puts[0].OptionType != "PUT" {
		t.Fatalf("put option_type = %q, want PUT", got.Expirations[0].Puts[0].OptionType)
	}
}

func TestAgentJSONRenderSingleOptionQuoteIncludesValidationFields(t *testing.T) {
	q := &model.OptionQuote{
		Underlying:        "AAPL",
		Expiration:        "20260717",
		Strike:            290,
		OptionType:        model.OptionTypePut,
		Last:              1.23,
		Bid:               1.20,
		Ask:               1.25,
		Mid:               1.225,
		Mark:              1.23,
		Volume:            1234,
		OpenInterest:      0,
		ImpliedVolatility: 0,
		Greeks:            model.Greeks{Delta: -0.24, Gamma: 0.01, Theta: -0.04, Vega: 0.12, Rho: -0.02},
		Timestamp:         time.Date(2026, 7, 9, 14, 58, 0, 0, time.UTC),
		MarketDataType:    "real_time",
		Warnings:          []string{"open_interest_unavailable", "implied_volatility_unavailable"},
	}

	var buf bytes.Buffer
	if err := renderOptionQuoteJSON(&buf, q, "IBKR"); err != nil {
		t.Fatalf("renderOptionQuoteJSON: %v", err)
	}

	var got struct {
		Underlying        string   `json:"underlying"`
		Expiration        string   `json:"expiration"`
		Right             string   `json:"right"`
		Strike            float64  `json:"strike"`
		Source            string   `json:"source"`
		Last              float64  `json:"last"`
		Bid               float64  `json:"bid"`
		Ask               float64  `json:"ask"`
		Mid               float64  `json:"mid"`
		Mark              float64  `json:"mark"`
		Volume            int64    `json:"volume"`
		OpenInterest      int32    `json:"open_interest"`
		ImpliedVolatility float64  `json:"implied_volatility"`
		MarketDataType    string   `json:"market_data_type"`
		Warnings          []string `json:"warnings"`
		Greeks            struct {
			Delta float64 `json:"delta"`
		} `json:"greeks"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.Underlying != "AAPL" || got.Expiration != "20260717" || got.Right != "P" {
		t.Fatalf("contract identity mismatch: %+v", got)
	}
	if got.Source != "IBKR" || got.MarketDataType != "real_time" {
		t.Fatalf("source/market data type mismatch: %+v", got)
	}
	if got.Bid != 1.20 || got.Ask != 1.25 || got.Mid != 1.225 || got.Mark != 1.23 || got.Last != 1.23 {
		t.Fatalf("price fields mismatch: %+v", got)
	}
	if got.Volume != 1234 || got.OpenInterest != 0 || got.ImpliedVolatility != 0 {
		t.Fatalf("availability fields mismatch: %+v", got)
	}
	if len(got.Warnings) != 2 || got.Warnings[0] != "open_interest_unavailable" || got.Warnings[1] != "implied_volatility_unavailable" {
		t.Fatalf("warnings = %#v, want explicit unavailable-data warnings", got.Warnings)
	}
	if got.Greeks.Delta != -0.24 {
		t.Fatalf("delta = %v, want -0.24", got.Greeks.Delta)
	}
}

func TestOptionQuoteValidationWarningsFlagMissingPriceData(t *testing.T) {
	q := &model.OptionQuote{
		Underlying: "AAPL",
		Expiration: "20260717",
		Strike:     290,
		OptionType: model.OptionTypePut,
	}

	warnings := optionQuoteValidationWarnings(q)

	if !slices.Contains(warnings, "no_price_data") {
		t.Fatalf("warnings = %#v, want no_price_data", warnings)
	}
	if !slices.Contains(warnings, "bid_unavailable") || !slices.Contains(warnings, "ask_unavailable") {
		t.Fatalf("warnings = %#v, want bid/ask unavailable warnings", warnings)
	}
}

func TestAgentJSONRenderDashboard(t *testing.T) {
	summaries := []*analysisv1.StockQuickSummary{{
		Symbol:           "AAPL",
		Price:            201.23,
		Trend:            "bullish",
		Rsi:              55,
		IvRank:           62,
		MaxPain:          200,
		Pcr:              0.92,
		RangeLow_1S:      190,
		RangeHigh_1S:     215,
		Recommendation:   "Hold",
		OpportunityScore: 77,
	}}

	var buf bytes.Buffer
	if err := renderDashboardJSON(&buf, summaries, "iv-rank", "ibkr"); err != nil {
		t.Fatalf("renderDashboardJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["sort"] != "iv-rank" {
		t.Fatalf("sort = %v, want iv-rank", got["sort"])
	}
	if got["source"] != "ibkr" {
		t.Fatalf("source = %v, want ibkr", got["source"])
	}
	rawSummaries, ok := got["summaries"].([]any)
	if !ok || len(rawSummaries) != 1 {
		t.Fatalf("summaries = %#v, want one item", got["summaries"])
	}
	first := rawSummaries[0].(map[string]any)
	if first["opportunity_score"] != float64(77) {
		t.Fatalf("opportunity_score = %v, want 77", first["opportunity_score"])
	}
}

func TestWriteJSONDestinationDashWritesToStdoutOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONDestination(&buf, "-", map[string]string{"ok": "yes"}); err != nil {
		t.Fatalf("writeJSONDestination: %v", err)
	}
	if !strings.Contains(buf.String(), `"ok": "yes"`) {
		t.Fatalf("stdout JSON missing payload: %s", buf.String())
	}
}
