import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ShockRegimeCard } from './ShockRegimeCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ShockRegimeCard', () => {
  it('renders regime state and confirmations', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'ibkr',
          state: 'critical',
          score: 88,
          vix_change_ratio: 3.5,
          triggered_view: 'shock',
          confirmations: [
            { id: 'VIX', label: 'VIX', dimension: 'volatility', change_pct: 35, weight: 1.2, contribution: 42, source: 'ibkr', basis: 'realtime', as_of: '2026-06-13T15:30:00Z' },
          ],
        }),
      }),
    )

    render(<ShockRegimeCard />)

    await waitFor(() => expect(screen.getByText('critical')).toBeInTheDocument())
    expect(screen.getByText('score 88')).toBeInTheDocument()
    expect(screen.getByText('VIX')).toBeInTheDocument()
    expect(screen.getByText('+35.00%')).toBeInTheDocument()
  })

  it('renders shock source warnings inline', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'yfinance',
          state: 'watch',
          score: 32,
          vix_change_ratio: 1.3,
          confirmations: [],
          warnings: ['quotes: broker quotes degraded: ibkr offline'],
        }),
      }),
    )

    render(<ShockRegimeCard />)

    await waitFor(() => expect(screen.getByText('警告 1')).toBeInTheDocument())
    expect(screen.getByText('quotes: broker quotes degraded: ibkr offline')).toBeInTheDocument()
  })
})
