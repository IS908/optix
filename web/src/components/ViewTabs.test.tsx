import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ViewTabs } from './ViewTabs'

describe('ViewTabs', () => {
  it('renders five tabs and fires onSelect', () => {
    const onSelect = vi.fn()
    render(<ViewTabs active="intraday" locked={false} onSelect={onSelect} onFollow={() => {}} />)
    fireEvent.click(screen.getByText('冲击'))
    expect(onSelect).toHaveBeenCalledWith('shock')
    expect(screen.queryByText('跟随时钟')).toBeNull() // 未锁定不显示回位
  })

  it('shows follow-clock button only when locked', () => {
    const onFollow = vi.fn()
    render(<ViewTabs active="event" locked={true} onSelect={() => {}} onFollow={onFollow} />)
    fireEvent.click(screen.getByText('跟随时钟'))
    expect(onFollow).toHaveBeenCalled()
  })
})
