import { usePoll } from '../api/usePoll'
import type { ShockAnalogsDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'

export function ShockAnalogsCard() {
  const { data } = usePoll<ShockAnalogsDTO>('/api/intel/shock/analogs', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="shock-analogs-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">历史类比</h3>
        <span className="text-[10px] text-zinc-600">{data.source}</span>
      </div>
      <div className="mt-3 space-y-2">
        {data.rows.slice(0, 4).map((row) => (
          <div key={row.name} className="grid grid-cols-[1fr_44px] items-center gap-3 text-xs">
            <div className="min-w-0">
              <div className="truncate font-medium text-zinc-300">{row.name}</div>
              <div className="flex gap-1 text-[10px] text-zinc-600">
                <span>{row.category}</span>
                <span className="text-zinc-700">·</span>
                <span>{row.next_session_bias}</span>
              </div>
            </div>
            <div className="text-right tabular-nums text-zinc-400">{(row.similarity * 100).toFixed(0)}%</div>
          </div>
        ))}
      </div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
