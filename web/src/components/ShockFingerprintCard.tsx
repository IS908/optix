import { usePoll } from '../api/usePoll'
import type { ShockFingerprintDTO, ShockOptionStress } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

function formatSignedPct(value: number) {
  const sign = value > 0 ? '+' : ''
  return `${sign}${(value * 100).toFixed(1)}%`
}

function OptionStressRow({ row }: { row: ShockOptionStress }) {
  return (
    <div className="grid grid-cols-[44px_1fr_72px] items-center gap-2 text-xs">
      <div>
        <div className="font-medium text-zinc-300">{row.underlying}</div>
        <div className="text-[10px] text-zinc-600">{row.basis}</div>
      </div>
      <div className="min-w-0 text-zinc-500">
        <span className={row.iv_skew >= 0.05 ? 'text-amber-300' : 'text-zinc-500'}>IV skew {formatSignedPct(row.iv_skew)}</span>
        <span className="mx-1 text-zinc-700">·</span>
        <span>Vol {row.volume}</span>
      </div>
      <div className="text-right tabular-nums text-zinc-500">OI {row.open_interest}</div>
    </div>
  )
}

export function ShockFingerprintCard() {
  const { data } = usePoll<ShockFingerprintDTO>('/api/intel/shock/fingerprint', 60_000)

  if (!data) {
    return <LoadingCard title="冲击指纹" testId="shock-fingerprint-loading" />
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
      {data.option_stress && data.option_stress.length > 0 ? (
        <div className="mt-4 border-t border-zinc-800 pt-3">
          <div className="mb-2 text-[10px] uppercase tracking-normal text-zinc-600">Option stress</div>
          <div className="space-y-2">
            {data.option_stress.slice(0, 4).map((row) => (
              <OptionStressRow key={row.underlying} row={row} />
            ))}
          </div>
        </div>
      ) : null}
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
