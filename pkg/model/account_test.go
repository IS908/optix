package model

import "testing"

func TestPositionIsOption(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{"stock", Position{SecType: "STK"}, false},
		{"option", Position{SecType: "OPT"}, true},
		{"empty", Position{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.IsOption(); got != tt.want {
				t.Errorf("IsOption() = %v, want %v", got, tt.want)
			}
		})
	}
}
