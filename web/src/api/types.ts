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
