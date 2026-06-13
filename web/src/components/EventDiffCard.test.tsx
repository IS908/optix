import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { EventDiffCard } from './EventDiffCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('EventDiffCard', () => {
  it('renders statement verdict and changed sentences', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T14:00:00Z',
          source: 'local_statement_fixture',
          prior_title: 'prior',
          current_title: 'current',
          prior_published_at: '2026-03-18T18:00:00Z',
          current_published_at: '2026-04-29T18:00:00Z',
          added: [{ text: 'Inflation remains elevated', status: 'added', hits: ['hawkish'] }],
          removed: [{ text: 'Inflation has eased', status: 'removed', hits: ['dovish'] }],
          unchanged: [],
          hawkish_hits: 1,
          dovish_hits: 1,
          verdict: 'mixed',
        }),
      }),
    )

    render(<EventDiffCard />)

    await waitFor(() => expect(screen.getByText('mixed')).toBeInTheDocument())
    expect(screen.getByText(/Inflation remains elevated/)).toBeInTheDocument()
    expect(screen.getByText(/hawkish 1/)).toBeInTheDocument()
  })
})
