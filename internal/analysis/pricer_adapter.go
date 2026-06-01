package analysis

import (
	"context"

	"github.com/IS908/optix/pkg/model"
)

// GreeksPricer adapts *Client to the portfolio.OptionPricer interface,
// translating model.OptionType ↔ the client's "call"/"put" string and
// model.Greeks ↔ PriceOptionResult. Kept here (not in portfolio) so the
// portfolio package stays free of gRPC/analysis dependencies.
type GreeksPricer struct{ Client *Client }

func (p GreeksPricer) PriceOption(ctx context.Context, spot, strike, tYears, r, iv float64, ot model.OptionType) (model.Greeks, error) {
	res, err := p.Client.PriceOption(ctx, spot, strike, tYears, r, iv, 0.0, otString(ot))
	if err != nil {
		return model.Greeks{}, err
	}
	return model.Greeks{Price: res.Price, Delta: res.Delta, Gamma: res.Gamma, Theta: res.Theta, Vega: res.Vega, Rho: res.Rho}, nil
}

func (p GreeksPricer) ImpliedVol(ctx context.Context, mark, spot, strike, tYears, r float64, ot model.OptionType) (float64, bool, error) {
	res, err := p.Client.ImpliedVol(ctx, mark, spot, strike, tYears, r, 0.0, otString(ot))
	if err != nil {
		return 0, false, err
	}
	return res.IV, res.Converged, nil
}

func otString(ot model.OptionType) string {
	if ot == model.OptionTypePut {
		return "put"
	}
	return "call"
}
