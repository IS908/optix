import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { EventPatternsCard } from './EventPatternsCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('EventPatternsCard', () => {
  it('renders historical event pattern rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T14:00:00Z',
          source: 'yfinance daily bars + built-in event calendar',
          rows: [
            {
              id: 'SPX',
              label: 'SPX',
              source: 'yfinance',
              basis: 'historical',
              sample_n: 4,
              avg_event_move_pct: 1.23,
              avg_next_move_pct: -0.45,
              directional_consistency: 0.75,
            },
          ],
        }),
      }),
    )

    render(<EventPatternsCard />)

    await waitFor(() => expect(screen.getByText('SPX')).toBeInTheDocument())
    expect(screen.getByText('n=4')).toBeInTheDocument()
    expect(screen.getByText('+1.23% / -0.45%')).toBeInTheDocument()
  })
})
