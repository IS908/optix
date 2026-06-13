package intel

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

// PulseProvider 是 handlers 对 PulseService 的最小依赖面（测试注入 fake）。
type PulseProvider interface {
	Snapshot(ctx context.Context, view marketdata.View, withSpark bool) (*marketdata.PulseSnapshot, error)
}

// Handlers 承载 /api/intel/* 端点。Pulse 为 nil 时 pulse 端点回 503
// （state 端点纯本地计算，永远可用）。Now 为 nil 时用 time.Now（测试注入）。
type Handlers struct {
	Pulse   PulseProvider
	Journal *IntelJournal // nil → /api/intel/journal 回 503
	Now     func() time.Time
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
}

type stateJSON struct {
	Now            time.Time `json:"now"`
	Phase          string    `json:"phase"`
	View           string    `json:"view"`
	IsTradingDay   bool      `json:"is_trading_day"`
	EarlyClose     bool      `json:"early_close"`
	NextTransition time.Time `json:"next_transition"`
	NextPhase      string    `json:"next_phase"`
	CalendarStale  bool      `json:"calendar_stale"`
}

func (h *Handlers) handleState(w http.ResponseWriter, _ *http.Request) {
	now := h.now().In(nyLoc)
	phase := PhaseAt(now)
	nt, np := NextTransition(now)
	_, early := earlyCloseAt(now)
	writeJSON(w, stateJSON{
		Now:            now,
		Phase:          string(phase),
		View:           string(ViewFor(phase)),
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
		view = ViewFor(PhaseAt(h.now()))
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

// ─── Pulse DTO（从 cli/pulse.go 提升；CLI 与 HTTP 共用，契约编译期钉死）──────

// AssetDTO 是 pulse JSON 契约的资产条目。Price 用指针：pct-only 代理输出 null。
type AssetDTO struct {
	ID        string    `json:"id"`
	Class     string    `json:"class"`
	Label     string    `json:"label"`
	Price     *float64  `json:"price"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"change_pct"`
	Basis     string    `json:"basis"`
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
			Basis: string(a.Basis), AsOf: a.AsOf,
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
