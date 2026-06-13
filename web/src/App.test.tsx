import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import App from './App'
import type { ViewName } from './api/types'

function okJSON(body: unknown) {
  return Promise.resolve({
    ok: true,
    json: () => Promise.resolve(body),
  } as Response)
}

function installFetch(autoView: ViewName = 'event') {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url === '/api/intel/state') {
      return okJSON({
        now: '2026-06-10T11:00:00-04:00',
        phase: 'intraday',
        view: autoView,
        base_view: 'intraday',
        view_override: { source: 'event_calendar', reason: 'Jun CPI' },
        is_trading_day: true,
        early_close: false,
        next_transition: '2026-06-10T16:00:00-04:00',
        next_phase: 'postclose',
        calendar_stale: false,
      })
    }
    if (url.startsWith('/api/intel/pulse')) {
      const view = new URL(url, 'http://127.0.0.1').searchParams.get('view') ?? autoView
      return okJSON({ snapshot_at: '2026-06-10T15:00:00Z', view, view_inferred: true, assets: [] })
    }
    if (url.startsWith('/api/intel/journal')) {
      return okJSON({
        trading_date: '2026-06-10',
        now: '2026-06-10T15:00:00Z',
        is_trading_day: true,
        checkpoints: [],
        narratives: [],
        judgments: [],
        hit_rate: { window: '20d', hit: 0, miss: 0, rate: 0 },
      })
    }
    if (url.endsWith('/api/intel/event/rates')) {
      return okJSON({ as_of: '2026-06-10T15:00:00Z', source: 'test', universe_note: '', rows: [] })
    }
    if (url.endsWith('/api/intel/event/diff')) {
      return okJSON({
        as_of: '2026-06-10T15:00:00Z',
        source: 'test',
        prior_title: 'prior',
        current_title: 'current',
        prior_published_at: '2026-05-10T18:00:00Z',
        current_published_at: '2026-06-10T18:00:00Z',
        added: [],
        removed: [],
        unchanged: [],
        hawkish_hits: 0,
        dovish_hits: 0,
        verdict: 'neutral',
      })
    }
    if (url.endsWith('/api/intel/event/patterns') || url.endsWith('/api/intel/event/sensitivity')) {
      return okJSON({ as_of: '2026-06-10T15:00:00Z', source: 'test', rows: [] })
    }
    return okJSON({})
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('App view override behavior', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('follows automatic event override until the user manually selects a view', async () => {
    const fetchMock = installFetch('event')
    render(<App />)

    await waitFor(() => expect(screen.getByText('自动触发：Jun CPI')).toBeInTheDocument())
    expect(screen.queryByText('跟随自动')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '盘中' }))
    await waitFor(() => expect(screen.getByText('跟随自动')).toBeInTheDocument())
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/intel/pulse?view=intraday'))

    fireEvent.click(screen.getByText('跟随自动'))
    await waitFor(() => expect(screen.queryByText('跟随自动')).toBeNull())
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/intel/pulse?view=event'))
  })
})
