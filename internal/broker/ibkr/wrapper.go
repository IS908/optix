package ibkr

import (
	"fmt"
	"sort"
	"sync"

	"github.com/IS908/optix/pkg/model"
	"github.com/scmhub/ibapi"
)

// pendingQuote accumulates tick data for a snapshot market data request.
type pendingQuote struct {
	mu sync.Mutex

	// right is "C" or "P" for option-contract requests, "" for stock quotes.
	// TickSize uses it to reject OPTION_CALL_*/OPTION_PUT_* ticks for the
	// wrong side — IB can otherwise deliver the opposite contract's volume/OI
	// onto this request (#193 finding 3).
	right string

	bid   float64
	ask   float64
	mark  float64
	last  float64
	close float64

	// markFromTick is true once a genuine MARK_PRICE tick (generic tick 221)
	// has arrived. While false, setOptionComputation is allowed to use
	// MODEL_OPTION's computed price as a mark fallback; once true, the real
	// MARK_PRICE tick wins and MODEL_OPTION must not overwrite it (#193
	// finding 4).
	markFromTick bool

	volume       float64
	openInterest int32

	// impliedVolatility is the resolved IV, recomputed on every
	// TickOptionComputation from the tiered ivModel/ivLast/ivBid/ivAsk
	// fields below — see resolveIVLocked (#193 finding 4).
	impliedVolatility float64
	ivModel           float64
	ivLast            float64
	ivBid             float64
	ivAsk             float64

	greeks         model.Greeks
	marketDataType string
	done           chan struct{}
	once           sync.Once
}

type quoteSnapshot struct {
	bid               float64
	ask               float64
	mark              float64
	last              float64
	close             float64
	volume            float64
	openInterest      int32
	impliedVolatility float64
	greeks            model.Greeks
	marketDataType    string
}

func (pq *pendingQuote) setPrice(tickType ibapi.TickType, price float64) {
	if price <= 0 {
		return
	}

	pq.mu.Lock()
	defer pq.mu.Unlock()

	switch tickType {
	case ibapi.BID, ibapi.DELAYED_BID:
		pq.bid = price
	case ibapi.ASK, ibapi.DELAYED_ASK:
		pq.ask = price
	case ibapi.MARK_PRICE:
		pq.mark = price
		pq.markFromTick = true
	case ibapi.LAST, ibapi.DELAYED_LAST:
		pq.last = price
	case ibapi.CLOSE, ibapi.DELAYED_CLOSE:
		pq.close = price
	}
}

// complete reports whether the key validation fields — both sides of the
// market plus a price and IV/delta — have all arrived. Used by maybeComplete
// to close `done` as soon as the quote is usable rather than always burning
// the full collection window (#193 finding 2: ReqMktData(snapshot=false) is
// streaming, so TickSnapshotEnd never fires and `done` would otherwise only
// close on error or timeout).
func (pq *pendingQuote) complete() bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.bid > 0 && pq.ask > 0 &&
		(pq.mark > 0 || pq.last > 0) &&
		pq.impliedVolatility > 0 &&
		pq.greeks.Delta != 0
}

// maybeComplete closes `done` (idempotently, via pq.once) once complete()
// is satisfied. Safe to call after every tick — when the fields aren't all
// in yet it's a no-op, so the caller falls through to the existing
// timeout/error paths unchanged.
func (pq *pendingQuote) maybeComplete() {
	if pq.complete() {
		pq.once.Do(func() { close(pq.done) })
	}
}

func (pq *pendingQuote) setVolume(volume float64) {
	if volume <= 0 {
		return
	}
	pq.mu.Lock()
	pq.volume = volume
	pq.mu.Unlock()
}

func (pq *pendingQuote) setOpenInterest(openInterest int32) {
	if openInterest <= 0 {
		return
	}
	pq.mu.Lock()
	pq.openInterest = openInterest
	pq.mu.Unlock()
}

func (pq *pendingQuote) setMarketDataType(marketDataType string) {
	if marketDataType == "" {
		return
	}
	pq.mu.Lock()
	pq.marketDataType = marketDataType
	pq.mu.Unlock()
}

func (pq *pendingQuote) setOptionComputation(tickType ibapi.TickType, impliedVol, delta, optPrice, gamma, vega, theta float64) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if impliedVol > 0 && impliedVol != ibapi.UNSET_FLOAT {
		switch tickType {
		case ibapi.MODEL_OPTION, ibapi.DELAYED_MODEL_OPTION:
			pq.ivModel = impliedVol
		case ibapi.LAST_OPTION_COMPUTATION, ibapi.DELAYED_LAST_OPTION:
			pq.ivLast = impliedVol
		case ibapi.BID_OPTION_COMPUTATION, ibapi.DELAYED_BID_OPTION:
			pq.ivBid = impliedVol
		case ibapi.ASK_OPTION_COMPUTATION, ibapi.DELAYED_ASK_OPTION:
			pq.ivAsk = impliedVol
		}
		pq.impliedVolatility = pq.resolveIVLocked()
	}
	if optPrice > 0 && optPrice != ibapi.UNSET_FLOAT {
		pq.greeks.Price = optPrice
		// MODEL_OPTION's computed price is only a mark fallback: it must
		// yield to a genuine MARK_PRICE tick (221) once one has arrived, and
		// must never override one already received (#193 finding 4).
		if (tickType == ibapi.MODEL_OPTION || tickType == ibapi.DELAYED_MODEL_OPTION) && !pq.markFromTick {
			pq.mark = optPrice
		}
	}
	if delta != ibapi.UNSET_FLOAT {
		pq.greeks.Delta = delta
	}
	if gamma != ibapi.UNSET_FLOAT {
		pq.greeks.Gamma = gamma
	}
	if theta != ibapi.UNSET_FLOAT {
		pq.greeks.Theta = theta
	}
	if vega != ibapi.UNSET_FLOAT {
		pq.greeks.Vega = vega
	}
}

// resolveIVLocked picks the implied volatility to report given IB's four
// possible IV sources, in descending reliability order (#193 finding 4):
//
//  1. MODEL_OPTION — IB's live pricing-model IV, continuously recomputed
//     from the current NBBO; the most representative "current" IV.
//  2. LAST_OPTION_COMPUTATION — IV implied by the last executed trade price;
//     can lag the model during fast-moving markets or a stale last trade.
//  3. BID/ASK_OPTION_COMPUTATION midpoint — IV backed out of the quoted
//     bid/ask themselves, with no trade or model computation behind it;
//     least reliable, used only when nothing better has arrived. Uses the
//     average when both sides are present, otherwise whichever side is.
//
// Caller must hold pq.mu.
func (pq *pendingQuote) resolveIVLocked() float64 {
	if pq.ivModel > 0 {
		return pq.ivModel
	}
	if pq.ivLast > 0 {
		return pq.ivLast
	}
	if pq.ivBid > 0 && pq.ivAsk > 0 {
		return (pq.ivBid + pq.ivAsk) / 2
	}
	if pq.ivBid > 0 {
		return pq.ivBid
	}
	return pq.ivAsk
}

func (pq *pendingQuote) snapshot() quoteSnapshot {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return quoteSnapshot{
		bid:               pq.bid,
		ask:               pq.ask,
		mark:              pq.mark,
		last:              pq.last,
		close:             pq.close,
		volume:            pq.volume,
		openInterest:      pq.openInterest,
		impliedVolatility: pq.impliedVolatility,
		greeks:            pq.greeks,
		marketDataType:    pq.marketDataType,
	}
}

// pendingOI accumulates Open Interest data for an option contract market data
// request (streaming, not snapshot, since OI requires generic tick 101).
//
// On reqMktData with genericTickList="101", IB delivers tick type
// OPTION_OPEN_INTEREST (which the scmhub/ibapi library exposes via TickSize
// callback as a generic-tick value, distinct from the 86 used for non-options).
// We close `done` as soon as ANY OI tick arrives so the caller can cancel the
// streaming subscription quickly.
type pendingOI struct {
	mu           sync.Mutex
	openInterest int32
	iv           float64
	done         chan struct{}
	once         sync.Once
}

type oiSnapshot struct {
	openInterest int32
	iv           float64
}

func (po *pendingOI) setOpenInterest(openInterest int32) {
	if openInterest <= 0 {
		return
	}

	po.mu.Lock()
	po.openInterest = openInterest
	po.mu.Unlock()
	po.once.Do(func() { close(po.done) })
}

func (po *pendingOI) setIV(iv float64) {
	if iv <= 0 {
		return
	}

	po.mu.Lock()
	if po.iv == 0 {
		po.iv = iv
	}
	po.mu.Unlock()
}

func (po *pendingOI) snapshot() oiSnapshot {
	po.mu.Lock()
	defer po.mu.Unlock()

	return oiSnapshot{
		openInterest: po.openInterest,
		iv:           po.iv,
	}
}

// pendingDepth accumulates market depth rows from UpdateMktDepth /
// UpdateMktDepthL2 callbacks. IB reports side 0 as ask and side 1 as bid;
// operation 2 deletes a level, while 0/1 insert or update it.
type pendingDepth struct {
	mu     sync.Mutex
	levels map[string]model.MarketDepthLevel
	want   int
	done   chan struct{}
	once   sync.Once
}

func (pd *pendingDepth) update(position, operation, side int64, price float64, size ibapi.Decimal) {
	sideName := depthSide(side)
	if sideName == "" {
		return
	}
	pos := int(position)
	key := fmt.Sprintf("%s:%d", sideName, pos)

	pd.mu.Lock()
	defer pd.mu.Unlock()

	if operation == 2 || price <= 0 || size.Float() <= 0 {
		delete(pd.levels, key)
		return
	}
	pd.levels[key] = model.MarketDepthLevel{
		Side:     sideName,
		Position: pos,
		Price:    price,
		Size:     size.Float(),
	}
	if pd.completeLocked() {
		pd.once.Do(func() { close(pd.done) })
	}
}

func (pd *pendingDepth) snapshot() []model.MarketDepthLevel {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	out := make([]model.MarketDepthLevel, 0, len(pd.levels))
	for _, level := range pd.levels {
		out = append(out, level)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Side != out[j].Side {
			return out[i].Side == "bid"
		}
		return out[i].Position < out[j].Position
	})
	return out
}

func (pd *pendingDepth) completeLocked() bool {
	var bid, ask int
	for _, level := range pd.levels {
		switch level.Side {
		case "bid":
			bid++
		case "ask":
			ask++
		}
	}
	want := pd.want
	if want <= 0 {
		want = 1
	}
	return bid >= want && ask >= want
}

func depthSide(side int64) string {
	switch side {
	case 0:
		return "ask"
	case 1:
		return "bid"
	default:
		return ""
	}
}

func marketDataTypeName(marketDataType int64) string {
	switch ibapi.MarketDataType(marketDataType) {
	case ibapi.REALTIME:
		return "real_time"
	case ibapi.FROZEN:
		return "frozen"
	case ibapi.DELAYED:
		return "delayed"
	case ibapi.DELAYED_FROZEN:
		return "delayed_frozen"
	default:
		return "unknown"
	}
}

// pendingBars accumulates historical bars for a single request.
// `once` guards `done` so the End and Error paths can both attempt to
// close idempotently — see #28.
type pendingBars struct {
	bars []ibapi.Bar
	done chan struct{}
	err  error
	once sync.Once
}

// pendingOptParams holds option chain parameters returned by IB.
// IB may call SecurityDefinitionOptionParameter multiple times for different
// exchanges (and even multiple times for SMART), so we union all results.
// `once` guards `done` against double-close from concurrent End + Error
// (#28).
type pendingOptParams struct {
	expSet      map[string]struct{}  // deduplicated expirations
	strikeSet   map[float64]struct{} // deduplicated strikes
	expirations []string
	strikes     []float64
	multiplier  string
	done        chan struct{}
	err         error
	once        sync.Once
}

// pendingContractDetails holds the conID returned by ReqContractDetails.
// `once` guards `done` against double-close from concurrent End + Error
// (#28).
type pendingContractDetails struct {
	conID int64
	done  chan struct{}
	once  sync.Once
}

// pendingPositions accumulates account positions. ReqPositions is
// account-global (no reqID), so this is a singleton slot on IbWrapper,
// not a map entry.
type pendingPositions struct {
	positions []model.Position
	done      chan struct{}
}

// pendingExecutions accumulates executions for a single ReqExecutions request.
// `once` guards `done` against double-close from concurrent End + Error
// (#28).
type pendingExecutions struct {
	executions []model.Execution
	done       chan struct{}
	once       sync.Once
}

type ibAPIError struct {
	code    int64
	message string
}

func (e *ibAPIError) Error() string {
	return fmt.Sprintf("IB error %d: %s", e.code, e.message)
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
	oi              map[int64]*pendingOI
	depth           map[int64]*pendingDepth
	positions       *pendingPositions // singleton — ReqPositions has no reqID
	executions      map[int64]*pendingExecutions
	errors          map[int64]chan error

	// strictReqIDs marks reqIDs that opted into strict sub-2000 error
	// routing via registerStrictError (see strictErrorCodes / Error).
	// Populated/cleared under mu, same as the maps above.
	strictReqIDs map[int64]bool

	nextValidID   chan int64
	connectErrors chan error

	// disconnectCh is signaled when ibapi detects a TCP connection drop.
	// Buffered 1 so ConnectionClosed() never blocks.
	disconnectCh chan struct{}

	// pingCh receives a signal on each CurrentTime() callback, used by Ping().
	// Buffered 1 so the callback never blocks.
	pingCh chan struct{}
}

func newIbWrapper() *IbWrapper {
	return &IbWrapper{
		quotes:          make(map[int64]*pendingQuote),
		bars:            make(map[int64]*pendingBars),
		optParams:       make(map[int64]*pendingOptParams),
		contractDetails: make(map[int64]*pendingContractDetails),
		oi:              make(map[int64]*pendingOI),
		depth:           make(map[int64]*pendingDepth),
		executions:      make(map[int64]*pendingExecutions),
		errors:          make(map[int64]chan error),
		strictReqIDs:    make(map[int64]bool),
		nextValidID:     make(chan int64, 1),
		connectErrors:   make(chan error, 1),
		disconnectCh:    make(chan struct{}, 1),
		pingCh:          make(chan struct{}, 1),
	}
}

// --- helpers to register pending requests ---------------------------------

// registerQuote registers a pending market-data request. `right` is "C" or
// "P" for option-contract requests (used to filter side-specific volume/OI
// ticks, see TickSize) and "" for stock quotes, which never receive
// OPTION_CALL_*/OPTION_PUT_* ticks in the first place.
func (w *IbWrapper) registerQuote(reqID int64, right string) *pendingQuote {
	pq := &pendingQuote{done: make(chan struct{}), right: right}
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

func (w *IbWrapper) registerOI(reqID int64) *pendingOI {
	po := &pendingOI{done: make(chan struct{})}
	w.mu.Lock()
	w.oi[reqID] = po
	w.mu.Unlock()
	return po
}

func (w *IbWrapper) registerDepth(reqID int64, levels int) *pendingDepth {
	pd := &pendingDepth{
		done:   make(chan struct{}),
		levels: make(map[string]model.MarketDepthLevel),
		want:   levels,
	}
	w.mu.Lock()
	w.depth[reqID] = pd
	w.mu.Unlock()
	return pd
}

func (w *IbWrapper) registerPositions() *pendingPositions {
	pp := &pendingPositions{done: make(chan struct{})}
	w.mu.Lock()
	w.positions = pp
	w.mu.Unlock()
	return pp
}

func (w *IbWrapper) unregisterPositions() {
	w.mu.Lock()
	w.positions = nil
	w.mu.Unlock()
}

func (w *IbWrapper) registerExecutions(reqID int64) *pendingExecutions {
	pe := &pendingExecutions{done: make(chan struct{})}
	w.mu.Lock()
	w.executions[reqID] = pe
	w.mu.Unlock()
	return pe
}

func (w *IbWrapper) registerError(reqID int64) chan error {
	ch := make(chan error, 1)
	w.mu.Lock()
	w.errors[reqID] = ch
	w.mu.Unlock()
	return ch
}

// registerStrictError is like registerError, but also opts reqID into
// strict sub-2000 error routing: Error() will route strictErrorCodes to
// this reqID's errCh instead of dropping them as informational noise (see
// strictErrorCodes / Error).
//
// Used ONLY by GetOptionQuoteDetails (client.go). Fast-failing on a
// contract-validation error like 200 ("no security definition") is that
// command's literal purpose. Every other reqID-keyed request type —
// GetQuote, GetMarketDepth, GetHistoricalBars, GetOptionChain,
// fetchOIForContract, GetExecutions, getConID — must keep the pre-#193
// tolerant behavior (sub-2000 codes silently dropped), since a review
// follow-up found the unconditional whitelist broke GetMarketDepth's
// documented partial-rows-on-timeout contract and GetQuote's
// no-subscription history fallback. Those call sites continue to use plain
// registerError.
func (w *IbWrapper) registerStrictError(reqID int64) chan error {
	ch := make(chan error, 1)
	w.mu.Lock()
	w.errors[reqID] = ch
	w.strictReqIDs[reqID] = true
	w.mu.Unlock()
	return ch
}

func (w *IbWrapper) unregister(reqID int64) {
	w.mu.Lock()
	delete(w.quotes, reqID)
	delete(w.bars, reqID)
	delete(w.optParams, reqID)
	delete(w.contractDetails, reqID)
	delete(w.oi, reqID)
	delete(w.depth, reqID)
	delete(w.executions, reqID)
	delete(w.errors, reqID)
	delete(w.strictReqIDs, reqID)
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

// UpdateMktDepth receives aggregated order-book rows.
func (w *IbWrapper) UpdateMktDepth(reqID ibapi.TickerID, position int64, operation int64, side int64, price float64, size ibapi.Decimal) {
	w.mu.Lock()
	pd, ok := w.depth[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	pd.update(position, operation, side, price, size)
}

// UpdateMktDepthL2 receives exchange/market-maker order-book rows. For M7's
// liquidity state we only need normalized side/position/price/size levels.
func (w *IbWrapper) UpdateMktDepthL2(reqID ibapi.TickerID, position int64, _ string, operation int64, side int64, price float64, size ibapi.Decimal, _ bool) {
	w.mu.Lock()
	pd, ok := w.depth[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	pd.update(position, operation, side, price, size)
}

// TickPrice is called for BID, ASK, LAST, CLOSE price ticks during a snapshot.
func (w *IbWrapper) TickPrice(reqID ibapi.TickerID, tickType ibapi.TickType, price float64, _ ibapi.TickAttrib) {
	w.mu.Lock()
	pq, ok := w.quotes[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	pq.setPrice(tickType, price)
	pq.maybeComplete()
}

// TickSize is called for VOLUME, OPEN_INTEREST and other size-based ticks.
//
// For stock-quote requests (registered via registerQuote), only VOLUME is captured.
// For option-OI requests (registered via registerOI), tick types 27 (call OI), 28
// (put OI), and 86 (generic OPEN_INTEREST for the specific contract) are all
// treated as the contract's open interest. Whichever arrives first closes the
// done channel so the caller can cancel the streaming subscription quickly.
//
// OPTION_CALL_VOLUME/OPEN_INTEREST and OPTION_PUT_VOLUME/OPEN_INTEREST are
// per-underlying aggregates split by side — IB will happily deliver BOTH to
// the same reqID regardless of which right was requested. Accepting whichever
// arrived last (as this used to) can silently attribute a put's volume/OI to
// a call request or vice versa. We only accept the tick matching pq.right;
// tick type 86 is already scoped to the specific contract requested, so it's
// side-safe and always accepted (#193 finding 3).
func (w *IbWrapper) TickSize(reqID ibapi.TickerID, tickType ibapi.TickType, size ibapi.Decimal) {
	w.mu.Lock()
	pq, qOK := w.quotes[reqID]
	po, oiOK := w.oi[reqID]
	w.mu.Unlock()

	if qOK {
		switch tickType {
		case ibapi.VOLUME, ibapi.DELAYED_VOLUME:
			pq.setVolume(size.Float())
			return
		case ibapi.OPTION_CALL_VOLUME:
			if pq.right == "C" {
				pq.setVolume(size.Float())
			}
			return
		case ibapi.OPTION_PUT_VOLUME:
			if pq.right == "P" {
				pq.setVolume(size.Float())
			}
			return
		case ibapi.OPEN_INTEREST, 86:
			pq.setOpenInterest(int32(size.Float()))
			return
		case ibapi.OPTION_CALL_OPEN_INTEREST:
			if pq.right == "C" {
				pq.setOpenInterest(int32(size.Float()))
			}
			return
		case ibapi.OPTION_PUT_OPEN_INTEREST:
			if pq.right == "P" {
				pq.setOpenInterest(int32(size.Float()))
			}
			return
		}
	}

	if oiOK {
		// Option contract OI ticks. IB sends tick type 86 (OPEN_INTEREST) for
		// individual option contracts when generic-tick 101 is requested. Some
		// servers also send 27/28 on the option contract itself. Accept any.
		if tickType == 86 || tickType == 27 || tickType == 28 {
			oi := int32(size.Float())
			po.setOpenInterest(oi)
		}
	}
}

// TickOptionComputation receives implied volatility and Greeks for an option
// contract (delivered automatically when ReqMktData is called on an OPT
// contract). OI enrichment keeps capturing only IV; single-contract quote
// validation also captures IBKR's option computation fields.
func (w *IbWrapper) TickOptionComputation(
	reqID ibapi.TickerID, tickType ibapi.TickType, _ int64,
	impliedVol, delta float64, optPrice float64, _ float64,
	gamma float64, vega float64, theta float64, _ float64,
) {
	w.mu.Lock()
	po, ok := w.oi[reqID]
	pq, qOK := w.quotes[reqID]
	w.mu.Unlock()
	if ok {
		po.setIV(impliedVol)
	}
	if qOK {
		pq.setOptionComputation(tickType, impliedVol, delta, optPrice, gamma, vega, theta)
		pq.maybeComplete()
	}
}

// MarketDataType receives whether the request is live, frozen or delayed.
func (w *IbWrapper) MarketDataType(reqID int64, marketDataType int64) {
	w.mu.Lock()
	pq, ok := w.quotes[reqID]
	w.mu.Unlock()
	if !ok {
		return
	}
	pq.setMarketDataType(marketDataTypeName(marketDataType))
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
		pb.once.Do(func() { close(pb.done) })
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
		pcd.once.Do(func() { close(pcd.done) })
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
		pp.once.Do(func() { close(pp.done) })
	}
}

// Position is called once per account holding in response to ReqPositions.
func (w *IbWrapper) Position(account string, contract *ibapi.Contract, position ibapi.Decimal, avgCost float64) {
	w.mu.Lock()
	pp := w.positions
	w.mu.Unlock()
	if pp != nil && contract != nil {
		pp.positions = append(pp.positions, positionFromIB(account, contract, position, avgCost))
	}
}

// PositionEnd signals all positions have been delivered.
func (w *IbWrapper) PositionEnd() {
	w.mu.Lock()
	pp := w.positions
	w.mu.Unlock()
	if pp != nil {
		close(pp.done)
	}
}

// ExecDetails is called once per fill in response to ReqExecutions.
func (w *IbWrapper) ExecDetails(reqID int64, contract *ibapi.Contract, execution *ibapi.Execution) {
	w.mu.Lock()
	pe, ok := w.executions[reqID]
	w.mu.Unlock()
	if ok && contract != nil && execution != nil {
		pe.executions = append(pe.executions, executionFromIB(contract, execution))
	}
}

// ExecDetailsEnd signals all executions for this request have been delivered.
func (w *IbWrapper) ExecDetailsEnd(reqID int64) {
	w.mu.Lock()
	pe, ok := w.executions[reqID]
	w.mu.Unlock()
	if ok {
		pe.once.Do(func() { close(pe.done) })
	}
}

// ConnectionClosed is called by ibapi when the TCP connection to TWS is dropped
// (e.g., TWS restart, network failure). We forward the signal to Client so it
// can mark the connection unhealthy immediately, before the next health check.
func (w *IbWrapper) ConnectionClosed() {
	select {
	case w.disconnectCh <- struct{}{}:
	default: // already signaled; Client hasn't drained it yet
	}
}

// CurrentTime is called in response to ReqCurrentTime. Used by Ping() as a
// lightweight round-trip probe to verify the TCP connection is still alive.
func (w *IbWrapper) CurrentTime(_ int64) {
	select {
	case w.pingCh <- struct{}{}:
	default:
	}
}

func (w *IbWrapper) sendConnectError(err error) {
	select {
	case w.connectErrors <- err:
	default:
	}
}

// strictErrorCodes whitelists sub-2000 IB error codes that are genuine
// per-request failures, not informational noise — but ONLY for reqIDs that
// opted into strict routing via registerStrictError. Most of the sub-2000
// range (farm-connectivity chatter etc.) really is safe to drop for every
// request type — see the general `errCode < 2000` filter below — but a
// handful of codes in that range are real request-level errors IB happens
// to number below 2000, and dropping them silently burns the caller's full
// collection window instead of failing fast with the real reason (#193
// finding 1).
//
// This started as an UNCONDITIONAL whitelist applied to every reqID-keyed
// request via the shared Error() callback. A review follow-up found that
// broke GetMarketDepth's documented partial-rows-on-timeout contract (a
// routed 354 discarded an already-collected partial snapshot) and
// GetQuote's no-subscription history fallback (a routed 354 hard-failed
// instead of falling through to isNoSubscriptionErr's 10089/10090 history
// path, unlike the adjacent 10167 tolerance below). Strict routing is now
// opt-in per reqID; only GetOptionQuoteDetails registers strict, since
// fast-failing on contract validation is that command's literal purpose.
// Every other sub-2000 code, and every non-strict reqID, is still swallowed
// exactly as it was pre-#193.
//
//   - 200 — "No security definition has been found for the request": the
//     contract itself doesn't exist (bad strike/expiry/right). This is
//     exactly what `option-quote` contract validation needs to catch.
//   - 162 — "Historical Market Data Service error message": per-request
//     historical/market-data query failure (e.g. no data returned).
//   - 300 — "Can't find EId with tickerId": the reqID/ticker is unknown to
//     TWS (e.g. request was already cancelled or never valid).
//   - 321 — "Error validating request": malformed contract/request fields.
//
// 354 ("Requested market data is not subscribed") is deliberately NOT in
// this set, even though option-quote is the only strict caller. IB can fire
// 354 for a contract that has no live-data subscription before its
// delayed/frozen ticks arrive; those ticks can still complete a usable
// (degraded) quote (finding 2's early-completion path, or the timeout
// fallback). Strict-routing 354 would kill that quote before it had a
// chance to degrade gracefully — the same reasoning the 10167 special case
// below already applies to "not subscribed, delayed data incoming". So 354
// is always informational, for every reqID, strict or not.
var strictErrorCodes = map[int64]bool{
	200: true,
	162: true,
	300: true,
	321: true,
}

// Error routes IB errors to the corresponding pending request's error channel,
// or logs them as informational messages (errCode < 2000 are often warnings).
func (w *IbWrapper) Error(reqID ibapi.TickerID, _ int64, errCode int64, errString string, _ string) {
	ibErr := &ibAPIError{code: errCode, message: errString}
	if errCode == ibClientIDInUseCode {
		w.sendConnectError(ibErr)
		return
	}
	// Codes < 2000 are informational (e.g., "Market data farm connection"),
	// EXCEPT strictErrorCodes on a reqID that opted into strict routing via
	// registerStrictError — those are genuine per-request failures that
	// must reach the caller (#193 finding 1, scoped per review follow-up).
	if errCode < 2000 {
		w.mu.Lock()
		strict := w.strictReqIDs[reqID]
		w.mu.Unlock()
		if !strict || !strictErrorCodes[errCode] {
			return
		}
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
		case ch <- ibErr:
		default:
		}
		// Also close the corresponding data channel so callers unblock.
		w.mu.Lock()
		if pq, has := w.quotes[reqID]; has {
			pq.once.Do(func() { close(pq.done) })
		}
		if pb, has := w.bars[reqID]; has {
			pb.once.Do(func() { close(pb.done) })
		}
		if pp, has := w.optParams[reqID]; has {
			pp.once.Do(func() { close(pp.done) })
		}
		if pcd, has := w.contractDetails[reqID]; has {
			pcd.once.Do(func() { close(pcd.done) })
		}
		if po, has := w.oi[reqID]; has {
			po.once.Do(func() { close(po.done) })
		}
		if pe, has := w.executions[reqID]; has {
			pe.once.Do(func() { close(pe.done) })
		}
		w.mu.Unlock()
	}
}
