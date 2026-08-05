package scanjournal

import (
	"context"
	"fmt"
	"sort"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

type Band struct {
	Label        string  `json:"label"`
	N            int     `json:"n"`
	HitRate      float64 `json:"hit_rate"`
	AvgPnL       float64 `json:"avg_pnl"`
	TouchedRate  float64 `json:"touched_rate"`
	AvgMaxBreach float64 `json:"avg_max_breach"`
}

type StatsResult struct {
	Window string `json:"window"`
	By     string `json:"by"`
	Bands  []Band `json:"bands"`
	Note   string `json:"note,omitempty"`
}

type settledPair struct {
	cand model.ScanCandidate
	rec  model.ScanReconciliation
}

// Stats 分档统计（void 排除在所有指标外）。window ∈ all|30d|90d；by ∈ score-band|rank|dte。
func (s *Service) Stats(ctx context.Context, window, by string) (StatsResult, error) {
	fromDate := ""
	switch window {
	case "", "all":
		window = "all"
	case "30d", "90d":
		days := 30
		if window == "90d" {
			days = 90
		}
		fromDate = s.Now().In(intelshared.NY()).AddDate(0, 0, -days).Format(dateLayout)
	default:
		return StatsResult{}, fmt.Errorf("invalid window %q (use all|30d|90d)", window)
	}
	if by == "" {
		by = "score-band"
	}
	if by != "score-band" && by != "rank" && by != "dte" {
		return StatsResult{}, fmt.Errorf("invalid --by %q (use score-band|rank|dte)", by)
	}
	cands, err := s.Store.ListScanCandidatesSince(ctx, fromDate)
	if err != nil {
		return StatsResult{}, err
	}
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.CandidateID)
	}
	recs, err := s.Store.ListScanReconciliations(ctx, ids)
	if err != nil {
		return StatsResult{}, err
	}
	pairs := []settledPair{}
	for _, c := range cands {
		if r, ok := recs[c.CandidateID]; ok && r.Outcome != "void" {
			pairs = append(pairs, settledPair{cand: c, rec: r})
		}
	}
	out := StatsResult{Window: window, By: by}
	if len(pairs) < 3 {
		out.Bands = []Band{bandOf("all", pairs)}
		out.Note = fmt.Sprintf("only %d settled candidates — banding degraded to a single group", len(pairs))
		return out, nil
	}
	switch by {
	case "score-band":
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].cand.Score > pairs[j].cand.Score })
		third := len(pairs) / 3
		rem := len(pairs) % 3
		sizes := []int{third, third, third}
		for i := 0; i < rem; i++ {
			sizes[i]++
		}
		labels := []string{"high", "mid", "low"}
		start := 0
		for i, size := range sizes {
			out.Bands = append(out.Bands, bandOf(labels[i], pairs[start:start+size]))
			start += size
		}
	case "rank":
		out.Bands = groupBy(pairs, func(p settledPair) string { return fmt.Sprintf("rank-%d", p.cand.Rank) })
	case "dte":
		out.Bands = groupBy(pairs, func(p settledPair) string {
			switch {
			case p.cand.DTE <= 10:
				return "dte-7-10"
			case p.cand.DTE <= 17:
				return "dte-11-17"
			default:
				return "dte-18-24"
			}
		})
	}
	return out, nil
}

func groupBy(pairs []settledPair, key func(settledPair) string) []Band {
	groups := map[string][]settledPair{}
	order := []string{}
	for _, p := range pairs {
		k := key(p)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}
	sort.Strings(order)
	bands := make([]Band, 0, len(order))
	for _, k := range order {
		bands = append(bands, bandOf(k, groups[k]))
	}
	return bands
}

func bandOf(label string, pairs []settledPair) Band {
	b := Band{Label: label, N: len(pairs)}
	if len(pairs) == 0 {
		return b
	}
	hits, touched := 0, 0
	pnl, breach := 0.0, 0.0
	for _, p := range pairs {
		if p.rec.Outcome == "hit" {
			hits++
		}
		if p.rec.Touched {
			touched++
		}
		pnl += p.rec.RealizedPnL
		breach += p.rec.MaxBreachPct
	}
	n := float64(len(pairs))
	b.HitRate = float64(hits) / n
	b.AvgPnL = pnl / n
	b.TouchedRate = float64(touched) / n
	b.AvgMaxBreach = breach / n
	return b
}
