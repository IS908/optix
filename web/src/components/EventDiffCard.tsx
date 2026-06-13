import { usePoll } from '../api/usePoll'
import type { EventDiffDTO, EventStatementSentence } from '../api/types'

function topSentences(rows: EventStatementSentence[]) {
  return rows.slice(0, 2)
}

export function EventDiffCard() {
  const { data } = usePoll<EventDiffDTO>('/api/intel/event/diff', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="event-diff-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium text-zinc-300">声明措辞 Diff</h3>
        <span className="rounded border border-zinc-800 px-2 py-1 text-[10px] uppercase tracking-normal text-zinc-400">{data.verdict}</span>
      </div>
      <div className="mt-2 flex gap-4 text-xs">
        <span className="text-red-400">hawkish {data.hawkish_hits}</span>
        <span className="text-emerald-400">dovish {data.dovish_hits}</span>
      </div>
      <div className="mt-3 space-y-2">
        {topSentences(data.added).map((row) => (
          <div key={`a-${row.text}`} className="text-xs text-zinc-400">
            <span className="mr-2 text-emerald-400">+</span>
            {row.text}
          </div>
        ))}
        {topSentences(data.removed).map((row) => (
          <div key={`r-${row.text}`} className="text-xs text-zinc-500">
            <span className="mr-2 text-red-400">-</span>
            {row.text}
          </div>
        ))}
      </div>
      <div className="mt-3 text-[10px] text-zinc-600">{data.source}</div>
    </div>
  )
}
