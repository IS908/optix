package portfolio

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func ComputeHistoricalBeta(symbolBars, spyBars []model.OHLCV, minObservations, maxObservations int) (float64, int, time.Time, error) {
	symReturns := dailyReturns(symbolBars)
	spyReturns := dailyReturns(spyBars)
	if len(symReturns) == 0 || len(spyReturns) == 0 {
		return 0, 0, time.Time{}, fmt.Errorf("not enough bars for beta")
	}

	type pair struct {
		t   time.Time
		sym float64
		spy float64
	}
	var pairs []pair
	for day, sr := range symReturns {
		if mr, ok := spyReturns[day]; ok {
			pairs = append(pairs, pair{t: day, sym: sr, spy: mr})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].t.Before(pairs[j].t) })
	if maxObservations > 0 && len(pairs) > maxObservations {
		pairs = pairs[len(pairs)-maxObservations:]
	}
	if len(pairs) < minObservations {
		return 0, len(pairs), time.Time{}, fmt.Errorf("beta needs at least %d aligned observations, got %d", minObservations, len(pairs))
	}

	var meanSym, meanSpy float64
	for _, p := range pairs {
		meanSym += p.sym
		meanSpy += p.spy
	}
	meanSym /= float64(len(pairs))
	meanSpy /= float64(len(pairs))

	var cov, variance float64
	for _, p := range pairs {
		ds := p.sym - meanSym
		dm := p.spy - meanSpy
		cov += ds * dm
		variance += dm * dm
	}
	if variance == 0 || math.IsNaN(variance) {
		return 0, len(pairs), time.Time{}, fmt.Errorf("benchmark variance is zero")
	}
	beta := cov / variance
	if math.IsNaN(beta) || math.IsInf(beta, 0) {
		return 0, len(pairs), time.Time{}, fmt.Errorf("beta is non-finite")
	}
	return beta, len(pairs), pairs[len(pairs)-1].t, nil
}

func dailyReturns(bars []model.OHLCV) map[time.Time]float64 {
	bars = append([]model.OHLCV(nil), bars...)
	sort.Slice(bars, func(i, j int) bool { return bars[i].Timestamp.Before(bars[j].Timestamp) })
	out := make(map[time.Time]float64, len(bars))
	for i := 1; i < len(bars); i++ {
		prev := bars[i-1].Close
		cur := bars[i].Close
		if prev <= 0 || cur <= 0 {
			continue
		}
		day := time.Date(bars[i].Timestamp.UTC().Year(), bars[i].Timestamp.UTC().Month(), bars[i].Timestamp.UTC().Day(), 0, 0, 0, 0, time.UTC)
		out[day] = cur/prev - 1
	}
	return out
}
