package ibkr

import (
	"fmt"
	"sync"

	"github.com/scmhub/ibapi"
)

// pendingQuote accumulates tick data for a snapshot market data request.
type pendingQuote struct {
	bid    float64
	ask    float64
	last   float64
	close  float64
	volume float64
	done   chan struct{}
	once   sync.Once
}

// pendingBars accumulates historical bars for a single request.
type pendingBars struct {
	bars []ibapi.Bar
	done chan struct{}
	err  error
}

// pendingOptParams holds option chain parameters returned by IB.
// IB may call SecurityDefinitionOptionParameter multiple times for different
// exchanges (and even multiple times for SMART), so we union all results.
type pendingOptParams struct {
	expSet     map[string]struct{}  // deduplicated expirations
	strikeSet  map[float64]struct{} // deduplicated strikes
	expirations []string
	strikes     []float64
	multiplier  string
	done        chan struct{}
	err         error
}

// pendingContractDetails holds the conID returned by ReqContractDetails.
type pendingContractDetails struct {
	conID int64
	done  chan struct{}
}

// IbWrapper implements ibapi.EWrapper and routes callbacks to waiting goroutines
// via per-request channels.  It embeds ibapi.Wrapper for all unimplemented methods.
type IbWrapper struct {
	ibapi.Wrapper // provides no-op implementations for all EWrapper methods

	mu              sync.Mutex
	quotes          map[int64]*pendingQuote
	bars            map[int64]*pendingBars
	optParams       map[int64]*pendingOptParams
	contractDetails map[int64]*pendingContractDetails
	errors          map[int64]chan error
	nextValidID     chan int64
}

func newIbWrapper() *IbWrapper {
	return &IbWrapper{
		quotes:          make(map[int64]*pendingQuote),
		bars:            make(map[int64]*pendingBars),
		optParams:       make(map[int64]*pendingOptParams),
		contractDetails: make(map[int64]*pendingContractDetails),
		errors:          make(map[int64]chan error),
		nextValidID:     make(chan int64, 1),
	}
}

// --- helpers to register pending requests ---------------------------------

func (w *IbWrapper) registerQuote(reqID int64) *pendingQuote {
	pq := &pendingQuote{done: make(chan struct{})}
	w.mu.Lock()
	w.quotes[reqID] = pq
	w.mu.Unlock()
	return pq
}

func (w *IbWrapper) registerBars(reqID int64) *pendingBars {
	pb := &pendingBars{done: make(chan struct{})}
	w.mu.Lock()
	w.bars[reqID] = pb
	w.mu.Unlock()
	return pb
}

func (w *IbWrapper) registerOptParams(reqID int64) *pendingOptParams {
	pp := &pendingOptParams{
		done:      make(chan struct{}),
		expSet:    make(map[string]struct{}),
		strikeSet: make(map[float64]struct{}),
	}
	w.mu.Lock()
	w.optParams[reqID] = pp
	w.mu.Unlock()
	return pp
}

func (w *IbWrapper) registerContractDetails(reqID int64) *pendingContractDetails {
	pcd := &pendingContractDetails{done: make(chan struct{})}
	w.mu.Lock()
	w.contractDetails[reqID] = pcd
	w.mu.Unlock()
	return pcd
}

func (w *IbWrapper) registerError(reqID int64) chan error {
	ch := make(chan error, 1)
	w.mu.Lock()
	w.errors[reqID] = ch
	w.mu.Unlock()
	return ch
}

func (w *IbWrapper) unregister(reqID int64) {
	w.mu.Lock()
	delete(w.quotes, reqID)
	delete(w.bars, reqID)
	delete(w.optParams, reqID)
	delete(w.contractDetails, reqID)
	delete(w.errors, reqID)
	w.mu.Unlock()
}

// --- EWrapper callbacks ---------------------------------------------------

// NextValidID is called right after Connect; we capture it for initial reqID seeding.
func (w *IbWrapper) NextValidID(reqID int64) {
	select {
	case w.nextValidID <- reqID:
	default:
	}
}

// TickPrice is called for BID, ASK, LAST, CLOSE price ticks during a snapshot.
func (w *IbWrapper) TickPrice(reqID ibapi.TickerID, tickType ibapi.TickType, price float64, _ ibapi.TickAttrib) {
	w.mu.Lock()
	pq, ok := w.quotes[reqID]
	w.mu.Unlock()
	if !ok || price <= 0 {
		return
	}
	switch tickType {
	case ibapi.BID:
		pq.bid = price
	case ibapi.ASK:
		pq.ask = price
	case ibapi.LAST:
		pq.last = price
	case ibapi.CLOSE:
		pq.close = price
	}
}

// TickSize is called for VOLUME tick.
func (w *IbWrapper) TickSize(reqID ibapi.TickerID, tickType ibapi.TickType, size ibapi.Decimal) {
	w.mu.Lock()
	pq, ok := w.quotes[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	if tickType == ibapi.VOLUME {
		pq.volume = size.Float()
	}
}

// TickSnapshotEnd signals that all snapshot ticks for this request have arrived.
func (w *IbWrapper) TickSnapshotEnd(reqID int64) {
	w.mu.Lock()
	pq, ok := w.quotes[reqID]
	w.mu.Unlock()
	if ok {
		pq.once.Do(func() { close(pq.done) })
	}
}

// HistoricalData delivers one bar at a time.
func (w *IbWrapper) HistoricalData(reqID int64, bar *ibapi.Bar) {
	w.mu.Lock()
	pb, ok := w.bars[reqID]
	w.mu.Unlock()
	if ok {
		pb.bars = append(pb.bars, *bar)
	}
}

// HistoricalDataEnd signals all bars have been delivered.
func (w *IbWrapper) HistoricalDataEnd(reqID int64, _, _ string) {
	w.mu.Lock()
	pb, ok := w.bars[reqID]
	w.mu.Unlock()
	if ok {
		close(pb.done)
	}
}

// ContractDetails is called for each matching contract from ReqContractDetails.
// We capture the first result's ConID.
func (w *IbWrapper) ContractDetails(reqID int64, cd *ibapi.ContractDetails) {
	w.mu.Lock()
	pcd, ok := w.contractDetails[reqID]
	w.mu.Unlock()
	if ok && cd != nil && pcd.conID == 0 {
		pcd.conID = cd.Contract.ConID
	}
}

// ContractDetailsEnd signals all matching contracts have been delivered.
func (w *IbWrapper) ContractDetailsEnd(reqID int64) {
	w.mu.Lock()
	pcd, ok := w.contractDetails[reqID]
	w.mu.Unlock()
	if ok {
		close(pcd.done)
	}
}

// SecurityDefinitionOptionParameter delivers one exchange's option params.
// We only capture the first SMART/CBOE result.
func (w *IbWrapper) SecurityDefinitionOptionParameter(
	reqID int64, exchange string, _ int64, _ string,
	multiplier string, expirations []string, strikes []float64,
) {
	w.mu.Lock()
	pp, ok := w.optParams[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	// Union expirations and strikes across all exchange callbacks.
	// IB sends one callback per exchange (CBOE, ISE, SMART, …), sometimes
	// multiple per exchange with different subsets — we accumulate the union.
	w.mu.Lock()
	for _, e := range expirations {
		pp.expSet[e] = struct{}{}
	}
	for _, s := range strikes {
		pp.strikeSet[s] = struct{}{}
	}
	if pp.multiplier == "" {
		pp.multiplier = multiplier
	}
	w.mu.Unlock()
}

// SecurityDefinitionOptionParameterEnd signals all exchanges have been returned.
func (w *IbWrapper) SecurityDefinitionOptionParameterEnd(reqID int64) {
	w.mu.Lock()
	pp, ok := w.optParams[reqID]
	if ok {
		// Materialise deduplicated sets into sorted slices.
		pp.expirations = make([]string, 0, len(pp.expSet))
		for e := range pp.expSet {
			pp.expirations = append(pp.expirations, e)
		}
		pp.strikes = make([]float64, 0, len(pp.strikeSet))
		for s := range pp.strikeSet {
			pp.strikes = append(pp.strikes, s)
		}
	}
	w.mu.Unlock()
	if ok {
		close(pp.done)
	}
}

// Error routes IB errors to the corresponding pending request's error channel,
// or logs them as informational messages (errCode < 2000 are often warnings).
func (w *IbWrapper) Error(reqID ibapi.TickerID, _ int64, errCode int64, errString string, _ string) {
	// Codes < 2000 are informational (e.g., "Market data farm connection").
	if errCode < 2000 {
		return
	}
	// 10167 = "Not subscribed; displaying delayed data" — IB will still deliver
	// the delayed ticks, so treat this as informational and let the data arrive.
	if errCode == 10167 {
		return
	}
	w.mu.Lock()
	ch, ok := w.errors[reqID]
	w.mu.Unlock()
	if ok {
		select {
		case ch <- fmt.Errorf("IB error %d: %s", errCode, errString):
		default:
		}
		// Also close the corresponding data channel so callers unblock.
		w.mu.Lock()
		if pq, has := w.quotes[reqID]; has {
			pq.once.Do(func() { close(pq.done) })
		}
		if pb, has := w.bars[reqID]; has {
			select {
			case <-pb.done:
			default:
				close(pb.done)
			}
		}
		if pp, has := w.optParams[reqID]; has {
			select {
			case <-pp.done:
			default:
				close(pp.done)
			}
		}
		w.mu.Unlock()
	}
}
