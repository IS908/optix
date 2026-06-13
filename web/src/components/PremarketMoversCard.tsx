import { usePoll } from '../api/usePoll'
import type { Mover, MoversDTO } from '../api/types'

function MoverRow({ mover, direction }: { mover: Mover; direction: '↑' | '↓' }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="w-4">{direction}</span>
      <span className="w-16 text-zinc-300">
        {mover.symbol}
        {mover.watchlist && <span className="text-amber-400"> ★</span>}
      </span>
      <span className={`w-16 tabular-nums ${mover.pct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
        {mover.pct >= 0 ? '+' : ''}
        {mover.pct.toFixed(2)}%
      </span>
      <span className="text-zinc-500">量比 {mover.vol_ratio.toFixed(1)}x</span>
    </div>
  )
}

export function PremarketMoversCard() {
  const { data } = usePoll<MoversDTO>('/api/intel/premarket/movers', 30_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="movers-loading" />
  }

  const empty = data.gainers.length === 0 && data.losers.length === 0

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">盘前异动</h3>
      <div className="mt-1 text-[10px] text-zinc-600">{data.universe_note}</div>
      {empty ? (
        <div className="mt-3 text-xs text-zinc-600">盘前无数据（非交易时段/周末）</div>
      ) : (
        <div className="mt-2 space-y-1">
          {data.gainers.map((mover) => (
            <MoverRow key={`g-${mover.symbol}`} mover={mover} direction="↑" />
          ))}
          {data.losers.map((mover) => (
            <MoverRow key={`l-${mover.symbol}`} mover={mover} direction="↓" />
          ))}
        </div>
      )}
    </div>
  )
}
