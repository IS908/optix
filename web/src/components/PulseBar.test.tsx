import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PulseBar } from './PulseBar'
import type { PulseResponse } from '../api/types'

const pulse: PulseResponse = {
  snapshot_at: '2026-06-12T13:00:00Z',
  view: 'intraday',
  view_inferred: true,
  assets: [
    { id: 'SPX', class: 'index', label: 'SPX', price: 7361.59, change: 93.9, change_pct: 1.29, basis: 'delayed', source: 'yfinance', basis_note: 'yfinance delayed quote', as_of: '2026-06-12T13:00:00Z' },
    { id: 'SOX_PROXY', class: 'stock', label: 'SOX (via SOXX)', price: null, change: 0, change_pct: -1.38, basis: 'approx', source: 'yfinance', basis_note: 'SOXX ETF pct-only proxy for SOX premarket; no SOX index level', as_of: '2026-06-12T13:00:00Z' },
  ],
  missing: ['US10Y'],
}

describe('PulseBar', () => {
  it('renders price, em-dash for pct-only, and greyed missing', () => {
    render(<PulseBar pulse={pulse} stale={false} />)
    expect(screen.getByText('7,361.59')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument() // pctOnly 无点位
    expect(screen.getByText('US10Y')).toBeInTheDocument()
    expect(screen.getByText('缺席')).toBeInTheDocument()
    expect(screen.getByText('yfinance · delayed')).toBeInTheDocument()
    expect(screen.getByText(/SOXX ETF pct-only proxy/)).toBeInTheDocument()
  })

  it('shows stale banner and loading skeleton', () => {
    render(<PulseBar pulse={pulse} stale={true} />)
    expect(screen.getByText(/数据延迟/)).toBeInTheDocument()
    const { container } = render(<PulseBar pulse={null} stale={false} />)
    expect(container.querySelector('[data-testid="pulse-loading"]')).toBeTruthy()
  })
})
