import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { PostcloseMoversCard } from './PostcloseMoversCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('PostcloseMoversCard', () => {
  it('renders regular, after-hours, and combined moves', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          universe_note: '自选股 + 内置精选集',
          gainers: [{ symbol: 'AAPL', regular_pct: 5, after_hours_pct: 4.76, combined_pct: 10, volume: 2500, watchlist: true }],
          losers: [{ symbol: 'NVDA', regular_pct: -2, after_hours_pct: -3, combined_pct: -4.94, volume: 1800, watchlist: false }],
        }),
      }),
    )

    render(<PostcloseMoversCard />)

    await waitFor(() => expect(screen.getByText(/AAPL/)).toBeInTheDocument())
    expect(screen.getByText('+10.00%')).toBeInTheDocument()
    expect(screen.getByText('-4.94%')).toBeInTheDocument()
    expect(screen.getByText(/盘后 \+4.76%/)).toBeInTheDocument()
  })
})
