import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { EventSensitivityCard } from './EventSensitivityCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('EventSensitivityCard', () => {
  it('renders sensitivity scores', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T14:00:00Z',
          source: 'computed from event-window daily returns',
          rows: [{ id: 'VIX', label: 'VIX', risk_on: -1, rates_up: 0, dollar_up: 0.5, sample_n: 6 }],
        }),
      }),
    )

    render(<EventSensitivityCard />)

    await waitFor(() => expect(screen.getByText('VIX')).toBeInTheDocument())
    expect(screen.getByText('-1.00')).toBeInTheDocument()
    expect(screen.getByText('+0.50')).toBeInTheDocument()
  })
})
