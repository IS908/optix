//go:build integration

package analysis_test

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/internal/analysis"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
)

// Run with: go test -tags=integration ./internal/analysis/ -v
// Requires: Python server running at localhost:50052
//   python -m optix_engine.grpc_server.server

const pythonServerAddr = "localhost:50052"

func newClient(t *testing.T) *analysis.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = ctx

	c, err := analysis.NewClient(pythonServerAddr)
	if err != nil {
		t.Fatalf("connect to Python server at %s: %v", pythonServerAddr, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestPriceOption_CallATM verifies BS call pricing round-trips through Python.
func TestPriceOption_CallATM(t *testing.T) {
	c := newClient(t)

	// ATM call: S=K=100, T=1yr, r=5%, sigma=20%
	// Expected price ≈ 10.45
	result, err := c.PriceOption(
		context.Background(),
		100, 100, 1.0, 0.05, 0.20, 0.0,
		"call",
	)
	if err != nil {
		t.Fatalf("PriceOption: %v", err)
	}

	t.Logf("Call price: %.4f  delta=%.4f gamma=%.4f theta=%.4f vega=%.4f rho=%.4f",
		result.Price, result.Delta, result.Gamma, result.Theta, result.Vega, result.Rho)

	if result.Price < 10.0 || result.Price > 11.0 {
		t.Errorf("expected call price ≈ 10.45, got %.4f", result.Price)
	}
	if result.Delta < 0.4 || result.Delta > 0.7 {
		t.Errorf("expected ATM call delta ≈ 0.5-0.6, got %.4f", result.Delta)
	}
	if result.Gamma <= 0 {
		t.Errorf("expected gamma > 0, got %.4f", result.Gamma)
	}
	if result.Theta >= 0 {
		t.Errorf("expected theta < 0 (time decay), got %.4f", result.Theta)
	}
	if result.Vega <= 0 {
		t.Errorf("expected vega > 0, got %.4f", result.Vega)
	}
}

// TestPriceOption_PutCallParity verifies C - P = S - K*e^(-rT) holds over gRPC.
func TestPriceOption_PutCallParity(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()

	S, K, T, r, sigma := 100.0, 100.0, 1.0, 0.05, 0.20

	call, err := c.PriceOption(ctx, S, K, T, r, sigma, 0, "call")
	if err != nil {
		t.Fatalf("PriceOption call: %v", err)
	}
	put, err := c.PriceOption(ctx, S, K, T, r, sigma, 0, "put")
	if err != nil {
		t.Fatalf("PriceOption put: %v", err)
	}

	// Put-call parity: C - P = S - K*e^(-rT)
	parity := S - K*0.951229 // K*e^(-0.05*1) ≈ 95.12
	diff := call.Price - put.Price
	t.Logf("C=%.4f  P=%.4f  C-P=%.4f  expected≈%.4f", call.Price, put.Price, diff, parity)

	if diff < parity-0.1 || diff > parity+0.1 {
		t.Errorf("put-call parity violation: C-P=%.4f, expected≈%.4f", diff, parity)
	}
}

// TestGetMaxPain verifies max pain calculation returns a valid strike.
func TestGetMaxPain(t *testing.T) {
	c := newClient(t)

	// Build a simple option chain
	chain := []*marketdatav1.OptionChainExpiry{
		{
			Expiration:   "2026-04-17",
			DaysToExpiry: 30,
			Calls: []*marketdatav1.OptionQuote{
				{Strike: 185, OpenInterest: 100},
				{Strike: 190, OpenInterest: 500},
				{Strike: 195, OpenInterest: 200},
				{Strike: 200, OpenInterest: 50},
			},
			Puts: []*marketdatav1.OptionQuote{
				{Strike: 185, OpenInterest: 50},
				{Strike: 190, OpenInterest: 200},
				{Strike: 195, OpenInterest: 500},
				{Strike: 200, OpenInterest: 100},
			},
		},
	}

	maxPain, expiry, err := c.GetMaxPain(context.Background(), "AAPL", chain)
	if err != nil {
		t.Fatalf("GetMaxPain: %v", err)
	}

	t.Logf("Max Pain: $%.2f  expiry=%s", maxPain, expiry)

	validStrikes := map[float64]bool{185: true, 190: true, 195: true, 200: true}
	if !validStrikes[maxPain] {
		t.Errorf("max pain %.2f is not one of the input strikes", maxPain)
	}
}
