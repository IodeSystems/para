import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'

// Bundles src/background.ts to dist/background.js and emits manifest.json beside it.
export default defineConfig({
  publicDir: false,
  build: {
    target: 'es2023',
    rollupOptions: {
      input: { background: 'src/background.ts' },
      output: { entryFileNames: '[name].js', format: 'es' },
    },
  },
  plugins: [
    {
      name: 'emit-manifest',
      generateBundle() {
        this.emitFile({
          type: 'asset',
          fileName: 'manifest.json',
          source: readFileSync('manifest.json', 'utf8'),
        })
      },
    },
  ],
})
