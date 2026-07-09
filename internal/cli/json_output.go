package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
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
