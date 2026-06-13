import { usePoll } from '../api/usePoll'
import type { EventRatePathRow, EventRatesDTO } from '../api/types'

function pct(v: number) {
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}

function num(v: number) {
  if (Math.abs(v) >= 1000) return v.toFixed(0)
  return v.toFixed(2)
}

function RateRow({ row }: { row: EventRatePathRow }) {
  if (row.missing) {
    return (
      <div className="grid grid-cols-[70px_1fr_92px] items-center gap-3 text-xs text-zinc-600">
        <span className="font-medium text-zinc-400">{row.id}</span>
        <span>{row.note ?? 'missing'}</span>
        <span className="text-right">--</span>
      </div>
    )
  }
  const tone = row.change_pct >= 0 ? 'text-emerald-400' : 'text-red-400'
  return (
    <div className="grid grid-cols-[70px_1fr_92px] items-center gap-3 text-xs">
      <div>
        <div className="font-medium text-zinc-300">{row.id}</div>
        <div className="text-[10px] text-zinc-600">{row.basis}</div>
      </div>
      <div className="min-w-0 text-zinc-500">
        <span className="text-zinc-400">{num(row.current)}</span>
        <span className="mx-1 text-zinc-700">vs</span>
        <span>{num(row.pre_event)}</span>
      </div>
      <div className={`text-right tabular-nums ${tone}`}>{pct(row.change_pct)}</div>
    </div>
  )
}

export function EventRatesCard() {
  const { data } = usePoll<EventRatesDTO>('/api/intel/event/rates', 60_000)

  if (!data) {
    return <div className="h-44 animate-pulse rounded-lg bg-zinc-900" data-testid="event-rates-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium text-zinc-300">利率路径定价</h3>
          <div className="mt-1 text-[10px] text-zinc-600">{data.source}</div>
        </div>
        <div className="rounded border border-zinc-800 px-2 py-1 text-[10px] text-zinc-500">{data.rows.length} assets</div>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-2">
        {data.rows.map((row) => (
          <RateRow key={row.id} row={row} />
        ))}
      </div>
    </div>
  )
}
