package scanjournal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

type RegisterPayload struct {
	ScanDate     string           `json:"scan_date"`
	SymbolSource string           `json:"symbol_source"`
	Candidates   []CandidateInput `json:"candidates"`
}

type CandidateInput struct {
	Rank               int      `json:"rank"`
	Symbol             string   `json:"symbol"`
	Expiry             string   `json:"expiry"`
	DTE                int      `json:"dte"`
	Strike             float64  `json:"strike"`
	Spot               float64  `json:"spot"`
	Bid                float64  `json:"bid"`
	Ask                *float64 `json:"ask,omitempty"`
	Mid                *float64 `json:"mid,omitempty"`
	IV                 *float64 `json:"iv,omitempty"`
	Delta              *float64 `json:"delta,omitempty"`
	OI                 *int     `json:"oi,omitempty"`
	Volume             *int     `json:"volume,omitempty"`
	CushionPct         float64  `json:"cushion_pct"`
	PremiumYieldPct    float64  `json:"premium_yield_pct"`
	AnnualizedYieldPct float64  `json:"annualized_yield_pct"`
	Score              float64  `json:"score"`
	IBKRBid            *float64 `json:"ibkr_bid,omitempty"`
	IBKRAsk            *float64 `json:"ibkr_ask,omitempty"`
	IBKROptionIV       *float64 `json:"ibkr_option_iv,omitempty"`
	IBKROptionDelta    *float64 `json:"ibkr_option_delta,omitempty"`
}

type RegisterResult struct {
	Registered int    `json:"registered"`
	Skipped    int    `json:"skipped"`
	ScanDate   string `json:"scan_date"`
}

// CandidateID 确定性派生（与 UNIQUE 约束同锚点，可读且天然幂等）。
func CandidateID(scanDate, symbol, expiry string, strike float64) string {
	return fmt.Sprintf("%s:%s:%s:%s", scanDate, symbol, expiry, strconv.FormatFloat(strike, 'f', -1, 64))
}

const dateLayout = "2006-01-02"

// Register 校验整批（任一失败即拒绝、store 零写入），通过后单事务入库。
func (s *Service) Register(ctx context.Context, p RegisterPayload) (RegisterResult, error) {
	scanDate := strings.TrimSpace(p.ScanDate)
	if scanDate == "" {
		scanDate = s.Now().In(intelshared.NY()).Format(dateLayout)
	} else if _, err := time.Parse(dateLayout, scanDate); err != nil {
		return RegisterResult{}, fmt.Errorf("invalid scan_date %q: %w", p.ScanDate, err)
	}
	if len(p.Candidates) == 0 {
		return RegisterResult{}, fmt.Errorf("no candidates in payload")
	}
	now := s.Now().UTC()
	cands := make([]model.ScanCandidate, 0, len(p.Candidates))
	for i, in := range p.Candidates {
		sym := intelshared.NormalizeSymbol(in.Symbol)
		if sym == "" {
			return RegisterResult{}, fmt.Errorf("candidate %d: empty symbol", i+1)
		}
		if in.Rank != i+1 {
			return RegisterResult{}, fmt.Errorf("candidate %d (%s): rank %d not contiguous from 1", i+1, sym, in.Rank)
		}
		if in.Strike <= 0 || in.Spot <= 0 {
			return RegisterResult{}, fmt.Errorf("candidate %s: strike/spot must be > 0", sym)
		}
		if in.Bid <= 0 {
			return RegisterResult{}, fmt.Errorf("candidate %s: bid must be > 0", sym)
		}
		if _, err := time.Parse(dateLayout, in.Expiry); err != nil {
			return RegisterResult{}, fmt.Errorf("candidate %s: invalid expiry %q", sym, in.Expiry)
		}
		if in.Expiry <= scanDate {
			return RegisterResult{}, fmt.Errorf("candidate %s: expiry %s must be after scan_date %s", sym, in.Expiry, scanDate)
		}
		cands = append(cands, model.ScanCandidate{
			CandidateID: CandidateID(scanDate, sym, in.Expiry, in.Strike),
			ScanDate:    scanDate, Rank: in.Rank, Symbol: sym, Right: "P",
			Expiry: in.Expiry, DTE: in.DTE, Strike: in.Strike, Spot: in.Spot, Bid: in.Bid,
			Ask: in.Ask, Mid: in.Mid, IV: in.IV, Delta: in.Delta, OI: in.OI, Volume: in.Volume,
			CushionPct: in.CushionPct, PremiumYieldPct: in.PremiumYieldPct,
			AnnualizedYieldPct: in.AnnualizedYieldPct, Score: in.Score,
			IBKRBid: in.IBKRBid, IBKRAsk: in.IBKRAsk,
			IBKROptionIV: in.IBKROptionIV, IBKROptionDelta: in.IBKROptionDelta,
			SymbolSource: strings.TrimSpace(p.SymbolSource), CreatedAt: now,
		})
	}
	registered, skipped, err := s.Store.InsertScanCandidates(ctx, cands)
	if err != nil {
		return RegisterResult{}, fmt.Errorf("insert candidates: %w", err)
	}
	return RegisterResult{Registered: registered, Skipped: skipped, ScanDate: scanDate}, nil
}
