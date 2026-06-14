import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SlotGrid } from './SlotGrid'

function journalResponse() {
  return Promise.resolve({
    ok: true,
    json: () =>
      Promise.resolve({
        trading_date: '2026-06-12',
        now: '2026-06-12T11:00:00-04:00',
        is_trading_day: true,
        checkpoints: [],
        narratives: [],
        judgments: [],
        hit_rate: { window: 'today', hit: 0, miss: 0, rate: 0 },
      }),
  } as Response)
}

describe('SlotGrid', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(journalResponse()))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('does not render unsupported intraday placeholder cards', async () => {
    render(<SlotGrid view="intraday" />)

    await waitFor(() => expect(screen.getByText('叙事流')).toBeInTheDocument())
    expect(screen.queryByText('盘中异动')).toBeNull()
    expect(screen.queryByText('板块热力')).toBeNull()
  })
})
