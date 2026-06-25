import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { IntradayMoversCard } from './IntradayMoversCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('IntradayMoversCard', () => {
  it('renders mover rows with source metadata', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-25T15:00:00Z',
          source: 'ibkr',
          basis: 'realtime',
          universe_note: 'watchlist plus curated liquid US names',
          gainers: [{ symbol: 'AAPL', source: 'ibkr', basis: 'realtime', as_of: '2026-06-25T15:00:00Z', last: 110, open: 100, pct: 10, volume: 2500, watchlist: true }],
          losers: [{ symbol: 'MSFT', source: 'ibkr', basis: 'realtime', as_of: '2026-06-25T15:00:00Z', last: 95, open: 100, pct: -5, volume: 2000, watchlist: false }],
        }),
      }),
    )

    render(<IntradayMoversCard />)

    await waitFor(() => expect(screen.getByText(/AAPL/)).toBeInTheDocument())
    expect(screen.getByText('盘中异动')).toBeInTheDocument()
    expect(screen.getByText('ibkr · realtime')).toBeInTheDocument()
    expect(screen.getByText('+10.00%')).toBeInTheDocument()
    expect(screen.getByText('-5.00%')).toBeInTheDocument()
  })

  it('renders warnings and treats null mover lists as empty degraded data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          source: 'ibkr-preferred',
          basis: 'realtime',
          universe_note: 'range',
          gainers: null,
          losers: null,
          warnings: ['quote source unavailable'],
        }),
      }),
    )

    render(<IntradayMoversCard />)

    await waitFor(() => expect(screen.getByText(/盘中暂无可用异动/)).toBeInTheDocument())
    expect(screen.getByText('quote source unavailable')).toBeInTheDocument()
  })
})
