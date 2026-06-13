package marketdata

import (
	"math"
	"testing"
)

func TestParsePutCallRatio(t *testing.T) {
	raw := []byte(`{"underlying":"SPX","expirations":[{"expiration":"20260620",
        "calls":[{"strike":6000,"volume":100,"openInterest":500},{"strike":6100,"volume":50,"openInterest":300}],
        "puts":[{"strike":5900,"volume":80,"openInterest":700},{"strike":5800,"volume":40,"openInterest":400}]}]}`)
	pc, err := parsePutCallRatio(raw)
	if err != nil {
		t.Fatal(err)
	}
	// calls OI=800 vol=150；puts OI=1100 vol=120 → PCOI=1.375 PCVol=0.8
	if pc.Underlying != "SPX" || pc.Expiration != "20260620" {
		t.Errorf("header = %+v", pc)
	}
	if pc.CallOI != 800 || pc.PutOI != 1100 || pc.CallVol != 150 || pc.PutVol != 120 {
		t.Errorf("sums = %+v", pc)
	}
	if math.Abs(pc.PCOI-1.375) > 1e-9 || math.Abs(pc.PCVol-0.8) > 1e-9 {
		t.Errorf("ratios PCOI=%v PCVol=%v", pc.PCOI, pc.PCVol)
	}
}

func TestParsePutCallRatio_NoExpirations(t *testing.T) {
	if _, err := parsePutCallRatio([]byte(`{"underlying":"X","expirations":[]}`)); err == nil {
		t.Error("empty expirations must error (no chain)")
	}
}

func TestParsePutCallRatio_ZeroCallOI(t *testing.T) {
	// callOI=0 → PCOI 不能除零，定义为 0（调用方判不可用）
	raw := []byte(`{"underlying":"X","expirations":[{"expiration":"20260620",
        "calls":[],"puts":[{"strike":1,"volume":10,"openInterest":100}]}]}`)
	pc, err := parsePutCallRatio(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pc.PCOI != 0 || pc.PCVol != 0 {
		t.Errorf("zero call side → ratios 0, got %+v", pc)
	}
}
