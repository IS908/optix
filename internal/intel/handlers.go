package intel

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/IS908/optix/internal/eventintel"
	"github.com/IS908/optix/internal/intraday"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/postclose"
	"github.com/IS908/optix/internal/premarket"
	"github.com/IS908/optix/internal/shockintel"
)

// PulseProvider 是 handlers 对 PulseService 的最小依赖面（测试注入 fake）。
type PulseProvider interface {
	Snapshot(ctx context.Context, view marketdata.View, withSpark bool) (*marketdata.PulseSnapshot, error)
}

// Handlers 承载 /api/intel/* 端点。Pulse 为 nil 时 pulse 端点回 503
// （state 端点纯本地计算，永远可用）。Now 为 nil 时用 time.Now（测试注入）。
type Handlers struct {
	Pulse     PulseProvider
	Journal   *IntelJournal       // nil → /api/intel/journal 回 503
	Intraday  *intraday.Service   // nil → /api/intel/intraday/* 回 503
	Premarket *premarket.Service  // nil → /api/intel/premarket/* 回 503
	Postclose *postclose.Service  // nil → /api/intel/postclose/* 回 503
	Event     *eventintel.Service // nil → /api/intel/event/* 回 503
	Shock     *shockintel.Service // nil → /api/intel/shock/* 回 503
	Watchlist func(ctx context.Context) ([]string, error)
	Now       func() time.Time

	shockOverrideMu    sync.Mutex
	shockOverrideCache *shockOverrideCacheEntry
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Register 把 intel 路由挂到 mux（Go 1.22 方法+路径模式）。
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/intel/state", h.handleState)
	mux.HandleFunc("GET /api/intel/pulse", h.handlePulse)
	mux.HandleFunc("GET /api/intel/journal", h.handleJournal)
	mux.HandleFunc("GET /api/intel/intraday/movers", h.handleIntradayMovers)
	mux.HandleFunc("GET /api/intel/intraday/sector-heatmap", h.handleIntradaySectorHeatmap)
	mux.HandleFunc("GET /api/intel/premarket/overnight", h.handlePMOvernight)
	mux.HandleFunc("GET /api/intel/premarket/gaps", h.handlePMGaps)
	mux.HandleFunc("GET /api/intel/premarket/movers", h.handlePMMovers)
	mux.HandleFunc("GET /api/intel/premarket/sentiment", h.handlePMSentiment)
	mux.HandleFunc("GET /api/intel/postclose/earnings", h.handlePCEarnings)
	mux.HandleFunc("GET /api/intel/postclose/timeline", h.handlePCTimeline)
	mux.HandleFunc("GET /api/intel/postclose/read-across", h.handlePCReadAcross)
	mux.HandleFunc("GET /api/intel/postclose/movers", h.handlePCMovers)
	mux.HandleFunc("GET /api/intel/event/rates", h.handleEventRates)
	mux.HandleFunc("GET /api/intel/event/diff", h.handleEventDiff)
	mux.HandleFunc("GET /api/intel/event/patterns", h.handleEventPatterns)
	mux.HandleFunc("GET /api/intel/event/sensitivity", h.handleEventSensitivity)
	mux.HandleFunc("GET /api/intel/shock/regime", h.handleShockRegime)
	mux.HandleFunc("GET /api/intel/shock/fingerprint", h.handleShockFingerprint)
	mux.HandleFunc("GET /api/intel/shock/analogs", h.handleShockAnalogs)
	mux.HandleFunc("GET /api/intel/shock/liquidity", h.handleShockLiquidity)
}

type stateJSON struct {
	Now            time.Time     `json:"now"`
	Phase          string        `json:"phase"`
	View           string        `json:"view"`
	BaseView       string        `json:"base_view,omitempty"`
	ViewOverride   *viewOverride `json:"view_override,omitempty"`
	IsTradingDay   bool          `json:"is_trading_day"`
	EarlyClose     bool          `json:"early_close"`
	NextTransition time.Time     `json:"next_transition"`
	NextPhase      string        `json:"next_phase"`
	CalendarStale  bool          `json:"calendar_stale"`
}

func (h *Handlers) handleState(w http.ResponseWriter, r *http.Request) {
	now := h.now().In(nyLoc)
	phase := PhaseAt(now)
	resolved := h.resolveAutoView(r.Context(), now, phase)
	nt, np := NextTransition(now)
	_, early := earlyCloseAt(now)
	writeJSON(w, stateJSON{
		Now:            now,
		Phase:          string(phase),
		View:           string(resolved.View),
		BaseView:       string(resolved.BaseView),
		ViewOverride:   resolved.Override,
		IsTradingDay:   isTradingDay(now),
		EarlyClose:     early,
		NextTransition: nt.In(nyLoc),
		NextPhase:      string(np),
		CalendarStale:  CalendarStale(now),
	})
}

func (h *Handlers) handlePulse(w http.ResponseWriter, r *http.Request) {
	if h.Pulse == nil {
		writeError(w, "pulse service unavailable", http.StatusServiceUnavailable)
		return
	}
	view := marketdata.View(r.URL.Query().Get("view"))
	inferred := false
	if view == "" {
		// HTTP auto-view: phase clock PLUS override promotion to event/shock via
		// resolveAutoView (FOMC/CPI day → event, live shock regime → shock).
		// Deliberately ASYMMETRIC with the CLI `optix pulse` auto-view, which
		// returns only phase views (premarket/intraday/postclose). Same schema,
		// different inference — a snapshot's `view` field is not portable across
		// the two surfaces when view_inferred=true. See cli/pulse.go and the
		// CLAUDE.md `handlers.go` description. (#163)
		now := h.now()
		view = h.resolveAutoView(r.Context(), now, PhaseAt(now)).View
		inferred = true
	} else if !ValidView(view) {
		writeError(w, "invalid view: use premarket|intraday|postclose|event|shock", http.StatusBadRequest)
		return
	}
	withSpark := r.URL.Query().Get("spark") == "1" // 仅字面 1 为真（spec）
	snap, err := h.Pulse.Snapshot(r.Context(), view, withSpark)
	if err != nil {
		// ValidView 已挡住非法 view；此处只剩编程错误。
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, ToPulseDTO(snap, inferred))
}

func (h *Handlers) handleJournal(w http.ResponseWriter, r *http.Request) {
	if h.Journal == nil {
		writeError(w, "intel journal unavailable", http.StatusServiceUnavailable)
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = TradingDate(h.now())
	}
	snap, err := h.Journal.ReadJournal(r.Context(), date)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, snap)
}

func (h *Handlers) handlePMOvernight(w http.ResponseWriter, r *http.Request) {
	if h.Premarket == nil {
		writeError(w, "premarket service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Premarket.Overnight(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePMGaps(w http.ResponseWriter, r *http.Request) {
	if h.Premarket == nil {
		writeError(w, "premarket service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Premarket.Gaps(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePMMovers(w http.ResponseWriter, r *http.Request) {
	if h.Premarket == nil {
		writeError(w, "premarket service unavailable", http.StatusServiceUnavailable)
		return
	}
	var watchlist []string
	if h.Watchlist != nil {
		watchlist, _ = h.Watchlist(r.Context())
	}
	dto, err := h.Premarket.Movers(r.Context(), watchlist)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePMSentiment(w http.ResponseWriter, r *http.Request) {
	if h.Premarket == nil {
		writeError(w, "premarket service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Premarket.Sentiment(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleIntradayMovers(w http.ResponseWriter, r *http.Request) {
	if h.Intraday == nil {
		writeError(w, "intraday service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Intraday.Movers(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleIntradaySectorHeatmap(w http.ResponseWriter, r *http.Request) {
	if h.Intraday == nil {
		writeError(w, "intraday service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Intraday.SectorHeatmap(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePCEarnings(w http.ResponseWriter, r *http.Request) {
	if h.Postclose == nil {
		writeError(w, "postclose service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Postclose.Earnings(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePCTimeline(w http.ResponseWriter, r *http.Request) {
	if h.Postclose == nil {
		writeError(w, "postclose service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Postclose.Timeline(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePCReadAcross(w http.ResponseWriter, r *http.Request) {
	if h.Postclose == nil {
		writeError(w, "postclose service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Postclose.ReadAcross(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handlePCMovers(w http.ResponseWriter, r *http.Request) {
	if h.Postclose == nil {
		writeError(w, "postclose service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Postclose.Movers(r.Context(), h.watchlist(r.Context()))
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleEventRates(w http.ResponseWriter, r *http.Request) {
	if h.Event == nil {
		writeError(w, "event service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Event.Rates(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleEventDiff(w http.ResponseWriter, r *http.Request) {
	if h.Event == nil {
		writeError(w, "event service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Event.Diff(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleEventPatterns(w http.ResponseWriter, r *http.Request) {
	if h.Event == nil {
		writeError(w, "event service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Event.Patterns(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleEventSensitivity(w http.ResponseWriter, r *http.Request) {
	if h.Event == nil {
		writeError(w, "event service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Event.Sensitivity(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleShockRegime(w http.ResponseWriter, r *http.Request) {
	if h.Shock == nil {
		writeError(w, "shock service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Shock.Regime(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleShockFingerprint(w http.ResponseWriter, r *http.Request) {
	if h.Shock == nil {
		writeError(w, "shock service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Shock.Fingerprint(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleShockAnalogs(w http.ResponseWriter, r *http.Request) {
	if h.Shock == nil {
		writeError(w, "shock service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Shock.Analogs(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) handleShockLiquidity(w http.ResponseWriter, r *http.Request) {
	if h.Shock == nil {
		writeError(w, "shock service unavailable", http.StatusServiceUnavailable)
		return
	}
	dto, err := h.Shock.Liquidity(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dto)
}

func (h *Handlers) watchlist(ctx context.Context) []string {
	if h.Watchlist == nil {
		return nil
	}
	watchlist, _ := h.Watchlist(ctx)
	return watchlist
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ─── Pulse DTO（从 cli/pulse.go 提升；schema 共用、契约编译期钉死；auto-inferred view 不共用，详见 handlePulse / cli/pulse.go #163）──────

// AssetDTO 是 pulse JSON 契约的资产条目。Price 用指针：pct-only 代理输出 null。
type AssetDTO struct {
	ID        string    `json:"id"`
	Class     string    `json:"class"`
	Label     string    `json:"label"`
	Price     *float64  `json:"price"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"change_pct"`
	Basis     string    `json:"basis"`
	Source    string    `json:"source"`
	BasisNote string    `json:"basis_note"`
	AsOf      time.Time `json:"as_of"`
	Spark     []float64 `json:"spark,omitempty"`
	SparkWin  string    `json:"spark_window,omitempty"`
}

// PulseDTO 是 pulse JSON 契约顶层（agent / SPA / CLI 共用）。
type PulseDTO struct {
	SnapshotAt   time.Time  `json:"snapshot_at"`
	View         string     `json:"view"`
	ViewInferred bool       `json:"view_inferred"`
	Assets       []AssetDTO `json:"assets"`
	Missing      []string   `json:"missing,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
}

// ToPulseDTO 把快照转为线上契约。Assets 永远是 []（空也不为 null）。
func ToPulseDTO(snap *marketdata.PulseSnapshot, inferred bool) PulseDTO {
	out := PulseDTO{
		SnapshotAt: snap.SnapshotAt, View: string(snap.View), ViewInferred: inferred,
		Assets:  make([]AssetDTO, 0, len(snap.Assets)),
		Missing: snap.Missing, Warnings: snap.Warnings,
	}
	for _, a := range snap.Assets {
		dto := AssetDTO{
			ID: a.Ref.ID, Class: string(a.Ref.Class), Label: a.Label,
			Change: a.Change, ChangePct: a.ChangePct,
			Basis: string(a.Basis), Source: a.Source,
			BasisNote: marketdata.BasisNote(a.Quote), AsOf: a.AsOf,
			Spark: a.Spark, SparkWin: a.SparkWindow,
		}
		if !a.PctOnly {
			p := a.Price
			dto.Price = &p
		}
		out.Assets = append(out.Assets, dto)
	}
	return out
}
