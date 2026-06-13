import type { ViewName } from '../api/types'

const TABS: { view: ViewName; label: string; manualOnly?: boolean }[] = [
  { view: 'premarket', label: '盘前' },
  { view: 'intraday', label: '盘中' },
  { view: 'postclose', label: '收盘后' },
  { view: 'event', label: '事件日', manualOnly: true },
  { view: 'shock', label: '冲击', manualOnly: true },
]

export interface ViewTabsProps {
  active: ViewName
  autoView: ViewName
  locked: boolean // 手动锁定中
  overrideReason?: string
  onSelect: (v: ViewName) => void
  onFollow: () => void // 回到自动 resolver
}

export function ViewTabs({ active, autoView, locked, overrideReason, onSelect, onFollow }: ViewTabsProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex items-center gap-1">
        {TABS.map((t) => {
          const autoTriggered = !locked && autoView === t.view && t.manualOnly
          return (
            <button
              key={t.view}
              onClick={() => onSelect(t.view)}
              className={`rounded px-3 py-1.5 text-sm ${
                active === t.view ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {t.label}
              {t.manualOnly && <span className="ml-1 text-[10px] text-zinc-600">{autoTriggered ? '自动' : '手动'}</span>}
            </button>
          )
        })}
      </div>
      {!locked && overrideReason && (
        <span className="rounded border border-amber-900/70 bg-amber-950/40 px-2 py-1 text-xs text-amber-200">
          自动触发：{overrideReason}
        </span>
      )}
      {locked && (
        <button onClick={onFollow} className="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-400 hover:text-zinc-200">
          跟随自动
        </button>
      )}
    </div>
  )
}
