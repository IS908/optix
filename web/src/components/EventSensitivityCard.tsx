import { usePoll } from '../api/usePoll'
import type { EventSensitivityDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'

function score(v: number) {
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}`
}

function tone(v: number) {
  if (v > 0.25) return 'text-emerald-400'
  if (v < -0.25) return 'text-red-400'
  return 'text-zinc-500'
}

export function EventSensitivityCard() {
  const { data } = usePoll<EventSensitivityDTO>('/api/intel/event/sensitivity', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="event-sensitivity-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">资产敏感度矩阵</h3>
      <div className="mt-3 space-y-2">
        <div className="grid grid-cols-[56px_repeat(3,1fr)] gap-2 text-[10px] uppercase tracking-normal text-zinc-600">
          <span>asset</span>
          <span className="text-right">risk</span>
          <span className="text-right">rates</span>
          <span className="text-right">dollar</span>
        </div>
        {data.rows.map((row) => (
          <div key={row.id} className="grid grid-cols-[56px_repeat(3,1fr)] gap-2 text-xs">
            <span className="font-medium text-zinc-300">{row.id}</span>
            <span className={`text-right tabular-nums ${tone(row.risk_on)}`}>{score(row.risk_on)}</span>
            <span className={`text-right tabular-nums ${tone(row.rates_up)}`}>{score(row.rates_up)}</span>
            <span className={`text-right tabular-nums ${tone(row.dollar_up)}`}>{score(row.dollar_up)}</span>
          </div>
        ))}
      </div>
      <div className="mt-3 text-[10px] text-zinc-600">{data.source}</div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
