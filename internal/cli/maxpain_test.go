package cli

import (
	"strings"
	"testing"
)

func TestNewMaxPainCmdSourceFlag(t *testing.T) {
	cmd := newMaxPainCmd()
	if got, _ := cmd.Flags().GetString("source"); got != "auto" {
		t.Errorf("default --source = %q, want auto", got)
	}
}

func TestNewMaxPainCmdHelpMentionsExpiry(t *testing.T) {
	cmd := newMaxPainCmd()
	if !strings.Contains(cmd.Long, "expiry") {
		t.Errorf("Long help should mention expiry; got:\n%s", cmd.Long)
	}
}

func TestValidateSourceFlag(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ibkr", true},
		{"yfinance", true},
		{"auto", true},
		{"AUTO", true},
		{"foo", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			err := validateSourceFlag(tc.in)
			if (err == nil) != tc.want {
				t.Errorf("validateSourceFlag(%q) err=%v, wantOK=%v", tc.in, err, tc.want)
			}
		})
	}
}

func TestValidateOutputFormat(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"text", true},
		{"json", true},
		{"JSON", true},
		{"xml", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			err := validateOutputFormat(tc.in)
			if (err == nil) != tc.want {
				t.Errorf("validateOutputFormat(%q) err=%v, wantOK=%v", tc.in, err, tc.want)
			}
		})
	}
}
