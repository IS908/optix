package cli

import (
	"context"
	"testing"

	"github.com/IS908/optix/pkg/model"
)

type fakeCloser struct{ closed *int }

func (f fakeCloser) Close() error { *f.closed++; return nil }

// disconnectRecorder is a minimal broker.Broker stub. It exists to prove
// RegisterBrokerCleanup compiles against the full broker.Broker interface
// and that runCleanup actually invokes Disconnect on registered brokers.
type disconnectRecorder struct{ n *int }

func (d disconnectRecorder) Connect(ctx context.Context) error { return nil }
func (d disconnectRecorder) Disconnect() error                 { *d.n++; return nil }
func (d disconnectRecorder) IsConnected() bool                 { return false }

func (d disconnectRecorder) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	return nil, nil
}

func (d disconnectRecorder) GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error) {
	return nil, nil
}

func (d disconnectRecorder) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	return nil, nil
}

func TestRunCleanupClosesRegisteredInReverseOrder(t *testing.T) {
	cleanupMu.Lock()
	saved := cleanupItems
	cleanupItems = nil
	cleanupMu.Unlock()
	defer func() {
		cleanupMu.Lock()
		cleanupItems = saved
		cleanupMu.Unlock()
	}()

	n := 0
	RegisterCleanup(fakeCloser{closed: &n})
	disconnects := 0
	RegisterBrokerCleanup(disconnectRecorder{n: &disconnects})
	runCleanup()
	if n != 1 || disconnects != 1 {
		t.Fatalf("cleanup ran store=%d broker=%d, want 1/1", n, disconnects)
	}
	// registry drained — second run is a no-op
	runCleanup()
	if n != 1 || disconnects != 1 {
		t.Fatalf("second cleanup must be no-op, got store=%d broker=%d", n, disconnects)
	}
}
