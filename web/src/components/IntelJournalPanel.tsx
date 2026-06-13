import { usePoll } from '../api/usePoll'
import type { IntelJournalSnapshot, IntelJudgment } from '../api/types'

const cpStatusStyle: Record<string, string> = {
  written: 'bg-emerald-900 text-emerald-300',
  due: 'bg-amber-900 text-amber-300',
  pending: 'bg-zinc-800 text-zinc-500',
}

const dirArrow: Record<string, string> = { up: '↑', down: '↓', flat: '→' }

const outcomeStyle: Record<string, string> = {
  hit: 'text-emerald-400',
  miss: 'text-red-400',
  void: 'text-zinc-500',
  push: 'text-zinc-400',
}

function JudgmentRow({ j }: { j: IntelJudgment }) {
  const rec = j.reconciliation
  const status = rec ? rec.outcome : '⏳待结算'
  const cls = rec ? (outcomeStyle[rec.outcome] ?? 'text-zinc-400') : 'text-amber-400'
  return (
    <div className={`flex items-center gap-2 text-xs ${j.supersedes ? 'opacity-40' : ''}`}>
      <span className="w-16 text-zinc-300">{j.asset_id}</span>
      <span className="w-6">{dirArrow[j.direction] ?? '?'}</span>
      <span className="w-14 text-zinc-500">{j.threshold_pct.toFixed(2)}%</span>
      <span className="w-12 text-zinc-500">c{j.confidence}</span>
      <span className="w-20 text-zinc-500">→{j.expiry_checkpoint}</span>
      <span className={`tabular-nums ${cls}`}>
        {status}
        {rec && rec.outcome !== 'void' ? ` ${rec.delta_pct >= 0 ? '+' : ''}${rec.delta_pct.toFixed(2)}%` : ''}
      </span>
    </div>
  )
}

export function IntelJournalPanel() {
  const { data, stale } = usePoll<IntelJournalSnapshot>('/api/intel/journal', 30_000)

  if (!data) {
    return <div className="h-40 animate-pulse rounded-lg bg-zinc-900" data-testid="journal-loading" />
  }
  const hr = data.hit_rate
  const denom = hr.hit + hr.miss
  // 每检查点最新叙事按 checkpoint 排序展示;默认展开最后一条
  const latestBody = data.narratives.length > 0 ? data.narratives[data.narratives.length - 1] : null

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
      {stale && <div className="mb-2 text-xs text-amber-400">数据延迟,正在重试</div>}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-zinc-300">叙事流</h3>
        <span className="text-xs text-zinc-500">
          命中率 {denom > 0 ? `${hr.hit}/${denom} (${Math.round(hr.rate * 100)}%)` : '—'}
        </span>
      </div>

      <div className="mt-3 flex flex-wrap gap-1">
        {data.checkpoints.map((c) => (
          <span key={c.kind} className={`rounded px-2 py-0.5 text-[10px] ${cpStatusStyle[c.status] ?? ''}`}>
            {c.label} {c.status === 'written' ? '✓' : c.status === 'due' ? '!' : '·'}
          </span>
        ))}
      </div>

      {latestBody ? (
        <div className="mt-3 rounded border border-zinc-800 bg-zinc-950/50 p-2">
          <div className="text-[10px] text-zinc-500">
            {latestBody.checkpoint} · {latestBody.phase}
          </div>
          {/* 纯文本 pre-wrap：agent 文本不可信,React 默认转义,杜绝 XSS */}
          <div className="mt-1 whitespace-pre-wrap text-xs text-zinc-300">{latestBody.body}</div>
        </div>
      ) : (
        <div className="mt-3 text-xs text-zinc-600">今日暂无检查点记录</div>
      )}

      {data.judgments.length > 0 && (
        <div className="mt-3 space-y-1">
          {data.judgments.map((j) => (
            <JudgmentRow key={j.judgment_id} j={j} />
          ))}
        </div>
      )}
    </div>
  )
}
