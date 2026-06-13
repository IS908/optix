import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ShockAnalogsCard } from './ShockAnalogsCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ShockAnalogsCard', () => {
  it('renders analog matches', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'local_shock_template_fixture',
          rows: [
            { name: 'COVID liquidity shock', date: '2020-03-12T00:00:00Z', category: 'liquidity', similarity: 0.92, next_session_bias: 'mixed', matched_features: ['equity', 'vol'] },
          ],
        }),
      }),
    )

    render(<ShockAnalogsCard />)

    await waitFor(() => expect(screen.getByText('COVID liquidity shock')).toBeInTheDocument())
    expect(screen.getByText('92%')).toBeInTheDocument()
    expect(screen.getByText('mixed')).toBeInTheDocument()
  })
})
