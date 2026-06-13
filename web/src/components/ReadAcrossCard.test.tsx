import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ReadAcrossCard } from './ReadAcrossCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ReadAcrossCard', () => {
  it('renders read-across edges', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          sector_source: '<embedded>',
          edges: [
            {
              driver: 'AAPL',
              peer: 'MSFT',
              sector_id: 'mega-cap-tech',
              sector_label: 'Mega-cap Tech',
              direction: 'positive',
              confidence: 0.65,
              lag: 'T+1 open',
              driver_after_hours_pct: 3.0,
              note: 'AAPL 盘后 3.00%，同板块观察 MSFT',
            },
          ],
        }),
      }),
    )

    render(<ReadAcrossCard />)

    await waitFor(() => expect(screen.getByText(/AAPL → MSFT/)).toBeInTheDocument())
    expect(screen.getByText(/Mega-cap Tech/)).toBeInTheDocument()
    expect(screen.getByText(/65%/)).toBeInTheDocument()
  })
})
