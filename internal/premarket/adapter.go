package premarket

import (
	"context"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

type marketAdapter struct {
	pythonBin string
	pulse     *marketdata.PulseService
}

func NewMarketAdapter(pythonBin string) MarketSource {
	router := marketdata.NewYFinanceRouter(pythonBin)
	return &marketAdapter{pythonBin: pythonBin, pulse: marketdata.NewPulseService(router, nil)}
}

func (a *marketAdapter) Quotes(ctx context.Context, ids []string) (map[string]marketdata.Quote, error) {
	out := map[string]marketdata.Quote{}
	for _, id := range ids {
		q, err := a.pulse.QuoteByID(ctx, id)
		if err != nil {
			continue
		}
		out[id] = q
	}
	return out, nil
}

func (a *marketAdapter) DailyBars(ctx context.Context, id string, lookback time.Duration) ([]model.OHLCV, error) {
	if id == headlineSymbol {
		bySymbol, err := marketdata.RawBatchBars(ctx, a.pythonBin, []string{"^GSPC"}, "1d", "2y")
		if err != nil {
			return nil, err
		}
		return bySymbol["^GSPC"], nil
	}
	ref, ok := marketdata.LookupAssetRef(id)
	if !ok {
		return nil, nil
	}
	src := marketdata.NewYFinanceSource(a.pythonBin)
	return src.Bars(ctx, ref, "1d", lookback)
}

func (a *marketAdapter) PremarketBars(ctx context.Context, rawSymbols []string, days int) (map[string][]model.OHLCV, error) {
	period := "5d"
	if days > 5 {
		period = "1mo"
	}
	return marketdata.RawBatchBars(ctx, a.pythonBin, rawSymbols, "5m", period)
}

func (a *marketAdapter) PutCallRatio(ctx context.Context, underlying string) (marketdata.PCRatio, error) {
	return marketdata.PutCallRatio(ctx, a.pythonBin, underlying)
}
