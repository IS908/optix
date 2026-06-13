package marketdata

import "testing"

func TestParseRawBatchBars(t *testing.T) {
	raw := []byte(`{"AAPL":[{"ts":"2026-06-12T08:00:00-04:00","open":200,"high":201,"low":199,"close":200.5,"volume":1000}],
        "NVDA":[{"ts":"2026-06-12T08:00:00-04:00","open":120,"high":121,"low":119,"close":120.5,"volume":2000}]}`)
	out, err := parseRawBatchBars(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["AAPL"]) != 1 || out["AAPL"][0].Close != 200.5 {
		t.Fatalf("AAPL = %+v", out["AAPL"])
	}
	if out["NVDA"][0].Volume != 2000 {
		t.Errorf("NVDA volume = %d", out["NVDA"][0].Volume)
	}
	if out["AAPL"][0].Timestamp.IsZero() {
		t.Error("timestamp not parsed")
	}
}
