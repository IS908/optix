import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// vite.config 未开 globals，testing-library 不会自动注册 cleanup。手动注册一次，
// 否则上个用例遗留的已挂载组件/hook（及其 visibilitychange 监听器）会污染下个用例。
afterEach(cleanup)
