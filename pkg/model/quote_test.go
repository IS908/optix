package model

import (
	"testing"
	"time"
)

func TestUSMarketSession(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal("could not load America/New_York timezone")
	}

	tests := []struct {
		name string
		time time.Time
		want MarketSession
	}{
		{
			name: "pre-market 4:00 AM",
			time: time.Date(2026, 3, 25, 4, 0, 0, 0, et), // Wednesday
			want: SessionPreMarket,
		},
		{
			name: "pre-market 8:30 AM",
			time: time.Date(2026, 3, 25, 8, 30, 0, 0, et),
			want: SessionPreMarket,
		},
		{
			name: "regular open 9:30 AM",
			time: time.Date(2026, 3, 25, 9, 30, 0, 0, et),
			want: SessionRegular,
		},
		{
			name: "regular midday",
			time: time.Date(2026, 3, 25, 12, 0, 0, 0, et),
			want: SessionRegular,
		},
		{
			name: "regular close 3:59 PM",
			time: time.Date(2026, 3, 25, 15, 59, 0, 0, et),
			want: SessionRegular,
		},
		{
			name: "post-market 4:00 PM",
			time: time.Date(2026, 3, 25, 16, 0, 0, 0, et),
			want: SessionPostMarket,
		},
		{
			name: "post-market 7:59 PM",
			time: time.Date(2026, 3, 25, 19, 59, 0, 0, et),
			want: SessionPostMarket,
		},
		{
			name: "closed 8:00 PM",
			time: time.Date(2026, 3, 25, 20, 0, 0, 0, et),
			want: SessionClosed,
		},
		{
			name: "closed 3:59 AM",
			time: time.Date(2026, 3, 25, 3, 59, 0, 0, et),
			want: SessionClosed,
		},
		{
			name: "Saturday closed",
			time: time.Date(2026, 3, 28, 12, 0, 0, 0, et), // Saturday
			want: SessionClosed,
		},
		{
			name: "Sunday closed",
			time: time.Date(2026, 3, 29, 12, 0, 0, 0, et), // Sunday
			want: SessionClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := USMarketSession(tt.time)
			if got != tt.want {
				t.Errorf("USMarketSession(%v) = %q, want %q", tt.time, got, tt.want)
			}
		})
	}
}

func TestMarketSession_IsExtendedHours(t *testing.T) {
	if !SessionPreMarket.IsExtendedHours() {
		t.Error("pre_market should be extended hours")
	}
	if !SessionPostMarket.IsExtendedHours() {
		t.Error("post_market should be extended hours")
	}
	if SessionRegular.IsExtendedHours() {
		t.Error("regular should not be extended hours")
	}
	if SessionClosed.IsExtendedHours() {
		t.Error("closed should not be extended hours")
	}
}

func TestMarketSession_IsOpen(t *testing.T) {
	if !SessionPreMarket.IsOpen() {
		t.Error("pre_market should be open")
	}
	if !SessionRegular.IsOpen() {
		t.Error("regular should be open")
	}
	if !SessionPostMarket.IsOpen() {
		t.Error("post_market should be open")
	}
	if SessionClosed.IsOpen() {
		t.Error("closed should not be open")
	}
}

func TestMarketSession_Label(t *testing.T) {
	tests := []struct {
		session MarketSession
		want    string
	}{
		{SessionPreMarket, "盘前"},
		{SessionRegular, "盘中"},
		{SessionPostMarket, "盘后"},
		{SessionClosed, "休市"},
	}
	for _, tt := range tests {
		if got := tt.session.Label(); got != tt.want {
			t.Errorf("%q.Label() = %q, want %q", tt.session, got, tt.want)
		}
	}
}
