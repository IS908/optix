package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/broker/yfinance"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

// maxPainClientID is the IBKR ClientID used by `optix max-pain`. Distinct
// from quote(1), analyze(2), dashboard(3), positions(4), trades(5),
// `analyze --watchlist`(6), and journal(7). Scheduler workers start at 10
// and the web pool starts at 30.
const maxPainClientID = 8

// validateSourceFlag accepts "ibkr", "yfinance", or "auto" (case-insensitive).
func validateSourceFlag(s string) error {
	switch strings.ToLower(s) {
	case "ibkr", "yfinance", "auto":
		return nil
	}
	return fmt.Errorf("invalid --source %q: use ibkr | yfinance | auto", s)
}

func validateOutputFormat(s string) error {
	switch strings.ToLower(s) {
	case "text", "json":
		return nil
	}
	return fmt.Errorf("invalid --format %q: use text | json", s)
}

// MaxPainOutput is the agent-stable JSON contract for `optix max-pain`.
type MaxPainOutput struct {
	Symbol           string  `json:"symbol"`
	Spot             float64 `json:"spot"`
	Expiry           string  `json:"expiry"` // YYYYMMDD as returned by Python
	ExpiryWeekday    string  `json:"expiry_weekday"`
	DaysToExpiry     int     `json:"days_to_expiry"`
	MaxPain          float64 `json:"max_pain"`
	MaxPainOffsetPct float64 `json:"max_pain_offset_pct"`
	StrikesEvaluated int     `json:"strikes_evaluated"`
	TotalOI          int64   `json:"total_oi"`
	CallOI           int64   `json:"call_oi"`
	PutOI            int64   `json:"put_oi"`
	Source           string  `json:"source"`
}

func newMaxPainCmd() *cobra.Command {
	var expiry, source, format, analysisAddr string

	cmd := &cobra.Command{
		Use:   "max-pain <SYMBOL>",
		Short: "Compute Max Pain for one option expiration",
		// We render our own errors (FormatExpiryError, JSON expiry envelope) and
		// don't want cobra dumping a usage banner on every runtime failure.
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Compute the Max Pain price for one option expiration, sourcing the
option chain from IBKR or Yahoo Finance.

--expiry chooses a specific expiration (YYYY-MM-DD); without it the
broker's nearest expiry is used.

--source picks the data source: ibkr | yfinance | auto (default). auto
prefers IBKR and falls back to Yahoo Finance.

Output supports --format text|json. JSON includes a max_pain_offset_pct
field convenient for agents: (max_pain - spot) / spot × 100.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			symbol := strings.ToUpper(args[0])
			ctx := context.Background()

			if err := validateSourceFlag(source); err != nil {
				return err
			}
			source = strings.ToLower(source)
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)

			expiryCompact, err := parseExpiryFlag(expiry, time.Now())
			if err != nil {
				return err
			}

			b, banner, err := buildMaxPainBroker(ctx, source)
			if err != nil {
				return cliExit(err, exitIBKRUnreachable)
			}
			defer b.Disconnect()
			fmt.Fprintln(os.Stderr, banner)

			// Resolve "auto" to the actual broker that connected so the JSON
			// `source` field reflects reality ("ibkr" or "yfinance") per the
			// design spec.
			if source == "auto" {
				if fb, ok := b.(*broker.FallbackBroker); ok {
					if fb.UsingFallback() {
						source = "yfinance"
					} else {
						source = "ibkr"
					}
				}
			}

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open db: %w", err), exitSQLiteErr)
			}
			defer store.Close()

			mdSvc := server.NewMarketDataService(b, store)
			chain, err := mdSvc.GetOptionChainWithOI(ctx, symbol, expiryCompact)
			if err != nil {
				var miss *broker.ErrExpiryNotAvailable
				if errors.As(err, &miss) {
					tmpl := fmt.Sprintf("optix max-pain %s --expiry %%s", symbol)
					return renderExpiryError(miss, tmpl, format)
				}
				return cliExit(fmt.Errorf("fetch chain for %s: %w", symbol, err), exitIBKRUnreachable)
			}

			// Spot is best-effort: a missing quote disables max_pain_offset_pct
			// but doesn't fail the command.
			quote, qErr := mdSvc.GetQuote(ctx, symbol)
			spot := 0.0
			if qErr == nil && quote != nil {
				spot = quote.Last
			}

			// Compute Max Pain via Python analysis engine.
			ac, err := analysis.NewClient(analysisAddr)
			if err != nil {
				return cliExit(fmt.Errorf("connect analysis engine at %s: %w", analysisAddr, err), exitGenericErr)
			}
			defer ac.Close()

			protoChain := buildProtoChainForMaxPain(chain)
			mp, expBack, err := ac.GetMaxPain(ctx, symbol, protoChain)
			if err != nil {
				return cliExit(fmt.Errorf("compute max pain: %w", err), exitGenericErr)
			}

			result := MaxPainOutput{
				Symbol:           symbol,
				Spot:             spot,
				Expiry:           expBack,
				MaxPain:          mp,
				Source:           source,
				StrikesEvaluated: countStrikes(chain),
				CallOI:           sumOI(chain, true),
				PutOI:            sumOI(chain, false),
			}
			result.TotalOI = result.CallOI + result.PutOI
			if spot > 0 {
				result.MaxPainOffsetPct = (mp - spot) / spot * 100
			}
			// Python may return YYYYMMDD (raw IB) or YYYY-MM-DD (formatIBExpiry);
			// try both.
			if expBack != "" {
				t, perr := time.Parse("20060102", expBack)
				if perr != nil {
					t, perr = time.Parse("2006-01-02", expBack)
				}
				if perr == nil {
					result.ExpiryWeekday = t.Weekday().String()
					result.DaysToExpiry = int(time.Until(t).Hours()/24 + 0.5)
				}
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			renderMaxPainText(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&expiry, "expiry", "", "Option expiration YYYY-MM-DD (default: nearest)")
	cmd.Flags().StringVar(&source, "source", "auto", "Data source: ibkr | yfinance | auto")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", "localhost:50052", "Python analysis engine gRPC address")
	return cmd
}

// buildMaxPainBroker picks a broker implementation based on --source. The
// returned banner is a one-line description suitable for stderr.
func buildMaxPainBroker(ctx context.Context, source string) (broker.Broker, string, error) {
	switch source {
	case "ibkr":
		b := ibkr.New(ibkr.Config{Host: ibHost, Port: ibPort, ClientID: maxPainClientID})
		if err := b.Connect(ctx); err != nil {
			return nil, "", fmt.Errorf("IBKR unreachable; try --source auto or --source yfinance: %w", err)
		}
		return b, "📡 Data source: IBKR (real-time)", nil
	case "yfinance":
		b := yfinance.New(yfinance.Config{PythonBin: pythonBin})
		if err := b.Connect(ctx); err != nil {
			return nil, "", fmt.Errorf("Yahoo Finance fetch failed: %w", err)
		}
		return b, "📡 Data source: Yahoo Finance (delayed)", nil
	case "auto":
		b := factory.NewWithFallback(ibkr.Config{Host: ibHost, Port: ibPort, ClientID: maxPainClientID}, pythonBin)
		if err := b.Connect(ctx); err != nil {
			return nil, "", fmt.Errorf("both sources failed: %w", err)
		}
		return b, b.SourceBanner(), nil
	}
	return nil, "", fmt.Errorf("unreachable: validateSourceFlag should have caught %q", source)
}

// buildProtoChainForMaxPain converts the broker's model.OptionChain into the
// proto shape expected by analysisv1.MaxPainRequest. Only Strike and
// OpenInterest are needed; the Python engine ignores the rest.
func buildProtoChainForMaxPain(chain *model.OptionChain) []*marketdatav1.OptionChainExpiry {
	out := make([]*marketdatav1.OptionChainExpiry, 0, len(chain.Expirations))
	for _, e := range chain.Expirations {
		pe := &marketdatav1.OptionChainExpiry{Expiration: e.Expiration}
		for _, c := range e.Calls {
			pe.Calls = append(pe.Calls, &marketdatav1.OptionQuote{
				Strike: c.Strike, OpenInterest: c.OpenInterest,
			})
		}
		for _, p := range e.Puts {
			pe.Puts = append(pe.Puts, &marketdatav1.OptionQuote{
				Strike: p.Strike, OpenInterest: p.OpenInterest,
			})
		}
		out = append(out, pe)
	}
	return out
}

// countStrikes returns the number of distinct strike prices across all
// expirations and option types in the chain.
func countStrikes(chain *model.OptionChain) int {
	set := map[float64]struct{}{}
	for _, e := range chain.Expirations {
		for _, c := range e.Calls {
			set[c.Strike] = struct{}{}
		}
		for _, p := range e.Puts {
			set[p.Strike] = struct{}{}
		}
	}
	return len(set)
}

// sumOI totals Open Interest for either calls (calls=true) or puts across
// every expiration in the chain.
func sumOI(chain *model.OptionChain, calls bool) int64 {
	var total int64
	for _, e := range chain.Expirations {
		quotes := e.Puts
		if calls {
			quotes = e.Calls
		}
		for _, q := range quotes {
			total += int64(q.OpenInterest)
		}
	}
	return total
}

func renderMaxPainText(r MaxPainOutput) {
	fmt.Println()
	fmt.Printf("Symbol:       %s\n", r.Symbol)
	if r.Spot > 0 {
		fmt.Printf("Spot:         $%.2f\n", r.Spot)
	}
	expHuman := dashed(r.Expiry)
	if r.ExpiryWeekday != "" {
		expHuman = fmt.Sprintf("%s (%s, %d days away)", expHuman, r.ExpiryWeekday, r.DaysToExpiry)
	}
	fmt.Printf("Expiry:       %s\n", expHuman)
	if r.Spot > 0 {
		pctSign := "+"
		if r.MaxPainOffsetPct < 0 {
			pctSign = ""
		}
		fmt.Printf("Max Pain:     $%.2f  (%s%.2f%% vs spot)\n", r.MaxPain, pctSign, r.MaxPainOffsetPct)
	} else {
		fmt.Printf("Max Pain:     $%.2f\n", r.MaxPain)
	}
	fmt.Printf("Strikes:      %d evaluated\n", r.StrikesEvaluated)
	fmt.Printf("Total OI:     %d contracts (calls: %d, puts: %d)\n", r.TotalOI, r.CallOI, r.PutOI)
}

// renderExpiryError converts a broker.ErrExpiryNotAvailable into either a
// human-readable text error (text format) or a structured JSON document on
// stdout (json format). The returned error carries an empty message when
// JSON is emitted so cobra prints nothing extra.
func renderExpiryError(miss *broker.ErrExpiryNotAvailable, tmpl, format string) error {
	if format == "json" {
		type avail struct {
			Expiry   string `json:"expiry"`
			DaysAway int    `json:"days_away"`
		}
		entries := make([]avail, 0, len(miss.Available))
		reqT, _ := time.Parse("20060102", miss.Requested)
		for _, e := range miss.Available {
			t, _ := time.Parse("20060102", e)
			days := 0
			if !reqT.IsZero() && !t.IsZero() {
				days = int(t.Sub(reqT).Hours() / 24)
				if days < 0 {
					days = -days
				}
			}
			entries = append(entries, avail{Expiry: dashed(e), DaysAway: days})
		}
		// Sort by absolute distance from the requested date ascending, with a
		// chronological tiebreak so closest-first is deterministic. YYYY-MM-DD
		// lex order equals chronological order.
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].DaysAway != entries[j].DaysAway {
				return entries[i].DaysAway < entries[j].DaysAway
			}
			return entries[i].Expiry < entries[j].Expiry
		})
		var closest string
		if len(entries) > 0 {
			closest = entries[0].Expiry
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"error":      "expiry_not_available",
			"requested":  dashed(miss.Requested),
			"available":  entries,
			"suggestion": closest,
		})
		return errors.New("") // cobra prints nothing extra
	}
	return errors.New(FormatExpiryError(miss.Underlying, miss.Requested, miss.Available, tmpl))
}
