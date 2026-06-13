package shockintel

import (
	"testing"
	"time"
)

func TestBuildRegimeTriggerCriticalOnVIXAndCrossAssetStress(t *testing.T) {
	now := fixedShockNow()
	quotes := map[string]ShockQuote{
		"VIX": {ID: "VIX", Label: "VIX", Price: 29, ChangePct: 35, Source: "ibkr", Basis: "realtime", AsOf: now},
		"SPY": {ID: "SPY", Label: "SPY", Price: 510, ChangePct: -3.2, Source: "ibkr", Basis: "realtime", AsOf: now},
		"QQQ": {ID: "QQQ", Label: "QQQ", Price: 440, ChangePct: -4.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"HYG": {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.8, Source: "ibkr", Basis: "realtime", AsOf: now},
		"TLT": {ID: "TLT", Label: "TLT", Price: 93, ChangePct: 2.4, Source: "ibkr", Basis: "realtime", AsOf: now},
		"UUP": {ID: "UUP", Label: "UUP", Price: 31, ChangePct: 1.2, Source: "ibkr", Basis: "realtime", AsOf: now},
	}
	liquidity := LiquidityDTO{
		Rows: []LiquidityRow{
			{ID: "SPY", SpreadBps: 8.5, SpreadZ: 3.2, State: "stressed", DepthAvailable: true},
			{ID: "HYG", SpreadBps: 22, SpreadZ: 4.1, State: "stressed", DepthAvailable: true},
		},
	}

	dto := BuildRegimeTrigger(quotes, liquidity, now)

	if dto.State != "critical" {
		t.Fatalf("state = %s, want critical; dto=%#v", dto.State, dto)
	}
	if dto.TriggeredView != "shock" {
		t.Fatalf("triggered_view = %q, want shock", dto.TriggeredView)
	}
	if dto.Score < 80 {
		t.Fatalf("score = %.1f, want >= 80", dto.Score)
	}
	if dto.VIXSigma < 3 {
		t.Fatalf("vix sigma = %.2f, want >= 3", dto.VIXSigma)
	}
	if len(dto.Confirmations) < 4 {
		t.Fatalf("confirmations = %d, want at least 4", len(dto.Confirmations))
	}
}

func TestBuildRegimeTriggerNormalWhenSignalsAreQuiet(t *testing.T) {
	now := fixedShockNow()
	quotes := map[string]ShockQuote{
		"VIX": {ID: "VIX", Label: "VIX", Price: 14, ChangePct: -2, Source: "yfinance", Basis: "delayed", AsOf: now},
		"SPY": {ID: "SPY", Label: "SPY", Price: 510, ChangePct: 0.2, Source: "yfinance", Basis: "delayed", AsOf: now},
		"QQQ": {ID: "QQQ", Label: "QQQ", Price: 440, ChangePct: 0.1, Source: "yfinance", Basis: "delayed", AsOf: now},
	}

	dto := BuildRegimeTrigger(quotes, LiquidityDTO{}, now)

	if dto.State != "normal" {
		t.Fatalf("state = %s, want normal; dto=%#v", dto.State, dto)
	}
	if dto.TriggeredView != "" {
		t.Fatalf("triggered_view = %q, want empty", dto.TriggeredView)
	}
	if dto.Score >= 25 {
		t.Fatalf("score = %.1f, want < 25", dto.Score)
	}
}

func fixedShockNow() time.Time {
	return time.Date(2026, 6, 13, 15, 30, 0, 0, time.UTC)
}
