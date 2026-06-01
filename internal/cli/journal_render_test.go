package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestRenderTripsTableShowsCurrency(t *testing.T) {
	out := captureStdout(t, func() {
		renderTripsTable([]model.RoundTrip{{
			Symbol: "0700", Currency: "HKD", Direction: "LONG",
			OpenTime: time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC),
			OpenQty:  100, OpenAvgPrice: 10, RealizedPnL: 200, Status: "open",
		}})
	})
	if !strings.Contains(out, "Ccy") || !strings.Contains(out, "HKD") {
		t.Fatalf("rendered trips table missing currency column/value:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}
