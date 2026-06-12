/// <reference types="vitest/config" />
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// base=/intel/：产物挂在 optix-server 的 /intel/ 子路径下。
export default defineConfig({
  base: '/intel/',
  plugins: [react(), tailwindcss()],
  server: {
    // dev 期：vite :5173 代理 API 到 optix server :8080
    proxy: { '/api': 'http://127.0.0.1:8080' },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test-setup.ts',
  },
})
