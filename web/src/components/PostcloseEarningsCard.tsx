import { usePoll } from '../api/usePoll'
import type { EarningsReport, PostcloseEarningsDTO } from '../api/types'
import { DataWarnings } from './DataWarnings'
import { LoadingCard } from './LoadingCard'

function labelClass(label: string) {
  if (label === 'beat') return 'text-emerald-400'
  if (label === 'miss') return 'text-red-400'
  return 'text-zinc-400'
}

function epsLine(report: EarningsReport) {
  if (report.eps_reported !== undefined && report.eps_estimate !== undefined) {
    return `EPS ${report.eps_reported.toFixed(2)} vs ${report.eps_estimate.toFixed(2)}`
  }
  if (report.eps_estimate !== undefined) return `EPS consensus ${report.eps_estimate.toFixed(2)}`
  return 'EPS consensus unavailable'
}

export function PostcloseEarningsCard() {
  const { data, stale } = usePoll<PostcloseEarningsDTO>('/api/intel/postclose/earnings', 60_000)

  if (!data) {
    return <LoadingCard title="财报速递" testId="postclose-earnings-loading" />
  }
  const reports = data.reports ?? []

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      {stale && <div className="mb-2 text-xs text-amber-400">数据延迟,正在重试</div>}
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium text-zinc-300">财报速递</h3>
          <div className="mt-1 text-[10px] text-zinc-600">{data.universe_note}</div>
        </div>
        <span className="text-right text-[10px] text-zinc-600">{data.source}</span>
      </div>

      {reports.length === 0 ? (
        <div className="mt-3 text-xs text-zinc-600">暂无近期财报行</div>
      ) : (
        <div className="mt-3 space-y-2">
          {reports.slice(0, 6).map((report) => (
            <div key={`${report.symbol}-${report.event_time}`} className="flex items-center justify-between gap-3 text-xs">
              <div className="min-w-0">
                <span className="font-medium text-zinc-200">{report.symbol}</span>
                <span className={`ml-2 ${labelClass(report.surprise_label)}`}>{report.surprise_label}</span>
                <div className="truncate text-zinc-500">{epsLine(report)}</div>
              </div>
              <span className="shrink-0 text-right text-[10px] text-zinc-600">
                <span className="block">{report.timing || 'unknown'}</span>
                <span className="block">{[report.source, report.basis].filter(Boolean).join(' · ')}</span>
              </span>
            </div>
          ))}
        </div>
      )}
      <DataWarnings warnings={data.warnings} />
    </div>
  )
}
