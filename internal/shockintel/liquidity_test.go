package shockintel

import "testing"

func TestBuildLiquidityStateUsesSpreadAndDepth(t *testing.T) {
	now := fixedShockNow()
	quotes := map[string]ShockQuote{
		"SPY": {ID: "SPY", Label: "SPY", Price: 500, Bid: 499.90, Ask: 500.10, BidSize: 1000, AskSize: 900, Source: "ibkr", Basis: "realtime", AsOf: now},
		"HYG": {ID: "HYG", Label: "HYG", Price: 75, Bid: 74.80, Ask: 75.20, BidSize: 150, AskSize: 120, Source: "ibkr", Basis: "realtime", AsOf: now},
	}
	depth := map[string]DepthSnapshot{
		"SPY": {ID: "SPY", Source: "ibkr", Basis: "realtime", AsOf: now, Levels: []DepthLevel{
			{Side: "bid", Price: 499.90, Size: 1000},
			{Side: "bid", Price: 499.80, Size: 800},
			{Side: "ask", Price: 500.10, Size: 900},
			{Side: "ask", Price: 500.20, Size: 850},
		}},
	}

	dto := BuildLiquidityState(quotes, depth, now)

	spy := findLiquidityRow(t, dto.Rows, "SPY")
	if !spy.DepthAvailable {
		t.Fatalf("SPY depth available = false")
	}
	if spy.Top5BidDepth != 1800 || spy.Top5AskDepth != 1750 {
		t.Fatalf("SPY depth = %.0f/%.0f, want 1800/1750", spy.Top5BidDepth, spy.Top5AskDepth)
	}
	if spy.State != "normal" {
		t.Fatalf("SPY state = %s, want normal", spy.State)
	}
	hyg := findLiquidityRow(t, dto.Rows, "HYG")
	if hyg.State != "stressed" {
		t.Fatalf("HYG state = %s, want stressed; row=%#v", hyg.State, hyg)
	}
	if hyg.DepthAvailable {
		t.Fatalf("HYG depth available = true, want false")
	}
}

func TestBuildLiquidityStateReturnsMissingRows(t *testing.T) {
	dto := BuildLiquidityState(map[string]ShockQuote{}, nil, fixedShockNow())
	if len(dto.Rows) == 0 {
		t.Fatal("expected liquidity universe rows")
	}
	for _, row := range dto.Rows {
		if !row.Missing {
			t.Fatalf("%s missing = false, want true", row.ID)
		}
	}
	if len(dto.Warnings) == 0 {
		t.Fatal("expected missing quote warnings")
	}
}

func TestBuildLiquidityStateDoesNotTreatMissingSpreadAsNormal(t *testing.T) {
	now := fixedShockNow()
	dto := BuildLiquidityState(map[string]ShockQuote{
		"SPY": {ID: "SPY", Label: "SPY", Price: 500, Source: "yfinance", Basis: "delayed", AsOf: now},
	}, nil, now)

	spy := findLiquidityRow(t, dto.Rows, "SPY")
	if !spy.Missing {
		t.Fatalf("SPY missing = false, want true for unavailable bid/ask")
	}
	if spy.State != "missing" {
		t.Fatalf("SPY state = %s, want missing", spy.State)
	}
	if len(dto.Warnings) == 0 {
		t.Fatal("expected bid/ask warning")
	}
}

func TestBuildLiquidityStateCanDeriveSpreadFromDepth(t *testing.T) {
	now := fixedShockNow()
	dto := BuildLiquidityState(
		map[string]ShockQuote{
			"SPY": {ID: "SPY", Label: "SPY", Price: 500, Source: "ibkr", Basis: "realtime", AsOf: now},
		},
		map[string]DepthSnapshot{
			"SPY": {ID: "SPY", Source: "ibkr", Basis: "realtime", AsOf: now, Levels: []DepthLevel{
				{Side: "bid", Price: 499.90, Size: 1000},
				{Side: "bid", Price: 499.80, Size: 800},
				{Side: "ask", Price: 500.10, Size: 900},
				{Side: "ask", Price: 500.20, Size: 850},
			}},
		},
		now,
	)

	spy := findLiquidityRow(t, dto.Rows, "SPY")
	if spy.Missing {
		t.Fatalf("SPY missing = true, want false with depth-derived bid/ask; row=%#v", spy)
	}
	if spy.SpreadBps < 3.9 || spy.SpreadBps > 4.1 {
		t.Fatalf("SPY spread = %.2f, want about 4bp", spy.SpreadBps)
	}
	if spy.State != "normal" {
		t.Fatalf("SPY state = %s, want normal", spy.State)
	}
}

func findLiquidityRow(t *testing.T, rows []LiquidityRow, id string) LiquidityRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("liquidity row %q not found in %#v", id, rows)
	return LiquidityRow{}
}
