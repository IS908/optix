package marketdata

import "testing"

func TestParseRawEarnings(t *testing.T) {
	raw := []byte(`{
		"AAPL": [
			{
				"symbol": "AAPL",
				"event_time": "2026-07-30T20:00:00Z",
				"timing": "postmarket",
				"eps_estimate": 1.43,
				"eps_reported": 1.57,
				"eps_surprise_pct": 9.79
			}
		],
		"NVDA": []
	}`)
	got, err := parseRawEarnings(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["AAPL"]) != 1 {
		t.Fatalf("AAPL rows = %d", len(got["AAPL"]))
	}
	row := got["AAPL"][0]
	if row.Symbol != "AAPL" || row.Timing != "postmarket" {
		t.Fatalf("row identity = %+v", row)
	}
	if row.EventTime.IsZero() {
		t.Fatal("event_time not parsed")
	}
	if row.EPSEstimate == nil || *row.EPSEstimate != 1.43 {
		t.Fatalf("eps_estimate = %#v", row.EPSEstimate)
	}
	if row.EPSReported == nil || *row.EPSReported != 1.57 {
		t.Fatalf("eps_reported = %#v", row.EPSReported)
	}
	if row.EPSSurprisePct == nil || *row.EPSSurprisePct != 9.79 {
		t.Fatalf("eps_surprise_pct = %#v", row.EPSSurprisePct)
	}
	if got["NVDA"] == nil {
		t.Fatal("empty symbol should decode as non-nil slice")
	}
}

func TestParseRawEarningsSkipsBadTimes(t *testing.T) {
	raw := []byte(`{"AAPL":[{"symbol":"AAPL","event_time":"bad","eps_estimate":1.0}]}`)
	got, err := parseRawEarnings(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["AAPL"]) != 0 {
		t.Fatalf("bad timestamp rows = %+v", got["AAPL"])
	}
}
