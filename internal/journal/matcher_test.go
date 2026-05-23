package journal

import (
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func mkExec(id, symbol, side string, qty, price float64, ts time.Time) model.Execution {
	return model.Execution{
		ExecID: id, Time: ts, Account: "DU1", Symbol: symbol,
		SecType: "STK", Side: side, Shares: qty, Price: price, AvgPrice: price,
	}
}

func TestMatcherSimpleLongOpenClose(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 150, t0),
		mkExec("E2", "AAPL", "SLD", 100, 160, t1),
	}, t1.Add(time.Hour))
	if len(rts) != 1 {
		t.Fatalf("len=%d, want 1: %+v", len(rts), rts)
	}
	r := rts[0]
	if r.Direction != "LONG" || r.Status != "closed" || r.RealizedPnL != 1000 ||
		r.OpenQty != 100 || r.CloseQty != 100 || r.HoldingDays != 2.0 {
		t.Errorf("rt = %+v", r)
	}
}

func TestMatcherShortOpenClose(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "TSLA", "SLD", 50, 200, t0),
		mkExec("E2", "TSLA", "BOT", 50, 180, t1),
	}, t1.Add(time.Hour))
	if len(rts) != 1 || rts[0].Direction != "SHORT" || rts[0].Status != "closed" || rts[0].RealizedPnL != 1000 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherScalingOut(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 100, t0),
		mkExec("E2", "AAPL", "SLD", 30, 110, t0.Add(24*time.Hour)),
		mkExec("E3", "AAPL", "SLD", 70, 120, t0.Add(48*time.Hour)),
	}, t0.Add(72*time.Hour))
	if len(rts) != 2 {
		t.Fatalf("len=%d", len(rts))
	}
	var total float64
	for _, r := range rts {
		total += r.RealizedPnL
	}
	if total != 1700 {
		t.Errorf("total PnL = %v, want 1700", total)
	}
}

func TestMatcherScalingIn(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 30, 100, t0),
		mkExec("E2", "AAPL", "BOT", 70, 110, t0.Add(24*time.Hour)),
		mkExec("E3", "AAPL", "SLD", 100, 120, t0.Add(48*time.Hour)),
	}, t0.Add(72*time.Hour))
	if len(rts) != 2 {
		t.Fatalf("len=%d", len(rts))
	}
	var total float64
	for _, r := range rts {
		total += r.RealizedPnL
	}
	if total != 1300 {
		t.Errorf("total PnL = %v, want 1300", total)
	}
}

func TestMatcherStillOpen(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 150, t0),
	}, t0.Add(72*time.Hour))
	if len(rts) != 1 || rts[0].Status != "open" || rts[0].CloseQty != 0 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherEmptyInput(t *testing.T) {
	if rts := MatchRoundTrips(nil, time.Now()); len(rts) != 0 {
		t.Errorf("expected empty, got %+v", rts)
	}
}

func TestMatcherOptionExpiry(t *testing.T) {
	openTime := time.Date(2026, 4, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1", Symbol: "AAPL",
		SecType: "OPT", Expiration: "20260418", Strike: 200, Right: "C",
		Side: "BOT", Shares: 1, Price: 5.0, AvgPrice: 5.0,
	}}, asOf)
	if len(rts) != 1 || rts[0].Status != "expired" || rts[0].RealizedPnL != -500.0 || rts[0].Multiplier != 100 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherOptionStillOpenBeforeExpiry(t *testing.T) {
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1", Symbol: "AAPL",
		SecType: "OPT", Expiration: "20260620", Strike: 200, Right: "C",
		Side: "BOT", Shares: 1, Price: 5.0, AvgPrice: 5.0,
	}}, asOf)
	if len(rts) != 1 || rts[0].Status != "open" {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestGroupByPermID(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	mk := func(id string, permID int64, symbol string) model.Execution {
		return model.Execution{ExecID: id, PermID: permID, Account: "DU1", Symbol: symbol, Time: t0}
	}
	groups := groupByPermID([]model.Execution{
		mk("A1", 100, "GLW"),
		mk("A2", 100, "GLW"),
		mk("A3", 100, "GLW"),
		mk("B1", 200, "TSLA"),
		mk("C1", 0, "AAPL"), // PermID=0 should not collapse
		mk("C2", 0, "AAPL"),
	})
	if len(groups) != 4 {
		t.Fatalf("got %d groups, want 4: %+v", len(groups), groups)
	}
	// Find the 3-row group; it must contain A1/A2/A3.
	var triple []model.Execution
	for _, g := range groups {
		if len(g) == 3 {
			triple = g
		}
	}
	if len(triple) != 3 {
		t.Fatalf("expected one group of size 3, groups=%+v", groups)
	}
	ids := map[string]bool{triple[0].ExecID: true, triple[1].ExecID: true, triple[2].ExecID: true}
	if !ids["A1"] || !ids["A2"] || !ids["A3"] {
		t.Errorf("triple group execs = %+v", triple)
	}
}

// Defensive: two different accounts must not have their executions merged
// even if PermID collides. PermID is supposed to be globally unique, but the
// matcher should not silently cross-contaminate accounts if that invariant
// ever leaks.
func TestGroupByPermIDIsolatesAccounts(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	groups := groupByPermID([]model.Execution{
		{ExecID: "A1", PermID: 999, Account: "DU1", Symbol: "GLW", Time: t0},
		{ExecID: "A2", PermID: 999, Account: "DU1", Symbol: "GLW", Time: t0},
		{ExecID: "B1", PermID: 999, Account: "DU2", Symbol: "TSLA", Time: t0},
	})
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (one per account): %+v", len(groups), groups)
	}
	// Each group must be account-pure.
	for i, g := range groups {
		account := g[0].Account
		for _, e := range g {
			if e.Account != account {
				t.Errorf("group %d mixes accounts: %+v", i, g)
			}
		}
	}
}

func TestEnrichBAGFromLegsHappyPath(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	group := []model.Execution{
		{ExecID: "BAG", PermID: 1, Symbol: "GLW", SecType: "BAG",
			Side: "SLD", Shares: 2, AvgPrice: -0.90, Time: t0},
		{ExecID: "LEG1", PermID: 1, Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "SLD", Shares: 2, AvgPrice: 1.30, Time: t0},
		{ExecID: "LEG2", PermID: 1, Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 177.5, Right: "P",
			Side: "BOT", Shares: 2, AvgPrice: 0.40, Time: t0},
	}
	out := enrichBAGFromLegs(group)
	if len(out) != 1 {
		t.Fatalf("len=%d, want 1 (BAG only, legs dropped): %+v", len(out), out)
	}
	if out[0].SecType != "BAG" || out[0].Expiration != "20260522" {
		t.Errorf("bag = %+v; want SecType=BAG, Expiration=20260522", out[0])
	}
}

func TestEnrichBAGFromLegsCalendarUsesLatestExpiry(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	group := []model.Execution{
		{ExecID: "BAG", PermID: 1, Symbol: "AAPL", SecType: "BAG",
			Side: "BOT", Shares: 1, AvgPrice: 0.50, Time: t0},
		{ExecID: "L1", PermID: 1, Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260529", Strike: 200, Right: "C",
			Side: "SLD", Shares: 1, AvgPrice: 1.20, Time: t0},
		{ExecID: "L2", PermID: 1, Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260619", Strike: 200, Right: "C",
			Side: "BOT", Shares: 1, AvgPrice: 1.70, Time: t0},
	}
	out := enrichBAGFromLegs(group)
	if len(out) != 1 || out[0].Expiration != "20260619" {
		t.Errorf("calendar BAG expiration = %q, want 20260619 (max of legs): %+v",
			out[0].Expiration, out)
	}
}

func TestEnrichBAGFromLegsNoBAGPassthrough(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	group := []model.Execution{
		{ExecID: "S1", PermID: 1, Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260522", Strike: 200, Right: "P",
			Side: "SLD", Shares: 1, AvgPrice: 2.50, Time: t0},
	}
	out := enrichBAGFromLegs(group)
	if len(out) != 1 || out[0].ExecID != "S1" {
		t.Errorf("no-BAG group should pass through unchanged, got %+v", out)
	}
}

func TestNormalizeBAGPriceTruthTable(t *testing.T) {
	cases := []struct {
		name      string
		inSide    string
		inPrice   float64
		wantSide  string
		wantPrice float64
	}{
		// IBKR's Side is authoritative; normalize only takes |price|.
		{"SLD credit (open credit spread)", "SLD", -0.90, "SLD", 0.90},
		{"BOT credit (close credit spread at credit, rare)", "BOT", -0.20, "BOT", 0.20},
		{"BOT debit (open debit spread)", "BOT", 0.50, "BOT", 0.50},
		{"SLD debit (close debit spread)", "SLD", 0.70, "SLD", 0.70},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := model.Execution{SecType: "BAG", Side: c.inSide, AvgPrice: c.inPrice}
			out := normalizeBAGPrice(in)
			if out.Side != c.wantSide || out.AvgPrice != c.wantPrice {
				t.Errorf("normalize(%s @ %v) = (%s, %v), want (%s, %v)",
					c.inSide, c.inPrice, out.Side, out.AvgPrice, c.wantSide, c.wantPrice)
			}
		})
	}
}

func TestNormalizeBAGPricePassthroughForNonBAG(t *testing.T) {
	in := model.Execution{SecType: "OPT", Side: "SLD", AvgPrice: -1.50}
	out := normalizeBAGPrice(in)
	if out.Side != "SLD" || out.AvgPrice != -1.50 {
		t.Errorf("non-BAG should pass through, got %+v", out)
	}
}

func TestMultiplierForBAG(t *testing.T) {
	if got := multiplierFor("BAG"); got != 100 {
		t.Errorf("multiplierFor(BAG) = %v, want 100", got)
	}
	if got := multiplierFor("OPT"); got != 100 {
		t.Errorf("multiplierFor(OPT) = %v, want 100", got)
	}
	if got := multiplierFor("STK"); got != 1 {
		t.Errorf("multiplierFor(STK) = %v, want 1", got)
	}
}

func TestEmitOpenBAGExpiresLikeOPT(t *testing.T) {
	k := instrumentKey{
		account: "DU1", symbol: "GLW", secType: "BAG",
		expiration: "20260522",
	}
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	lo := lot{time: openTime, qty: 2, avgPrice: 0.90, execID: "BAG-OPEN"}
	asOf := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC) // after 5/22 expiry

	rt := emitOpen(k, lo, "SHORT", 100, asOf)
	if rt.Status != "expired" {
		t.Fatalf("status = %q, want expired (BAG should expire like OPT)", rt.Status)
	}
	// SHORT expired credit captured: 0.90 × 2 × 100 = 180
	if rt.RealizedPnL != 180 {
		t.Errorf("realized = %v, want 180", rt.RealizedPnL)
	}
}

// T1: Issue #29 reproduction. One GLW Bull Put Spread sent by IBKR as 3
// executions sharing PermID=12345 — must collapse to exactly 1 round trip
// with the BAG's signed credit captured at expiry.
func TestMatcherIssue29_GLWBullPutSpread(t *testing.T) {
	openTime := time.Date(2026, 5, 15, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC) // after 5/22 expiry
	execs := []model.Execution{
		{ExecID: "BAG", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "BAG",
			Side: "SLD", Shares: 2, Price: -0.90, AvgPrice: -0.90},
		{ExecID: "LEG1", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "SLD", Shares: 2, Price: 1.30, AvgPrice: 1.30},
		{ExecID: "LEG2", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 177.5, Right: "P",
			Side: "BOT", Shares: 2, Price: 0.40, AvgPrice: 0.40},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 {
		t.Fatalf("got %d trips, want 1 (one spread = one trip): %+v", len(rts), rts)
	}
	r := rts[0]
	if r.Symbol != "GLW" || r.SecType != "BAG" {
		t.Errorf("symbol/sectype = %q/%q, want GLW/BAG", r.Symbol, r.SecType)
	}
	if r.Status != "expired" {
		t.Errorf("status = %q, want expired", r.Status)
	}
	if r.RealizedPnL != 180 {
		t.Errorf("realized = %v, want +180 (credit 0.90 × 2 contracts × 100)", r.RealizedPnL)
	}
	if r.Direction != "SHORT" {
		t.Errorf("direction = %q, want SHORT", r.Direction)
	}
}

// T2: Bull Put Spread closed via BTC combo (not held to expiry).
// Open: SLD BAG @ -0.90 (credit 180). Close: BOT BAG @ -0.20 (debit 40 to cover).
// Net realized: +140.
func TestMatcherBullPutSpreadBTC(t *testing.T) {
	openTime := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	closeTime := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	asOf := closeTime.Add(time.Hour)
	execs := []model.Execution{
		{ExecID: "BAG-O", PermID: 100, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "BAG",
			Side: "SLD", Shares: 2, Price: -0.90, AvgPrice: -0.90},
		{ExecID: "L1-O", PermID: 100, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "SLD", Shares: 2, AvgPrice: 1.30},
		{ExecID: "L2-O", PermID: 100, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 177.5, Right: "P",
			Side: "BOT", Shares: 2, AvgPrice: 0.40},
		{ExecID: "BAG-C", PermID: 200, Time: closeTime, Account: "DU1",
			Symbol: "GLW", SecType: "BAG",
			Side: "BOT", Shares: 2, Price: -0.20, AvgPrice: -0.20},
		{ExecID: "L1-C", PermID: 200, Time: closeTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "BOT", Shares: 2, AvgPrice: 0.50},
		{ExecID: "L2-C", PermID: 200, Time: closeTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 177.5, Right: "P",
			Side: "SLD", Shares: 2, AvgPrice: 0.30},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 {
		t.Fatalf("len=%d, want 1 closed spread: %+v", len(rts), rts)
	}
	if rts[0].Status != "closed" {
		t.Errorf("status = %q, want closed", rts[0].Status)
	}
	if rts[0].RealizedPnL != 140 {
		t.Errorf("realized = %v, want +140 (open 0.90 - close 0.20 = 0.70 × 2 × 100)",
			rts[0].RealizedPnL)
	}
}

// T3: Iron Condor — 1 BAG + 4 OPT legs sharing PermID, all OTM at expiry.
func TestMatcherIronCondorExpired(t *testing.T) {
	openTime := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	execs := []model.Execution{
		{ExecID: "BAG", PermID: 300, Time: openTime, Account: "DU1",
			Symbol: "SPY", SecType: "BAG",
			Side: "SLD", Shares: 1, AvgPrice: -1.50},
		{ExecID: "L1", PermID: 300, Time: openTime, Account: "DU1",
			Symbol: "SPY", SecType: "OPT",
			Expiration: "20260522", Strike: 500, Right: "C",
			Side: "SLD", Shares: 1, AvgPrice: 2.00},
		{ExecID: "L2", PermID: 300, Time: openTime, Account: "DU1",
			Symbol: "SPY", SecType: "OPT",
			Expiration: "20260522", Strike: 510, Right: "C",
			Side: "BOT", Shares: 1, AvgPrice: 1.00},
		{ExecID: "L3", PermID: 300, Time: openTime, Account: "DU1",
			Symbol: "SPY", SecType: "OPT",
			Expiration: "20260522", Strike: 450, Right: "P",
			Side: "SLD", Shares: 1, AvgPrice: 2.00},
		{ExecID: "L4", PermID: 300, Time: openTime, Account: "DU1",
			Symbol: "SPY", SecType: "OPT",
			Expiration: "20260522", Strike: 440, Right: "P",
			Side: "BOT", Shares: 1, AvgPrice: 1.50},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 || rts[0].Status != "expired" || rts[0].RealizedPnL != 150 {
		t.Fatalf("ironcondor rts = %+v (want 1 expired @ +150)", rts)
	}
}

// T4: Calendar spread, asOf past the LATER leg → BAG expired.
func TestMatcherCalendarFullyExpired(t *testing.T) {
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC) // past 6/19
	execs := []model.Execution{
		{ExecID: "BAG", PermID: 400, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "BAG",
			Side: "BOT", Shares: 1, AvgPrice: 0.50}, // long calendar (debit)
		{ExecID: "L1", PermID: 400, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260529", Strike: 200, Right: "C",
			Side: "SLD", Shares: 1, AvgPrice: 1.20},
		{ExecID: "L2", PermID: 400, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260619", Strike: 200, Right: "C",
			Side: "BOT", Shares: 1, AvgPrice: 1.70},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 || rts[0].Status != "expired" {
		t.Fatalf("calendar fully-expired rts = %+v", rts)
	}
	// long calendar @ 0.50 debit, expires worthless → -50
	if rts[0].RealizedPnL != -50 {
		t.Errorf("realized = %v, want -50 (debit lost at expiry)", rts[0].RealizedPnL)
	}
}

// T5: Calendar spread, asOf BETWEEN the two leg expirations → still open
// (BAG anchors on the LATER leg).
func TestMatcherCalendarMidStateOpen(t *testing.T) {
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC) // front expired, back alive
	execs := []model.Execution{
		{ExecID: "BAG", PermID: 500, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "BAG",
			Side: "BOT", Shares: 1, AvgPrice: 0.50},
		{ExecID: "L1", PermID: 500, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260529", Strike: 200, Right: "C",
			Side: "SLD", Shares: 1, AvgPrice: 1.20},
		{ExecID: "L2", PermID: 500, Time: openTime, Account: "DU1",
			Symbol: "AAPL", SecType: "OPT",
			Expiration: "20260619", Strike: 200, Right: "C",
			Side: "BOT", Shares: 1, AvgPrice: 1.70},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 || rts[0].Status != "open" {
		t.Fatalf("calendar mid-state rts = %+v (want 1 open)", rts)
	}
}

// T6: Single-leg OPT trade with no BAG row matches as before — Path A
// does not change behavior for non-spread option trades.
func TestMatcherSoloOPTUnchanged(t *testing.T) {
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	execs := []model.Execution{{
		ExecID: "SOLO", PermID: 789, Time: openTime, Account: "DU1",
		Symbol: "TSLA", SecType: "OPT",
		Expiration: "20260522", Strike: 200, Right: "P",
		Side: "SLD", Shares: 1, AvgPrice: 2.50,
	}}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 1 || rts[0].Direction != "SHORT" ||
		rts[0].Status != "expired" || rts[0].RealizedPnL != 250 {
		t.Fatalf("solo OPT rts = %+v (want 1 SHORT expired @ +250)", rts)
	}
}

// T7: Stock trade — Path A does not change behavior.
func TestMatcherSTKUnchangedWithPermID(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)
	execs := []model.Execution{
		{ExecID: "S1", PermID: 11, Time: t0, Account: "DU1",
			Symbol: "AAPL", SecType: "STK",
			Side: "BOT", Shares: 100, AvgPrice: 150},
		{ExecID: "S2", PermID: 22, Time: t1, Account: "DU1",
			Symbol: "AAPL", SecType: "STK",
			Side: "SLD", Shares: 100, AvgPrice: 160},
	}
	rts := MatchRoundTrips(execs, t1.Add(time.Hour))
	if len(rts) != 1 || rts[0].RealizedPnL != 1000 {
		t.Fatalf("STK rts = %+v (want +1000)", rts)
	}
}

// T8: BAG spread + solo OPT coexist in one account, no cross-contamination.
func TestMatcherBAGAndSoloOPTCoexist(t *testing.T) {
	openTime := time.Date(2026, 5, 15, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	execs := []model.Execution{
		// GLW spread (3 rows)
		{ExecID: "BAG", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "BAG",
			Side: "SLD", Shares: 2, AvgPrice: -0.90},
		{ExecID: "L1", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "SLD", Shares: 2, AvgPrice: 1.30},
		{ExecID: "L2", PermID: 12345, Time: openTime, Account: "DU1",
			Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 177.5, Right: "P",
			Side: "BOT", Shares: 2, AvgPrice: 0.40},
		// Solo TSLA put
		{ExecID: "SOLO", PermID: 789, Time: openTime, Account: "DU1",
			Symbol: "TSLA", SecType: "OPT",
			Expiration: "20260522", Strike: 200, Right: "P",
			Side: "SLD", Shares: 1, AvgPrice: 2.50},
	}
	rts := MatchRoundTrips(execs, asOf)
	if len(rts) != 2 {
		t.Fatalf("len=%d, want 2 (one BAG + one OPT): %+v", len(rts), rts)
	}
	var glw, tsla *model.RoundTrip
	for i := range rts {
		switch rts[i].Symbol {
		case "GLW":
			glw = &rts[i]
		case "TSLA":
			tsla = &rts[i]
		}
	}
	if glw == nil || tsla == nil {
		t.Fatalf("missing GLW or TSLA trip: %+v", rts)
	}
	if glw.RealizedPnL != 180 {
		t.Errorf("GLW realized = %v, want +180", glw.RealizedPnL)
	}
	if tsla.RealizedPnL != 250 {
		t.Errorf("TSLA realized = %v, want +250", tsla.RealizedPnL)
	}
}
