import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
  },
  server: {
    strictPort: true,
    proxy: {
      // Proxy API + WS calls to gateway in dev mode (avoids CORS)
      '/v1': {
        target: 'http://localhost:18790',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:18790',
        ws: true,
      },
      '/health': {
        target: 'http://localhost:18790',
        changeOrigin: true,
      },
    },
  },
})
