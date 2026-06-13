import { usePoll } from '../api/usePoll'
import type { ShockLiquidityDTO, ShockLiquidityRow } from '../api/types'

function LiquidityRowView({ row }: { row: ShockLiquidityRow }) {
  if (row.missing) {
    return (
      <div className="grid grid-cols-[52px_1fr_62px] items-center gap-2 text-xs text-zinc-600">
        <span className="font-medium text-zinc-400">{row.id}</span>
        <span>{row.note ?? 'missing'}</span>
        <span className="text-right">--</span>
      </div>
    )
  }
  const tone = row.state === 'severe' || row.state === 'stressed' ? 'text-red-400' : row.state === 'watch' ? 'text-amber-300' : 'text-emerald-400'
  return (
    <div className="grid grid-cols-[52px_1fr_72px] items-center gap-2 text-xs">
      <div>
        <div className="font-medium text-zinc-300">{row.id}</div>
        <div className="text-[10px] text-zinc-600">{row.basis}</div>
      </div>
      <div className="min-w-0 text-zinc-500">
        <span className={tone}>{row.state}</span>
        <span className="mx-1 text-zinc-700">·</span>
        <span>{row.spread_bps.toFixed(1)}bp</span>
        {row.depth_available ? <span className="ml-1 text-zinc-600">depth</span> : null}
      </div>
      <div className="text-right tabular-nums text-zinc-500">z {row.spread_z.toFixed(1)}</div>
    </div>
  )
}

export function ShockLiquidityCard() {
  const { data } = usePoll<ShockLiquidityDTO>('/api/intel/shock/liquidity', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="shock-liquidity-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">流动性状态</h3>
        <span className="text-[10px] text-zinc-600">{data.source}</span>
      </div>
      <div className="mt-3 space-y-2">
        {data.rows.slice(0, 6).map((row) => (
          <LiquidityRowView key={row.id} row={row} />
        ))}
      </div>
    </div>
  )
}
