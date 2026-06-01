package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/journal"
	"github.com/IS908/optix/pkg/model"
)

// GapWarningHours is the staleness threshold for the sync-gap warning shown
// in the CLI and web UI. 6 days = 144 hours; chosen so we warn one full day
// before IBKR's 7-day execution-history window slips past the last sync.
const GapWarningHours = 6 * 24

// accountReader is the subset of broker.AccountReader the JournalService uses.
// Local interface so tests can substitute a fake without pulling in broker.
type accountReader interface {
	GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error)
}

// JournalService orchestrates the trade-journal sync + read paths.
// A nil broker is permitted; only SyncExecutions requires one.
type JournalService struct {
	broker accountReader
	store  *sqlite.Store
}

func NewJournalService(b accountReader, store *sqlite.Store) *JournalService {
	return &JournalService{broker: b, store: store}
}

// SyncExecutions fetches the last ~7 days from the broker and writes any new
// rows to SQLite. Idempotent. On broker error, the error is recorded into
// sync_state.last_error AND returned to the caller.
func (s *JournalService) SyncExecutions(ctx context.Context) (int, error) {
	if s.broker == nil {
		return 0, fmt.Errorf("no broker configured")
	}
	execs, err := s.broker.GetExecutions(ctx, model.ExecutionFilter{})
	if err != nil {
		cur, _ := s.store.GetSyncState(ctx)
		_ = s.store.UpdateSyncState(ctx, sqlite.SyncState{
			LastSyncAt:    cur.LastSyncAt,
			LastSyncCount: cur.LastSyncCount,
			LastError:     err.Error(),
		})
		return 0, fmt.Errorf("get executions: %w", err)
	}
	n, err := s.store.UpsertExecutions(ctx, execs)
	if err != nil {
		return 0, fmt.Errorf("upsert: %w", err)
	}
	if uerr := s.store.UpdateSyncState(ctx, sqlite.SyncState{
		LastSyncAt:    time.Now().UTC(),
		LastSyncCount: n,
		LastError:     "",
	}); uerr != nil {
		return n, fmt.Errorf("update sync state: %w", uerr)
	}
	return n, nil
}

func (s *JournalService) ListExecutions(ctx context.Context, f sqlite.JournalFilter) ([]model.Execution, error) {
	return s.store.ListExecutions(ctx, f)
}

// RoundTripFilter narrows ListRoundTrips. Status "" means all.
type RoundTripFilter struct {
	Symbol string
	Status string // open | closed | expired
	Since  time.Time
	Until  time.Time
}

func (s *JournalService) ListRoundTrips(ctx context.Context, f RoundTripFilter) ([]model.RoundTrip, error) {
	// Load executions WITHOUT time filtering — the matcher needs the full
	// open/close history to pair fills correctly. Time-window narrowing
	// happens below, anchored on trip CloseTime instead of execution time.
	// This fixes the "open before window, close inside window" miscount
	// (see issue #29 follow-up).
	execs, err := s.store.ListExecutions(ctx, sqlite.JournalFilter{Symbol: f.Symbol})
	if err != nil {
		return nil, err
	}
	// Matcher expects ASC; store returns DESC.
	sort.SliceStable(execs, func(i, j int) bool { return execs[i].Time.Before(execs[j].Time) })
	trips := journal.MatchRoundTrips(execs, time.Now().UTC())

	// Filter by CloseTime window when set. Boundary contract: Since is
	// inclusive lower bound, Until is inclusive upper bound (callers — CLI
	// and webui — already adjust YYYY-MM-DD inputs to EOD via
	// `t.Add(24h - 1s)`; tests must mirror that). Status=open trips have a
	// zero CloseTime and are dropped from windowed queries; callers wanting
	// open trips should query without Since/Until (and optionally pass
	// Status="open").
	hasWindow := !f.Since.IsZero() || !f.Until.IsZero()
	if hasWindow {
		filtered := trips[:0]
		for _, t := range trips {
			if t.CloseTime.IsZero() {
				continue // open trip has no close anchor → skip in windowed query
			}
			if !f.Since.IsZero() && t.CloseTime.Before(f.Since) {
				continue
			}
			if !f.Until.IsZero() && t.CloseTime.After(f.Until) {
				continue
			}
			filtered = append(filtered, t)
		}
		trips = filtered
	}

	if f.Status != "" {
		wanted := strings.ToLower(f.Status)
		filtered := trips[:0]
		for _, t := range trips {
			if t.Status == wanted {
				filtered = append(filtered, t)
			}
		}
		trips = filtered
	}
	sort.SliceStable(trips, func(i, j int) bool { return trips[i].OpenTime.After(trips[j].OpenTime) })
	return trips, nil
}

func (s *JournalService) Review(ctx context.Context, since, until time.Time) (model.ReviewSummary, error) {
	trips, err := s.ListRoundTrips(ctx, RoundTripFilter{Since: since, Until: until})
	if err != nil {
		return model.ReviewSummary{}, err
	}
	sum := model.ReviewSummary{Since: since, Until: until, TotalRoundTrips: len(trips)}
	// TotalExecutions counts raw fills with execution time in [since, until],
	// intentionally a different metric than TotalRoundTrips. Trips are
	// windowed by inclusive CloseTime ∈ [since, until] (see ListRoundTrips —
	// callers pre-adjust YYYY-MM-DD to EOD), executions by raw time inclusive
	// — they answer different questions: "how many trades happened this
	// period" vs "how many positions realized P&L".
	execs, err := s.store.ListExecutions(ctx, sqlite.JournalFilter{Since: since, Until: until})
	if err != nil {
		return model.ReviewSummary{}, err
	}
	sum.TotalExecutions = len(execs)

	// Keyed by (Symbol, SecType, Currency) so AAPL stock and AAPL options
	// produce separate SymbolStats rows. Review P&L is USD-only for now.
	bySymKey := func(t model.RoundTrip) string {
		return t.Symbol + "|" + t.SecType + "|" + model.NormalizeCurrency(t.Currency)
	}
	bySym := make(map[string]*model.SymbolStats)
	bySymWins := make(map[string]int)
	bySymClosed := make(map[string]int) // denominator for per-symbol WinRate
	var totalHold float64

	for _, t := range trips {
		if model.NormalizeCurrency(t.Currency) != "USD" {
			sum.ExcludedNonUSDRoundTrips++
			continue
		}
		switch t.Status {
		case "open":
			sum.OpenRoundTrips++
		case "expired":
			sum.ExpiredRoundTrips++
		case "closed":
			sum.ClosedRoundTrips++
		}
		if t.Status == "closed" || t.Status == "expired" {
			sum.TotalRealizedPnL += t.RealizedPnL
			totalHold += t.HoldingDays
			if t.RealizedPnL > 0 {
				sum.WinCount++
			} else if t.RealizedPnL < 0 {
				sum.LossCount++
			}
		}
		k := bySymKey(t)
		ss, ok := bySym[k]
		if !ok {
			ss = &model.SymbolStats{
				Symbol:   t.Symbol,
				SecType:  t.SecType,
				Currency: model.NormalizeCurrency(t.Currency),
			}
			bySym[k] = ss
		}
		ss.RoundTripCount++
		ss.RealizedPnL += t.RealizedPnL
		if t.Status == "closed" || t.Status == "expired" {
			bySymClosed[k]++
			if t.RealizedPnL > 0 {
				bySymWins[k]++
			}
		}
	}

	closed := sum.ClosedRoundTrips + sum.ExpiredRoundTrips
	if closed > 0 {
		sum.WinRate = float64(sum.WinCount) / float64(closed)
		sum.AvgRealizedPnL = sum.TotalRealizedPnL / float64(closed)
		sum.AvgHoldingDays = totalHold / float64(closed)
	}
	for k, ss := range bySym {
		// Per-symbol WinRate uses closed+expired as denominator, matching the
		// aggregate semantics. Open trips don't count for or against win rate.
		if c := bySymClosed[k]; c > 0 {
			ss.WinRate = float64(bySymWins[k]) / float64(c)
		}
		sum.BySymbol = append(sum.BySymbol, *ss)
	}
	sort.Slice(sum.BySymbol, func(i, j int) bool {
		return sum.BySymbol[i].RealizedPnL > sum.BySymbol[j].RealizedPnL
	})
	return sum, nil
}

func (s *JournalService) SyncState(ctx context.Context) (sqlite.SyncState, error) {
	return s.store.GetSyncState(ctx)
}
