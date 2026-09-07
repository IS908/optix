import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ShockFingerprintCard } from './ShockFingerprintCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ShockFingerprintCard', () => {
  it('labels the loading state', () => {
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))

    render(<ShockFingerprintCard />)

    expect(screen.getByText('冲击指纹')).toBeInTheDocument()
    expect(screen.getByTestId('shock-fingerprint-loading')).toBeInTheDocument()
  })

  it('renders active fingerprints', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'computed',
          rows: [
            { kind: 'liquidity', label: 'Liquidity shock', score: 90, normalized_score: 0.9, active: true, evidence: ['VIX up'], missing: [] },
            { kind: 'policy', label: 'Policy shock', score: 55, normalized_score: 0.55, active: true, evidence: ['rates up'], missing: [] },
          ],
        }),
      }),
    )

    render(<ShockFingerprintCard />)

    await waitFor(() => expect(screen.getByText('Liquidity shock')).toBeInTheDocument())
    expect(screen.getByText('Policy shock')).toBeInTheDocument()
    expect(screen.getByText('90')).toBeInTheDocument()
  })

  it('renders option stress rows when available', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          as_of: '2026-06-13T15:30:00Z',
          source: 'computed',
          rows: [
            { kind: 'liquidity', label: 'Liquidity shock', score: 70, normalized_score: 0.7, active: true, evidence: ['SPY option IV skew elevated'], missing: [] },
          ],
          option_stress: [
            {
              underlying: 'SPY',
              source: 'ibkr',
              basis: 'delayed',
              as_of: '2026-06-13T15:30:00Z',
              iv_skew: 0.08,
              volume: 2500,
              open_interest: 12000,
              note: 'exp=20260717 atm_iv=0.42 put_call_iv_skew=0.08',
            },
          ],
        }),
      }),
    )

    render(<ShockFingerprintCard />)

    await waitFor(() => expect(screen.getByText('SPY')).toBeInTheDocument())
    expect(screen.getByText('IV skew +8.0%')).toBeInTheDocument()
    expect(screen.getByText('OI 12000')).toBeInTheDocument()
  })
})

it('shows unavailable partial metrics without turning them into zeros', async () => {
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue({ok:true,json:async()=>({source:'ibkr',rows:[],option_stress:[{underlying:'SPY',basis:'realtime',iv_skew:0,volume:0,open_interest:25,missing_metrics:['iv_skew','volume']}]})}))
 render(<ShockFingerprintCard />)
 await waitFor(()=>expect(screen.getByText('SPY')).toBeInTheDocument())
 expect(screen.getByText('IV skew —')).toBeInTheDocument()
 expect(screen.getByText('Vol —')).toBeInTheDocument()
 expect(screen.getByText('OI 25')).toBeInTheDocument()
})

it('renders a measured zero skew', async () => {
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue({ok:true,json:async()=>({source:'ibkr',rows:[],option_stress:[{underlying:'SPY',basis:'realtime',iv_skew:0,volume:5,open_interest:25,missing_metrics:[]}]})}))
 render(<ShockFingerprintCard />)
 await waitFor(()=>expect(screen.getByText('IV skew 0.0%')).toBeInTheDocument())
})
