import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/auth': { target: 'http://127.0.0.1:8790', changeOrigin: false },
      '/api': { target: 'http://127.0.0.1:8790', changeOrigin: true },
    },
  },
  build: { sourcemap: true },
})
