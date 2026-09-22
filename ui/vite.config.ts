import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  server: {
    port: 5173,
    // Proxy API + agent/mission control-plane calls to the core server (HTTP on :8080),
    // and PocketBase (auth / collections) to the embedded instance on :8090.
    proxy: {
      '/api.v1.': { target: 'http://localhost:8080', changeOrigin: true },
      '/agents': { target: 'http://localhost:8080', changeOrigin: true },
      '/missions': { target: 'http://localhost:8080', changeOrigin: true },
      '/swagger': { target: 'http://localhost:8080', changeOrigin: true },
      '/api/': { target: 'http://localhost:8090', changeOrigin: true },
      '/_/': { target: 'http://localhost:8090', changeOrigin: true },
    },
  },
})
