import { usePoll } from '../api/usePoll'
import type { ShockFingerprintDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'

export function ShockFingerprintCard() {
  const { data } = usePoll<ShockFingerprintDTO>('/api/intel/shock/fingerprint', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="shock-fingerprint-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">冲击指纹</h3>
        <span className="text-[10px] text-zinc-600">{data.source}</span>
      </div>
      <div className="mt-3 space-y-2">
        {data.rows.map((row) => (
          <div key={row.kind} className="grid grid-cols-[1fr_44px] items-center gap-3 text-xs">
            <div className="min-w-0">
              <div className={row.active ? 'font-medium text-zinc-300' : 'text-zinc-500'}>{row.label}</div>
              <div className="truncate text-[10px] text-zinc-600">{row.evidence.slice(0, 2).join(' · ') || row.missing.slice(0, 2).join(' missing ')}</div>
            </div>
            <div className={row.active ? 'text-right tabular-nums text-amber-300' : 'text-right tabular-nums text-zinc-500'}>{row.score.toFixed(0)}</div>
          </div>
        ))}
      </div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
