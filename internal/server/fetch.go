package server

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/internal/broker/ibkr"
	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/IS908/optix/pkg/model"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FetchSymbolData fetches quote + historical bars + option chain for a single symbol
// and packs everything into a SingleStockData proto (ready for the analysis engine).
// It is shared by the CLI commands (analyze, dashboard) and the web UI.
func FetchSymbolData(
	ctx context.Context,
	symbol string,
	svc *MarketDataService,
	ibClient *ibkr.Client,
) (*analysisv1.SingleStockData, error) {
	// 1. Current quote
	quote, err := svc.GetQuote(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get quote: %w", err)
	}

	// 2. Historical bars (~1 year daily) via service (caches to ohlcv_bars).
	bars, err := svc.GetHistoricalBars(ctx, symbol, "1 day", 365)
	if err != nil {
		return nil, fmt.Errorf("get historical bars: %w", err)
	}
	if len(bars) < 20 {
		return nil, fmt.Errorf("insufficient historical data: %d bars (need ≥ 20)", len(bars))
	}

	// 3. Option chain — non-fatal (structure-only from IB without live subscription).
	// Fetched via service for consistency; saves snapshot_time to option_quotes.
	chain, chainErr := svc.GetOptionChain(ctx, symbol, "")
	if chainErr != nil {
		chain = &model.OptionChain{Underlying: symbol}
	}

	return &analysisv1.SingleStockData{
		Symbol:         symbol,
		Quote:          modelQuoteToProto(quote),
		HistoricalBars: modelBarsToProto(bars),
		OptionChain:    modelChainToProto(chain),
	}, nil
}

// ── Model → Proto conversions ─────────────────────────────────────────────────

func modelBarsToProto(bars []model.OHLCV) []*marketdatav1.OHLCV {
	out := make([]*marketdatav1.OHLCV, 0, len(bars))
	for _, b := range bars {
		out = append(out, &marketdatav1.OHLCV{
			Timestamp: timestamppb.New(b.Timestamp),
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
		})
	}
	return out
}

func modelChainToProto(chain *model.OptionChain) []*marketdatav1.OptionChainExpiry {
	if chain == nil {
		return nil
	}
	out := make([]*marketdatav1.OptionChainExpiry, 0, len(chain.Expirations))
	now := time.Now()
	for _, exp := range chain.Expirations {
		dte := 0
		if t, err := time.ParseInLocation("20060102", exp.Expiration, time.Local); err == nil {
			dte = int(t.Sub(now).Hours() / 24)
			if dte < 0 {
				dte = 0
			}
		}
		displayExp := formatIBExpiry(exp.Expiration)

		calls := make([]*marketdatav1.OptionQuote, 0, len(exp.Calls))
		for _, c := range exp.Calls {
			calls = append(calls, &marketdatav1.OptionQuote{
				Underlying:        chain.Underlying,
				Expiration:        displayExp,
				Strike:            c.Strike,
				OptionType:        marketdatav1.OptionType_OPTION_TYPE_CALL,
				OpenInterest:      c.OpenInterest,
				ImpliedVolatility: c.ImpliedVolatility,
			})
		}
		puts := make([]*marketdatav1.OptionQuote, 0, len(exp.Puts))
		for _, p := range exp.Puts {
			puts = append(puts, &marketdatav1.OptionQuote{
				Underlying:        chain.Underlying,
				Expiration:        displayExp,
				Strike:            p.Strike,
				OptionType:        marketdatav1.OptionType_OPTION_TYPE_PUT,
				OpenInterest:      p.OpenInterest,
				ImpliedVolatility: p.ImpliedVolatility,
			})
		}
		out = append(out, &marketdatav1.OptionChainExpiry{
			Expiration:   displayExp,
			DaysToExpiry: int32(dte),
			Calls:        calls,
			Puts:         puts,
		})
	}
	return out
}

func modelQuoteToProto(q *model.StockQuote) *marketdatav1.StockQuote {
	if q == nil {
		return nil
	}
	return &marketdatav1.StockQuote{
		Symbol:    q.Symbol,
		Last:      q.Last,
		Bid:       q.Bid,
		Ask:       q.Ask,
		Volume:    q.Volume,
		Change:    q.Change,
		ChangePct: q.ChangePct,
		High:      q.High,
		Low:       q.Low,
		Open:      q.Open,
		Close:     q.Close,
		High_52W:  q.High52W,
		Low_52W:   q.Low52W,
		AvgVolume: q.AvgVolume,
		Timestamp: timestamppb.New(q.Timestamp),
	}
}

// formatIBExpiry converts "20250315" → "2025-03-15".
func formatIBExpiry(s string) string {
	if len(s) == 8 {
		return s[:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	return s
}
