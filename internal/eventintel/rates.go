package eventintel

import (
	"fmt"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

type eventAsset struct {
	ID    string
	Label string
	Class marketdata.AssetClass
	Kind  string
}

var ratePathAssets = []eventAsset{
	{ID: "US2Y", Label: "US 2Y", Class: marketdata.ClassFuture, Kind: "yield_proxy"},
	{ID: "US10Y", Label: "US 10Y", Class: marketdata.ClassYield, Kind: "yield_proxy"},
	{ID: "DXY", Label: "DXY", Class: marketdata.ClassFX, Kind: "dollar"},
	{ID: "GOLD", Label: "Gold", Class: marketdata.ClassFuture, Kind: "commodity"},
	{ID: "SPX", Label: "SPX", Class: marketdata.ClassIndex, Kind: "equity_index"},
	{ID: "NDX", Label: "NDX", Class: marketdata.ClassIndex, Kind: "equity_index"},
	{ID: "VIX", Label: "VIX", Class: marketdata.ClassIndex, Kind: "volatility"},
}

func BuildRatePath(quotes map[string]marketdata.Quote, bars map[string][]model.OHLCV, asOf time.Time) RatePathDTO {
	out := RatePathDTO{
		AsOf:         asOf.UTC(),
		Source:       "yfinance",
		UniverseNote: "Rate rows are yield/price proxies, not CME Fed Funds futures pricing.",
		Rows:         []RatePathRow{},
	}
	for _, asset := range ratePathAssets {
		row := RatePathRow{ID: asset.ID, Label: asset.Label, Kind: asset.Kind, Source: out.Source}
		q, ok := quotes[asset.ID]
		if !ok {
			row.Missing = true
			row.Note = "quote missing"
			out.Rows = append(out.Rows, row)
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: quote missing", asset.ID))
			continue
		}
		row.Label = nonEmpty(q.Label, asset.Label)
		row.Basis = string(q.Basis)
		row.AsOf = q.AsOf.UTC()
		row.Current = q.Price
		row.PreEvent = preEventBaseline(q, bars[asset.ID])
		if row.PreEvent <= 0 {
			row.Missing = true
			row.Note = "baseline unavailable"
			out.Rows = append(out.Rows, row)
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: baseline unavailable", asset.ID))
			continue
		}
		row.Change = row.Current - row.PreEvent
		row.ChangePct = row.Change / row.PreEvent * 100
		out.Rows = append(out.Rows, row)
	}
	return out
}

func preEventBaseline(q marketdata.Quote, bars []model.OHLCV) float64 {
	if first := earliestPositiveClose(bars); first > 0 {
		return first
	}
	if q.Change != 0 && q.Price-q.Change > 0 {
		return q.Price - q.Change
	}
	if q.ChangePct != 0 {
		denom := 1 + q.ChangePct/100
		if denom > 0 {
			return q.Price / denom
		}
	}
	return q.Price
}

func earliestPositiveClose(bars []model.OHLCV) float64 {
	var (
		found bool
		ts    time.Time
		close float64
	)
	for _, bar := range bars {
		if bar.Close <= 0 {
			continue
		}
		if !found || bar.Timestamp.Before(ts) {
			found = true
			ts = bar.Timestamp
			close = bar.Close
		}
	}
	return close
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
