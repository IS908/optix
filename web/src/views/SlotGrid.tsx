import { SlotCard } from '../components/SlotCard'
import { IntelJournalPanel } from '../components/IntelJournalPanel'
import { viewSlots } from './slots'

export function SlotGrid({ view }: { view: string }) {
  const slots = viewSlots[view] ?? []
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      {slots.map((s) =>
        s.live === 'narrative' ? (
          <div key={s.title} className={s.span === 2 ? 'md:col-span-2' : ''}>
            <IntelJournalPanel />
          </div>
        ) : (
          <SlotCard key={s.title} slot={s} />
        ),
      )}
    </div>
  )
}
