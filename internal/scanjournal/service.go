// Package scanjournal 实现 sell-put 扫描复盘闭环：候选入库、到期对账、分档统计。
// 写路径只走 CLI（M3 护栏）。设计见 docs/superpowers/specs/2026-07-29-sellput-scan-journal-design.md。
package scanjournal

import (
	"context"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type Store interface {
	InsertScanCandidates(ctx context.Context, cands []model.ScanCandidate) (int, int, error)
	ExpiredUnsettledScanCandidates(ctx context.Context, beforeDate string) ([]model.ScanCandidate, error)
	InsertScanReconciliation(ctx context.Context, r model.ScanReconciliation) error
	ListScanCandidatesSince(ctx context.Context, fromDate string) ([]model.ScanCandidate, error)
	ListScanReconciliations(ctx context.Context, candidateIDs []string) (map[string]model.ScanReconciliation, error)
}

// BarsSource 提供任意个股的日线（对账取价用）。生产实现走 marketdata.RawBatchBars。
type BarsSource interface {
	DailyBars(ctx context.Context, symbols []string, period string) (map[string][]model.OHLCV, error)
}

type Service struct {
	Store Store
	Bars  BarsSource
	Now   func() time.Time // 可注入（测试钉死 NY 时刻）
}

func NewService(store Store, bars BarsSource) *Service {
	return &Service{Store: store, Bars: bars, Now: time.Now}
}
