import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SlotGrid } from './SlotGrid'

function journalResponse() {
  return Promise.resolve({
    ok: true,
    json: () =>
      Promise.resolve({
        trading_date: '2026-06-12',
        now: '2026-06-12T11:00:00-04:00',
        is_trading_day: true,
        checkpoints: [],
        narratives: [],
        judgments: [],
        hit_rate: { window: 'today', hit: 0, miss: 0, rate: 0 },
      }),
  } as Response)
}

function fixtureResponse(url: string) {
  if (url.startsWith('/api/intel/journal')) return journalResponse()
  if (url.endsWith('/api/intel/intraday/movers')) {
    return Promise.resolve({
      ok: true,
      json: async () => ({
        as_of: '',
        source: 'ibkr',
        basis: 'realtime',
        universe_note: 'test',
        gainers: [],
        losers: [],
      }),
    } as Response)
  }
  if (url.endsWith('/api/intel/intraday/sector-heatmap')) {
    return Promise.resolve({
      ok: true,
      json: async () => ({
        as_of: '',
        source: 'ibkr',
        basis: 'realtime',
        sector_source: 'test',
        rows: [],
      }),
    } as Response)
  }
  if (url.endsWith('/api/intel/event/rates')) return Promise.resolve({ ok: true, json: async () => ({ as_of: '', source: 'test', universe_note: '', rows: [] }) } as Response)
  if (url.endsWith('/api/intel/event/diff')) {
    return Promise.resolve({
      ok: true,
      json: async () => ({
        as_of: '',
        source: 'test',
        prior_title: 'prior',
        current_title: 'current',
        prior_published_at: '',
        current_published_at: '',
        added: [],
        removed: [],
        unchanged: [],
        hawkish_hits: 0,
        dovish_hits: 0,
        verdict: 'neutral',
      }),
    } as Response)
  }
  if (url.endsWith('/api/intel/event/patterns') || url.endsWith('/api/intel/event/sensitivity')) {
    return Promise.resolve({ ok: true, json: async () => ({ as_of: '', source: 'test', rows: [] }) } as Response)
  }
  if (url.endsWith('/api/intel/shock/regime')) {
    return Promise.resolve({ ok: true, json: async () => ({ as_of: '', source: 'test', state: 'normal', score: 0, vix_change_ratio: 0, confirmations: [] }) } as Response)
  }
  if (url.endsWith('/api/intel/shock/fingerprint')) {
    return Promise.resolve({ ok: true, json: async () => ({ as_of: '', source: 'test', rows: [] }) } as Response)
  }
  if (url.endsWith('/api/intel/shock/analogs') || url.endsWith('/api/intel/shock/liquidity')) {
    return Promise.resolve({ ok: true, json: async () => ({ as_of: '', source: 'test', rows: [] }) } as Response)
  }
  return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
}

describe('SlotGrid', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => fixtureResponse(String(input))))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders intraday intel cards with the narrative workflow', async () => {
    render(<SlotGrid view="intraday" />)

    await waitFor(() => expect(screen.getByText('叙事流')).toBeInTheDocument())
    expect(screen.getByText('盘中异动')).toBeInTheDocument()
    expect(screen.getByText('板块热力')).toBeInTheDocument()
  })

  it.each(['postclose', 'event', 'shock'])('renders the judgment workflow on %s view', async (view) => {
    render(<SlotGrid view={view} />)

    await waitFor(() => expect(screen.getByText('从当前视图登记判断')).toBeInTheDocument())
    expect(screen.getByText(/optix intel judge --asset SPX/)).toBeInTheDocument()
  })
})
