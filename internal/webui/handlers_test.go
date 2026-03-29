package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// ─── test helpers ─────────────────────────────────────────────────────────────

// quoteMockBroker extends mockBroker to return realistic quotes.
type quoteMockBroker struct {
	mockBroker
	quotes map[string]*model.StockQuote
}

func (m *quoteMockBroker) GetQuote(_ context.Context, symbol string) (*model.StockQuote, error) {
	if q, ok := m.quotes[symbol]; ok {
		return q, nil
	}
	return &model.StockQuote{
		Symbol:        symbol,
		Last:          100.0,
		Bid:           99.90,
		Ask:           100.10,
		Volume:        1000000,
		Change:        1.50,
		ChangePct:     1.52,
		Close:         98.50,
		MarketSession: model.SessionRegular,
		Timestamp:     time.Now(),
	}, nil
}

func (m *quoteMockBroker) GetHistoricalBars(_ context.Context, _, _, _, _ string) ([]model.OHLCV, error) {
	return nil, nil
}

func (m *quoteMockBroker) GetOptionChain(_ context.Context, _, _ string) (*model.OptionChain, error) {
	return nil, nil
}

var _ broker.Broker = (*quoteMockBroker)(nil)

func quoteMockFactory() brokerFactory {
	return func(_ context.Context, _ int64) (broker.Broker, error) {
		return &quoteMockBroker{
			mockBroker: mockBroker{connected: true},
			quotes: map[string]*model.StockQuote{
				"AAPL": {
					Symbol: "AAPL", Last: 178.50, Bid: 178.40, Ask: 178.60,
					Volume: 52000000, Change: 2.30, ChangePct: 1.30, Close: 176.20,
					High: 179.00, Low: 176.00, Open: 176.50,
					MarketSession: model.SessionRegular, Timestamp: time.Now(),
				},
				"TSLA": {
					Symbol: "TSLA", Last: 245.20, Bid: 245.00, Ask: 245.40,
					Volume: 85000000, Change: -3.80, ChangePct: -1.53, Close: 249.00,
					High: 250.00, Low: 243.50, Open: 249.00,
					MarketSession: model.SessionRegular, Timestamp: time.Now(),
				},
				"NVDA": {
					Symbol: "NVDA", Last: 890.00, Bid: 889.50, Ask: 890.50,
					Volume: 45000000, Change: 15.00, ChangePct: 1.71, Close: 875.00,
					High: 895.00, Low: 880.00, Open: 876.00,
					MarketSession: model.SessionPreMarket, Timestamp: time.Now(),
				},
			},
		}, nil
	}
}

// newTestServer creates a Server with in-memory SQLite and mock broker.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	s := &Server{
		cfg: Config{
			Addr:          "127.0.0.1:0",
			ForecastDays:  14,
			RiskTolerance: "moderate",
			Capital:       50000,
		},
		store:       store,
		lastRefresh: make(map[string]time.Time),
		brokerPool:  newBrokerPool(2, quoteMockFactory()),
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// addWatchlistSymbols adds symbols to the watchlist in the test store.
func addWatchlistSymbols(t *testing.T, s *Server, symbols ...string) {
	t.Helper()
	for _, sym := range symbols {
		err := s.store.AddToWatchlist(context.Background(), sym)
		if err != nil {
			t.Fatalf("failed to add watchlist symbol %s: %v", sym, err)
		}
	}
}

// ─── /api/quotes tests ───────────────────────────────────────────────────────

func TestHandleAPIQuotes_ReturnsJSON(t *testing.T) {
	s := newTestServer(t)
	addWatchlistSymbols(t, s, "AAPL", "TSLA")

	req := httptest.NewRequest("GET", "/api/quotes", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var data QuoteTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(data.Quotes) != 2 {
		t.Fatalf("expected 2 quotes, got %d", len(data.Quotes))
	}

	// Verify quote fields
	found := map[string]bool{}
	for _, q := range data.Quotes {
		found[q.Symbol] = true

		if q.Last <= 0 {
			t.Errorf("quote %s: last price should be > 0, got %f", q.Symbol, q.Last)
		}
		if q.MarketSession == "" {
			t.Errorf("quote %s: market_session should not be empty", q.Symbol)
		}
		if q.SessionLabel == "" {
			t.Errorf("quote %s: session_label should not be empty", q.Symbol)
		}
	}

	if !found["AAPL"] || !found["TSLA"] {
		t.Errorf("expected AAPL and TSLA in results, got: %+v", found)
	}

	// Verify top-level session info
	if data.MarketSession == "" {
		t.Error("top-level market_session should not be empty")
	}
	if data.SessionLabel == "" {
		t.Error("top-level session_label should not be empty")
	}
	if data.ServerTime.IsZero() {
		t.Error("server_time should not be zero")
	}
}

func TestHandleAPIQuotes_EmptyWatchlist(t *testing.T) {
	s := newTestServer(t)
	// No watchlist symbols added

	req := httptest.NewRequest("GET", "/api/quotes", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var data QuoteTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(data.Quotes) != 0 {
		t.Fatalf("expected 0 quotes for empty watchlist, got %d", len(data.Quotes))
	}

	// Session info should still be present
	if data.MarketSession == "" {
		t.Error("market_session should not be empty even with empty watchlist")
	}
}

func TestHandleAPIQuotes_QuoteValues(t *testing.T) {
	s := newTestServer(t)
	addWatchlistSymbols(t, s, "AAPL")

	req := httptest.NewRequest("GET", "/api/quotes", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	var data QuoteTickerResponse
	resp := w.Result()
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&data)

	if len(data.Quotes) != 1 {
		t.Fatalf("expected 1 quote, got %d", len(data.Quotes))
	}

	q := data.Quotes[0]
	if q.Symbol != "AAPL" {
		t.Errorf("expected AAPL, got %s", q.Symbol)
	}
	if q.Last != 178.50 {
		t.Errorf("expected last 178.50, got %f", q.Last)
	}
	if q.Bid != 178.40 {
		t.Errorf("expected bid 178.40, got %f", q.Bid)
	}
	if q.Ask != 178.60 {
		t.Errorf("expected ask 178.60, got %f", q.Ask)
	}
	if q.Change != 2.30 {
		t.Errorf("expected change 2.30, got %f", q.Change)
	}
	if q.Volume != 52000000 {
		t.Errorf("expected volume 52000000, got %d", q.Volume)
	}
	if q.Close != 176.20 {
		t.Errorf("expected close 176.20, got %f", q.Close)
	}
}

func TestHandleAPIQuotes_Concurrent(t *testing.T) {
	s := newTestServer(t)
	addWatchlistSymbols(t, s, "AAPL", "TSLA", "NVDA")

	const concurrent = 10
	results := make(chan int, concurrent)

	for i := 0; i < concurrent; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/api/quotes", nil)
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, req)
			results <- w.Result().StatusCode
		}()
	}

	for i := 0; i < concurrent; i++ {
		code := <-results
		if code != http.StatusOK {
			t.Errorf("concurrent request %d returned %d, expected 200", i, code)
		}
	}
}

// ─── /api/freshness tests ────────────────────────────────────────────────────

func TestHandleAPIFreshness_IncludesSession(t *testing.T) {
	s := newTestServer(t)
	addWatchlistSymbols(t, s, "AAPL")

	req := httptest.NewRequest("GET", "/api/freshness", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var data FreshnessResponse
	json.NewDecoder(resp.Body).Decode(&data)

	if data.MarketSession == "" {
		t.Error("freshness response should include market_session")
	}
	if data.SessionLabel == "" {
		t.Error("freshness response should include session_label")
	}
	// Must be a valid session value
	validSessions := map[string]bool{
		"pre_market": true, "regular": true, "post_market": true, "closed": true,
	}
	if !validSessions[data.MarketSession] {
		t.Errorf("unexpected market_session value: %s", data.MarketSession)
	}
}

// ─── Template rendering tests ────────────────────────────────────────────────

func TestDashboardTemplate_RendersWithNewFields(t *testing.T) {
	s := newTestServer(t)

	// Populate some snapshot data so the template has symbols to render
	_ = s.store.SaveWatchlistSnapshot(context.Background(), model.QuickSummary{
		Symbol:           "AAPL",
		Price:            178.50,
		Trend:            "bullish",
		RSI:              55.0,
		IVRank:           45.0,
		MaxPain:          175.0,
		PCR:              0.85,
		RangeLow1S:       170.0,
		RangeHigh1S:      187.0,
		Recommendation:   "★ Sell Put",
		OpportunityScore: 65.0,
	})
	addWatchlistSymbols(t, s, "AAPL")

	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("dashboard returned %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Verify dual-zone layout
	if !strings.Contains(html, "ticker-zone") {
		t.Error("dashboard HTML should contain ticker-zone div")
	}
	if !strings.Contains(html, "analysis-zone") {
		t.Error("dashboard HTML should contain analysis-zone div")
	}
	if !strings.Contains(html, "Real-Time Quotes") {
		t.Error("dashboard HTML should contain 'Real-Time Quotes' header")
	}
	if !strings.Contains(html, "Strategy Analysis") {
		t.Error("dashboard HTML should contain 'Strategy Analysis' header")
	}

	// Session badge should be present
	if !strings.Contains(html, "session-badge") {
		t.Error("dashboard HTML should contain session-badge")
	}

	// TickerPoller JS should be present
	if !strings.Contains(html, "TickerPoller") {
		t.Error("dashboard HTML should contain TickerPoller JavaScript")
	}
	if !strings.Contains(html, "/api/quotes") {
		t.Error("dashboard HTML should reference /api/quotes endpoint")
	}
}

func TestDashboardTemplate_EmptyState(t *testing.T) {
	s := newTestServer(t)
	// No watchlist, no snapshots

	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("dashboard empty state returned %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Should still have the ticker zone (even if JS will show "loading")
	if !strings.Contains(html, "ticker-zone") {
		t.Error("empty dashboard should still include ticker-zone")
	}

	// Should show empty state message
	if !strings.Contains(html, "No dashboard data") {
		t.Error("empty dashboard should show 'No dashboard data' message")
	}
}

func TestAnalyzeTemplate_RendersTickerCard(t *testing.T) {
	s := newTestServer(t)
	addWatchlistSymbols(t, s, "AAPL")

	// The analyze page with no cached data should still render
	req := httptest.NewRequest("GET", "/analyze/AAPL", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("analyze page returned %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Verify upper ticker zone
	if !strings.Contains(html, "ticker-zone") {
		t.Error("analyze HTML should contain ticker-zone div")
	}
	if !strings.Contains(html, "ticker-price") {
		t.Error("analyze HTML should contain ticker-price element")
	}
	if !strings.Contains(html, "ticker-bid") {
		t.Error("analyze HTML should contain ticker-bid element")
	}
	if !strings.Contains(html, "ticker-ask") {
		t.Error("analyze HTML should contain ticker-ask element")
	}

	// Extended hours note (hidden by default)
	if !strings.Contains(html, "extended-hours-note") {
		t.Error("analyze HTML should contain extended-hours-note div")
	}

	// Analysis zone
	if !strings.Contains(html, "analysis-zone") {
		t.Error("analyze HTML should contain analysis-zone div")
	}

	// AnalyzeTickerPoller JS
	if !strings.Contains(html, "AnalyzeTickerPoller") {
		t.Error("analyze HTML should contain AnalyzeTickerPoller JavaScript")
	}
}

// ─── MarketSession model tests ───────────────────────────────────────────────

func TestQuoteTickerItem_SessionFields(t *testing.T) {
	q := &model.StockQuote{
		Symbol:        "AAPL",
		Last:          178.50,
		Bid:           178.40,
		Ask:           178.60,
		Volume:        52000000,
		Change:        2.30,
		ChangePct:     1.30,
		Close:         176.20,
		MarketSession: model.SessionPreMarket,
		Timestamp:     time.Now(),
	}

	item := modelQuoteToTickerItem(q)

	if item.Symbol != "AAPL" {
		t.Errorf("expected AAPL, got %s", item.Symbol)
	}
	if item.MarketSession != "pre_market" {
		t.Errorf("expected pre_market, got %s", item.MarketSession)
	}
	if item.SessionLabel != "盘前" {
		t.Errorf("expected 盘前, got %s", item.SessionLabel)
	}
	if item.Close != 176.20 {
		t.Errorf("expected close 176.20, got %f", item.Close)
	}
}

// ─── /api/dashboard JSON response tests ──────────────────────────────────────

func TestHandleAPIDashboard_IncludesSession(t *testing.T) {
	s := newTestServer(t)

	// Add symbol and snapshot
	addWatchlistSymbols(t, s, "TSLA")
	_ = s.store.SaveWatchlistSnapshot(context.Background(), model.QuickSummary{
		Symbol: "TSLA", Price: 245.20, Trend: "bearish",
	})

	req := httptest.NewRequest("GET", "/api/dashboard", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("api/dashboard returned %d: %s", resp.StatusCode, string(body))
	}

	var data DashboardResponse
	json.NewDecoder(resp.Body).Decode(&data)

	if data.MarketSession == "" {
		t.Error("dashboard response should include market_session")
	}
	if data.SessionLabel == "" {
		t.Error("dashboard response should include session_label")
	}
}

// ─── Singleflight dedup for /api/quotes ──────────────────────────────────────

func TestAPIQuotes_Singleflight(t *testing.T) {
	// Track how many times the mock broker's GetQuote is called
	var callCount int32
	countingFactory := func(_ context.Context, _ int64) (broker.Broker, error) {
		return &countingMockBroker{
			mockBroker: mockBroker{connected: true},
			callCount:  &callCount,
		}, nil
	}

	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	s := &Server{
		cfg: Config{
			ForecastDays:  14,
			RiskTolerance: "moderate",
			Capital:       50000,
		},
		store:       store,
		lastRefresh: make(map[string]time.Time),
		brokerPool:  newBrokerPool(2, countingFactory),
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()

	_ = store.AddToWatchlist(context.Background(), "AAPL")

	// Fire multiple concurrent requests
	const n = 5
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			req := httptest.NewRequest("GET", "/api/quotes", nil)
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, req)
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}

	// singleflight should collapse these — broker GetQuote called at most ~2 times
	// (could be 1 if all land on same singleflight group, or 2 if there's a race)
	calls := atomic.LoadInt32(&callCount)
	if calls > 3 {
		t.Errorf("expected singleflight to deduplicate, but GetQuote called %d times for %d requests", calls, n)
	}
}

type countingMockBroker struct {
	mockBroker
	callCount *int32
}

func (m *countingMockBroker) GetQuote(_ context.Context, symbol string) (*model.StockQuote, error) {
	atomic.AddInt32(m.callCount, 1)
	return &model.StockQuote{
		Symbol:        symbol,
		Last:          100.0,
		Bid:           99.90,
		Ask:           100.10,
		Volume:        1000000,
		MarketSession: model.SessionRegular,
		Timestamp:     time.Now(),
	}, nil
}

// ─── Response types JSON serialization ───────────────────────────────────────

func TestSummaryData_ExtendedHoursFields(t *testing.T) {
	summary := SummaryData{
		Price:           178.50,
		Change:          2.30,
		ChangePct:       1.30,
		PreviousClose:   176.20,
		IsExtendedHours: true,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	json.Unmarshal(data, &decoded)

	if decoded["previous_close"] != 176.20 {
		t.Errorf("expected previous_close 176.20, got %v", decoded["previous_close"])
	}
	if decoded["is_extended_hours"] != true {
		t.Errorf("expected is_extended_hours true, got %v", decoded["is_extended_hours"])
	}
}

func TestSummaryData_ExtendedHoursOmittedWhenFalse(t *testing.T) {
	summary := SummaryData{
		Price:           178.50,
		IsExtendedHours: false, // zero value
		PreviousClose:   0,     // zero value
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}

	// omitempty should exclude zero-value fields
	s := string(data)
	if strings.Contains(s, "previous_close") {
		t.Error("previous_close should be omitted when zero")
	}
	if strings.Contains(s, "is_extended_hours") {
		t.Error("is_extended_hours should be omitted when false")
	}
}
