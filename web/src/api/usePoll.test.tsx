import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { usePoll } from './usePoll'

describe('usePoll', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('fetches immediately and keeps data on later failure (stale)', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ n: 1 }) })
      .mockRejectedValue(new Error('down'))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => usePoll<{ n: number }>('/x', 30_000))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(result.current.data).toEqual({ n: 1 })
    expect(result.current.stale).toBe(false)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000) // 第二次（失败）
    })
    expect(result.current.data).toEqual({ n: 1 }) // 旧数据保留
    expect(result.current.stale).toBe(true)
  })

  it('backs off exponentially up to 4x', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('down'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => usePoll('/x', 30_000))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000) // fail#1 后退避 60s
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(119_000) // fail#2 后退避 120s：未到不发
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('skips fetch while hidden', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    renderHook(() => usePoll('/x', 30_000))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90_000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
