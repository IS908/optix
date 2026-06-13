import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { PostcloseTimelineCard } from './PostcloseTimelineCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('PostcloseTimelineCard', () => {
  it('renders structured events', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          events: [
            {
              ts: '2026-06-13T00:00:00Z',
              symbol: 'AAPL',
              kind: 'mover',
              title: 'AAPL 盘后异动 +3.20%',
              detail: '常规盘 +1.00%，全天合并 +4.20%',
              severity: 'positive',
            },
          ],
        }),
      }),
    )

    render(<PostcloseTimelineCard />)

    await waitFor(() => expect(screen.getByText(/AAPL 盘后异动/)).toBeInTheDocument())
    expect(screen.getByText(/全天合并/)).toBeInTheDocument()
  })
})
