import { SlotCard } from '../components/SlotCard'
import { IntelJournalPanel } from '../components/IntelJournalPanel'
import { OvernightChainCard } from '../components/OvernightChainCard'
import { GapFillCard } from '../components/GapFillCard'
import { PremarketMoversCard } from '../components/PremarketMoversCard'
import { SentimentCard } from '../components/SentimentCard'
import { PostcloseEarningsCard } from '../components/PostcloseEarningsCard'
import { PostcloseTimelineCard } from '../components/PostcloseTimelineCard'
import { ReadAcrossCard } from '../components/ReadAcrossCard'
import { PostcloseMoversCard } from '../components/PostcloseMoversCard'
import { EventRatesCard } from '../components/EventRatesCard'
import { EventDiffCard } from '../components/EventDiffCard'
import { EventPatternsCard } from '../components/EventPatternsCard'
import { EventSensitivityCard } from '../components/EventSensitivityCard'
import { ShockRegimeCard } from '../components/ShockRegimeCard'
import { ShockFingerprintCard } from '../components/ShockFingerprintCard'
import { ShockAnalogsCard } from '../components/ShockAnalogsCard'
import { ShockLiquidityCard } from '../components/ShockLiquidityCard'
import { viewSlots, type SlotDef } from './slots'

function liveComponent(live: SlotDef['live']) {
  switch (live) {
    case 'narrative':
      return <IntelJournalPanel />
    case 'overnight':
      return <OvernightChainCard />
    case 'gaps':
      return <GapFillCard />
    case 'movers':
      return <PremarketMoversCard />
    case 'sentiment':
      return <SentimentCard />
    case 'postclose-earnings':
      return <PostcloseEarningsCard />
    case 'postclose-timeline':
      return <PostcloseTimelineCard />
    case 'postclose-read-across':
      return <ReadAcrossCard />
    case 'postclose-movers':
      return <PostcloseMoversCard />
    case 'event-rates':
      return <EventRatesCard />
    case 'event-diff':
      return <EventDiffCard />
    case 'event-patterns':
      return <EventPatternsCard />
    case 'event-sensitivity':
      return <EventSensitivityCard />
    case 'shock-regime':
      return <ShockRegimeCard />
    case 'shock-fingerprint':
      return <ShockFingerprintCard />
    case 'shock-analogs':
      return <ShockAnalogsCard />
    case 'shock-liquidity':
      return <ShockLiquidityCard />
    default:
      return null
  }
}

export function SlotGrid({ view }: { view: string }) {
  const slots = viewSlots[view] ?? []
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      {slots.map((s) =>
        s.live ? (
          <div key={s.title} className={s.span === 2 ? 'md:col-span-2' : ''}>
            {liveComponent(s.live)}
          </div>
        ) : (
          <SlotCard key={s.title} slot={s} />
        ),
      )}
    </div>
  )
}
