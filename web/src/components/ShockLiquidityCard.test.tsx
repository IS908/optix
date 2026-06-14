import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ShockLiquidityCard } from './ShockLiquidityCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ShockLiquidityCard', () => {
  it('renders liquidity rows and depth state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'ibkr',
          rows: [
            { id: 'SPY', label: 'SPY', source: 'ibkr', basis: 'realtime', as_of: '2026-06-13T15:30:00Z', bid: 499.9, ask: 500.1, mid: 500, spread_bps: 4, normal_spread_bps: 4, spread_ratio: 1, top_bid_size: 1000, top_ask_size: 900, top5_bid_depth: 1800, top5_ask_depth: 1750, depth_available: true, state: 'normal' },
            { id: 'HYG', label: 'HYG', source: 'ibkr', basis: 'realtime', as_of: '2026-06-13T15:30:00Z', bid: 74.8, ask: 75.2, mid: 75, spread_bps: 53.3, normal_spread_bps: 8, spread_ratio: 6.7, top_bid_size: 150, top_ask_size: 120, top5_bid_depth: 0, top5_ask_depth: 0, depth_available: false, state: 'stressed' },
          ],
        }),
      }),
    )

    render(<ShockLiquidityCard />)

    await waitFor(() => expect(screen.getByText('SPY')).toBeInTheDocument())
    expect(screen.getByText('stressed')).toBeInTheDocument()
    expect(screen.getByText('depth')).toBeInTheDocument()
    expect(screen.getByText('6.7× normal')).toBeInTheDocument()
    expect(screen.queryByText(/^z /)).not.toBeInTheDocument()
  })
})
