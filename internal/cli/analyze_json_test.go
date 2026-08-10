package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
)

// fullAnalyzeStockResponse fabricates an AnalyzeStockResponse with every
// section populated, mirroring a real AnalyzeStock reply closely enough to
// exercise every field buildAnalyzeOutput projects.
func fullAnalyzeStockResponse() *analysisv1.AnalyzeStockResponse {
	return &analysisv1.AnalyzeStockResponse{
		Summary: &analysisv1.StockSummary{
			Price:           201.23,
			Change:          1.2,
			ChangePct:       0.6,
			High_52W:        220,
			Low_52W:         150,
			AvgVolume_20D:   5_000_000,
			TodayVolume:     6_200_000,
			PreviousClose:   200.03,
			IsExtendedHours: true,
		},
		Technical: &analysisv1.TechnicalAnalysis{
			Trend:            "bullish",
			TrendScore:       0.42,
			TrendDescription: "Uptrend intact, price above all major MAs",
			Ma_20:            198,
			Ma_50:            190,
			Ma_200:           175,
			Rsi_14:           58.2,
			Macd:             1.1,
			MacdSignal:       0.9,
			MacdHistogram:    0.2,
			BollingerUpper:   210,
			BollingerMid:     200,
			BollingerLower:   190,
			SupportLevels: []*analysisv1.PriceLevel{
				{Price: 195, Source: "ma_20", Strength: 70},
			},
			ResistanceLevels: []*analysisv1.PriceLevel{
				{Price: 215, Source: "swing_high", Strength: 80},
			},
		},
		Options: &analysisv1.OptionsAnalysis{
			IvCurrent:            0.28,
			IvRank:               62,
			IvPercentile:         55,
			IvEnvironment:        "high",
			IvSkew:               0.05,
			MaxPain:              200,
			MaxPainExpiry:        "2026-06-19",
			PcrVolume:            0.95,
			PcrOi:                1.05,
			EarningsBeforeExpiry: true,
			NextEarningsDate:     "2026-07-24",
			OiClusters: []*analysisv1.OICluster{
				{Strike: 200, OptionType: marketdatav1.OptionType_OPTION_TYPE_PUT, OpenInterest: 5000, Significance: "support_wall"},
				{Strike: 215, OptionType: marketdatav1.OptionType_OPTION_TYPE_CALL, OpenInterest: 4200, Significance: "resistance_wall"},
			},
		},
		Outlook: &analysisv1.MarketOutlook{
			Direction:    "bullish",
			Confidence:   72,
			Rationale:    "Momentum and IV support upside",
			RangeLow_1S:  190,
			RangeHigh_1S: 215,
			RangeLow_2S:  180,
			RangeHigh_2S: 225,
			ForecastDays: 14,
			RiskEvents:   []string{"Earnings 2026-07-24"},
		},
		Strategies: []*analysisv1.StrategyRecommendation{
			{
				StrategyName: "Iron Condor",
				StrategyType: "iron_condor",
				Score:        78,
				Legs: []*analysisv1.StrategyLeg{
					{OptionType: marketdatav1.OptionType_OPTION_TYPE_PUT, Strike: 190, Expiration: "2026-06-19", Quantity: -1, Premium: 1.2},
					{OptionType: marketdatav1.OptionType_OPTION_TYPE_PUT, Strike: 185, Expiration: "2026-06-19", Quantity: 1, Premium: 0.6},
					{OptionType: marketdatav1.OptionType_OPTION_TYPE_CALL, Strike: 215, Expiration: "2026-06-19", Quantity: -1, Premium: 1.1},
					{OptionType: marketdatav1.OptionType_OPTION_TYPE_CALL, Strike: 220, Expiration: "2026-06-19", Quantity: 1, Premium: 0.5},
				},
				NetCredit:           2.2,
				MaxProfit:           220,
				MaxLoss:             280,
				RiskRewardRatio:     0.79,
				ProbabilityOfProfit: 68,
				BreakevenPrice:      192.2,
				MarginRequired:      500,
				Rationale:           "High IV rank supports credit strategies",
				RiskWarnings:        []string{"Earnings before expiry"},
			},
		},
	}
}

func TestBuildAnalyzeOutputFullResponse(t *testing.T) {
	resp := fullAnalyzeStockResponse()
	out := buildAnalyzeOutput(resp, "AAPL", 2, 14, "ibkr", true)

	if out.Symbol != "AAPL" || out.Weeks != 2 || out.ForecastDays != 14 || out.Source != "ibkr" {
		t.Fatalf("top-level metadata mismatch: %+v", out)
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("expected no warnings on a fully populated response, got %v", out.Warnings)
	}

	if out.Summary == nil || out.Summary.Price != 201.23 || out.Summary.PreviousClose != 200.03 || !out.Summary.IsExtendedHours {
		t.Fatalf("summary mismatch: %+v", out.Summary)
	}

	if out.Technical == nil || out.Technical.Trend != "bullish" || len(out.Technical.SupportLevels) != 1 || len(out.Technical.ResistanceLevels) != 1 {
		t.Fatalf("technical mismatch: %+v", out.Technical)
	}
	if out.Technical.SupportLevels[0].Source != "ma_20" {
		t.Fatalf("support level mismatch: %+v", out.Technical.SupportLevels[0])
	}

	if out.Options == nil {
		t.Fatalf("options missing")
	}
	if !out.Options.MaxPainAvailable || out.Options.MaxPain != 200 || out.Options.MaxPainExpiry != "2026-06-19" {
		t.Fatalf("max pain fields mismatch: %+v", out.Options)
	}
	if !out.Options.ExpiryRequested {
		t.Fatalf("expiry_requested should propagate userRequestedExpiry=true")
	}
	if len(out.Options.OIClusters) != 2 || out.Options.OIClusters[0].OptionType != "PUT" || out.Options.OIClusters[1].OptionType != "CALL" {
		t.Fatalf("oi clusters mismatch: %+v", out.Options.OIClusters)
	}

	if out.Outlook == nil || out.Outlook.Direction != "bullish" || len(out.Outlook.RiskEvents) != 1 {
		t.Fatalf("outlook mismatch: %+v", out.Outlook)
	}

	if len(out.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(out.Strategies))
	}
	strat := out.Strategies[0]
	if strat.Rank != 1 || strat.StrategyName != "Iron Condor" {
		t.Fatalf("strategy identity mismatch: %+v", strat)
	}
	if len(strat.Legs) != 4 {
		t.Fatalf("expected 4 legs, got %d", len(strat.Legs))
	}
	if strat.Legs[0].Direction != "sell" || strat.Legs[0].OptionType != "PUT" {
		t.Fatalf("leg[0] mismatch: %+v", strat.Legs[0])
	}
	if strat.Legs[1].Direction != "buy" || strat.Legs[1].OptionType != "PUT" {
		t.Fatalf("leg[1] mismatch: %+v", strat.Legs[1])
	}
	if strat.Legs[2].Direction != "sell" || strat.Legs[2].OptionType != "CALL" {
		t.Fatalf("leg[2] mismatch: %+v", strat.Legs[2])
	}
	if strat.Legs[3].Direction != "buy" || strat.Legs[3].OptionType != "CALL" {
		t.Fatalf("leg[3] mismatch: %+v", strat.Legs[3])
	}
	if strat.Legs[3].Premium != 0.5 {
		t.Fatalf("leg[3] premium mismatch: %+v", strat.Legs[3])
	}

	// Round-trip through JSON to make sure every populated section actually
	// serializes (not just that the Go struct fields are set).
	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"symbol", "weeks", "forecast_days", "source", "generated_at", "summary", "technical", "options", "outlook", "strategies"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected key %q in JSON output, got keys %v", key, got)
		}
	}
	if _, ok := got["warnings"]; ok {
		t.Fatalf("expected warnings to be omitted on a fully populated response, got %v", got["warnings"])
	}
}

func TestBuildAnalyzeOutputNilResponse(t *testing.T) {
	out := buildAnalyzeOutput(nil, "AAPL", 2, 14, "ibkr", false)
	if out.Symbol != "AAPL" {
		t.Fatalf("symbol mismatch: %+v", out)
	}
	if len(out.Warnings) != 1 || out.Warnings[0] != "no_analysis_data" {
		t.Fatalf("warnings = %v, want [no_analysis_data]", out.Warnings)
	}
	if out.Summary != nil || out.Technical != nil || out.Options != nil || out.Outlook != nil || out.Strategies != nil {
		t.Fatalf("expected all sections nil for a nil response: %+v", out)
	}
}

// TestBuildAnalyzeOutputSparseResponse covers the omitempty contract on a
// response where every sub-message is nil and there are no strategies —
// each absent section should both (a) leave the Go field nil/empty and (b)
// disappear from the marshaled JSON, while a matching warning code appears.
func TestBuildAnalyzeOutputSparseResponse(t *testing.T) {
	resp := &analysisv1.AnalyzeStockResponse{}
	out := buildAnalyzeOutput(resp, "ZZZZ", 2, 14, "", false)

	wantWarnings := []string{
		"summary_unavailable",
		"technical_unavailable",
		"options_unavailable",
		"outlook_unavailable",
		"no_strategy_recommendations",
	}
	if len(out.Warnings) != len(wantWarnings) {
		t.Fatalf("warnings = %v, want %v", out.Warnings, wantWarnings)
	}
	for i, w := range wantWarnings {
		if out.Warnings[i] != w {
			t.Fatalf("warnings[%d] = %q, want %q (full: %v)", i, out.Warnings[i], w, out.Warnings)
		}
	}
	if out.Summary != nil || out.Technical != nil || out.Options != nil || out.Outlook != nil || out.Strategies != nil {
		t.Fatalf("expected all sections nil/empty on a sparse response: %+v", out)
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"summary", "technical", "options", "outlook", "strategies", "source"} {
		if _, ok := got[key]; ok {
			t.Fatalf("expected key %q to be omitted on a sparse response, got %v", key, got[key])
		}
	}
	if _, ok := got["warnings"]; !ok {
		t.Fatalf("expected warnings key present on a sparse response")
	}
}

// TestBuildAnalyzeOutputMaxPainUnavailable covers the degraded-Max-Pain
// branch (o.MaxPain <= 0, i.e. analyze ran without --with-oi): mirrors
// printAnalysisReport's "Max Pain: N/A (rerun with --with-oi ...)" text
// branch as a max_pain_unavailable warning, and MaxPain/MaxPainExpiry stay
// omitted from JSON via omitempty.
func TestBuildAnalyzeOutputMaxPainUnavailable(t *testing.T) {
	resp := &analysisv1.AnalyzeStockResponse{
		Options: &analysisv1.OptionsAnalysis{
			IvCurrent:     0.22,
			IvRank:        40,
			IvPercentile:  35,
			IvEnvironment: "medium",
			PcrVolume:     1.1,
			PcrOi:         0.98,
			// MaxPain left at zero value: not fetched (no --with-oi).
		},
	}
	out := buildAnalyzeOutput(resp, "MSFT", 2, 14, "yfinance", false)

	if out.Options == nil {
		t.Fatalf("expected options section present")
	}
	if out.Options.MaxPainAvailable {
		t.Fatalf("expected max_pain_available=false")
	}
	if out.Options.MaxPain != 0 || out.Options.MaxPainExpiry != "" {
		t.Fatalf("expected max pain fields to stay zero-valued: %+v", out.Options)
	}
	found := false
	for _, w := range out.Warnings {
		if w == "max_pain_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected max_pain_unavailable warning, got %v", out.Warnings)
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	options, ok := got["options"].(map[string]any)
	if !ok {
		t.Fatalf("expected options object, got %v", got["options"])
	}
	if _, ok := options["max_pain"]; ok {
		t.Fatalf("expected max_pain key omitted, got %v", options["max_pain"])
	}
	if _, ok := options["max_pain_expiry"]; ok {
		t.Fatalf("expected max_pain_expiry key omitted, got %v", options["max_pain_expiry"])
	}
	if options["max_pain_available"] != false {
		t.Fatalf("expected max_pain_available=false in JSON, got %v", options["max_pain_available"])
	}
}

// TestAnalyzeWatchlistOutputShape covers the --watchlist --format json
// container: a stable object with a per-symbol "results" array, where a
// failed symbol appears as a structured {symbol, error} entry (no nested
// "analysis") alongside a successful {symbol, analysis} entry.
func TestAnalyzeWatchlistOutputShape(t *testing.T) {
	out := analyzeWatchlistOutput{
		Weeks:        2,
		ForecastDays: 14,
		Source:       "ibkr",
		GeneratedAt:  time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Results: []analyzeWatchlistEntry{
			{Symbol: "AAPL", Analysis: buildAnalyzeOutput(fullAnalyzeStockResponse(), "AAPL", 2, 14, "ibkr", false)},
			{Symbol: "BADSYM", Error: "fetch error: no security definition"},
		},
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}

	var got struct {
		Weeks        int    `json:"weeks"`
		ForecastDays int32  `json:"forecast_days"`
		Source       string `json:"source"`
		Results      []struct {
			Symbol   string          `json:"symbol"`
			Error    string          `json:"error"`
			Analysis json.RawMessage `json:"analysis"`
		} `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Weeks != 2 || got.ForecastDays != 14 || got.Source != "ibkr" {
		t.Fatalf("envelope metadata mismatch: %+v", got)
	}
	if len(got.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got.Results))
	}

	success := got.Results[0]
	if success.Symbol != "AAPL" || success.Error != "" || len(success.Analysis) == 0 {
		t.Fatalf("success entry mismatch: %+v", success)
	}

	failure := got.Results[1]
	if failure.Symbol != "BADSYM" || failure.Error == "" {
		t.Fatalf("failure entry mismatch: %+v", failure)
	}
	if len(failure.Analysis) != 0 {
		t.Fatalf("expected failure entry to omit analysis, got %s", failure.Analysis)
	}

	// Re-parse the raw bytes to confirm the failure entry's "analysis" key
	// is entirely absent (not merely null) — omitempty on a nil pointer.
	var rawEnvelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rawEnvelope); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	rawResults := rawEnvelope["results"].([]any)
	failureRaw := rawResults[1].(map[string]any)
	if _, ok := failureRaw["analysis"]; ok {
		t.Fatalf("expected analysis key entirely absent on failure entry, got %v", failureRaw["analysis"])
	}
}

// TestAnalyzeWatchlistOutputEmptyResultsIsEmptyArray guards against Results
// marshaling as JSON null when the watchlist itself is empty — callers
// should always be able to range over "results" without a null check.
func TestAnalyzeWatchlistOutputEmptyResultsIsEmptyArray(t *testing.T) {
	out := analyzeWatchlistOutput{
		Weeks:        2,
		ForecastDays: 14,
		GeneratedAt:  time.Now().UTC(),
		Results:      []analyzeWatchlistEntry{},
	}
	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	results, ok := got["results"].([]any)
	if !ok {
		t.Fatalf("expected results to decode as a JSON array, got %T: %v", got["results"], got["results"])
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results array, got %v", results)
	}
}

// TestAnalyzeOptionTypeLabel covers both branches so a future OptionType
// enum addition can't silently fall through to "CALL".
func TestAnalyzeOptionTypeLabel(t *testing.T) {
	if got := analyzeOptionTypeLabel(marketdatav1.OptionType_OPTION_TYPE_PUT); got != "PUT" {
		t.Fatalf("PUT label = %q, want PUT", got)
	}
	if got := analyzeOptionTypeLabel(marketdatav1.OptionType_OPTION_TYPE_CALL); got != "CALL" {
		t.Fatalf("CALL label = %q, want CALL", got)
	}
}
