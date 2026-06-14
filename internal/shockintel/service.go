package shockintel

import (
	"context"
	"time"
)

const shockBarLookback = 30 * 24 * time.Hour

type Service struct {
	src MarketSource
	Now func() time.Time
}

func NewService(src MarketSource) *Service {
	return &Service{src: src}
}

func NewDefaultService(pythonBin string) *Service {
	return NewService(NewYFinanceAdapter(pythonBin))
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Regime(ctx context.Context) (RegimeDTO, error) {
	now := s.now().UTC()
	quotes, warnings := s.quotes(ctx, shockQuoteIDs())
	liquidity, _ := s.Liquidity(ctx)
	out := BuildRegimeTrigger(quotes, liquidity, now)
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func (s *Service) Fingerprint(ctx context.Context) (FingerprintDTO, error) {
	now := s.now().UTC()
	quotes, warnings := s.quotes(ctx, shockQuoteIDs())
	liquidity, _ := s.Liquidity(ctx)
	options, optionWarnings := s.optionStress(ctx, optionStressUnderlyings())
	out := BuildShockFingerprint(quotes, liquidity, options, now)
	out.Warnings = append(warnings, out.Warnings...)
	out.Warnings = append(out.Warnings, optionWarnings...)
	return out, nil
}

func (s *Service) Analogs(ctx context.Context) (AnalogsDTO, error) {
	now := s.now().UTC()
	quotes, warnings := s.quotes(ctx, shockQuoteIDs())
	out := BuildShockAnalogs(BuildShockVector(quotes), defaultAnalogTemplates(), now)
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func (s *Service) Liquidity(ctx context.Context) (LiquidityDTO, error) {
	now := s.now().UTC()
	quotes, warnings := s.quotes(ctx, liquidityIDs())
	depth := map[string]DepthSnapshot{}
	if s.src == nil {
		warnings = append(warnings, "depth: shock source unavailable")
	} else if got, err := s.src.Depth(ctx, liquidityIDs(), 5); err != nil {
		warnings = append(warnings, "depth: "+err.Error())
	} else {
		depth = got
	}
	out := BuildLiquidityState(quotes, depth, now)
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func (s *Service) Bundle(ctx context.Context) (BundleDTO, error) {
	regime, _ := s.Regime(ctx)
	fingerprint, _ := s.Fingerprint(ctx)
	analogs, _ := s.Analogs(ctx)
	liquidity, _ := s.Liquidity(ctx)
	return BundleDTO{Regime: regime, Fingerprint: fingerprint, Analogs: analogs, Liquidity: liquidity}, nil
}

func (s *Service) quotes(ctx context.Context, ids []string) (map[string]ShockQuote, []string) {
	if s.src == nil {
		return map[string]ShockQuote{}, []string{"quotes: shock source unavailable"}
	}
	quotes, err := s.src.Quotes(ctx, ids)
	var warnings []string
	if err != nil {
		warnings = append(warnings, "quotes: "+err.Error())
	}
	if quotes == nil {
		quotes = map[string]ShockQuote{}
	}
	return quotes, warnings
}

func (s *Service) optionStress(ctx context.Context, underlyings []string) ([]OptionStress, []string) {
	if s.src == nil {
		return []OptionStress{}, []string{"option stress: shock source unavailable"}
	}
	optionCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	metrics, err := s.src.OptionMetrics(optionCtx, underlyings)
	var warnings []string
	if err != nil {
		warnings = append(warnings, "option stress: "+err.Error())
	}
	if metrics == nil {
		metrics = map[string]OptionStress{}
	}
	rows := make([]OptionStress, 0, len(underlyings))
	for _, underlying := range underlyings {
		if row, ok := metrics[underlying]; ok {
			rows = append(rows, row)
		}
	}
	return rows, warnings
}

func shockQuoteIDs() []string {
	return []string{"VIX", "SPY", "QQQ", "IWM", "TLT", "HYG", "LQD", "GLD", "USO", "UUP", "VIXY", "US10Y"}
}

func optionStressUnderlyings() []string {
	// Use liquid ETF option chains as the first implementation path for
	// SPX/SPY and VIX/VIXY stress proxies; index-option contracts need a
	// dedicated IBKR contract mapper.
	return []string{"SPY", "QQQ", "IWM", "VIXY"}
}

func liquidityIDs() []string {
	out := make([]string, 0, len(liquidityAssets))
	for _, asset := range liquidityAssets {
		out = append(out, asset.ID)
	}
	return out
}
