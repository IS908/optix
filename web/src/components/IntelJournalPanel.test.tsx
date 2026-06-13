import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { IntelJournalPanel } from './IntelJournalPanel'
import type { IntelJournalSnapshot } from '../api/types'

const snap: IntelJournalSnapshot = {
  trading_date: '2026-06-12',
  now: '2026-06-12T11:00:00-04:00',
  is_trading_day: true,
  checkpoints: [
    { kind: 'script', label: '剧本', due_at: '...', status: 'written' },
    { kind: 'first_check', label: '首验', due_at: '...', status: 'due' },
    { kind: 'set_tone', label: '定调', due_at: '...', status: 'pending' },
    { kind: 'reconcile', label: '对账', due_at: '...', status: 'pending' },
  ],
  narratives: [
    { entry_id: 'e1', trading_date: '2026-06-12', checkpoint: 'script', phase: 'premarket', body: '今日看涨', created_at: '...' },
  ],
  judgments: [
    { judgment_id: 'j1', trading_date: '2026-06-12', checkpoint: 'first_check', asset_id: 'SPX', asset_class: 'index', direction: 'up', threshold_pct: 0.5, confidence: 75, expiry_checkpoint: 'reconcile', expiry_at: '...', registered_price: 4200, registered_basis: 'delayed', created_at: '...' },
    { judgment_id: 'j2', trading_date: '2026-06-12', checkpoint: 'first_check', asset_id: 'VIX', asset_class: 'index', direction: 'down', threshold_pct: 0, confidence: 60, expiry_checkpoint: 'reconcile', expiry_at: '...', registered_price: 18, registered_basis: 'delayed', created_at: '...', reconciliation: { judgment_id: 'j2', expiry_price: 17, expiry_basis: 'delayed', outcome: 'hit', delta_pct: -5.5, settled_at: '...' } },
  ],
  hit_rate: { window: 'today', hit: 1, miss: 0, rate: 1 },
}

describe('IntelJournalPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => snap }))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders checkpoints, latest narrative, judgments and hit-rate', async () => {
    render(<IntelJournalPanel />)
    await waitFor(() => expect(screen.getByText('今日看涨')).toBeInTheDocument())
    expect(screen.getByText(/命中率 1\/1/)).toBeInTheDocument()
    expect(screen.getByText('SPX')).toBeInTheDocument()
    expect(screen.getByText(/⏳待结算/)).toBeInTheDocument() // j1 未结算
    expect(screen.getByText(/hit/)).toBeInTheDocument() // j2 已结算 hit
  })

  it('escapes agent prose (no XSS via dangerouslySetInnerHTML)', async () => {
    const xss = { ...snap, narratives: [{ ...snap.narratives[0], body: '<script>alert(1)</script>' }] }
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => xss })
    const { container } = render(<IntelJournalPanel />)
    await waitFor(() => expect(screen.getByText('<script>alert(1)</script>')).toBeInTheDocument())
    // 文本被转义渲染,DOM 里没有真实 <script> 元素
    expect(container.querySelector('script')).toBeNull()
  })
})
