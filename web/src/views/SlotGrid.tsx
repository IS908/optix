import { SlotCard } from '../components/SlotCard'
import { viewSlots } from './slots'

export function SlotGrid({ view }: { view: string }) {
  const slots = viewSlots[view] ?? []
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      {slots.map((s) => (
        <SlotCard key={s.title} slot={s} />
      ))}
    </div>
  )
}
