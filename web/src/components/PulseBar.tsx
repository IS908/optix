import type { PulseResponse } from '../api/types'

const basisBadge: Record<string, string> = {
  realtime: 'text-emerald-400',
  delayed: 'text-zinc-500',
  approx: 'text-amber-500',
  frozen: 'text-sky-500',
}

export function PulseBar({ pulse, stale }: { pulse: PulseResponse | null; stale: boolean }) {
  if (!pulse) {
    return <div className="h-20 animate-pulse rounded-lg bg-zinc-900" data-testid="pulse-loading" />
  }
  return (
    <div>
      {stale && (
        <div className="mb-2 rounded bg-amber-950 px-3 py-1 text-xs text-amber-400">数据延迟：上游拉取失败，正在退避重试</div>
      )}
      <div className="flex flex-wrap gap-3 pb-2">
        {pulse.assets.map((a) => {
          const sourceLabel = [a.source, a.basis].filter(Boolean).join(' · ')
          return (
            <div key={a.id} className="w-44 shrink-0 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2">
              <div className="flex items-baseline justify-between gap-2">
                <span className="min-w-0 truncate text-xs text-zinc-400">{a.label}</span>
                <span className={`shrink-0 whitespace-nowrap text-[10px] ${basisBadge[a.basis] ?? 'text-zinc-500'}`}>{sourceLabel}</span>
              </div>
              <div className="mt-1 text-sm font-semibold tabular-nums">
                {a.price === null ? '—' : a.price.toLocaleString(undefined, { maximumFractionDigits: 2 })}
              </div>
              <div className={`text-xs tabular-nums ${a.change_pct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                {a.change_pct >= 0 ? '+' : ''}
                {a.change_pct.toFixed(2)}%
              </div>
              {a.basis_note && <div className="mt-1 text-[10px] leading-snug text-zinc-600">{a.basis_note}</div>}
            </div>
          )
        })}
        {(pulse.missing ?? []).map((id) => (
          <div key={id} className="min-w-32 shrink-0 rounded-lg border border-zinc-800 bg-zinc-900/40 px-3 py-2 opacity-50">
            <span className="text-xs text-zinc-500">{id}</span>
            <div className="mt-1 text-sm text-zinc-600">缺席</div>
          </div>
        ))}
      </div>
      {(pulse.warnings ?? []).length > 0 && (
        <details className="mt-1 text-xs text-zinc-600">
          <summary>⚠ {pulse.warnings!.length} 条数据警告</summary>
          <ul className="mt-1 list-inside list-disc">
            {pulse.warnings!.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}
