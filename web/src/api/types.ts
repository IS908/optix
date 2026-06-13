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
