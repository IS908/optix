import { usePoll } from '../api/usePoll'
import type { PostcloseMover, PostcloseMoversDTO } from '../api/types'

function pct(v: number) {
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}

function MoverRow({ mover, direction }: { mover: PostcloseMover; direction: '↑' | '↓' }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="w-4">{direction}</span>
      <span className="w-16 text-zinc-300">
        {mover.symbol}
        {mover.watchlist && <span className="text-amber-400"> ★</span>}
      </span>
      <span className={`w-18 tabular-nums ${mover.combined_pct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{pct(mover.combined_pct)}</span>
      <span className="text-zinc-500">盘后 {pct(mover.after_hours_pct)}</span>
    </div>
  )
}

export function PostcloseMoversCard() {
  const { data } = usePoll<PostcloseMoversDTO>('/api/intel/postclose/movers', 30_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="postclose-movers-loading" />
  }

  const empty = data.gainers.length === 0 && data.losers.length === 0

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">全天合并异动</h3>
      <div className="mt-1 text-[10px] text-zinc-600">{data.universe_note}</div>
      {empty ? (
        <div className="mt-3 text-xs text-zinc-600">收盘后 bar 不足</div>
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
