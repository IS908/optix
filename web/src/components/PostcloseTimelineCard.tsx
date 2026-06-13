import { usePoll } from '../api/usePoll'
import type { PostcloseTimelineDTO, TimelineEvent } from '../api/types'

function severityClass(event: TimelineEvent) {
  if (event.severity === 'positive') return 'border-emerald-500/60 text-emerald-300'
  if (event.severity === 'negative') return 'border-red-500/60 text-red-300'
  return 'border-zinc-700 text-zinc-400'
}

export function PostcloseTimelineCard() {
  const { data } = usePoll<PostcloseTimelineDTO>('/api/intel/postclose/timeline', 60_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="postclose-timeline-loading" />
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      <h3 className="text-sm font-medium text-zinc-300">要点时间轴</h3>
      {data.events.length === 0 ? (
        <div className="mt-3 text-xs text-zinc-600">暂无结构化收盘后事件</div>
      ) : (
        <div className="mt-3 space-y-2">
          {data.events.slice(0, 8).map((event) => (
            <div key={`${event.kind}-${event.symbol ?? ''}-${event.title}`} className={`border-l-2 pl-2 text-xs ${severityClass(event)}`}>
              <div className="text-zinc-200">{event.title}</div>
              <div className="mt-0.5 text-zinc-500">{event.detail}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
