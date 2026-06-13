import type { SlotDef } from '../views/slots'

export function SlotCard({ slot }: { slot: SlotDef }) {
  return (
    <div
      className={`rounded-lg border border-zinc-800 bg-zinc-900/60 p-4 ${slot.span === 2 ? 'md:col-span-2' : ''}`}
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-zinc-300">{slot.title}</h3>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-xs text-zinc-500">{slot.milestone} 待实现</span>
      </div>
      <p className="mt-2 text-xs text-zinc-500">{slot.desc}</p>
      <div className="mt-6 h-16 rounded border border-dashed border-zinc-800" />
    </div>
  )
}
