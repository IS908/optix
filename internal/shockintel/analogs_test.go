package shockintel

import "testing"

func TestBuildShockAnalogsRanksSimilarTemplates(t *testing.T) {
	now := fixedShockNow()
	vector := ShockVector{
		Equity: -3.0,
		Vol:    30.0,
		Credit: -1.5,
		Rates:  -0.8,
		Dollar: 0.4,
		Oil:    -5.0,
		Gold:   1.0,
	}

	dto := BuildShockAnalogs(vector, defaultAnalogTemplates(), now)

	if len(dto.Rows) < 3 {
		t.Fatalf("analog rows = %d, want at least 3", len(dto.Rows))
	}
	if dto.Rows[0].Name != "COVID liquidity shock" {
		t.Fatalf("top analog = %q, want COVID liquidity shock; rows=%#v", dto.Rows[0].Name, dto.Rows)
	}
	if dto.Rows[0].Similarity < 0.70 {
		t.Fatalf("top similarity = %.2f, want >= 0.70", dto.Rows[0].Similarity)
	}
	if len(dto.Rows[0].MatchedFeatures) == 0 {
		t.Fatalf("matched features empty")
	}
}

func TestBuildShockVectorFromQuotesUsesCoreAssets(t *testing.T) {
	now := fixedShockNow()
	quotes := map[string]ShockQuote{
		"SPY":   {ID: "SPY", ChangePct: -2.0, AsOf: now},
		"QQQ":   {ID: "QQQ", ChangePct: -4.0, AsOf: now},
		"VIX":   {ID: "VIX", ChangePct: 25.0, AsOf: now},
		"HYG":   {ID: "HYG", ChangePct: -1.2, AsOf: now},
		"US10Y": {ID: "US10Y", ChangePct: 0.9, AsOf: now},
		"UUP":   {ID: "UUP", ChangePct: 1.1, AsOf: now},
		"USO":   {ID: "USO", ChangePct: 4.0, AsOf: now},
		"GLD":   {ID: "GLD", ChangePct: 0.5, AsOf: now},
	}

	vector := BuildShockVector(quotes)

	if vector.Equity != -3.0 {
		t.Fatalf("equity = %.2f, want -3.0", vector.Equity)
	}
	if vector.Vol != 25.0 || vector.Credit != -1.2 || vector.Rates != 0.9 {
		t.Fatalf("unexpected vector: %#v", vector)
	}
}
