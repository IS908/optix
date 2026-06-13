import { useEffect, useRef, useState } from 'react'

export interface PollState<T> {
  data: T | null
  error: string | null
  stale: boolean // 连续失败中（指数退避运行）
}

// 轮询 hook：固定 interval；失败指数退避（×2，封顶 4×interval ≈ 30→60→120s）；
// 标签页隐藏时不发请求（仅保持排程），回到前台立即刷新。
export function usePoll<T>(url: string, intervalMs: number): PollState<T> {
  const [state, setState] = useState<PollState<T>>({ data: null, error: null, stale: false })
  const failsRef = useRef(0)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    let aborted = false
    failsRef.current = 0
    setState({ data: null, error: null, stale: false })

    const delay = () => Math.min(intervalMs * 2 ** failsRef.current, intervalMs * 4)
    const schedule = () => {
      timer = setTimeout(tick, delay())
    }
    const tick = async () => {
      if (document.visibilityState === 'hidden') {
        schedule()
        return
      }
      try {
        const res = await fetch(url)
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = (await res.json()) as T
        if (aborted) return
        failsRef.current = 0
        setState({ data, error: null, stale: false })
      } catch (e) {
        if (aborted) return
        failsRef.current += 1
        setState((prev) => ({ data: prev.data, error: String(e), stale: true }))
      }
      if (!aborted) schedule()
    }

    tick()
    const onVisible = () => {
      if (document.visibilityState === 'visible' && !aborted) {
        if (timer) clearTimeout(timer)
        void tick()
      }
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      aborted = true
      if (timer) clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [url, intervalMs])

  return state
}
