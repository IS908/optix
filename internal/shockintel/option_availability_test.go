package shockintel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
)

func TestOptionStressMissingMetricsAreExplicit(t *testing.T) {
	chain := &model.OptionChain{UnderlyingPrice: 100, Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 100, OpenInterest: 25}}}}}
	adapter := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) {
		return testBroker{chains: map[string]*model.OptionChain{"SPY": chain}}, "ibkr", nil
	}, nil)
	rows, err := adapter.OptionMetrics(context.Background(), []string{"SPY"})
	raw, _ := json.Marshal(rows["SPY"])
	if err == nil || !strings.Contains(err.Error(), "iv_skew") || !strings.Contains(string(raw), `"missing_metrics":["iv_skew","volume"]`) {
		t.Fatalf("missing fields presented as valid zeros: %s, %v", raw, err)
	}
	dto := BuildShockFingerprint(nil, LiquidityDTO{}, []OptionStress{rows["SPY"]}, fixedShockNow())
	if !strings.Contains(strings.Join(dto.Rows[2].Missing, ","), "SPY iv_skew") {
		t.Fatalf("missing option signal not reported: %+v", dto.Rows[2])
	}
}

func TestOptionStressTrueZeroSkewIsAvailable(t *testing.T) {
	chain := &model.OptionChain{UnderlyingPrice: 100, Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 100, ImpliedVolatility: .3, Volume: 10, OpenInterest: 20}}, Puts: []model.OptionQuote{{Strike: 100, ImpliedVolatility: .3, Volume: 5, OpenInterest: 10}}}}}
	row, ok := summarizeOptionStress("SPY", chain, "ibkr", fixedShockNow())
	raw, _ := json.Marshal(row)
	if !ok || row.IVSkew != 0 || !strings.Contains(string(raw), `"missing_metrics":[]`) {
		t.Fatalf("valid zero skew lost: %s", raw)
	}
}

func TestOptionStressDoesNotInferATMFromStrikeMedian(t *testing.T) {
	chain := &model.OptionChain{Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 100, ImpliedVolatility: .2, OpenInterest: 20}}, Puts: []model.OptionQuote{{Strike: 100, ImpliedVolatility: .4}}}}}
	row, ok := summarizeOptionStress("SPY", chain, "ibkr", fixedShockNow())
	if !ok || row.IVSkew != 0 || !strings.Contains(strings.Join(row.MissingMetrics, ","), "underlying_price") {
		t.Fatalf("unknown spot produced ATM skew: %+v", row)
	}
}

type spotHintBroker struct {
	testBroker
	spot       float64
	spotSource string
}

func (b *spotHintBroker) GetOptionChainWithSpot(_ context.Context, underlying, expiry string, spot float64, source string) (*model.OptionChain, error) {
	b.spot = spot
	b.spotSource = source
	return b.testBroker.GetOptionChainWithOI(context.Background(), underlying, expiry)
}
func TestOptionMetricsReusesRecentSpotButRejectsStaleHint(t *testing.T) {
	for _, age := range []time.Duration{time.Second, time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			b := &spotHintBroker{testBroker: testBroker{chains: map[string]*model.OptionChain{"SPY": {UnderlyingPrice: 767, Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 767, OpenInterest: 12}}}}}}}}
			a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) { return b, "ibkr", nil }, nil)
			a.spotQuotes = map[string]ShockQuote{"SPY": {Price: 767, Source: "yfinance", AsOf: time.Now().Add(-age)}}
			a.OptionMetrics(context.Background(), []string{"SPY"})
			if age < 30*time.Second && (b.spot != 767 || b.spotSource != "yfinance") {
				t.Fatalf("fresh hint lost: %+v", b)
			}
			if age >= 30*time.Second && b.spot != 0 {
				t.Fatalf("stale hint reused: %+v", b)
			}
		})
	}
}

func TestWrappedFullChainSourceRemainsAvailableWithoutATMSampling(t *testing.T) {
	plain := testBroker{chains: map[string]*model.OptionChain{"SPY": {UnderlyingPrice: 100, Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 100, OpenInterest: 10}}}}}}}
	wrapped := broker.NewFallbackBroker(plain, plain)
	if err := wrapped.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) { return wrapped, "yfinance", nil }, nil)
	rows, _ := a.OptionMetrics(context.Background(), []string{"SPY"})
	if rows["SPY"].OpenInt != 10 {
		t.Fatalf("full-chain fallback lost: %v", rows)
	}
	if strings.Contains(rows["SPY"].Note, "nearest call/put sample") {
		t.Fatal("full-chain fallback mislabeled as ATM sample")
	}
}

func (b *spotHintBroker) CanSampleOptionStress() bool { return true }
