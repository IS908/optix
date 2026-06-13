import { useState } from 'react'
import { usePoll } from './api/usePoll'
import type { IntelState, PulseResponse, ViewName } from './api/types'
import { Header } from './components/Header'
import { PulseBar } from './components/PulseBar'
import { ViewTabs } from './components/ViewTabs'
import { SlotGrid } from './views/SlotGrid'

export default function App() {
  const state = usePoll<IntelState>('/api/intel/state', 60_000)
  const [manualView, setManualView] = useState<ViewName | null>(null)
  // 自动跟随状态机；手动点击后锁定，直到「跟随时钟」回位。
  const autoView: ViewName = state.data?.view ?? 'intraday'
  const view = manualView ?? autoView
  const pulse = usePoll<PulseResponse>(`/api/intel/pulse?view=${view}`, 30_000)

  return (
    <div className="min-h-screen bg-zinc-950 p-6 text-zinc-100">
      <div className="mx-auto flex max-w-6xl flex-col gap-5">
        <Header state={state.data} />
        <PulseBar pulse={pulse.data} stale={pulse.stale} />
        <ViewTabs
          active={view}
          locked={manualView !== null}
          onSelect={(v) => setManualView(v)}
          onFollow={() => setManualView(null)}
        />
        <SlotGrid view={view} />
      </div>
    </div>
  )
}
