package intel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

const (
	maxNarrativeBytes  = 8 * 1024
	reconcileTolerance = 30 * time.Minute // 到期价取 [expiry-30min, expiry] 内最近 bar
)

// PriceSource 是 judge 抓登记价的最小依赖面（marketdata.PulseService 满足；测试注入 fake）。
type PriceSource interface {
	QuoteByID(ctx context.Context, assetID string) (marketdata.Quote, error)
}

// IntelJournal 是 intel 判断日记的 store-service（无 broker；包 *sqlite.Store）。
type IntelJournal struct {
	store *sqlite.Store
	price PriceSource
	Now   func() time.Time // 测试注入；nil → time.Now
}

func NewIntelJournal(store *sqlite.Store, price PriceSource) *IntelJournal {
	return &IntelJournal{store: store, price: price}
}

func (j *IntelJournal) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// newID 生成业务唯一 ID（判断/叙事唯一键）：nano 时间 + 单调计数。CLI 写入是单进程
// 顺序调用，无需 crypto/rand；server 只读不写，不参与生成。
var newID = func() func() string {
	var n uint64
	return func() string {
		n++
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
	}
}()

// WriteNarrative 写一条叙事条目（append-only）。校验 kind，截断 body 到 8KB。
func (j *IntelJournal) WriteNarrative(ctx context.Context, kind, body string) (model.IntelNarrative, error) {
	if !IsCheckpointKind(kind) {
		return model.IntelNarrative{}, fmt.Errorf("invalid checkpoint kind %q", kind)
	}
	now := j.now()
	if len(body) > maxNarrativeBytes {
		body = body[:maxNarrativeBytes]
	}
	n := model.IntelNarrative{
		EntryID: newID(), TradingDate: TradingDate(now), Checkpoint: kind,
		Phase: string(PhaseAt(now)), Body: body, CreatedAt: now.UTC(),
	}
	if err := j.store.InsertIntelNarrative(ctx, n); err != nil {
		return model.IntelNarrative{}, err
	}
	return n, nil
}

// JudgmentInput 是 RegisterJudgment 的入参（agent 声明的断言；价格 optix 独占，不在此）。
type JudgmentInput struct {
	AssetID          string
	Direction        string // up|down|flat
	ThresholdPct     float64
	Confidence       int
	ExpiryCheckpoint string
	Rationale        string
	Supersedes       string
}

// RegisterJudgment 登记判断：校验断言、抓登记价、算 expiry_at、写入。
func (j *IntelJournal) RegisterJudgment(ctx context.Context, in JudgmentInput) (model.IntelJudgment, error) {
	now := j.now()
	if in.Direction != "up" && in.Direction != "down" && in.Direction != "flat" {
		return model.IntelJudgment{}, fmt.Errorf("invalid direction %q (use up|down|flat)", in.Direction)
	}
	if in.Confidence < 0 || in.Confidence > 100 {
		return model.IntelJudgment{}, fmt.Errorf("confidence must be 0-100, got %d", in.Confidence)
	}
	ref, ok := marketdata.LookupAssetRef(in.AssetID)
	if !ok {
		return model.IntelJudgment{}, fmt.Errorf("unknown asset %q", in.AssetID)
	}
	tradingDate := TradingDate(now)
	expiryAt, ok := CheckpointTime(tradingDate, in.ExpiryCheckpoint)
	if !ok {
		return model.IntelJudgment{}, fmt.Errorf("invalid expiry checkpoint %q (use first_check|set_tone|reconcile)", in.ExpiryCheckpoint)
	}
	if !expiryAt.After(now) {
		return model.IntelJudgment{}, fmt.Errorf("expiry checkpoint %q (%s) is not in the future", in.ExpiryCheckpoint, expiryAt.Format("15:04 MST"))
	}
	q, err := j.price.QuoteByID(ctx, in.AssetID)
	if err != nil {
		return model.IntelJudgment{}, fmt.Errorf("capture registered price: %w", err)
	}
	jd := model.IntelJudgment{
		JudgmentID: newID(), TradingDate: tradingDate, Checkpoint: currentCheckpoint(now),
		AssetID: in.AssetID, AssetClass: string(ref.Class), Direction: in.Direction,
		ThresholdPct: in.ThresholdPct, Confidence: in.Confidence,
		ExpiryCheckpoint: in.ExpiryCheckpoint, ExpiryAt: expiryAt,
		RegisteredPrice: q.Price, RegisteredBasis: string(q.Basis),
		Rationale: strings.TrimSpace(in.Rationale), Supersedes: in.Supersedes,
		CreatedAt: now.UTC(),
	}
	if err := j.store.InsertIntelJudgment(ctx, jd); err != nil {
		return model.IntelJudgment{}, err
	}
	return jd, nil
}

// currentCheckpoint 返回 now 所处的最近已到期日程检查点 kind（登记归属）；都未到 → "premarket-open"。
func currentCheckpoint(now time.Time) string {
	et := now.In(nyLoc)
	cur := "script"
	for _, c := range DailyCheckpoints {
		due, _ := CheckpointTime(et.Format("2006-01-02"), c.Kind)
		if !et.Before(due) {
			cur = c.Kind
		}
	}
	return cur
}

// ReconcileResult 是一次结算的汇总。
type ReconcileResult struct {
	Settled int `json:"settled"`
}

// Reconcile 结算所有到期未结算判断（对账步）。到期价从 pulse_bars 取最近 bar；无价 → void。幂等。
func (j *IntelJournal) Reconcile(ctx context.Context) (ReconcileResult, error) {
	now := j.now()
	pending, err := j.store.ExpiredUnsettledJudgments(ctx, now)
	if err != nil {
		return ReconcileResult{}, err
	}
	settled := 0
	for _, jd := range pending {
		price, _, ok, _ := j.store.PulseCloseNear(ctx, jd.AssetID, jd.ExpiryAt, reconcileTolerance)
		var rec model.IntelReconciliation
		if !ok {
			rec = model.IntelReconciliation{JudgmentID: jd.JudgmentID, ExpiryPrice: 0,
				ExpiryBasis: "", Outcome: "void", DeltaPct: 0, SettledAt: now.UTC()}
		} else {
			outcome, delta := Score(jd.Direction, jd.ThresholdPct, jd.RegisteredPrice, price)
			rec = model.IntelReconciliation{JudgmentID: jd.JudgmentID, ExpiryPrice: price,
				ExpiryBasis: jd.RegisteredBasis, Outcome: outcome, DeltaPct: delta, SettledAt: now.UTC()}
		}
		if err := j.store.InsertIntelReconciliation(ctx, rec); err != nil {
			return ReconcileResult{Settled: settled}, err
		}
		settled++
	}
	return ReconcileResult{Settled: settled}, nil
}

// HitRate 是命中率统计（分母排除 push/void）。
type HitRate struct {
	Window string  `json:"window"`
	Hit    int     `json:"hit"`
	Miss   int     `json:"miss"`
	Rate   float64 `json:"rate"`
}

// JournalSnapshot 是 /api/intel/journal 与 optix intel read 的返回。
type JournalSnapshot struct {
	TradingDate  string                 `json:"trading_date"`
	Now          time.Time              `json:"now"`
	IsTradingDay bool                   `json:"is_trading_day"`
	Checkpoints  []CheckpointState      `json:"checkpoints"`
	Narratives   []model.IntelNarrative `json:"narratives"`
	Judgments    []model.IntelJudgment  `json:"judgments"`
	HitRate      HitRate                `json:"hit_rate"`
}

// ReadJournal 组装某交易日快照：检查点状态 + 每检查点最新叙事 + 判断(内联结算) + 命中率。
func (j *IntelJournal) ReadJournal(ctx context.Context, tradingDate string) (JournalSnapshot, error) {
	now := j.now()
	allNarr, err := j.store.ListIntelNarratives(ctx, tradingDate)
	if err != nil {
		return JournalSnapshot{}, err
	}
	judgments, err := j.store.ListIntelJudgments(ctx, tradingDate)
	if err != nil {
		return JournalSnapshot{}, err
	}
	ids := make([]string, 0, len(judgments))
	for _, jd := range judgments {
		ids = append(ids, jd.JudgmentID)
	}
	recs, err := j.store.ListIntelReconciliations(ctx, ids)
	if err != nil {
		return JournalSnapshot{}, err
	}
	// 内联结算 + 命中率
	hr := HitRate{Window: "today"}
	for i := range judgments {
		if r, ok := recs[judgments[i].JudgmentID]; ok {
			rc := r
			judgments[i].Reconciliation = &rc
			switch r.Outcome {
			case "hit":
				hr.Hit++
			case "miss":
				hr.Miss++
			}
		}
	}
	if d := hr.Hit + hr.Miss; d > 0 {
		hr.Rate = float64(hr.Hit) / float64(d)
	}
	// 每检查点最新叙事（allNarr 已按 created_at 升序 → 后写覆盖）
	latest := map[string]model.IntelNarrative{}
	for _, n := range allNarr {
		latest[n.Checkpoint] = n
	}
	narr := make([]model.IntelNarrative, 0, len(latest))
	for _, n := range latest {
		narr = append(narr, n)
	}
	// written 集（用于检查点状态）—— 仅当 tradingDate 是今天才反映 due/pending；
	// 历史日查询时 now 仍是当前，检查点状态以历史日时钟无意义，此处按是否有条目标 written/否则 pending。
	written := map[string]bool{}
	for _, n := range allNarr {
		written[n.Checkpoint] = true
	}
	for _, jd := range judgments {
		written[jd.Checkpoint] = true
	}
	var checkpoints []CheckpointState
	if tradingDate == TradingDate(now) {
		checkpoints = CheckpointStatus(now, written)
	} else {
		// 历史日：只表达 written/未写（不催 due）
		for _, c := range DailyCheckpoints {
			due, _ := CheckpointTime(tradingDate, c.Kind)
			st := "pending"
			if written[c.Kind] {
				st = "written"
			}
			checkpoints = append(checkpoints, CheckpointState{Kind: c.Kind, Label: c.Label, DueAt: due, Status: st})
		}
	}
	dayT, _ := time.ParseInLocation("2006-01-02", tradingDate, nyLoc)
	return JournalSnapshot{
		TradingDate: tradingDate, Now: now.In(nyLoc), IsTradingDay: isTradingDay(dayT),
		Checkpoints: checkpoints, Narratives: narr, Judgments: judgments, HitRate: hr,
	}, nil
}

// StatusSnapshot 是 optix intel status 的返回（轻量：当前节奏 + 今日命中率）。
type StatusSnapshot struct {
	Now           time.Time         `json:"now"`
	Phase         string            `json:"phase"`
	IsTradingDay  bool              `json:"is_trading_day"`
	Checkpoints   []CheckpointState `json:"checkpoints"`
	JudgmentCount int               `json:"judgment_count_today"`
	HitRate       HitRate           `json:"hit_rate"`
}

func (j *IntelJournal) Status(ctx context.Context) (StatusSnapshot, error) {
	now := j.now()
	snap, err := j.ReadJournal(ctx, TradingDate(now))
	if err != nil {
		return StatusSnapshot{}, err
	}
	return StatusSnapshot{
		Now: now.In(nyLoc), Phase: string(PhaseAt(now)), IsTradingDay: isTradingDay(now),
		Checkpoints: snap.Checkpoints, JudgmentCount: len(snap.Judgments), HitRate: snap.HitRate,
	}, nil
}
