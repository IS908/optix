import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { PremarketMoversCard } from './PremarketMoversCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('PremarketMoversCard', () => {
  it('renders mover rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          universe_note: '仅自选股 + 内置精选集内排名',
          gainers: [{ symbol: 'AAPL', pct: 2.1, vol_ratio: 1.8, watchlist: true }],
          losers: [{ symbol: 'NVDA', pct: -3.2, vol_ratio: 2.5, watchlist: false }],
        }),
      }),
    )

    render(<PremarketMoversCard />)

    await waitFor(() => expect(screen.getByText(/AAPL/)).toBeInTheDocument())
    expect(screen.getByText('+2.10%')).toBeInTheDocument()
    expect(screen.getByText('-3.20%')).toBeInTheDocument()
  })

  it('renders empty state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ as_of: '', universe_note: '范围', gainers: [], losers: [] }),
      }),
    )

    render(<PremarketMoversCard />)

    await waitFor(() => expect(screen.getByText(/盘前无数据/)).toBeInTheDocument())
  })

  it('renders warnings and treats null mover lists as empty degraded data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          universe_note: '范围',
          gainers: null,
          losers: null,
          warnings: ['premarket bars unavailable'],
        }),
      }),
    )

    render(<PremarketMoversCard />)

    await waitFor(() => expect(screen.getByText(/盘前无数据/)).toBeInTheDocument())
    expect(screen.getByText('premarket bars unavailable')).toBeInTheDocument()
  })
})
