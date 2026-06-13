import { usePoll } from '../api/usePoll'
import type { GapsDTO } from '../api/types'

export function GapFillCard() {
  const { data } = usePoll<GapsDTO>('/api/intel/premarket/gaps', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="gaps-loading" />
  }

  const direction = data.direction === 'down' ? '↓' : '↑'
  const directionClass = data.implied_gap_pct >= 0 ? 'text-emerald-400' : 'text-red-400'

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">隐含开盘 + 跳空回补</h3>
      <div className="mt-2 text-sm">
        <span className="text-zinc-400">{data.symbol} 隐含跳空 </span>
        <span className={directionClass}>
          {direction}
          {Math.abs(data.implied_gap_pct).toFixed(2)}%
        </span>
        {data.band && <span className="ml-1 text-xs text-zinc-500">({data.band} 档)</span>}
      </div>

      {data.sample_n > 0 ? (
        <div className="mt-1 text-xs text-zinc-400">
          历史同档回补率 <span className="text-zinc-200">{(data.hist_fill_rate * 100).toFixed(0)}%</span>
          <span className={data.sample_n < 30 ? 'text-amber-500' : 'text-zinc-600'}> (n={data.sample_n})</span>
        </div>
      ) : (
        <div className="mt-1 text-xs text-zinc-600">历史统计不可用</div>
      )}

      {data.by_band.length > 0 && (
        <table className="mt-3 w-full text-[11px] text-zinc-500">
          <tbody>
            {data.by_band.map((band) => (
              <tr key={`${band.direction}-${band.band}`}>
                <td>
                  {band.direction === 'down' ? '↓' : '↑'} {band.band}
                </td>
                <td className="text-right tabular-nums">{(band.fill_rate * 100).toFixed(0)}%</td>
                <td className="text-right text-zinc-700">n={band.sample_n}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
