// Checks the dev stack: `API_PORT=7710 bin/dev -d` first. Signs in through the Vite proxy on
// 127.0.0.1:7711 with the admin token of the dev vault, and checks that a POST (capture) passes
// the Origin check and that /ws connects through the proxy.
import { readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { chromium } from 'playwright'

const repo = resolve(import.meta.dirname, '..', '..')
const base = `http://127.0.0.1:${process.env.UI_PORT ?? 7711}`
const token = readFileSync(join(repo, 'tmp/dev-home/vaults/dev/server/admin.token'), 'utf8').trim()

const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage()
let ws = false
page.on('websocket', () => (ws = true))
await page.goto(`${base}/#token=${token}`)
await page.getByRole('heading', { name: 'Today' }).waitFor()
await page.keyboard.press('c')
await page.getByLabel('What is on your mind?').fill('From the dev stack')
await page.keyboard.press('Control+Enter')
await page.getByText('From the dev stack').waitFor()
await new Promise((r) => setTimeout(r, 500))
await browser.close()
if (!ws) throw new Error('no WebSocket opened through the proxy')
console.log('dev stack ok: sign-in, POST through the proxy, /ws through the proxy')
