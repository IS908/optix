package eventintel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

type MarketSource interface {
	Quotes(ctx context.Context, ids []string) (map[string]marketdata.Quote, error)
	Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
	Statements(ctx context.Context) (StatementFixture, StatementFixture, error)
	EventDates(ctx context.Context) ([]EventDate, error)
}

type YFinanceAdapter struct {
	src *marketdata.YFinanceSource
	fed *FedSource
	bls *BLSSource
}

func NewYFinanceAdapter(pythonBin string) *YFinanceAdapter {
	return &YFinanceAdapter{src: marketdata.NewYFinanceSource(pythonBin), fed: NewFedSource(""), bls: NewBLSSource("")}
}

func (a *YFinanceAdapter) Quotes(ctx context.Context, ids []string) (map[string]marketdata.Quote, error) {
	return a.src.BatchQuotes(ctx, refsForIDs(ids))
}

func (a *YFinanceAdapter) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	return a.src.BatchBars(ctx, refsForIDs(ids), interval, lookback)
}

func (a *YFinanceAdapter) Statements(ctx context.Context) (StatementFixture, StatementFixture, error) {
	if a.fed != nil {
		prior, current, err := a.fed.Statements(ctx)
		if err == nil {
			return prior, current, nil
		}
		fallbackPrior, fallbackCurrent := defaultStatementFixtures()
		return fallbackPrior, fallbackCurrent, fmt.Errorf("fed.gov statements unavailable, using local fixtures: %w", err)
	}
	prior, current := defaultStatementFixtures()
	return prior, current, nil
}

func (a *YFinanceAdapter) EventDates(ctx context.Context) ([]EventDate, error) {
	events := defaultEventDates()
	var warnings []error
	if a.fed != nil {
		fedEvents, err := a.fed.EventDates(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("fed.gov calendar unavailable, using local FOMC calendar: %w", err))
		} else {
			events = mergeEventDates(events, fedEvents)
		}
	}
	if a.bls != nil {
		cpiEvents, err := a.bls.EventDates(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("bls.gov CPI calendar unavailable, using local CPI calendar: %w", err))
		} else {
			events = mergeEventDates(events, cpiEvents)
		}
	}
	return events, errors.Join(warnings...)
}

func refsForIDs(ids []string) []marketdata.AssetRef {
	classes := assetClasses()
	refs := make([]marketdata.AssetRef, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		class, ok := classes[id]
		if !ok {
			continue
		}
		refs = append(refs, marketdata.AssetRef{ID: id, Class: class})
		seen[id] = struct{}{}
	}
	return refs
}

func assetClasses() map[string]marketdata.AssetClass {
	out := map[string]marketdata.AssetClass{}
	for _, asset := range ratePathAssets {
		out[asset.ID] = asset.Class
	}
	for _, asset := range patternAssets {
		out[asset.ID] = asset.Class
	}
	for _, asset := range sensitivityAssets {
		if asset.Class != "" {
			out[asset.ID] = asset.Class
		}
	}
	return out
}

func defaultStatementFixtures() (StatementFixture, StatementFixture) {
	prior := StatementFixture{
		Title:       "FOMC prior statement fixture",
		Source:      "local_statement_fixture",
		PublishedAt: time.Date(2026, 3, 18, 18, 0, 0, 0, time.UTC),
		Text: "Recent indicators suggest that economic activity has continued to expand at a solid pace. " +
			"Inflation has eased from its highs but remains somewhat elevated. " +
			"The Committee will continue reducing its holdings of Treasury securities and agency debt.",
	}
	current := StatementFixture{
		Title:       "FOMC current statement fixture",
		Source:      "local_statement_fixture",
		PublishedAt: time.Date(2026, 4, 29, 18, 0, 0, 0, time.UTC),
		Text: "Recent indicators suggest that economic activity has continued to expand at a solid pace. " +
			"Inflation remains elevated. " +
			"The Committee judges that risks to achieving its employment and inflation goals have moved into better balance.",
	}
	return prior, current
}

func defaultEventDates() []EventDate {
	return []EventDate{
		{Date: eventDateUTC(2025, 1, 29), Kind: "FOMC", Label: "2025 Jan FOMC"},
		{Date: eventDateUTC(2025, 3, 19), Kind: "FOMC", Label: "2025 Mar FOMC"},
		{Date: eventDateUTC(2025, 5, 7), Kind: "FOMC", Label: "2025 May FOMC"},
		{Date: eventDateUTC(2025, 6, 18), Kind: "FOMC", Label: "2025 Jun FOMC"},
		{Date: eventDateUTC(2025, 7, 30), Kind: "FOMC", Label: "2025 Jul FOMC"},
		{Date: eventDateUTC(2025, 9, 17), Kind: "FOMC", Label: "2025 Sep FOMC"},
		{Date: eventDateUTC(2025, 10, 29), Kind: "FOMC", Label: "2025 Oct FOMC"},
		{Date: eventDateUTC(2025, 12, 10), Kind: "FOMC", Label: "2025 Dec FOMC"},
		{Date: eventDateUTC(2026, 1, 28), Kind: "FOMC", Label: "2026 Jan FOMC"},
		{Date: eventDateUTC(2026, 2, 13), Kind: "CPI", Label: "2026 Jan CPI"},
		{Date: eventDateUTC(2026, 3, 11), Kind: "CPI", Label: "2026 Feb CPI"},
		{Date: eventDateUTC(2026, 3, 18), Kind: "FOMC", Label: "2026 Mar FOMC"},
		{Date: eventDateUTC(2026, 4, 10), Kind: "CPI", Label: "2026 Mar CPI"},
		{Date: eventDateUTC(2026, 4, 29), Kind: "FOMC", Label: "2026 Apr FOMC"},
		{Date: eventDateUTC(2026, 5, 12), Kind: "CPI", Label: "2026 Apr CPI"},
		{Date: eventDateUTC(2026, 6, 10), Kind: "CPI", Label: "2026 May CPI"},
	}
}

func mergeEventDates(base, overrides []EventDate) []EventDate {
	byKey := make(map[string]EventDate, len(base)+len(overrides))
	for _, event := range base {
		byKey[eventDateKey(event)] = event
	}
	for _, event := range overrides {
		byKey[eventDateKey(event)] = event
	}
	out := make([]EventDate, 0, len(byKey))
	for _, event := range byKey {
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date.Equal(out[j].Date) {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Date.Before(out[j].Date)
	})
	return out
}

func eventDateKey(event EventDate) string {
	return event.Kind + ":" + event.Date.UTC().Format("2006-01-02")
}

func eventDateUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
