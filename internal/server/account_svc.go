package server

import (
	"context"
	"math"
	"sync"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
)

// accountEnrichConcurrency bounds the number of concurrent mark-price fetches
// when enriching positions, matching the IB-pacing-friendly bound used by the
// option-chain OI fetcher.
const accountEnrichConcurrency = 5

// AccountService reads account holdings and executions from the broker and
// enriches positions with mark-to-market P&L. It is the dedicated home for
// account logic, kept separate from MarketDataService.
type AccountService struct {
	broker broker.Broker
	market *MarketDataService // reused for stock mark prices (SQLite-cached)
}

// NewAccountService creates an AccountService. `b` supplies positions,
// executions and option marks; `m` supplies stock marks via GetQuote.
func NewAccountService(b broker.Broker, m *MarketDataService) *AccountService {
	return &AccountService{broker: b, market: m}
}

// GetPositions fetches all holdings and fills each one's mark-to-market P&L.
// Returns broker.ErrAccountNotSupported when the broker can't read accounts
// (e.g. running on the yfinance fallback).
func (s *AccountService) GetPositions(ctx context.Context) ([]model.Position, error) {
	ar, ok := s.broker.(broker.AccountReader)
	if !ok {
		return nil, broker.ErrAccountNotSupported
	}
	positions, err := ar.GetPositions(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.enrich(ctx, positions); err != nil {
		// A cancelled enrich leaves some positions with zero marks, which
		// downstream (portfolio concentration) would misread as an
		// OPRA-subscription gap. Surface the cancellation instead of
		// returning a misleading partial snapshot. See issue #50.
		return positions, err
	}
	return positions, nil
}

// GetExecutions fetches the last-7-days execution history. Returns
// broker.ErrAccountNotSupported when the broker can't read accounts.
func (s *AccountService) GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error) {
	ar, ok := s.broker.(broker.AccountReader)
	if !ok {
		return nil, broker.ErrAccountNotSupported
	}
	return ar.GetExecutions(ctx, filter)
}

// enrich fills LastPrice / MarketValue / UnrealizedPnL / UnrealizedPnLPct on
// each position in place, bounded to accountEnrichConcurrency concurrent mark
// fetches. Any individual mark failure is non-fatal: that position's P&L
// fields are left zero and the others are unaffected.
//
// Returns ctx.Err() if the context was cancelled while enriching (nil
// otherwise). A cancelled enrich produces a partially-marked slice that the
// caller must not present as complete — see GetPositions. See issue #50.
func (s *AccountService) enrich(ctx context.Context, positions []model.Position) error {
	oq, hasOptionQuoter := s.broker.(broker.OptionQuoter)

	sem := make(chan struct{}, accountEnrichConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex // guards positions[idx] mutation

	for i := range positions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			// Each goroutine owns a unique idx, so reading positions[idx]
			// here is safe without the mutex; the mutex guards only the
			// final write-back below.
			p := positions[idx]

			var mark float64
			if p.IsOption() {
				if !hasOptionQuoter {
					return
				}
				m, err := oq.GetOptionQuote(ctx, p.Symbol, p.Expiration, p.Right, p.Strike)
				if err != nil {
					return // graceful degradation: leave P&L zero
				}
				mark = m
			} else {
				q, err := s.market.GetQuote(ctx, p.Symbol)
				if err != nil || q == nil || q.Last <= 0 {
					return // graceful degradation: leave P&L zero
				}
				mark = q.Last
			}

			var marketValue float64
			if p.IsOption() {
				marketValue = p.Quantity * mark * p.Multiplier
			} else {
				marketValue = p.Quantity * mark
			}
			costBasis := p.Quantity * p.AvgCost
			unrealized := marketValue - costBasis
			var pct float64
			if costBasis != 0 {
				pct = unrealized / math.Abs(costBasis) * 100
			}

			mu.Lock()
			defer mu.Unlock()
			positions[idx].LastPrice = mark
			positions[idx].MarketValue = marketValue
			positions[idx].UnrealizedPnL = unrealized
			positions[idx].UnrealizedPnLPct = pct
		}(i)
	}
	wg.Wait()
	return ctx.Err()
}
