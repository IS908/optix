package shockintel

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildShockFingerprintClassifiesLiquidityAndPolicyStress(t *testing.T) {
	now := fixedShockNow()
	quotes := map[string]ShockQuote{
		"VIX":   {ID: "VIX", ChangePct: 28, Source: "ibkr", Basis: "realtime", AsOf: now},
		"SPY":   {ID: "SPY", ChangePct: -2.5, Source: "ibkr", Basis: "realtime", AsOf: now},
		"HYG":   {ID: "HYG", ChangePct: -1.4, Source: "ibkr", Basis: "realtime", AsOf: now},
		"US10Y": {ID: "US10Y", ChangePct: 1.3, Source: "ibkr", Basis: "realtime", AsOf: now},
		"UUP":   {ID: "UUP", ChangePct: 1.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"USO":   {ID: "USO", ChangePct: 0.6, Source: "ibkr", Basis: "realtime", AsOf: now},
	}
	liquidity := LiquidityDTO{Rows: []LiquidityRow{{ID: "HYG", SpreadRatio: 4.8, State: "stressed"}}}

	dto := BuildShockFingerprint(quotes, liquidity, nil, now)

	liquidityRow := findFingerprint(t, dto.Rows, "liquidity")
	if !liquidityRow.Active {
		t.Fatalf("liquidity active = false; row=%#v", liquidityRow)
	}
	if liquidityRow.Score < 60 {
		t.Fatalf("liquidity score = %.1f, want >= 60", liquidityRow.Score)
	}
	policyRow := findFingerprint(t, dto.Rows, "policy")
	if !policyRow.Active {
		t.Fatalf("policy active = false; row=%#v", policyRow)
	}
	if len(policyRow.Evidence) < 2 {
		t.Fatalf("policy evidence = %#v, want at least 2 items", policyRow.Evidence)
	}
}

func TestBuildShockFingerprintKeepsAllRowsWhenDataMissing(t *testing.T) {
	dto := BuildShockFingerprint(map[string]ShockQuote{}, LiquidityDTO{}, nil, fixedShockNow())
	if len(dto.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(dto.Rows))
	}
	for _, row := range dto.Rows {
		if row.Missing == nil {
			t.Fatalf("%s missing slice is nil", row.Kind)
		}
	}
}

func TestBuildShockFingerprintJSONRenamesConfidenceToNormalizedScore(t *testing.T) {
	dto := BuildShockFingerprint(map[string]ShockQuote{
		"VIX": {ID: "VIX", ChangePct: 28, Source: "ibkr", Basis: "realtime", AsOf: fixedShockNow()},
		"HYG": {ID: "HYG", ChangePct: -1.4, Source: "ibkr", Basis: "realtime", AsOf: fixedShockNow()},
	}, LiquidityDTO{}, nil, fixedShockNow())

	raw, err := json.Marshal(dto.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("confidence")) {
		t.Fatalf("fingerprint row JSON must not expose confidence: %s", string(raw))
	}
	if !bytes.Contains(raw, []byte("normalized_score")) {
		t.Fatalf("fingerprint row JSON must expose normalized_score: %s", string(raw))
	}
}

func findFingerprint(t *testing.T, rows []FingerprintRow, kind string) FingerprintRow {
	t.Helper()
	for _, row := range rows {
		if row.Kind == kind {
			return row
		}
	}
	t.Fatalf("fingerprint %q not found in %#v", kind, rows)
	return FingerprintRow{}
}
