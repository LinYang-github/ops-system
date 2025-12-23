import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    // 代理配置
    proxy: {
      '/api': {
        target: 'http://localhost:8080', // 指向你的 Go Master 端口
        changeOrigin: true,
      },
      '/download': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})