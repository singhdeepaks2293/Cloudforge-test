import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// Same-origin API proxy (Milestone 9): the frontend client uses
// relative URLs when VITE_API_URL is unset, and the dev server
// forwards /api, /healthz, /readyz and /metrics to the Go API on
// :8080 — the same paths nginx proxies in the Docker deployment.
// Setting VITE_API_URL still bypasses the proxy (split deployment).
const apiProxy = {
  '/api': 'http://localhost:8080',
  '/healthz': 'http://localhost:8080',
  '/readyz': 'http://localhost:8080',
  '/metrics': 'http://localhost:8080',
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    host: true,
    proxy: apiProxy,
  },
  preview: {
    port: 4173,
    host: true,
    proxy: apiProxy,
  },
})
