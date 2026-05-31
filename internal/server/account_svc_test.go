package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// fakeAccountBroker implements broker.Broker + broker.AccountReader +
// broker.OptionQuoter for AccountService tests. A symbol absent from `quotes`
// (or contract absent from `optMarks`) makes the corresponding fetch error,
// exercising the graceful-degradation path.
type fakeAccountBroker struct {
	positions  []model.Position
	executions []model.Execution
	quotes     map[string]float64 // stock symbol -> last price
	optMarks   map[string]float64 // "SYM|EXP|R|STRIKE" -> mark price
}

func (f *fakeAccountBroker) Connect(context.Context) error { return nil }
func (f *fakeAccountBroker) Disconnect() error             { return nil }
func (f *fakeAccountBroker) IsConnected() bool             { return true }

func (f *fakeAccountBroker) GetQuote(_ context.Context, sym string) (*model.StockQuote, error) {
	if last, ok := f.quotes[sym]; ok {
		return &model.StockQuote{Symbol: sym, Last: last}, nil
	}
	return nil, fmt.Errorf("no quote for %s", sym)
}

func (f *fakeAccountBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
	return nil, nil
}

func (f *fakeAccountBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func (f *fakeAccountBroker) GetPositions(context.Context) ([]model.Position, error) {
	return f.positions, nil
}

func (f *fakeAccountBroker) GetExecutions(context.Context, model.ExecutionFilter) ([]model.Execution, error) {
	return f.executions, nil
}

func (f *fakeAccountBroker) GetOptionQuote(_ context.Context, sym, exp, right string, strike float64) (float64, error) {
	key := fmt.Sprintf("%s|%s|%s|%.2f", sym, exp, right, strike)
	if m, ok := f.optMarks[key]; ok {
		return m, nil
	}
	return 0, fmt.Errorf("no option mark for %s", key)
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s = %.2f, want %.2f", name, got, want)
	}
}

func TestAccountServiceGetPositions(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeAccountBroker{
		positions: []model.Position{
			{Symbol: "AAPL", SecType: "STK", Quantity: 100, AvgCost: 245.30, Multiplier: 1},
			{Symbol: "TSLA", SecType: "STK", Quantity: -50, AvgCost: 380.00, Multiplier: 1},
			{Symbol: "NVDA", SecType: "STK", Quantity: 10, AvgCost: 800.00, Multiplier: 1},
			{Symbol: "AAPL", SecType: "OPT", Expiration: "20260515", Strike: 295, Right: "C",
				Quantity: 2, AvgCost: 310.00, Multiplier: 100},
			{Symbol: "MSFT", SecType: "OPT", Expiration: "20260620", Strike: 400, Right: "P",
				Quantity: 1, AvgCost: 1500.00, Multiplier: 100},
		},
		quotes: map[string]float64{
			"AAPL": 293.32,
			"TSLA": 293.32,
			// NVDA intentionally absent -> GetQuote errors
		},
		optMarks: map[string]float64{
			"AAPL|20260515|C|295.00": 4.00,
			// MSFT option intentionally absent -> GetOptionQuote errors
		},
	}

	market := NewMarketDataService(fake, store)
	acct := NewAccountService(fake, market)

	got, err := acct.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 positions, got %d", len(got))
	}

	find := func(sym, secType string) model.Position {
		for _, p := range got {
			if p.Symbol == sym && p.SecType == secType {
				return p
			}
		}
		t.Fatalf("position %s/%s not found", sym, secType)
		return model.Position{}
	}

	// long stock: MV = 100*293.32 = 29332; PnL = 29332 - 100*245.30 = 4802
	// pct = 4802 / |100*245.30| * 100 = 4802/24530*100 ≈ 19.58
	aapl := find("AAPL", "STK")
	assertFloat(t, "AAPL STK MarketValue", aapl.MarketValue, 29332.0)
	assertFloat(t, "AAPL STK UnrealizedPnL", aapl.UnrealizedPnL, 4802.0)
	assertFloat(t, "AAPL STK UnrealizedPnLPct", aapl.UnrealizedPnLPct, 19.58)

	// short stock: MV = -50*293.32 = -14666; PnL = -14666 - (-50*380) = 4334
	// pct = 4334 / |-50*380| * 100 = 4334/19000*100 ≈ 22.81 (positive: profitable short)
	tsla := find("TSLA", "STK")
	assertFloat(t, "TSLA STK MarketValue", tsla.MarketValue, -14666.0)
	assertFloat(t, "TSLA STK UnrealizedPnL", tsla.UnrealizedPnL, 4334.0)
	assertFloat(t, "TSLA STK UnrealizedPnLPct", tsla.UnrealizedPnLPct, 22.81)

	// stock with no quote -> P&L stays zero
	nvda := find("NVDA", "STK")
	assertFloat(t, "NVDA STK MarketValue", nvda.MarketValue, 0.0)
	assertFloat(t, "NVDA STK UnrealizedPnL", nvda.UnrealizedPnL, 0.0)

	// option with a mark: MV = 2*4.00*100 = 800; PnL = 800 - 2*310 = 180
	opt := find("AAPL", "OPT")
	assertFloat(t, "AAPL OPT MarketValue", opt.MarketValue, 800.0)
	assertFloat(t, "AAPL OPT UnrealizedPnL", opt.UnrealizedPnL, 180.0)

	// option with no mark -> P&L stays zero
	msft := find("MSFT", "OPT")
	assertFloat(t, "MSFT OPT MarketValue", msft.MarketValue, 0.0)
	assertFloat(t, "MSFT OPT UnrealizedPnL", msft.UnrealizedPnL, 0.0)
}

func TestAccountServiceGetExecutions(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeAccountBroker{
		executions: []model.Execution{
			{ExecID: "e1", Symbol: "AAPL", Side: "BOT", Shares: 100, Price: 245.30},
		},
	}
	market := NewMarketDataService(fake, store)
	acct := NewAccountService(fake, market)

	got, err := acct.GetExecutions(context.Background(), model.ExecutionFilter{})
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(got) != 1 || got[0].ExecID != "e1" {
		t.Errorf("unexpected executions: %+v", got)
	}
}

// nonAccountBroker implements broker.Broker but NOT broker.AccountReader,
// so AccountService must return broker.ErrAccountNotSupported for both calls.
type nonAccountBroker struct{}

func (n *nonAccountBroker) Connect(context.Context) error { return nil }
func (n *nonAccountBroker) Disconnect() error             { return nil }
func (n *nonAccountBroker) IsConnected() bool             { return false }
func (n *nonAccountBroker) GetQuote(_ context.Context, _ string) (*model.StockQuote, error) {
	return nil, nil
}
func (n *nonAccountBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
	return nil, nil
}
func (n *nonAccountBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func TestAccountServiceErrAccountNotSupported(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	nb := &nonAccountBroker{}
	market := NewMarketDataService(nb, store)
	acct := NewAccountService(nb, market)

	_, posErr := acct.GetPositions(context.Background())
	if !errors.Is(posErr, broker.ErrAccountNotSupported) {
		t.Errorf("GetPositions: got %v, want broker.ErrAccountNotSupported", posErr)
	}

	_, execErr := acct.GetExecutions(context.Background(), model.ExecutionFilter{})
	if !errors.Is(execErr, broker.ErrAccountNotSupported) {
		t.Errorf("GetExecutions: got %v, want broker.ErrAccountNotSupported", execErr)
	}
}

// TestAccountServiceGetPositionsCtxCancelled guards issue #50: when ctx is
// already cancelled, enrich leaves marks unpopulated. GetPositions must
// surface the cancellation rather than returning a misleading partial
// snapshot (which downstream would misread as an OPRA-subscription gap).
func TestAccountServiceGetPositionsCtxCancelled(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeAccountBroker{
		positions: []model.Position{
			{Symbol: "AAPL", SecType: "STK", Quantity: 100, AvgCost: 245.30, Multiplier: 1},
		},
		quotes: map[string]float64{"AAPL": 293.32},
	}
	market := NewMarketDataService(fake, store)
	acct := NewAccountService(fake, market)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call so enrich's goroutines hit ctx.Done()

	_, err = acct.GetPositions(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GetPositions on cancelled ctx: got %v, want context.Canceled", err)
	}
}
