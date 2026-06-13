// 与 internal/intel 的 Go DTO 逐字段对齐（手抄；M2 仅两个端点不上 codegen）。
export type ViewName = 'premarket' | 'intraday' | 'postclose' | 'event' | 'shock'
export type PhaseName = 'premarket' | 'intraday' | 'postclose' | 'closed'

export interface IntelState {
  now: string
  phase: PhaseName
  view: ViewName
  is_trading_day: boolean
  early_close: boolean
  next_transition: string
  next_phase: PhaseName
  calendar_stale: boolean
}

export interface PulseAsset {
  id: string
  class: string
  label: string
  price: number | null // pct-only 代理为 null
  change: number
  change_pct: number
  basis: 'realtime' | 'delayed' | 'approx' | 'frozen'
  as_of: string
  spark?: number[]
  spark_window?: string
}

export interface PulseResponse {
  snapshot_at: string
  view: ViewName
  view_inferred: boolean
  assets: PulseAsset[]
  missing?: string[]
  warnings?: string[]
}

export type CheckpointStatus = 'written' | 'due' | 'pending'

export interface CheckpointState {
  kind: string
  label: string
  due_at: string
  status: CheckpointStatus
}

export interface IntelNarrative {
  entry_id: string
  trading_date: string
  checkpoint: string
  phase: string
  body: string
  created_at: string
}

export interface IntelReconciliation {
  judgment_id: string
  expiry_price: number
  expiry_basis: string
  outcome: 'hit' | 'miss' | 'push' | 'void'
  delta_pct: number
  settled_at: string
}

export interface IntelJudgment {
  judgment_id: string
  trading_date: string
  checkpoint: string
  asset_id: string
  asset_class: string
  direction: 'up' | 'down' | 'flat'
  threshold_pct: number
  confidence: number
  expiry_checkpoint: string
  expiry_at: string
  registered_price: number
  registered_basis: string
  rationale?: string
  supersedes?: string
  created_at: string
  reconciliation?: IntelReconciliation
}

export interface HitRate {
  window: string
  hit: number
  miss: number
  rate: number
}

export interface IntelJournalSnapshot {
  trading_date: string
  now: string
  is_trading_day: boolean
  checkpoints: CheckpointState[]
  narratives: IntelNarrative[]
  judgments: IntelJudgment[]
  hit_rate: HitRate
}

export interface OvernightLink {
  id: string
  label: string
  pct: number
  basis: string
  as_of: string
}

export interface OvernightDTO {
  as_of: string
  links: OvernightLink[]
  consistency: { same_dir: number; total: number; note: string }
  warnings?: string[]
}

export interface GapStat {
  symbol: string
  direction: string
  band: string
  fill_rate: number
  sample_n: number
  lookback_days: number
  as_of: string
}

export interface GapsDTO {
  symbol: string
  implied_gap_pct: number
  direction: string
  band: string
  hist_fill_rate: number
  sample_n: number
  lookback_days: number
  by_band: GapStat[]
  as_of: string
  warnings?: string[]
}

export interface Mover {
  symbol: string
  pct: number
  vol_ratio: number
  watchlist: boolean
}

export interface MoversDTO {
  as_of: string
  universe_note: string
  gainers: Mover[]
  losers: Mover[]
  warnings?: string[]
}

export interface SentimentDTO {
  as_of: string
  pc_oi: number
  pc_vol: number
  pc_available: boolean
  vix: number
  vix3m: number
  vix_term_premium: number
  regime: string
  degraded_note: string
  warnings?: string[]
}

export interface EarningsReport {
  symbol: string
  event_time: string
  timing: string
  eps_estimate?: number
  eps_reported?: number
  eps_surprise_pct?: number
  surprise_label: string
  stale: boolean
}

export interface PostcloseEarningsDTO {
  as_of: string
  source: string
  universe_note: string
  reports: EarningsReport[]
  warnings?: string[]
}

export interface TimelineEvent {
  ts: string
  symbol?: string
  kind: string
  title: string
  detail: string
  severity: string
}

export interface PostcloseTimelineDTO {
  as_of: string
  events: TimelineEvent[]
  warnings?: string[]
}

export interface ReadAcrossEdge {
  driver: string
  peer: string
  sector_id: string
  sector_label: string
  direction: string
  confidence: number
  lag: string
  driver_after_hours_pct: number
  note: string
}

export interface ReadAcrossDTO {
  as_of: string
  sector_source: string
  edges: ReadAcrossEdge[]
  warnings?: string[]
}

export interface PostcloseMover {
  symbol: string
  regular_pct: number
  after_hours_pct: number
  combined_pct: number
  volume: number
  watchlist: boolean
}

export interface PostcloseMoversDTO {
  as_of: string
  universe_note: string
  gainers: PostcloseMover[]
  losers: PostcloseMover[]
  warnings?: string[]
}

export interface EventRatePathRow {
  id: string
  label: string
  kind: string
  source: string
  basis: string
  as_of: string
  pre_event: number
  current: number
  change: number
  change_pct: number
  missing?: boolean
  note?: string
}

export interface EventRatesDTO {
  as_of: string
  source: string
  universe_note: string
  rows: EventRatePathRow[]
  warnings?: string[]
}

export interface EventStatementSentence {
  text: string
  status: string
  hits?: string[]
}

export interface EventDiffDTO {
  as_of: string
  source: string
  prior_title: string
  current_title: string
  prior_published_at: string
  current_published_at: string
  added: EventStatementSentence[]
  removed: EventStatementSentence[]
  unchanged: EventStatementSentence[]
  hawkish_hits: number
  dovish_hits: number
  verdict: string
  warnings?: string[]
}

export interface EventPatternRow {
  id: string
  label: string
  source: string
  basis: string
  sample_n: number
  avg_event_move_pct: number
  avg_next_move_pct: number
  directional_consistency: number
  note?: string
}

export interface EventPatternsDTO {
  as_of: string
  source: string
  rows: EventPatternRow[]
  warnings?: string[]
}

export interface EventSensitivityRow {
  id: string
  label: string
  risk_on: number
  rates_up: number
  dollar_up: number
  sample_n: number
  note?: string
}

export interface EventSensitivityDTO {
  as_of: string
  source: string
  rows: EventSensitivityRow[]
  warnings?: string[]
}
