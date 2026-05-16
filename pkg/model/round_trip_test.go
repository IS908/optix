package model

import "testing"

func TestRoundTripIsOption(t *testing.T) {
	cases := []struct {
		name    string
		secType string
		want    bool
	}{
		{"stock", "STK", false},
		{"option", "OPT", true},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := RoundTrip{SecType: tc.secType}
			if got := rt.IsOption(); got != tc.want {
				t.Errorf("IsOption() = %v, want %v", got, tc.want)
			}
		})
	}
}
