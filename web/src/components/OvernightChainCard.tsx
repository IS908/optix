import { usePoll } from '../api/usePoll'
import type { OvernightDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

function pctClass(pct: number) {
  return pct >= 0 ? 'text-emerald-400' : 'text-red-400'
}

export function OvernightChainCard() {
  const { data, stale } = usePoll<OvernightDTO>('/api/intel/premarket/overnight', 30_000)

  if (!data) {
    return <LoadingCard title="隔夜传导链" testId="overnight-loading" />
  }
  const links = data.links ?? []

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      {stale && <div className="mb-2 text-xs text-amber-400">数据延迟,正在重试</div>}
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">隔夜传导链</h3>
        <span className="text-right text-xs text-zinc-500">{data.consistency.note || '—'}</span>
      </div>

      <div className="mt-3 flex items-center gap-1 overflow-x-auto">
        {links.map((link, index) => (
          <div key={link.id} className="flex items-center gap-1">
            <div className="min-w-24 rounded border border-zinc-800 px-2 py-1 text-center">
              <div className="text-[10px] text-zinc-400">{link.label}</div>
              <div className={`text-sm tabular-nums ${pctClass(link.pct)}`}>
                {link.pct >= 0 ? '+' : ''}
                {link.pct.toFixed(2)}%
              </div>
            </div>
            {index < links.length - 1 && <span className="text-zinc-600">→</span>}
          </div>
        ))}
      </div>

      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
