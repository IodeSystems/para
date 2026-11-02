import { execSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type PluginOption, type ProxyOptions } from 'vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react-swc'
import { VitePWA } from 'vite-plugin-pwa'

// bin/dev exports API_PORT (para serve, 7700), UI_PORT (API_PORT + 1), and UI_HOST (127.0.0.1
// unless dev.local.env sets it).
const apiPort = Number(process.env.API_PORT ?? 7700)
const uiPort = Number(process.env.UI_PORT ?? apiPort + 1)
const apiHost = `127.0.0.1:${apiPort}`

// Cache-bust escape hatch. A normal deploy already busts the precache (new asset hashes, new
// service worker). Bump this ONLY to make every client drop its precache when a bad asset was
// recorded with no other code change: it changes the workbox cacheId, so old caches are orphaned
// and cleanupOutdatedCaches evicts them.
const CACHE_BUST = 1

// The paths the Go server owns. The service worker must pass these to the network and never
// answer a navigation with the app shell. One list, in a file a Go test reads
// (internal/server/ui_test.go sweeps server.ServerGETRoutes against it).
const denylist = (JSON.parse(readFileSync('pwa-denylist.json', 'utf8')) as string[]).map(
  (s) => new RegExp(s),
)

// Build identity, written into index.html as <meta> tags and not into the JS: a stamp inside the
// entry chunk would change the entry on every build, and with it every chunk that imports it.
const git = (args: string): string => {
  try {
    return execSync(`git ${args}`, { encoding: 'utf8' }).trim() || 'dev'
  } catch {
    return 'dev'
  }
}
const UI_VERSION = git('describe --always --dirty')
const UI_COMMIT = git('rev-parse --short=9 HEAD')
const UI_BUILT = new Date().toISOString()

function buildStampMeta(): PluginOption {
  const meta = (name: string, content: string) => ({
    tag: 'meta',
    attrs: { name, content },
    injectTo: 'head' as const,
  })
  return {
    name: 'build-stamp-meta',
    transformIndexHtml: () => [
      meta('ui-version', UI_VERSION),
      meta('ui-commit', UI_COMMIT),
      meta('ui-built', UI_BUILT),
    ],
  }
}

// The dev server is a different origin from `para serve`, and the gate checks Host and Origin.
// Send the API's own Host (changeOrigin) and an Origin that names it, so a POST passes the Origin
// check. The browser's cookie is for the Vite host and rides through unchanged. `para serve` must
// run with --vault so the bare host is served (bin/dev does that).
const toApi: ProxyOptions = {
  target: `http://${apiHost}`,
  changeOrigin: true,
  ws: true,
  configure: (proxy) => {
    const fix = (req: { getHeader(n: string): unknown; setHeader(n: string, v: string): void }) => {
      if (req.getHeader('origin')) req.setHeader('origin', `http://${apiHost}`)
    }
    proxy.on('proxyReq', fix)
    proxy.on('proxyReqWs', fix)
  },
}

// The service worker precaches the app shell only. Mermaid and the diagram chunks it loads on
// demand are large and are needed only when a screen draws a diagram, so they stay out of the
// precache. After the bundle is known, the shell is every chunk reachable from the entry without
// entering the chunk that holds mermaid; everything else goes in `notShell`, which the manifest
// transform drops.
const notShell: string[] = []

function diagramsOutOfPrecache(): PluginOption {
  return {
    name: 'diagrams-out-of-precache',
    apply: 'build',
    generateBundle(_o, bundle) {
      const chunks = Object.values(bundle).filter((c) => c.type === 'chunk')
      const byName = new Map(chunks.map((c) => [c.fileName, c]))
      // The chunk that holds the mermaid package is the door to the diagram code.
      const door = new Set(
        chunks
          .filter((c) => c.moduleIds.some((m) => /node_modules\/\.pnpm\/mermaid@/.test(m)))
          .map((c) => c.fileName),
      )
      const entry = chunks.find((c) => c.isEntry)
      if (door.size === 0 || !entry) return
      const shell = new Set<string>()
      const walk = (name: string) => {
        if (shell.has(name) || door.has(name)) return
        shell.add(name)
        const c = byName.get(name)
        for (const n of [...(c?.imports ?? []), ...(c?.dynamicImports ?? [])]) walk(n)
      }
      walk(entry.fileName)
      notShell.length = 0
      for (const c of chunks) if (!shell.has(c.fileName)) notShell.push(c.fileName)
    },
  }
}

// Vendor code in a few stable chunks, so an app-only deploy re-downloads no vendor chunk.
function vendorChunk(id: string): string | undefined {
  if (!id.includes('node_modules')) return undefined
  if (/node_modules\/\.pnpm\/(react|react-dom|scheduler)@/.test(id)) return 'vendor-react'
  if (/node_modules\/\.pnpm\/(@mui|@emotion)/.test(id)) return 'vendor-mui'
  return undefined
}

export default defineConfig({
  plugins: [
    tanstackRouter({
      routesDirectory: 'src/routes',
      generatedRouteTree: 'src/routeTree.gen.ts',
      autoCodeSplitting: true,
    }),
    react(),
    // App shell only. `prompt`: a new version waits, and PwaUpdatePrompt reloads silently within
    // 10 s of load or asks. No runtimeCaching: vault content and API answers are never cached.
    VitePWA({
      registerType: 'prompt',
      strategies: 'generateSW',
      injectRegister: false,
      workbox: {
        cacheId: `para-v${CACHE_BUST}`,
        navigateFallback: '/index.html',
        navigateFallbackDenylist: denylist,
        cleanupOutdatedCaches: true,
        clientsClaim: true,
        maximumFileSizeToCacheInBytes: 4 * 1024 * 1024,
        // Chunks that only a diagram needs stay out of the precache (diagramsOutOfPrecache).
        manifestTransforms: [
          // Read at generation time, after generateBundle has filled notShell.
          (entries) => ({
            manifest: entries.filter((e) => !notShell.includes(e.url)),
            warnings: [],
          }),
        ],
      },
      manifest: {
        name: 'para',
        short_name: 'para',
        description: 'Tasks and blockers',
        theme_color: '#1f3a5f',
        background_color: '#f6f7f9',
        display: 'standalone',
        scope: '/',
        start_url: '/',
        icons: [
          { src: 'pwa-192x192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512x512.png', sizes: '512x512', type: 'image/png' },
          {
            src: 'pwa-maskable-512x512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
    }),
    buildStampMeta(),
    diagramsOutOfPrecache(),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    rollupOptions: {
      output: { manualChunks: vendorChunk },
    },
  },
  server: {
    host: process.env.UI_HOST ?? '127.0.0.1',
    port: uiPort,
    strictPort: true,
    // Every path `para serve` owns (ServerGETRoutes), one entry each.
    proxy: {
      '/api': toApi,
      '/auth': toApi,
      '/mcp': toApi,
      '/healthz': toApi,
      '/ws': toApi,
    },
  },
})
