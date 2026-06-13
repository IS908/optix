package eventintel

import (
	"context"
	"time"

	"github.com/IS908/optix/pkg/model"
)

const (
	rateBarLookback    = 8 * time.Hour
	eventDailyLookback = 540 * 24 * time.Hour
)

type Service struct {
	src MarketSource
	Now func() time.Time
}

func NewService(src MarketSource) *Service {
	return &Service{src: src}
}

func NewDefaultService(pythonBin string) *Service {
	return NewService(NewYFinanceAdapter(pythonBin))
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Rates(ctx context.Context) (RatePathDTO, error) {
	now := s.now().UTC()
	ids := assetIDs(ratePathAssets)
	if s.src == nil {
		out := BuildRatePath(nil, nil, now)
		out.Warnings = append([]string{"event source unavailable"}, out.Warnings...)
		return out, nil
	}
	quotes, qerr := s.src.Quotes(ctx, ids)
	bars, berr := s.src.Bars(ctx, ids, "5m", rateBarLookback)
	out := BuildRatePath(quotes, bars, now)
	if qerr != nil {
		out.Warnings = append([]string{"rates quotes: " + qerr.Error()}, out.Warnings...)
	}
	if berr != nil {
		out.Warnings = append([]string{"rates bars: " + berr.Error()}, out.Warnings...)
	}
	return out, nil
}

func (s *Service) Diff(ctx context.Context) (StatementDiffDTO, error) {
	now := s.now().UTC()
	if s.src == nil {
		out := emptyDiff(now)
		out.Warnings = append(out.Warnings, "event source unavailable")
		return out, nil
	}
	prior, current, err := s.src.Statements(ctx)
	if err != nil {
		out := emptyDiff(now)
		out.Warnings = append(out.Warnings, "statement fixtures: "+err.Error())
		return out, nil
	}
	out := BuildStatementDiff(prior, current, now)
	if out.Source == "" {
		out.Source = "local_statement_fixture"
	}
	return out, nil
}

func (s *Service) Patterns(ctx context.Context) (EventPatternsDTO, error) {
	now := s.now().UTC()
	events, warnings := s.eventDates(ctx)
	bars := map[string][]model.OHLCV{}
	if s.src == nil {
		warnings = append(warnings, "event source unavailable")
	} else if got, err := s.src.Bars(ctx, assetIDs(patternAssets), "1d", eventDailyLookback); err != nil {
		warnings = append(warnings, "patterns bars: "+err.Error())
	} else {
		bars = got
	}
	out := BuildEventPatterns(bars, events, now)
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func (s *Service) Sensitivity(ctx context.Context) (SensitivityDTO, error) {
	now := s.now().UTC()
	events, warnings := s.eventDates(ctx)
	bars := map[string][]model.OHLCV{}
	if s.src == nil {
		warnings = append(warnings, "event source unavailable")
	} else if got, err := s.src.Bars(ctx, assetIDs(patternAssets), "1d", eventDailyLookback); err != nil {
		warnings = append(warnings, "sensitivity bars: "+err.Error())
	} else {
		bars = got
	}
	windows := map[string][]EventWindowReturn{}
	for _, asset := range patternAssets {
		windows[asset.ID] = ExtractEventWindowReturns(bars[asset.ID], events)
	}
	out := BuildSensitivityMatrix(windows, now)
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func (s *Service) Bundle(ctx context.Context) (BundleDTO, error) {
	rates, _ := s.Rates(ctx)
	diff, _ := s.Diff(ctx)
	patterns, _ := s.Patterns(ctx)
	sensitivity, _ := s.Sensitivity(ctx)
	return BundleDTO{Rates: rates, Diff: diff, Patterns: patterns, Sensitivity: sensitivity}, nil
}

func (s *Service) eventDates(ctx context.Context) ([]EventDate, []string) {
	if s.src == nil {
		return nil, []string{"event calendar unavailable"}
	}
	events, err := s.src.EventDates(ctx)
	if err != nil {
		return nil, []string{"event calendar: " + err.Error()}
	}
	if len(events) == 0 {
		return nil, []string{"event calendar: empty"}
	}
	return events, nil
}

func emptyDiff(asOf time.Time) StatementDiffDTO {
	return StatementDiffDTO{
		AsOf:      asOf.UTC(),
		Source:    "local_statement_fixture",
		Added:     []StatementSentence{},
		Removed:   []StatementSentence{},
		Unchanged: []StatementSentence{},
		Verdict:   "neutral",
	}
}

func assetIDs(assets []eventAsset) []string {
	out := make([]string, 0, len(assets))
	seen := map[string]struct{}{}
	for _, asset := range assets {
		if _, ok := seen[asset.ID]; ok {
			continue
		}
		out = append(out, asset.ID)
		seen[asset.ID] = struct{}{}
	}
	return out
}
