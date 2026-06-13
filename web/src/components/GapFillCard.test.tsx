import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { GapFillCard } from './GapFillCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('GapFillCard', () => {
  it('renders implied gap and fill rate', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          symbol: 'SPX',
          implied_gap_pct: 0.7,
          direction: 'up',
          band: '0.5-1',
          hist_fill_rate: 0.62,
          sample_n: 143,
          lookback_days: 504,
          by_band: [{ symbol: 'SPX', direction: 'up', band: '0.5-1', fill_rate: 0.62, sample_n: 143, lookback_days: 504, as_of: '' }],
          as_of: '',
        }),
      }),
    )

    render(<GapFillCard />)

    await waitFor(() => expect(screen.getByText(/SPX 隐含跳空/)).toBeInTheDocument())
    expect(screen.getByText('↑0.70%')).toBeInTheDocument()
    expect(screen.getAllByText('62%').length).toBeGreaterThan(0)
  })
})
