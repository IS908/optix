import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { PostcloseEarningsCard } from './PostcloseEarningsCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('PostcloseEarningsCard', () => {
  it('renders earnings reports and surprise labels', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          source: 'yfinance earnings_dates',
          universe_note: '自选股 + 内置精选集',
          reports: [
            {
              symbol: 'AAPL',
              event_time: '2026-06-12T20:00:00Z',
              timing: 'postmarket',
              eps_estimate: 1.0,
              eps_reported: 1.1,
              eps_surprise_pct: 10,
              surprise_label: 'beat',
              stale: false,
            },
          ],
        }),
      }),
    )

    render(<PostcloseEarningsCard />)

    await waitFor(() => expect(screen.getByText(/AAPL/)).toBeInTheDocument())
    expect(screen.getByText(/beat/)).toBeInTheDocument()
    expect(screen.getByText(/EPS 1.10 vs 1.00/)).toBeInTheDocument()
  })
})
