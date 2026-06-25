import { usePoll } from '../api/usePoll'
import type { IntradayMover, IntradayMoversDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

function pct(value: number) {
  return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`
}

function compactVolume(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(0)}K`
  return value.toLocaleString()
}

function MoverRow({ mover, direction }: { mover: IntradayMover; direction: '↑' | '↓' }) {
  return (
    <div className="grid grid-cols-[1rem_minmax(3.5rem,1fr)_4.75rem_minmax(5.25rem,auto)] items-center gap-2 text-xs">
      <span className="w-4">{direction}</span>
      <span className="min-w-0 text-zinc-300">
        {mover.symbol}
        {mover.watchlist && <span className="text-amber-400"> ★</span>}
      </span>
      <span className={`tabular-nums ${mover.pct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{pct(mover.pct)}</span>
      <span className="min-w-0 text-right text-zinc-500">
        <span>{mover.last.toFixed(2)}</span>
        <span className="block truncate text-[10px] text-zinc-600">开 {mover.open.toFixed(2)} · {compactVolume(mover.volume)}</span>
      </span>
    </div>
  )
}

export function IntradayMoversCard() {
  const { data } = usePoll<IntradayMoversDTO>('/api/intel/intraday/movers', 30_000)

  if (!data) {
    return <LoadingCard title="盘中异动" testId="intraday-movers-loading" />
  }

  const gainers = data.gainers ?? []
  const losers = data.losers ?? []
  const empty = gainers.length === 0 && losers.length === 0
  const sourceLabel = [data.source, data.basis].filter(Boolean).join(' · ')

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">盘中异动</h3>
        {sourceLabel && <span className="whitespace-nowrap text-[10px] text-zinc-600">{sourceLabel}</span>}
      </div>
      <div className="mt-1 text-[10px] text-zinc-600">{data.universe_note}</div>
      {empty ? (
        <div className="mt-3 text-xs text-zinc-600">盘中暂无可用异动</div>
      ) : (
        <div className="mt-3 space-y-1.5">
          {gainers.map((mover) => (
            <MoverRow key={`g-${mover.symbol}`} mover={mover} direction="↑" />
          ))}
          {losers.map((mover) => (
            <MoverRow key={`l-${mover.symbol}`} mover={mover} direction="↓" />
          ))}
        </div>
      )}
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
