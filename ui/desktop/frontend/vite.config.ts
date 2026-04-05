import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
