package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

const (
	stressBetaCacheMaxAge  = 7 * 24 * time.Hour
	stressBetaLookbackDays = 120
	stressBetaMinObs       = 20
	stressBetaMaxObs       = 60
)

type stressBetaStore interface {
	GetFreshSymbolBeta(context.Context, string, time.Duration, time.Time) (model.SymbolBeta, bool, error)
	UpsertSymbolBeta(context.Context, model.SymbolBeta) error
}

type stressBarsProvider interface {
	GetHistoricalBars(context.Context, string, string, int) ([]model.OHLCV, error)
}

func buildStressBetaProvider(ctx context.Context, store stressBetaStore, bars stressBarsProvider, groups []portfolio.GreeksGroup, now time.Time, diag io.Writer) portfolio.StressBetaProvider {
	provider := portfolio.StaticStressBetaProvider{}
	if store == nil || bars == nil || len(groups) == 0 {
		return provider
	}

	var spyBars []model.OHLCV
	computeDisabled := false
	for _, group := range groups {
		symbol := strings.ToUpper(strings.TrimSpace(group.Key))
		if symbol == "" {
			continue
		}
		if _, ok := provider[symbol]; ok {
			continue
		}

		if cached, ok, err := store.GetFreshSymbolBeta(ctx, symbol, stressBetaCacheMaxAge, now); err == nil && ok {
			provider[symbol] = stressBetaSourceFromModel(cached, "historical_cache")
			continue
		} else if err != nil && diag != nil {
			fmt.Fprintf(diag, "warn: read beta cache for %s: %v\n", symbol, err)
		}

		if computeDisabled {
			continue
		}
		if len(spyBars) == 0 {
			fetched, err := bars.GetHistoricalBars(ctx, "SPY", "1 day", stressBetaLookbackDays)
			if err != nil {
				if diag != nil {
					fmt.Fprintf(diag, "warn: fetch SPY bars for beta: %v\n", err)
				}
				computeDisabled = true
				continue
			}
			spyBars = fetched
		}
		symbolBars, err := bars.GetHistoricalBars(ctx, symbol, "1 day", stressBetaLookbackDays)
		if err != nil {
			if diag != nil {
				fmt.Fprintf(diag, "warn: fetch %s bars for beta: %v\n", symbol, err)
			}
			continue
		}
		beta, observations, asOf, err := portfolio.ComputeHistoricalBeta(symbolBars, spyBars, stressBetaMinObs, stressBetaMaxObs)
		if err != nil {
			if diag != nil {
				fmt.Fprintf(diag, "warn: compute %s beta: %v\n", symbol, err)
			}
			continue
		}
		if asOf.IsZero() || now.Sub(asOf) > stressBetaCacheMaxAge {
			if diag != nil {
				fmt.Fprintf(diag, "warn: skip %s beta from stale bars as of %s\n", symbol, asOf.Format("2006-01-02"))
			}
			continue
		}
		entry := model.SymbolBeta{
			Symbol: symbol, Beta: beta, Observations: observations, AsOf: asOf, UpdatedAt: now,
		}
		if err := store.UpsertSymbolBeta(ctx, entry); err != nil && diag != nil {
			fmt.Fprintf(diag, "warn: cache %s beta: %v\n", symbol, err)
		}
		provider[symbol] = stressBetaSourceFromModel(entry, "historical_computed")
	}
	return provider
}

func stressBetaSourceFromModel(b model.SymbolBeta, source string) portfolio.StressBetaSource {
	asOf := b.AsOf
	updatedAt := b.UpdatedAt
	return portfolio.StressBetaSource{
		Key:          strings.ToUpper(b.Symbol),
		Beta:         b.Beta,
		Source:       source,
		Observations: b.Observations,
		AsOf:         &asOf,
		UpdatedAt:    &updatedAt,
	}
}
