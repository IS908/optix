import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { OvernightChainCard } from './OvernightChainCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('OvernightChainCard', () => {
  it('renders links and consistency note', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '',
          links: [
            { id: 'N225', label: '东京 N225', pct: 1.2, basis: 'delayed', as_of: '' },
            { id: 'ES', label: '美期 ES', pct: 0.3, basis: 'delayed', as_of: '' },
          ],
          consistency: { same_dir: 2, total: 2, note: '2 环方向一致 ↑' },
        }),
      }),
    )

    render(<OvernightChainCard />)

    await waitFor(() => expect(screen.getByText('东京 N225')).toBeInTheDocument())
    expect(screen.getByText('+1.20%')).toBeInTheDocument()
    expect(screen.getByText(/方向一致/)).toBeInTheDocument()
  })
})
