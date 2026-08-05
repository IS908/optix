package scanjournal

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

const (
	// GraceDays：到期后取不到价的宽限期（日历日）。窗口内 pending 重试，之后 void。
	GraceDays = 7
	// barsPeriod 覆盖 max DTE(24) + 宽限期(7) + 余量。
	barsPeriod = "3mo"
	// ExpiryBasisDelayed：对账价来自 yfinance 日线（延迟口径）。
	ExpiryBasisDelayed = "delayed"
)

type SettledRow struct {
	CandidateID  string  `json:"candidate_id"`
	Symbol       string  `json:"symbol"`
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	Outcome      string  `json:"outcome"`
	ExpiryClose  float64 `json:"expiry_close"`
	RealizedPnL  float64 `json:"realized_pnl"`
	Touched      bool    `json:"touched"`
	MaxBreachPct float64 `json:"max_breach_pct"`
}

type HitRate struct {
	Hit    int     `json:"hit"`
	Miss   int     `json:"miss"`
	Void   int     `json:"void"`
	Rate   float64 `json:"rate"`
	AvgPnL float64 `json:"avg_pnl"`
	Window string  `json:"window"`
}

type ReconcileResult struct {
	Settled  int          `json:"settled"`
	Void     int          `json:"void"`
	Pending  int          `json:"pending"`
	Results  []SettledRow `json:"results"`
	HitRate  HitRate      `json:"hit_rate"`
	Warnings []string     `json:"warnings,omitempty"`
}

// Reconcile 结算所有「已到期未结算」候选。单候选取价失败不中断批次。
func (s *Service) Reconcile(ctx context.Context) (ReconcileResult, error) {
	todayNY := s.Now().In(intelshared.NY())
	today := todayNY.Format(dateLayout)
	pending, err := s.Store.ExpiredUnsettledScanCandidates(ctx, today)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load expired: %w", err)
	}
	out := ReconcileResult{Results: []SettledRow{}}
	if len(pending) > 0 {
		symSet := map[string]bool{}
		for _, c := range pending {
			symSet[c.Symbol] = true
		}
		syms := make([]string, 0, len(symSet))
		for sym := range symSet {
			syms = append(syms, sym)
		}
		sort.Strings(syms)
		bars, err := s.Bars.DailyBars(ctx, syms, barsPeriod)
		if err != nil {
			// 数据源整体失败：全部按 pending 处理（宽限期规则下自然重试）
			out.Warnings = append(out.Warnings, fmt.Sprintf("daily bars unavailable: %v", err))
			bars = map[string][]model.OHLCV{}
		}
		for _, c := range pending {
			rec, state := settleCandidate(c, bars[c.Symbol], todayNY)
			switch state {
			case "pending":
				out.Pending++
				continue
			case "void":
				out.Void++
			}
			rec.SettledAt = s.Now().UTC()
			if err := s.Store.InsertScanReconciliation(ctx, rec); err != nil {
				return out, fmt.Errorf("insert reconciliation %s: %w", c.CandidateID, err)
			}
			out.Settled++
			out.Results = append(out.Results, SettledRow{
				CandidateID: c.CandidateID, Symbol: c.Symbol, Strike: c.Strike,
				Expiry: c.Expiry, Outcome: rec.Outcome, ExpiryClose: rec.ExpiryClose,
				RealizedPnL: rec.RealizedPnL, Touched: rec.Touched, MaxBreachPct: rec.MaxBreachPct,
			})
		}
	}
	hr, err := s.hitRateAll(ctx)
	if err != nil {
		return out, fmt.Errorf("hit rate: %w", err)
	}
	out.HitRate = hr
	return out, nil
}

// settleCandidate 纯函数：候选 + 该标的日线 + 今天(NY) → (对账记录, settled|pending|void)。
func settleCandidate(c model.ScanCandidate, bars []model.OHLCV, todayNY time.Time) (model.ScanReconciliation, string) {
	ny := intelshared.NY()
	var expiryClose float64
	found := false
	minLow := 0.0
	haveLow := false
	for _, b := range bars {
		d := b.Timestamp.In(ny).Format(dateLayout)
		if d == c.Expiry && b.Close > 0 {
			expiryClose = b.Close
			found = true
		}
		if d >= c.ScanDate && d <= c.Expiry && b.Low > 0 {
			if !haveLow || b.Low < minLow {
				minLow = b.Low
				haveLow = true
			}
		}
	}
	if !found {
		expiry, _ := time.Parse(dateLayout, c.Expiry)
		daysSince := int(todayNY.Sub(expiry.In(time.UTC)).Hours() / 24)
		if daysSince <= GraceDays {
			return model.ScanReconciliation{}, "pending"
		}
		return model.ScanReconciliation{
			CandidateID: c.CandidateID, ExpiryClose: 0, Outcome: "void",
			RealizedPnL: 0, Touched: false, MaxBreachPct: 0, ExpiryBasis: ExpiryBasisDelayed,
		}, "void"
	}
	outcome := "miss"
	if expiryClose > c.Strike {
		outcome = "hit"
	}
	intrinsic := c.Strike - expiryClose
	if intrinsic < 0 {
		intrinsic = 0
	}
	touched := haveLow && minLow < c.Strike
	breach := 0.0
	if touched {
		breach = (c.Strike - minLow) / c.Strike * 100.0
	}
	return model.ScanReconciliation{
		CandidateID: c.CandidateID, ExpiryClose: expiryClose, Outcome: outcome,
		RealizedPnL: c.Bid - intrinsic, Touched: touched, MaxBreachPct: breach,
		ExpiryBasis: ExpiryBasisDelayed,
	}, "settled"
}

// hitRateAll 全历史命中率（void 不进分母；avg_pnl 只平均 hit/miss）。
func (s *Service) hitRateAll(ctx context.Context) (HitRate, error) {
	cands, err := s.Store.ListScanCandidatesSince(ctx, "")
	if err != nil {
		return HitRate{}, err
	}
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.CandidateID)
	}
	recs, err := s.Store.ListScanReconciliations(ctx, ids)
	if err != nil {
		return HitRate{}, err
	}
	hr := HitRate{Window: "all"}
	pnlSum := 0.0
	for _, r := range recs {
		switch r.Outcome {
		case "hit":
			hr.Hit++
			pnlSum += r.RealizedPnL
		case "miss":
			hr.Miss++
			pnlSum += r.RealizedPnL
		case "void":
			hr.Void++
		}
	}
	if d := hr.Hit + hr.Miss; d > 0 {
		hr.Rate = float64(hr.Hit) / float64(d)
		hr.AvgPnL = pnlSum / float64(d)
	}
	return hr, nil
}
