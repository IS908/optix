import { usePoll } from '../api/usePoll'
import type { SentimentDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

export function SentimentCard() {
  const { data } = usePoll<SentimentDTO>('/api/intel/premarket/sentiment', 30_000)

  if (!data) {
    return <LoadingCard title="情绪定位" testId="sentiment-loading" />
  }

  const term = data.vix_term_premium
  const termLabel = term === 0 ? '—' : term >= 1 ? `${term.toFixed(2)} contango` : `${term.toFixed(2)} backwardation`

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-zinc-300">情绪定位</h3>
        <span className="rounded bg-zinc-800 px-2 py-0.5 text-xs text-zinc-300">{data.regime}</span>
      </div>
      <div className="mt-3 space-y-1 text-sm">
        <div className="text-zinc-400">
          P/C(OI): <span className="text-zinc-200">{data.pc_available ? data.pc_oi.toFixed(2) : '不可用'}</span>
        </div>
        <div className="text-zinc-400">
          VIX 期限: <span className="text-zinc-200">{termLabel}</span>
        </div>
      </div>
      <div className="mt-2 text-[10px] text-zinc-600">{data.degraded_note}</div>
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
