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
    port: 8320,
    // Proxy API + agent/mission control-plane calls to the core server (HTTP on :8321),
    // and PocketBase (auth / collections) to the embedded instance on :8090.
    proxy: {
      '/api.v1.': { target: 'http://localhost:8321', changeOrigin: true },
      '/agents': { target: 'http://localhost:8321', changeOrigin: true },
      '/missions': { target: 'http://localhost:8321', changeOrigin: true },
      // Scoped to the live SSE endpoint only, not '/telemetry' -- that
      // prefix would also swallow the SPA's own /telemetry/:agentId client
      // routes (direct navigation/refresh hits the dev server for real,
      // unlike client-side <Link> navigation, and core has no route for
      // most of that prefix).
      '/telemetry/live': { target: 'http://localhost:8321', changeOrigin: true },
      '/swagger': { target: 'http://localhost:8321', changeOrigin: true },
      '/api/': { target: 'http://localhost:8090', changeOrigin: true },
      '/_/': { target: 'http://localhost:8090', changeOrigin: true },
    },
  },
})
