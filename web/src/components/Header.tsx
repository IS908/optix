import type { IntelState } from '../api/types'

const phaseLabel: Record<string, string> = {
  premarket: '盘前',
  intraday: '盘中',
  postclose: '收盘后',
  closed: '休市',
}

function countdown(toISO: string): string {
  const ms = new Date(toISO).getTime() - Date.now()
  if (ms <= 0) return ''
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  return h > 0 ? `${h}h${m}m` : `${m}m`
}

export function Header({ state }: { state: IntelState | null }) {
  return (
    <header className="flex items-center justify-between">
      <h1 className="text-lg font-semibold tracking-wide">
        Optix · Market Intel
        {state?.calendar_stale && (
          <span className="ml-2 rounded bg-red-950 px-1.5 py-0.5 text-xs text-red-400" title="交易日历表已过期，仅按周末判定">
            日历过期
          </span>
        )}
      </h1>
      {state && (
        <div className="flex items-center gap-2 text-sm">
          <span className="rounded-full bg-zinc-800 px-3 py-1 text-zinc-300">
            {phaseLabel[state.phase] ?? state.phase}
            {state.early_close && ' · 半日'}
          </span>
          {state.phase === 'closed' && (
            <span className="text-xs text-zinc-500">
              距 {phaseLabel[state.next_phase]} {countdown(state.next_transition)}
            </span>
          )}
        </div>
      )}
    </header>
  )
}
