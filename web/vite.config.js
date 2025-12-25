import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/download': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  // --- 新增以下构建配置 ---
  build: {
    // 将警告阈值调大到 2000kb (2MB)
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        // 如果你实在想拆分文件，可以用这个配置（可选，不建议）
        // manualChunks(id) {
        //   if (id.includes('node_modules')) {
        //     return 'vendor';
        //   }
        // }
      }
    }
  }
})