package shockintel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
