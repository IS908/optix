package marketdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// scriptedSource: 可注入缺席与错误的 fake。
type scriptedSource struct {
	quotes  map[string]Quote
	err     error
	barsErr error
	calls   atomic.Int32
}

func (s *scriptedSource) Name() string { return "scripted" }
func (s *scriptedSource) BatchQuotes(_ context.Context, refs []AssetRef) (map[string]Quote, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]Quote{}
	for _, r := range refs {
		if q, ok := s.quotes[r.ID]; ok {
			out[r.ID] = q
		}
	}
	return out, nil
}
func (s *scriptedSource) Bars(context.Context, AssetRef, string, time.Duration) ([]model.OHLCV, error) {
	return nil, s.barsErr
}

func newTestPulse(src Source) *PulseService {
	r := NewRouter()
	for _, c := range []AssetClass{ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol} {
		r.Register(c, src)
	}
	return NewPulseService(r, nil) // store=nil → sparkline 路径跳过
}

func TestSnapshot_MissingAssetDoesNotKillScreen(t *testing.T) {
	src := &scriptedSource{quotes: map[string]Quote{
		"ES": {Ref: AssetRef{ID: "ES", Class: ClassFuture}, Price: 6012, Basis: BasisDelayed},
		// 其余资产缺席
	}}
	p := newTestPulse(src)
	snap, err := p.Snapshot(context.Background(), ViewPremarket, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Assets) == 0 {
		t.Fatal("present assets must render")
	}
	if len(snap.Missing) == 0 {
		t.Fatal("absent assets must be listed in Missing")
	}
	for _, id := range snap.Missing {
		if id == "ES" {
			t.Fatal("ES is present; must not be in Missing")
		}
	}
}

func TestSnapshot_SourceTotalFailureReturnsSnapshotWithWarnings(t *testing.T) {
	src := &scriptedSource{err: errors.New("python unavailable")}
	p := newTestPulse(src)
	snap, err := p.Snapshot(context.Background(), ViewIntraday, false)
	if err != nil {
		t.Fatalf("total source failure must not error the snapshot: %v", err)
	}
	if len(snap.Assets) != 0 || len(snap.Warnings) == 0 || len(snap.Missing) == 0 {
		t.Fatalf("snap = %+v", snap)
	}
}

func TestSnapshot_FrozenBasisAdjustment(t *testing.T) {
	// postclose 视图下，index 类 delayed → frozen（收盘定格）。
	src := &scriptedSource{quotes: map[string]Quote{
		"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 5993, Basis: BasisDelayed},
	}}
	p := newTestPulse(src)
	snap, _ := p.Snapshot(context.Background(), ViewPostclose, false)
	for _, a := range snap.Assets {
		if a.Ref.ID == "SPX" && a.Basis != BasisFrozen {
			t.Fatalf("postclose SPX basis = %s, want frozen", a.Basis)
		}
	}
}

func TestSnapshot_CacheTTLAndSingleflight(t *testing.T) {
	src := &scriptedSource{quotes: map[string]Quote{
		"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 1, Basis: BasisDelayed},
	}}
	p := newTestPulse(src)
	ctx := context.Background()
	_, _ = p.Snapshot(ctx, ViewIntraday, false)
	_, _ = p.Snapshot(ctx, ViewIntraday, false) // TTL 内 → 缓存命中
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("source calls = %d, want 1 (second hit served from cache)", got)
	}
}
