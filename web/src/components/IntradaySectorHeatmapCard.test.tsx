import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { IntradaySectorHeatmapCard } from './IntradaySectorHeatmapCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('IntradaySectorHeatmapCard', () => {
  it('renders sector rows with source metadata', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-25T15:00:00Z',
          source: 'ibkr',
          basis: 'realtime',
          sector_source: 'embedded sector map',
          rows: [{ sector_id: 'mega-cap-tech', sector_label: 'Mega-cap Tech', avg_pct: 4, sample_n: 2, gainers: 1, losers: 1, top_symbol: 'AAPL' }],
        }),
      }),
    )

    render(<IntradaySectorHeatmapCard />)

    await waitFor(() => expect(screen.getByText('Mega-cap Tech')).toBeInTheDocument())
    expect(screen.getByText('板块热力')).toBeInTheDocument()
    expect(screen.getByText('ibkr · realtime')).toBeInTheDocument()
    expect(screen.getByText('+4.00%')).toBeInTheDocument()
    expect(screen.getByText(/Top AAPL/)).toBeInTheDocument()
  })

  it('renders warnings and empty state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          source: 'ibkr-preferred',
          basis: 'realtime',
          sector_source: 'embedded sector map',
          rows: null,
          warnings: ['no intraday movers available'],
        }),
      }),
    )

    render(<IntradaySectorHeatmapCard />)

    await waitFor(() => expect(screen.getByText(/板块暂无可用样本/)).toBeInTheDocument())
    expect(screen.getByText('no intraday movers available')).toBeInTheDocument()
  })
})
