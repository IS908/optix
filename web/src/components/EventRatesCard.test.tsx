import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { EventRatesCard } from './EventRatesCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('EventRatesCard', () => {
  it('renders event repricing rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T14:00:00Z',
          source: 'yfinance',
          universe_note: '',
          rows: [
            {
              id: 'US10Y',
              label: 'US 10Y',
              kind: 'yield_proxy',
              source: 'yfinance',
              basis: 'approx',
              as_of: '2026-06-13T14:00:00Z',
              pre_event: 4,
              current: 4.25,
              change: 0.25,
              change_pct: 6.25,
            },
          ],
        }),
      }),
    )

    render(<EventRatesCard />)

    await waitFor(() => expect(screen.getByText('US10Y')).toBeInTheDocument())
    expect(screen.getByText('+6.25%')).toBeInTheDocument()
    expect(screen.getByText('approx')).toBeInTheDocument()
  })
})
