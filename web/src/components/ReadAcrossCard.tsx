import { usePoll } from '../api/usePoll'
import type { ReadAcrossDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

export function ReadAcrossCard() {
  const { data } = usePoll<ReadAcrossDTO>('/api/intel/postclose/read-across', 60_000)

  if (!data) {
    return <LoadingCard title="Read-across 传导" testId="read-across-loading" />
  }
  const edges = data.edges ?? []

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">Read-across 传导</h3>
        <span className="text-[10px] text-zinc-600">{data.sector_source}</span>
      </div>
      {edges.length === 0 ? (
        <div className="mt-3 text-xs text-zinc-600">未触发同板块传导边</div>
      ) : (
        <div className="mt-3 space-y-2">
          {edges.slice(0, 8).map((edge) => (
            <div key={`${edge.driver}-${edge.peer}`} className="text-xs">
              <div className="flex items-center justify-between gap-3">
                <span className="font-medium text-zinc-200">
                  {edge.driver} → {edge.peer}
                </span>
                <span className={edge.direction === 'positive' ? 'text-emerald-400' : 'text-red-400'}>
                  {(edge.signal_strength * 100).toFixed(0)}%
                </span>
              </div>
              <div className="mt-0.5 flex items-center justify-between gap-3 text-[10px] text-zinc-500">
                <span>{edge.sector_label}</span>
                <span>{edge.lag}</span>
              </div>
            </div>
          ))}
        </div>
      )}
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
