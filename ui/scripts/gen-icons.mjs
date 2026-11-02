// Renders public/icon.svg to the PNG icons with headless Chromium (playwright).
// Run: node scripts/gen-icons.mjs. The PNGs are committed; this is not part of the build.
// The maskable icon keeps its art inside the 80% safe zone on a full-bleed background.
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const root = join(import.meta.dirname, '..', 'public')
const svg = readFileSync(join(root, 'icon.svg'), 'utf8')
const inner = svg.replace(/^[\s\S]*?<rect[^>]*\/>/, '').replace('</svg>', '')

const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage()
async function render(name, size, html) {
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(`<body style="margin:0">${html(size)}</body>`)
  await page.screenshot({ path: join(root, name), omitBackground: false })
}
const plain = (s) =>
  `<img width=${s} height=${s} src="data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}">`
const maskable = (s) => {
  const art = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><rect width="512" height="512" fill="#1f3a5f"/><g transform="translate(76.8 76.8) scale(0.7)">${inner}</g></svg>`
  return `<img width=${s} height=${s} src="data:image/svg+xml;base64,${Buffer.from(art).toString('base64')}">`
}
await render('pwa-192x192.png', 192, plain)
await render('pwa-512x512.png', 512, plain)
await render('pwa-maskable-512x512.png', 512, maskable)
await render('apple-touch-icon.png', 180, plain)
await browser.close()
