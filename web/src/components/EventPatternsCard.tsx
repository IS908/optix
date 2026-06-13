import { usePoll } from '../api/usePoll'
import type { EventPatternsDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'

function pct(v: number) {
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}

export function EventPatternsCard() {
  const { data } = usePoll<EventPatternsDTO>('/api/intel/event/patterns', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="event-patterns-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">历史事件日模式</h3>
      <div className="mt-3 space-y-2">
        {data.rows.slice(0, 6).map((row) => (
          <div key={row.id} className="grid grid-cols-[56px_46px_1fr_52px] items-center gap-2 text-xs">
            <span className="font-medium text-zinc-300">{row.id}</span>
            <span className="text-zinc-600">n={row.sample_n}</span>
            <span className="tabular-nums text-zinc-400">
              {pct(row.avg_event_move_pct)} / {pct(row.avg_next_move_pct)}
            </span>
            <span className="text-right tabular-nums text-zinc-500">{(row.directional_consistency * 100).toFixed(0)}%</span>
          </div>
        ))}
      </div>
      <div className="mt-3 text-[10px] text-zinc-600">{data.source}</div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
