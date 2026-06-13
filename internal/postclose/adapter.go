package postclose

import (
	"context"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

type marketAdapter struct {
	pythonBin string
}

func NewMarketAdapter(pythonBin string) MarketSource {
	return &marketAdapter{pythonBin: pythonBin}
}

func NewDefaultService(pythonBin string) (*Service, error) {
	sm, source, err := portfolio.ResolveSectorMap("")
	if err != nil {
		return nil, err
	}
	return NewService(NewMarketAdapter(pythonBin), sm, source), nil
}

func (a *marketAdapter) Earnings(ctx context.Context, symbols []string, limit int) (map[string][]marketdata.EarningsEvent, error) {
	return marketdata.RawEarnings(ctx, a.pythonBin, symbols, limit)
}

func (a *marketAdapter) PostcloseBars(ctx context.Context, symbols []string, days int) (map[string][]model.OHLCV, error) {
	period := "5d"
	if days > 5 {
		period = "1mo"
	}
	return marketdata.RawBatchBars(ctx, a.pythonBin, symbols, "5m", period)
}
