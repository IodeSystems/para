import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react-swc'

// bin/dev exports API_PORT (para serve, 7700), UI_PORT (API_PORT + 1), and
// UI_HOST (127.0.0.1 unless dev.local.env sets it).
const apiPort = Number(process.env.API_PORT ?? 7700)
const uiPort = Number(process.env.UI_PORT ?? apiPort + 1)

export default defineConfig({
  plugins: [
    tanstackRouter({ routesDirectory: 'src/routes', generatedRouteTree: 'src/routeTree.gen.ts' }),
    react(),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    host: process.env.UI_HOST ?? '127.0.0.1',
    port: uiPort,
    strictPort: true,
    proxy: {
      '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true, ws: true },
      '/mcp': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true, ws: true },
    },
  },
})
