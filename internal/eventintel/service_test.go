package eventintel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

func TestServiceRatesDegradesOnQuoteFailure(t *testing.T) {
	svc := NewService(&fakeEventSource{quoteErr: errors.New("offline")})
	svc.Now = fixedNow

	dto, err := svc.Rates(context.Background())
	if err != nil {
		t.Fatalf("Rates error = %v", err)
	}
	if dto.Rows == nil {
		t.Fatal("rows should be non-nil on degradation")
	}
	if len(dto.Warnings) == 0 {
		t.Fatal("expected warning on quote failure")
	}
}

func TestServiceBundleKeepsNonNilSlices(t *testing.T) {
	svc := NewService(&fakeEventSource{})
	svc.Now = fixedNow

	bundle, err := svc.Bundle(context.Background())
	if err != nil {
		t.Fatalf("Bundle error = %v", err)
	}
	if bundle.Rates.Rows == nil || bundle.Diff.Added == nil || bundle.Diff.Removed == nil || bundle.Diff.Unchanged == nil ||
		bundle.Patterns.Rows == nil || bundle.Sensitivity.Rows == nil {
		t.Fatalf("bundle contains nil slices: %#v", bundle)
	}
	if len(bundle.Rates.Rows) == 0 || len(bundle.Patterns.Rows) == 0 || len(bundle.Sensitivity.Rows) == 0 {
		t.Fatalf("expected populated event rows: %#v", bundle)
	}
}

func TestServiceKeepsFallbackEventDataWithWarnings(t *testing.T) {
	svc := NewService(&fallbackEventSource{})
	svc.Now = fixedNow

	diff, err := svc.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff error = %v", err)
	}
	if len(diff.Warnings) == 0 || len(diff.Added) == 0 {
		t.Fatalf("diff should include fallback data plus warning: %#v", diff)
	}

	patterns, err := svc.Patterns(context.Background())
	if err != nil {
		t.Fatalf("Patterns error = %v", err)
	}
	if len(patterns.Warnings) == 0 || len(patterns.Rows) == 0 {
		t.Fatalf("patterns should include fallback events plus warning: %#v", patterns)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 13, 14, 0, 0, 0, time.UTC)
}

type fakeEventSource struct {
	quoteErr     error
	barErr       error
	statementErr error
	eventErr     error
}

func (f *fakeEventSource) Quotes(context.Context, []string) (map[string]marketdata.Quote, error) {
	if f.quoteErr != nil {
		return nil, f.quoteErr
	}
	now := fixedNow()
	out := map[string]marketdata.Quote{}
	for _, asset := range append(ratePathAssets, patternAssets...) {
		if _, ok := out[asset.ID]; ok {
			continue
		}
		price := 100.0
		if asset.ID == "US10Y" {
			price = 4.25
		}
		out[asset.ID] = marketdata.Quote{
			Ref:       marketdata.AssetRef{ID: asset.ID, Class: asset.Class},
			Label:     asset.Label,
			Price:     price,
			Change:    1,
			ChangePct: 1,
			AsOf:      now,
			Basis:     marketdata.BasisDelayed,
		}
	}
	return out, nil
}

func (f *fakeEventSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	if f.barErr != nil {
		return nil, f.barErr
	}
	out := map[string][]model.OHLCV{}
	for _, asset := range patternAssets {
		out[asset.ID] = []model.OHLCV{
			dailyBar(2026, 5, 5, 100),
			dailyBar(2026, 5, 6, 101),
			dailyBar(2026, 5, 7, 102),
			dailyBar(2026, 6, 9, 100),
			dailyBar(2026, 6, 10, 99),
			dailyBar(2026, 6, 11, 98),
		}
	}
	return out, nil
}

func (f *fakeEventSource) Statements(context.Context) (StatementFixture, StatementFixture, error) {
	if f.statementErr != nil {
		return StatementFixture{}, StatementFixture{}, f.statementErr
	}
	prior, current := defaultStatementFixtures()
	return prior, current, nil
}

func (f *fakeEventSource) EventDates(context.Context) ([]EventDate, error) {
	if f.eventErr != nil {
		return nil, f.eventErr
	}
	return []EventDate{
		{Date: dateUTC(2026, 5, 6), Kind: "FOMC", Label: "May FOMC"},
		{Date: dateUTC(2026, 6, 10), Kind: "CPI", Label: "Jun CPI"},
	}, nil
}

type fallbackEventSource struct {
	fakeEventSource
}

func (f *fallbackEventSource) Statements(context.Context) (StatementFixture, StatementFixture, error) {
	prior, current := defaultStatementFixtures()
	return prior, current, errors.New("remote statements down")
}

func (f *fallbackEventSource) EventDates(context.Context) ([]EventDate, error) {
	return []EventDate{
		{Date: dateUTC(2026, 5, 6), Kind: "FOMC", Label: "May FOMC"},
		{Date: dateUTC(2026, 6, 10), Kind: "CPI", Label: "Jun CPI"},
	}, errors.New("remote calendar down")
}
