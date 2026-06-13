import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SentimentCard } from './SentimentCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('SentimentCard', () => {
  it('renders regime and degraded P/C', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          pc_oi: 0,
          pc_vol: 0,
          pc_available: false,
          vix: 20,
          vix3m: 22,
          vix_term_premium: 1.1,
          regime: '偏多',
          degraded_note: '降级口径',
        }),
      }),
    )

    render(<SentimentCard />)

    await waitFor(() => expect(screen.getByText('偏多')).toBeInTheDocument())
    expect(screen.getByText('不可用')).toBeInTheDocument()
    expect(screen.getByText(/contango/)).toBeInTheDocument()
  })
})
