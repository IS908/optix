import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { GapFillCard } from './GapFillCard'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('GapFillCard', () => {
  it('renders implied gap and fill rate', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          symbol: 'SPX',
          implied_gap_pct: 0.7,
          direction: 'up',
          band: '0.5-1',
          hist_fill_rate: 0.62,
          sample_n: 143,
          lookback_days: 504,
          by_band: [{ symbol: 'SPX', direction: 'up', band: '0.5-1', fill_rate: 0.62, sample_n: 143, lookback_days: 504, as_of: '' }],
          as_of: '',
        }),
      }),
    )

    render(<GapFillCard />)

    await waitFor(() => expect(screen.getByText(/SPX 隐含跳空/)).toBeInTheDocument())
    expect(screen.getByText('↑0.70%')).toBeInTheDocument()
    expect(screen.getAllByText('62%').length).toBeGreaterThan(0)
  })

  // #175 — backend emits direction='' for sub-threshold gaps (|gapPct| < 0.25%)
  // while still returning the signed pct. The headline must render a neutral
  // glyph + neutral color rather than picking the arrow from one source and
  // the color from another (which produced "↑ in red" for sub-threshold
  // negative gaps).
  it('renders a neutral glyph and color for sub-threshold gaps (empty direction)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          symbol: 'SPX',
          implied_gap_pct: -0.1,
          direction: '',
          band: '',
          hist_fill_rate: 0,
          sample_n: 0,
          lookback_days: 504,
          by_band: [],
          as_of: '',
        }),
      }),
    )

    render(<GapFillCard />)

    await waitFor(() => expect(screen.getByText(/SPX 隐含跳空/)).toBeInTheDocument())
    // Neutral arrow (→), NOT ↑ or ↓.
    const glyphSpan = screen.getByText(/0\.10%/)
    expect(glyphSpan.textContent).toContain('→')
    expect(glyphSpan.textContent).not.toContain('↑')
    expect(glyphSpan.textContent).not.toContain('↓')
    // Neutral color (zinc), NOT red or emerald — glyph and color must agree.
    expect(glyphSpan.className).toMatch(/text-zinc/)
    expect(glyphSpan.className).not.toMatch(/text-red/)
    expect(glyphSpan.className).not.toMatch(/text-emerald/)
  })

  it('renders warnings and treats null band stats as empty degraded data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          symbol: 'SPX',
          implied_gap_pct: -0.4,
          direction: 'down',
          band: '',
          hist_fill_rate: 0,
          sample_n: 0,
          lookback_days: 504,
          by_band: null,
          as_of: '',
          warnings: ['gap stats cache unavailable'],
        }),
      }),
    )

    render(<GapFillCard />)

    await waitFor(() => expect(screen.getByText(/SPX 隐含跳空/)).toBeInTheDocument())
    expect(screen.getByText('历史统计不可用')).toBeInTheDocument()
    expect(screen.getByText('gap stats cache unavailable')).toBeInTheDocument()
  })
})
