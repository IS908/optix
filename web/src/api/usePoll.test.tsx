import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { usePoll } from './usePoll'

// 组件 unmount（移除 visibilitychange 监听器）由 test-setup.ts 的全局 afterEach(cleanup)
// 负责；这里只复位 timer/mock/global stub。
describe('usePoll', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
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

  it('does not spawn a parallel poll loop when re-shown mid-flight', async () => {
    // 一次永不 resolve 的 fetch：把首个 tick 卡在 in-flight，
    // 期间触发 visibilitychange，确认不会另起第二条轮询链。
    let resolveFetch: (v: unknown) => void = () => {}
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise((res) => { resolveFetch = res }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const visGetter = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')

    renderHook(() => usePoll('/x', 30_000))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0) // 首个 tick 发起 fetch，卡在 in-flight
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // 标签页隐藏→重现：onVisible 在 in-flight 时必须不另起 tick
    await act(async () => {
      visGetter.mockReturnValue('hidden')
      document.dispatchEvent(new Event('visibilitychange'))
      visGetter.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(fetchMock).toHaveBeenCalledTimes(1) // 仍只有一条在途请求

    // 解开在途请求 → 正常 schedule 下一次，总链路只有一条
    await act(async () => {
      resolveFetch({ ok: true, json: async () => ({ n: 1 }) })
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2) // 一次解开 + 一次定时，无并发倍增
  })
})
