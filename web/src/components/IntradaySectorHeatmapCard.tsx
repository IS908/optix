import { usePoll } from '../api/usePoll'
import type { IntradaySectorHeatmapDTO, IntradaySectorHeatmapRow } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

function pct(value: number) {
  return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`
}

function HeatmapRow({ row }: { row: IntradaySectorHeatmapRow }) {
  const tone = row.avg_pct >= 0 ? 'text-emerald-400' : 'text-red-400'
  const barTone = row.avg_pct >= 0 ? 'bg-emerald-500/60' : 'bg-red-500/60'
  const width = `${Math.min(100, Math.max(8, Math.abs(row.avg_pct) * 8))}%`
  return (
    <div className="space-y-1.5 text-xs">
      <div className="grid grid-cols-[minmax(0,1fr)_4.5rem] items-center gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium text-zinc-300">{row.sector_label}</div>
          <div className="truncate text-[10px] text-zinc-600">
            Top {row.top_symbol || '--'} · {row.gainers}/{row.sample_n} up
          </div>
        </div>
        <div className={`text-right tabular-nums ${tone}`}>{pct(row.avg_pct)}</div>
      </div>
      <div className="h-1.5 rounded bg-zinc-800">
        <div className={`h-1.5 rounded ${barTone}`} style={{ width }} />
      </div>
    </div>
  )
}

export function IntradaySectorHeatmapCard() {
  const { data } = usePoll<IntradaySectorHeatmapDTO>('/api/intel/intraday/sector-heatmap', 30_000)

  if (!data) {
    return <LoadingCard title="板块热力" testId="intraday-sector-heatmap-loading" />
  }

  const rows = data.rows ?? []
  const sourceLabel = [data.source, data.basis].filter(Boolean).join(' · ')

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">板块热力</h3>
        {sourceLabel && <span className="whitespace-nowrap text-[10px] text-zinc-600">{sourceLabel}</span>}
      </div>
      <div className="mt-1 truncate text-[10px] text-zinc-600">{data.sector_source}</div>
      {rows.length === 0 ? (
        <div className="mt-3 text-xs text-zinc-600">板块暂无可用样本</div>
      ) : (
        <div className="mt-3 space-y-3">
          {rows.slice(0, 6).map((row) => (
            <HeatmapRow key={row.sector_id} row={row} />
          ))}
        </div>
      )}
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
