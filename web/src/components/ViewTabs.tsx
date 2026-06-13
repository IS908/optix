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
  locked: boolean // 手动锁定中
  onSelect: (v: ViewName) => void
  onFollow: () => void // 回到跟随时钟
}

export function ViewTabs({ active, locked, onSelect, onFollow }: ViewTabsProps) {
  return (
    <div className="flex items-center gap-1">
      {TABS.map((t) => (
        <button
          key={t.view}
          onClick={() => onSelect(t.view)}
          className={`rounded px-3 py-1.5 text-sm ${
            active === t.view ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'
          }`}
        >
          {t.label}
          {t.manualOnly && <span className="ml-1 text-[10px] text-zinc-600">手动</span>}
        </button>
      ))}
      {locked && (
        <button onClick={onFollow} className="ml-2 rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-400 hover:text-zinc-200">
          跟随时钟
        </button>
      )}
    </div>
  )
}
