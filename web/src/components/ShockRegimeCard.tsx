import { usePoll } from '../api/usePoll'
import type { ShockRegimeDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'

function pct(v: number) {
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}

export function ShockRegimeCard() {
  const { data } = usePoll<ShockRegimeDTO>('/api/intel/shock/regime', 45_000)

  if (!data) {
    return <div className="h-44 animate-pulse rounded-lg bg-zinc-900" data-testid="shock-regime-loading" />
  }

  const tone = data.state === 'critical' || data.state === 'shock' ? 'text-red-400' : data.state === 'watch' ? 'text-amber-300' : 'text-emerald-400'
  const scoreLine = `score ${data.score.toFixed(0)}`
  const vixLine = `VIX Δ/10 ${data.vix_change_ratio.toFixed(1)}`

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium text-zinc-300">Regime 触发器</h3>
          <div className="mt-1 text-[10px] text-zinc-600">{data.source}</div>
        </div>
        <div className="text-right">
          <div className={`text-sm font-semibold uppercase ${tone}`}>{data.state}</div>
          <div className="text-[10px] text-zinc-500">
            <span>{scoreLine}</span>
            <span className="mx-1 text-zinc-700">·</span>
            <span>{vixLine}</span>
          </div>
        </div>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-2">
        {data.confirmations.slice(0, 6).map((row) => (
          <div key={`${row.id}-${row.dimension}`} className="grid grid-cols-[56px_1fr_76px] items-center gap-2 text-xs">
            <span className="font-medium text-zinc-300">{row.id}</span>
            <span className="truncate text-zinc-500">{row.dimension}</span>
            <span className={row.change_pct >= 0 ? 'text-right tabular-nums text-emerald-400' : 'text-right tabular-nums text-red-400'}>{pct(row.change_pct)}</span>
          </div>
        ))}
      </div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
