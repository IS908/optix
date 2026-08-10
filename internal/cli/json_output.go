package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/IS908/optix/pkg/model"
)

func writeJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeJSONDestination(stdout io.Writer, path string, payload any) error {
	if path == "-" {
		return writeJSON(stdout, payload)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

type quoteOutput struct {
	Symbol        string              `json:"symbol"`
	Last          float64             `json:"last"`
	Bid           float64             `json:"bid"`
	Ask           float64             `json:"ask"`
	Volume        int64               `json:"volume"`
	Change        float64             `json:"change"`
	ChangePct     float64             `json:"change_pct"`
	High          float64             `json:"high"`
	Low           float64             `json:"low"`
	Open          float64             `json:"open"`
	Close         float64             `json:"close"`
	High52W       float64             `json:"high_52w"`
	Low52W        float64             `json:"low_52w"`
	AvgVolume     float64             `json:"avg_volume"`
	Timestamp     time.Time           `json:"timestamp"`
	MarketSession model.MarketSession `json:"market_session"`
	SessionLabel  string              `json:"session_label"`
	Source        string              `json:"source,omitempty"`
}

func renderQuoteJSON(w io.Writer, q *model.StockQuote, source string) error {
	return writeJSON(w, quoteOutput{
		Symbol:        q.Symbol,
		Last:          q.Last,
		Bid:           q.Bid,
		Ask:           q.Ask,
		Volume:        q.Volume,
		Change:        q.Change,
		ChangePct:     q.ChangePct,
		High:          q.High,
		Low:           q.Low,
		Open:          q.Open,
		Close:         q.Close,
		High52W:       q.High52W,
		Low52W:        q.Low52W,
		AvgVolume:     q.AvgVolume,
		Timestamp:     q.Timestamp,
		MarketSession: q.MarketSession,
		SessionLabel:  q.MarketSession.Label(),
		Source:        source,
	})
}

type optionChainOutput struct {
	Underlying      string                    `json:"underlying"`
	UnderlyingPrice float64                   `json:"underlying_price"`
	Source          string                    `json:"source,omitempty"`
	Expirations     []optionChainExpiryOutput `json:"expirations"`
}

type optionChainExpiryOutput struct {
	Expiration   string              `json:"expiration"`
	DaysToExpiry int                 `json:"days_to_expiry"`
	Calls        []optionQuoteOutput `json:"calls"`
	Puts         []optionQuoteOutput `json:"puts"`
}

type optionQuoteOutput struct {
	Underlying        string       `json:"underlying"`
	Expiration        string       `json:"expiration"`
	Right             string       `json:"right"`
	Strike            float64      `json:"strike"`
	OptionType        string       `json:"option_type"`
	Last              float64      `json:"last"`
	Bid               float64      `json:"bid"`
	Ask               float64      `json:"ask"`
	Mid               float64      `json:"mid"`
	Mark              float64      `json:"mark"`
	Volume            int64        `json:"volume"`
	OpenInterest      int32        `json:"open_interest"`
	ImpliedVolatility float64      `json:"implied_volatility"`
	Greeks            greeksOutput `json:"greeks"`
	Timestamp         time.Time    `json:"timestamp"`
	Source            string       `json:"source,omitempty"`
	MarketDataType    string       `json:"market_data_type"`
	Warnings          []string     `json:"warnings"`
}

type greeksOutput struct {
	Price float64 `json:"price"`
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
	Rho   float64 `json:"rho"`
}

func renderOptionChainJSON(w io.Writer, chain *model.OptionChain, source string) error {
	out := optionChainOutput{
		Underlying:      chain.Underlying,
		UnderlyingPrice: chain.UnderlyingPrice,
		Source:          source,
		Expirations:     make([]optionChainExpiryOutput, 0, len(chain.Expirations)),
	}
	for _, exp := range chain.Expirations {
		item := optionChainExpiryOutput{
			Expiration:   exp.Expiration,
			DaysToExpiry: exp.DaysToExpiry,
			Calls:        optionQuotesOutput(exp.Calls),
			Puts:         optionQuotesOutput(exp.Puts),
		}
		out.Expirations = append(out.Expirations, item)
	}
	return writeJSON(w, out)
}

func optionQuotesOutput(quotes []model.OptionQuote) []optionQuoteOutput {
	out := make([]optionQuoteOutput, 0, len(quotes))
	for _, q := range quotes {
		out = append(out, optionQuoteOutputFrom(q, ""))
	}
	return out
}

func renderOptionQuoteJSON(w io.Writer, quote *model.OptionQuote, source string) error {
	return writeJSON(w, optionQuoteOutputFrom(*quote, source))
}

func optionQuoteOutputFrom(q model.OptionQuote, source string) optionQuoteOutput {
	return optionQuoteOutput{
		Underlying:        q.Underlying,
		Expiration:        q.Expiration,
		Right:             optionRight(q.OptionType),
		Strike:            q.Strike,
		OptionType:        q.OptionType.String(),
		Last:              q.Last,
		Bid:               q.Bid,
		Ask:               q.Ask,
		Mid:               q.Mid,
		Mark:              q.Mark,
		Volume:            q.Volume,
		OpenInterest:      q.OpenInterest,
		ImpliedVolatility: q.ImpliedVolatility,
		Greeks:            greeksOutputFrom(q.Greeks),
		Timestamp:         q.Timestamp,
		Source:            source,
		MarketDataType:    optionMarketDataType(q.MarketDataType),
		Warnings:          optionQuoteValidationWarnings(&q),
	}
}

func optionRight(t model.OptionType) string {
	switch t {
	case model.OptionTypeCall:
		return "C"
	case model.OptionTypePut:
		return "P"
	default:
		return ""
	}
}

func optionMarketDataType(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// ibkrErrorDetail returns the raw IB error text (stripped of the
// "ibkr_error: " prefix ibkr.GetOptionQuoteDetails attaches to Warnings) for
// the first such warning found, or "" when the quote is nil or carries none.
// Used to surface the real per-request failure reason (e.g. "IB error 200:
// No security definition has been found for the request") instead of the
// generic "no usable price data" message — see #193 findings 1 and 6(a).
func ibkrErrorDetail(q *model.OptionQuote) string {
	if q == nil {
		return ""
	}
	const prefix = "ibkr_error: "
	for _, w := range q.Warnings {
		if strings.HasPrefix(w, prefix) {
			return strings.TrimPrefix(w, prefix)
		}
	}
	return ""
}

func optionQuoteValidationWarnings(q *model.OptionQuote) []string {
	if q == nil {
		return []string{"quote_unavailable"}
	}
	warnings := make([]string, 0, len(q.Warnings)+8)
	seen := make(map[string]bool, len(q.Warnings)+8)
	add := func(w string) {
		if w == "" || seen[w] {
			return
		}
		seen[w] = true
		warnings = append(warnings, w)
	}
	for _, w := range q.Warnings {
		add(w)
	}
	if q.Bid <= 0 {
		add("bid_unavailable")
	}
	if q.Ask <= 0 {
		add("ask_unavailable")
	}
	if q.Last <= 0 {
		add("last_unavailable")
	}
	if q.Mark <= 0 && q.Mid <= 0 && q.Last <= 0 && (q.Bid <= 0 || q.Ask <= 0) {
		add("no_price_data")
	}
	if q.OpenInterest <= 0 {
		add("open_interest_unavailable")
	}
	if q.ImpliedVolatility <= 0 {
		add("implied_volatility_unavailable")
	}
	if q.Greeks.Delta == 0 && q.Greeks.Gamma == 0 && q.Greeks.Theta == 0 && q.Greeks.Vega == 0 && q.Greeks.Rho == 0 {
		add("greeks_unavailable")
	}
	if q.MarketDataType == "" {
		add("market_data_type_unknown")
	}
	return warnings
}

func greeksOutputFrom(g model.Greeks) greeksOutput {
	return greeksOutput{
		Price: g.Price,
		Delta: g.Delta,
		Gamma: g.Gamma,
		Theta: g.Theta,
		Vega:  g.Vega,
		Rho:   g.Rho,
	}
}

type dashboardOutput struct {
	Sort        string                   `json:"sort"`
	Source      string                   `json:"source,omitempty"`
	GeneratedAt time.Time                `json:"generated_at"`
	Summaries   []dashboardSummaryOutput `json:"summaries"`
}

type dashboardSummaryOutput struct {
	Symbol           string  `json:"symbol"`
	Price            float64 `json:"price"`
	Trend            string  `json:"trend"`
	RSI              float64 `json:"rsi"`
	IVRank           float64 `json:"iv_rank"`
	MaxPain          float64 `json:"max_pain"`
	PCR              float64 `json:"pcr"`
	RangeLow1S       float64 `json:"range_low_1s"`
	RangeHigh1S      float64 `json:"range_high_1s"`
	Recommendation   string  `json:"recommendation"`
	OpportunityScore float64 `json:"opportunity_score"`
}

func renderDashboardJSON(w io.Writer, summaries []*analysisv1.StockQuickSummary, sortBy, dataSource string) error {
	out := dashboardOutput{
		Sort:        sortBy,
		Source:      dataSource,
		GeneratedAt: time.Now().UTC(),
		Summaries:   make([]dashboardSummaryOutput, 0, len(summaries)),
	}
	for _, s := range summaries {
		out.Summaries = append(out.Summaries, dashboardSummaryOutput{
			Symbol:           s.Symbol,
			Price:            s.Price,
			Trend:            s.Trend,
			RSI:              s.Rsi,
			IVRank:           s.IvRank,
			MaxPain:          s.MaxPain,
			PCR:              s.Pcr,
			RangeLow1S:       s.RangeLow_1S,
			RangeHigh1S:      s.RangeHigh_1S,
			Recommendation:   s.Recommendation,
			OpportunityScore: s.OpportunityScore,
		})
	}
	return writeJSON(w, out)
}

type positionsOutput struct {
	Positions []positionOutput `json:"positions"`
	Source    string           `json:"source,omitempty"`
}

type positionOutput struct {
	Account          string  `json:"account"`
	Symbol           string  `json:"symbol"`
	SecType          string  `json:"sec_type"`
	Expiration       string  `json:"expiration,omitempty"`
	Strike           float64 `json:"strike,omitempty"`
	Right            string  `json:"right,omitempty"`
	Quantity         float64 `json:"quantity"`
	AvgCost          float64 `json:"avg_cost"`
	Multiplier       float64 `json:"multiplier"`
	Currency         string  `json:"currency,omitempty"`
	LastPrice        float64 `json:"last_price"`
	MarketValue      float64 `json:"market_value"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
}

func renderPositionsJSON(w io.Writer, positions []model.Position, source string) error {
	out := positionsOutput{
		Positions: make([]positionOutput, 0, len(positions)),
		Source:    source,
	}
	for _, p := range positions {
		out.Positions = append(out.Positions, positionOutput{
			Account:          p.Account,
			Symbol:           p.Symbol,
			SecType:          p.SecType,
			Expiration:       p.Expiration,
			Strike:           p.Strike,
			Right:            p.Right,
			Quantity:         p.Quantity,
			AvgCost:          p.AvgCost,
			Multiplier:       p.Multiplier,
			Currency:         p.Currency,
			LastPrice:        p.LastPrice,
			MarketValue:      p.MarketValue,
			UnrealizedPnL:    p.UnrealizedPnL,
			UnrealizedPnLPct: p.UnrealizedPnLPct,
		})
	}
	return writeJSON(w, out)
}

type tradesOutput struct {
	Executions []executionOutput `json:"executions"`
	Source     string            `json:"source,omitempty"`
}

type executionOutput struct {
	ExecID     string    `json:"exec_id"`
	Time       time.Time `json:"time"`
	Account    string    `json:"account"`
	Symbol     string    `json:"symbol"`
	SecType    string    `json:"sec_type"`
	Expiration string    `json:"expiration,omitempty"`
	Strike     float64   `json:"strike,omitempty"`
	Right      string    `json:"right,omitempty"`
	Side       string    `json:"side"`
	Shares     float64   `json:"shares"`
	Price      float64   `json:"price"`
	AvgPrice   float64   `json:"avg_price"`
	Currency   string    `json:"currency,omitempty"`
	Exchange   string    `json:"exchange,omitempty"`
	OrderID    int64     `json:"order_id"`
	PermID     int64     `json:"perm_id"`
}

// ─── Analyze (optix analyze --format json) ────────────────────────────────────
//
// analyzeOutput is the agent-stable JSON contract for a single `optix
// analyze` run, and doubles as the per-symbol payload inside
// `--watchlist --format json` (see analyzeWatchlistEntry). It is a curated
// projection of AnalyzeStockResponse — not protojson — so the wire contract
// stays stable across proto field renames. Every value is sourced directly
// from the response; the only derived data are the same degraded/zero-value
// checks printAnalysisReport already performs for its own alternate-text
// branches (e.g. "Max Pain: N/A ...", "No strategy recommendations
// available."), re-expressed as stable machine-readable warning codes.

type analyzeOutput struct {
	Symbol       string    `json:"symbol"`
	Weeks        int       `json:"weeks"`
	ForecastDays int32     `json:"forecast_days"`
	Source       string    `json:"source,omitempty"`
	GeneratedAt  time.Time `json:"generated_at"`

	Summary    *analyzeSummaryOutput   `json:"summary,omitempty"`
	Technical  *analyzeTechnicalOutput `json:"technical,omitempty"`
	Options    *analyzeOptionsOutput   `json:"options,omitempty"`
	Outlook    *analyzeOutlookOutput   `json:"outlook,omitempty"`
	Strategies []analyzeStrategyOutput `json:"strategies,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

type analyzeSummaryOutput struct {
	Price           float64 `json:"price"`
	Change          float64 `json:"change"`
	ChangePct       float64 `json:"change_pct"`
	High52W         float64 `json:"high_52w"`
	Low52W          float64 `json:"low_52w"`
	AvgVolume20D    float64 `json:"avg_volume_20d"`
	TodayVolume     int64   `json:"today_volume"`
	PreviousClose   float64 `json:"previous_close,omitempty"`
	IsExtendedHours bool    `json:"is_extended_hours,omitempty"`
}

type analyzePriceLevelOutput struct {
	Price    float64 `json:"price"`
	Source   string  `json:"source"`
	Strength float64 `json:"strength"`
}

type analyzeTechnicalOutput struct {
	Trend            string  `json:"trend"`
	TrendScore       float64 `json:"trend_score"`
	TrendDescription string  `json:"trend_description,omitempty"`

	MA20  float64 `json:"ma_20"`
	MA50  float64 `json:"ma_50"`
	MA200 float64 `json:"ma_200"`

	RSI14         float64 `json:"rsi_14"`
	MACD          float64 `json:"macd"`
	MACDSignal    float64 `json:"macd_signal"`
	MACDHistogram float64 `json:"macd_histogram"`

	BollingerUpper float64 `json:"bollinger_upper,omitempty"`
	BollingerMid   float64 `json:"bollinger_mid,omitempty"`
	BollingerLower float64 `json:"bollinger_lower,omitempty"`

	SupportLevels    []analyzePriceLevelOutput `json:"support_levels,omitempty"`
	ResistanceLevels []analyzePriceLevelOutput `json:"resistance_levels,omitempty"`
}

type analyzeOIClusterOutput struct {
	Strike       float64 `json:"strike"`
	OptionType   string  `json:"option_type"`
	OpenInterest int32   `json:"open_interest"`
	Significance string  `json:"significance"`
}

type analyzeOptionsOutput struct {
	IVCurrent     float64 `json:"iv_current"`
	IVRank        float64 `json:"iv_rank"`
	IVPercentile  float64 `json:"iv_percentile"`
	IVEnvironment string  `json:"iv_environment,omitempty"`
	IVSkew        float64 `json:"iv_skew,omitempty"`

	// MaxPainAvailable mirrors the same `o.MaxPain > 0` check
	// printAnalysisReport uses to decide between printing the Max Pain price
	// and "N/A (rerun with --with-oi ...)". MaxPain/MaxPainExpiry are only
	// populated when true.
	MaxPainAvailable bool    `json:"max_pain_available"`
	MaxPain          float64 `json:"max_pain,omitempty"`
	MaxPainExpiry    string  `json:"max_pain_expiry,omitempty"`
	ExpiryRequested  bool    `json:"expiry_requested,omitempty"`

	PCRVolume float64 `json:"pcr_volume"`
	PCROI     float64 `json:"pcr_oi"`

	OIClusters []analyzeOIClusterOutput `json:"oi_clusters,omitempty"`

	EarningsBeforeExpiry bool   `json:"earnings_before_expiry,omitempty"`
	NextEarningsDate     string `json:"next_earnings_date,omitempty"`
}

type analyzeOutlookOutput struct {
	Direction  string  `json:"direction"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale,omitempty"`

	RangeLow1S  float64 `json:"range_low_1s"`
	RangeHigh1S float64 `json:"range_high_1s"`
	RangeLow2S  float64 `json:"range_low_2s"`
	RangeHigh2S float64 `json:"range_high_2s"`

	ForecastDays int32    `json:"forecast_days"`
	RiskEvents   []string `json:"risk_events,omitempty"`
}

type analyzeStrategyLegOutput struct {
	Direction  string  `json:"direction"` // "buy" | "sell", derived from the quantity sign (see printStrategy)
	OptionType string  `json:"option_type"`
	Strike     float64 `json:"strike"`
	Expiration string  `json:"expiration"`
	Quantity   int32   `json:"quantity"`
	Premium    float64 `json:"premium"`
}

type analyzeStrategyOutput struct {
	Rank         int     `json:"rank"`
	StrategyName string  `json:"strategy_name"`
	StrategyType string  `json:"strategy_type"`
	Score        float64 `json:"score"`

	Legs []analyzeStrategyLegOutput `json:"legs,omitempty"`

	NetCredit           float64 `json:"net_credit"`
	MaxProfit           float64 `json:"max_profit"`
	MaxLoss             float64 `json:"max_loss"`
	RiskRewardRatio     float64 `json:"risk_reward_ratio"`
	ProbabilityOfProfit float64 `json:"probability_of_profit"`
	BreakevenPrice      float64 `json:"breakeven_price"`
	MarginRequired      float64 `json:"margin_required"`

	Rationale    string   `json:"rationale,omitempty"`
	RiskWarnings []string `json:"risk_warnings,omitempty"`
}

// analyzeWatchlistOutput is the agent-stable JSON contract for `optix
// analyze --watchlist --format json`: one stable envelope wrapping a
// per-symbol results array. Results is never null (empty slice when the
// watchlist itself is empty) so callers can always range over it.
type analyzeWatchlistOutput struct {
	Weeks        int                     `json:"weeks"`
	ForecastDays int32                   `json:"forecast_days"`
	Source       string                  `json:"source,omitempty"`
	GeneratedAt  time.Time               `json:"generated_at"`
	Results      []analyzeWatchlistEntry `json:"results"`
}

// analyzeWatchlistEntry is a single --watchlist result: either a populated
// Analysis on success, or an Error on a per-symbol fetch/analyze failure.
// A per-symbol failure never aborts the batch — see runWatchlistAnalysis.
type analyzeWatchlistEntry struct {
	Symbol   string         `json:"symbol"`
	Error    string         `json:"error,omitempty"`
	Analysis *analyzeOutput `json:"analysis,omitempty"`
}

// analyzeOptionTypeLabel mirrors the CALL/PUT labeling printAnalysisReport
// and printStrategy already compute inline for OI clusters and strategy
// legs.
func analyzeOptionTypeLabel(t marketdatav1.OptionType) string {
	if t == marketdatav1.OptionType_OPTION_TYPE_PUT {
		return "PUT"
	}
	return "CALL"
}

func analyzePriceLevelsOutput(levels []*analysisv1.PriceLevel) []analyzePriceLevelOutput {
	if len(levels) == 0 {
		return nil
	}
	out := make([]analyzePriceLevelOutput, 0, len(levels))
	for _, l := range levels {
		if l == nil {
			continue
		}
		out = append(out, analyzePriceLevelOutput{Price: l.Price, Source: l.Source, Strength: l.Strength})
	}
	return out
}

func analyzeStrategyLegsOutput(leg *analysisv1.StrategyLeg) analyzeStrategyLegOutput {
	direction := "buy"
	if leg.Quantity < 0 {
		direction = "sell"
	}
	return analyzeStrategyLegOutput{
		Direction:  direction,
		OptionType: analyzeOptionTypeLabel(leg.OptionType),
		Strike:     leg.Strike,
		Expiration: leg.Expiration,
		Quantity:   leg.Quantity,
		Premium:    leg.Premium,
	}
}

// buildAnalyzeStrategyOutput projects one StrategyRecommendation, matching
// the numbering printAnalysisReport assigns via printStrategy(i+1, ...).
func buildAnalyzeStrategyOutput(rank int, strat *analysisv1.StrategyRecommendation) analyzeStrategyOutput {
	out := analyzeStrategyOutput{
		Rank:                rank,
		StrategyName:        strat.StrategyName,
		StrategyType:        strat.StrategyType,
		Score:               strat.Score,
		NetCredit:           strat.NetCredit,
		MaxProfit:           strat.MaxProfit,
		MaxLoss:             strat.MaxLoss,
		RiskRewardRatio:     strat.RiskRewardRatio,
		ProbabilityOfProfit: strat.ProbabilityOfProfit,
		BreakevenPrice:      strat.BreakevenPrice,
		MarginRequired:      strat.MarginRequired,
		Rationale:           strat.Rationale,
		RiskWarnings:        strat.RiskWarnings,
	}
	if len(strat.Legs) > 0 {
		out.Legs = make([]analyzeStrategyLegOutput, 0, len(strat.Legs))
		for _, leg := range strat.Legs {
			if leg == nil {
				continue
			}
			out.Legs = append(out.Legs, analyzeStrategyLegsOutput(leg))
		}
	}
	return out
}

// buildAnalyzeOutput projects an AnalyzeStockResponse into the curated JSON
// contract shared by single-symbol and --watchlist analyze runs. A nil resp
// mirrors printAnalysisReport's own defensive "No analysis data returned."
// nil check.
func buildAnalyzeOutput(resp *analysisv1.AnalyzeStockResponse, symbol string, weeks int, forecastDays int32, source string, userRequestedExpiry bool) *analyzeOutput {
	out := &analyzeOutput{
		Symbol:       symbol,
		Weeks:        weeks,
		ForecastDays: forecastDays,
		Source:       source,
		GeneratedAt:  time.Now().UTC(),
	}
	if resp == nil {
		out.Warnings = append(out.Warnings, "no_analysis_data")
		return out
	}

	if s := resp.Summary; s != nil {
		out.Summary = &analyzeSummaryOutput{
			Price:           s.Price,
			Change:          s.Change,
			ChangePct:       s.ChangePct,
			High52W:         s.High_52W,
			Low52W:          s.Low_52W,
			AvgVolume20D:    s.AvgVolume_20D,
			TodayVolume:     s.TodayVolume,
			PreviousClose:   s.PreviousClose,
			IsExtendedHours: s.IsExtendedHours,
		}
	} else {
		out.Warnings = append(out.Warnings, "summary_unavailable")
	}

	if t := resp.Technical; t != nil {
		out.Technical = &analyzeTechnicalOutput{
			Trend:            t.Trend,
			TrendScore:       t.TrendScore,
			TrendDescription: t.TrendDescription,
			MA20:             t.Ma_20,
			MA50:             t.Ma_50,
			MA200:            t.Ma_200,
			RSI14:            t.Rsi_14,
			MACD:             t.Macd,
			MACDSignal:       t.MacdSignal,
			MACDHistogram:    t.MacdHistogram,
			BollingerUpper:   t.BollingerUpper,
			BollingerMid:     t.BollingerMid,
			BollingerLower:   t.BollingerLower,
			SupportLevels:    analyzePriceLevelsOutput(t.SupportLevels),
			ResistanceLevels: analyzePriceLevelsOutput(t.ResistanceLevels),
		}
	} else {
		out.Warnings = append(out.Warnings, "technical_unavailable")
	}

	if o := resp.Options; o != nil {
		opts := &analyzeOptionsOutput{
			IVCurrent:            o.IvCurrent,
			IVRank:               o.IvRank,
			IVPercentile:         o.IvPercentile,
			IVEnvironment:        o.IvEnvironment,
			IVSkew:               o.IvSkew,
			PCRVolume:            o.PcrVolume,
			PCROI:                o.PcrOi,
			EarningsBeforeExpiry: o.EarningsBeforeExpiry,
			NextEarningsDate:     o.NextEarningsDate,
		}
		if o.MaxPain > 0 {
			opts.MaxPainAvailable = true
			opts.MaxPain = o.MaxPain
			opts.MaxPainExpiry = maxPainExpiryAnnotation(o.MaxPainExpiry)
			opts.ExpiryRequested = userRequestedExpiry
		} else {
			out.Warnings = append(out.Warnings, "max_pain_unavailable")
		}
		if len(o.OiClusters) > 0 {
			opts.OIClusters = make([]analyzeOIClusterOutput, 0, len(o.OiClusters))
			for _, cl := range o.OiClusters {
				if cl == nil {
					continue
				}
				opts.OIClusters = append(opts.OIClusters, analyzeOIClusterOutput{
					Strike:       cl.Strike,
					OptionType:   analyzeOptionTypeLabel(cl.OptionType),
					OpenInterest: cl.OpenInterest,
					Significance: cl.Significance,
				})
			}
		}
		out.Options = opts
	} else {
		out.Warnings = append(out.Warnings, "options_unavailable")
	}

	if ol := resp.Outlook; ol != nil {
		out.Outlook = &analyzeOutlookOutput{
			Direction:    ol.Direction,
			Confidence:   ol.Confidence,
			Rationale:    ol.Rationale,
			RangeLow1S:   ol.RangeLow_1S,
			RangeHigh1S:  ol.RangeHigh_1S,
			RangeLow2S:   ol.RangeLow_2S,
			RangeHigh2S:  ol.RangeHigh_2S,
			ForecastDays: ol.ForecastDays,
			RiskEvents:   ol.RiskEvents,
		}
	} else {
		out.Warnings = append(out.Warnings, "outlook_unavailable")
	}

	if len(resp.Strategies) == 0 {
		out.Warnings = append(out.Warnings, "no_strategy_recommendations")
	} else {
		out.Strategies = make([]analyzeStrategyOutput, 0, len(resp.Strategies))
		for i, strat := range resp.Strategies {
			if strat == nil {
				continue
			}
			out.Strategies = append(out.Strategies, buildAnalyzeStrategyOutput(i+1, strat))
		}
	}

	return out
}

func renderTradesJSON(w io.Writer, executions []model.Execution, source string) error {
	out := tradesOutput{
		Executions: make([]executionOutput, 0, len(executions)),
		Source:     source,
	}
	for _, e := range executions {
		out.Executions = append(out.Executions, executionOutput{
			ExecID:     e.ExecID,
			Time:       e.Time,
			Account:    e.Account,
			Symbol:     e.Symbol,
			SecType:    e.SecType,
			Expiration: e.Expiration,
			Strike:     e.Strike,
			Right:      e.Right,
			Side:       e.Side,
			Shares:     e.Shares,
			Price:      e.Price,
			AvgPrice:   e.AvgPrice,
			Currency:   e.Currency,
			Exchange:   e.Exchange,
			OrderID:    e.OrderID,
			PermID:     e.PermID,
		})
	}
	return writeJSON(w, out)
}
