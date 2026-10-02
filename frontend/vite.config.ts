import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    // 热更新模式（npm run dev:hot）下页面由 Vite 提供，
    // 后端绑定调用（@wailsio/runtime 走 window.location.origin + /wails/...）
    // 转发到 server 模式起的 Go 进程。
    proxy: {
      '/wails': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
