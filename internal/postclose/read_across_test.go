package postclose

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/IS908/optix/internal/portfolio"
)

func TestBuildReadAcrossEdgesUsesSameSectorPeers(t *testing.T) {
	sm := &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
			"META": "mega-cap-tech",
			"NVDA": "ai-chips",
		},
	}
	movers := []Mover{
		{Symbol: "AAPL", AfterHoursPct: 3.0},
		{Symbol: "MSFT", AfterHoursPct: 0.2},
		{Symbol: "META", AfterHoursPct: -0.1},
		{Symbol: "NVDA", AfterHoursPct: 0.4},
	}
	edges := BuildReadAcrossEdges(movers, sm)
	if len(edges) != 2 {
		t.Fatalf("edges = %+v", edges)
	}
	if edges[0].Driver != "AAPL" || edges[0].Peer != "META" && edges[0].Peer != "MSFT" {
		t.Fatalf("first edge = %+v", edges[0])
	}
	if edges[0].SectorLabel != "Mega-cap Tech" || edges[0].Direction != "positive" {
		t.Fatalf("edge metadata = %+v", edges[0])
	}
	if edges[0].SignalStrength <= 0.6 || edges[0].Lag != "T+1 open" {
		t.Fatalf("signal strength/lag = %+v", edges[0])
	}
	raw, err := json.Marshal(edges[0])
	if err != nil {
		t.Fatalf("marshal edge = %v", err)
	}
	if bytes.Contains(raw, []byte("confidence")) {
		t.Fatalf("read-across JSON must not expose confidence for magnitude-only signal: %s", string(raw))
	}
	if !bytes.Contains(raw, []byte("signal_strength")) {
		t.Fatalf("read-across JSON missing signal_strength: %s", string(raw))
	}
	if !strings.Contains(edges[0].Note, "非历史命中率") {
		t.Fatalf("note should disclose signal-strength semantics: %+v", edges[0])
	}
}
