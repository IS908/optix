package cli

import (
	"github.com/IS908/optix/internal/premarket"
	"strings"
	"testing"
)

func TestRenderPremarketUnavailableVolume(t *testing.T) {
	out := captureStdout(t, func() {
		renderPremarket(premarket.BundleDTO{Movers: premarket.MoversDTO{Gainers: []premarket.Mover{{Symbol: "AAPL", Pct: 2, MissingMetrics: []string{"vol_ratio"}}}}})
	})
	if !strings.Contains(out, "量比 不可用") || strings.Contains(out, "量比 0.0x") {
		t.Fatalf("missing volume rendered as zero: %s", out)
	}
}
