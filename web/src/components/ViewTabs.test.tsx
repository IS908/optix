import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ViewTabs } from './ViewTabs'

describe('ViewTabs', () => {
  it('renders five tabs and fires onSelect', () => {
    const onSelect = vi.fn()
    render(<ViewTabs active="intraday" autoView="intraday" locked={false} onSelect={onSelect} onFollow={() => {}} />)
    fireEvent.click(screen.getByText('冲击'))
    expect(onSelect).toHaveBeenCalledWith('shock')
    expect(screen.queryByText('跟随自动')).toBeNull() // 未锁定不显示回位
  })

  it('shows follow-clock button only when locked', () => {
    const onFollow = vi.fn()
    render(<ViewTabs active="event" autoView="event" locked={true} onSelect={() => {}} onFollow={onFollow} />)
    fireEvent.click(screen.getByText('跟随自动'))
    expect(onFollow).toHaveBeenCalled()
  })

  it('labels automatic event and shock tabs separately from manual lock', () => {
    render(<ViewTabs active="event" autoView="event" locked={false} overrideReason="Jun CPI" onSelect={() => {}} onFollow={() => {}} />)
    expect(screen.getByText('自动触发：Jun CPI')).toBeInTheDocument()
    expect(screen.getByText('自动')).toBeInTheDocument()
  })
})
