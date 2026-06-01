package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

func newPositionsCmd() *cobra.Command {
	var typeFilter string
	var format string

	cmd := &cobra.Command{
		Use:   "positions",
		Short: "Show current account holdings (stocks + options) with P&L",
		Long: `Show the current account holdings snapshot from IBKR.

Stocks and options are shown in separate sections. Stock P&L uses the live
quote; option P&L uses the option's mark price (requires an OPRA market-data
subscription — without it the option's mark/P&L columns show "—").

Requires IBKR TWS/Gateway — account data is not available via Yahoo Finance.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)
			typeFilter = strings.ToLower(typeFilter)
			if typeFilter != "" && typeFilter != "stk" && typeFilter != "opt" {
				return fmt.Errorf("invalid --type %q: use stk or opt", typeFilter)
			}

			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 4,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			if format == "json" {
				fmt.Fprintln(os.Stderr, b.SourceBanner())
			} else {
				fmt.Println(b.SourceBanner())
			}

			market := server.NewMarketDataService(b, store)
			acct := server.NewAccountService(b, market)

			positions, err := acct.GetPositions(ctx)
			if err != nil {
				if errors.Is(err, broker.ErrAccountNotSupported) {
					return cliExit(fmt.Errorf("账户数据需要 IBKR 连接，当前已回退到 Yahoo Finance（无账户接口）。请确认 TWS/Gateway 在运行"), exitIBKRUnreachable)
				}
				return cliExit(fmt.Errorf("get positions: %w", err), exitIBKRUnreachable)
			}

			var stocks, options []model.Position
			for _, p := range positions {
				if p.IsOption() {
					options = append(options, p)
				} else {
					stocks = append(stocks, p)
				}
			}

			showStocks := typeFilter == "" || typeFilter == "stk"
			showOptions := typeFilter == "" || typeFilter == "opt"

			if format == "json" {
				selected := make([]model.Position, 0, len(positions))
				if showStocks {
					selected = append(selected, stocks...)
				}
				if showOptions {
					selected = append(selected, options...)
				}
				return renderPositionsJSON(os.Stdout, selected, b.SourceName())
			}

			if len(positions) == 0 {
				fmt.Println("No open positions.")
				return nil
			}

			if showStocks {
				fmt.Println("\n═══ STOCK POSITIONS ═══")
				if len(stocks) == 0 {
					fmt.Println("  (none)")
				} else {
					fmt.Printf("%-10s %-8s %8s %10s %10s %14s %14s %8s\n",
						"Account", "Symbol", "Qty", "AvgCost", "Last", "MktValue", "UnrealPnL", "%")
					for _, p := range stocks {
						printStockPosition(p)
					}
				}
			}

			if showOptions {
				fmt.Println("\n═══ OPTION POSITIONS ═══")
				if len(options) == 0 {
					fmt.Println("  (none)")
				} else {
					fmt.Printf("%-10s %-8s %-10s %8s %-2s %6s %10s %8s %14s %14s %8s\n",
						"Account", "Symbol", "Expiry", "Strike", "R", "Qty", "AvgCost", "Mark", "MktValue", "UnrealPnL", "%")
					for _, p := range options {
						printOptionPosition(p)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "Filter by position type: stk | opt (default: both)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

// printStockPosition renders one stock row. Mark-derived columns show "—" when
// the live quote was unavailable (LastPrice left zero by AccountService).
func printStockPosition(p model.Position) {
	last, mv, pnl, pct := "—", "—", "—", "—"
	if p.LastPrice > 0 {
		last = fmt.Sprintf("%.2f", p.LastPrice)
		mv = fmt.Sprintf("%.2f", p.MarketValue)
		pnl = fmt.Sprintf("%+.2f", p.UnrealizedPnL)
		pct = fmt.Sprintf("%+.1f%%", p.UnrealizedPnLPct)
	}
	fmt.Printf("%-10s %-8s %8.0f %10.2f %10s %14s %14s %8s\n",
		p.Account, p.Symbol, p.Quantity, p.AvgCost, last, mv, pnl, pct)
}

// printOptionPosition renders one option row. Identity + cost columns always
// render; mark-derived columns show "—" when the option mark was unavailable
// (e.g. no OPRA subscription).
func printOptionPosition(p model.Position) {
	expiry := p.Expiration
	if len(expiry) == 8 {
		expiry = expiry[:4] + "-" + expiry[4:6] + "-" + expiry[6:]
	}
	mark, mv, pnl, pct := "—", "—", "—", "—"
	if p.LastPrice > 0 {
		mark = fmt.Sprintf("%.2f", p.LastPrice)
		mv = fmt.Sprintf("%.2f", p.MarketValue)
		pnl = fmt.Sprintf("%+.2f", p.UnrealizedPnL)
		pct = fmt.Sprintf("%+.1f%%", p.UnrealizedPnLPct)
	}
	fmt.Printf("%-10s %-8s %-10s %8.2f %-2s %6.0f %10.2f %8s %14s %14s %8s\n",
		p.Account, p.Symbol, expiry, p.Strike, p.Right, p.Quantity, p.AvgCost, mark, mv, pnl, pct)
}
