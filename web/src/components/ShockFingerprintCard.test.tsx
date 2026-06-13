import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ShockFingerprintCard } from './ShockFingerprintCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ShockFingerprintCard', () => {
  it('renders active fingerprints', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'computed',
          rows: [
            { kind: 'liquidity', label: 'Liquidity shock', score: 90, confidence: 0.9, active: true, evidence: ['VIX up'], missing: [] },
            { kind: 'policy', label: 'Policy shock', score: 55, confidence: 0.55, active: true, evidence: ['rates up'], missing: [] },
          ],
        }),
      }),
    )

    render(<ShockFingerprintCard />)

    await waitFor(() => expect(screen.getByText('Liquidity shock')).toBeInTheDocument())
    expect(screen.getByText('Policy shock')).toBeInTheDocument()
    expect(screen.getByText('90')).toBeInTheDocument()
  })
})
