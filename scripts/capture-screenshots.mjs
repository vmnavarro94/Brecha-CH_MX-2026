#!/usr/bin/env node
//
// Capture dashboard screenshots for docs/img/.
//
// Usage:
//   node scripts/capture-screenshots.mjs [base-url] [wait-seconds]
//
// Defaults:
//   base-url      = http://localhost:8088
//   wait-seconds  = 45  (lets spread models warm and at least one trade execute)

import puppeteer from 'puppeteer'
import { mkdir, writeFile } from 'node:fs/promises'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OUT_DIR = join(__dirname, '..', 'docs', 'img')

const BASE_URL = process.argv[2] ?? 'http://localhost:8088'
const WARM_SECONDS = Number(process.argv[3] ?? 45)

const VIEWPORT = { width: 1600, height: 1100, deviceScaleFactor: 2 }

const PANELS = [
  { name: 'statusbar',         selector: '#bx-top' },
  { name: 'spread-chart',      selector: '#bx-zscore' },
  { name: 'price-table',       selector: '#bx-prices' },
  { name: 'pnl-chart',         selector: '#bx-pnl' },
  { name: 'spread-heatmap',    selector: '#bx-heatmap' },
  { name: 'opportunity-feed',  selector: '#bx-feed' },
  { name: 'tweaks-panel',      selector: '.tw-panel' },
  { name: 'trade-history',     selector: '#bx-trades' },
  { name: 'backtest-panel',    selector: '#bx-backtest' },
]

async function ensureDir(p) {
  await mkdir(p, { recursive: true })
}

async function snapPanel(page, selector, outPath) {
  const handle = await page.$(selector)
  if (!handle) {
    console.warn(`[skip] selector not found: ${selector}`)
    return false
  }
  await handle.screenshot({ path: outPath })
  return true
}

async function snapFull(page, outPath) {
  await page.screenshot({ path: outPath, fullPage: true })
}

async function snapClip(page, x, y, width, height, outPath) {
  await page.screenshot({ path: outPath, clip: { x, y, width, height } })
}

async function main() {
  await ensureDir(OUT_DIR)

  const browser = await puppeteer.launch({
    headless: 'new',
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
  })
  const page = await browser.newPage()
  await page.setViewport(VIEWPORT)

  console.log(`Opening ${BASE_URL}…`)
  await page.goto(BASE_URL, { waitUntil: 'networkidle2', timeout: 30_000 })

  console.log(`Waiting ${WARM_SECONDS}s for spread models to warm + trades to flow…`)
  await new Promise((r) => setTimeout(r, WARM_SECONDS * 1000))

  console.log('Capturing full dashboard…')
  await snapFull(page, join(OUT_DIR, '00-dashboard-full.png'))

  for (const { name, selector } of PANELS) {
    const outPath = join(OUT_DIR, `${name}.png`)
    const ok = await snapPanel(page, selector, outPath)
    console.log(`  ${ok ? '✓' : '✗'} ${name} (${selector})`)
  }

  // Pair picker open state.
  try {
    const btn = await page.$('[data-testid="pair-picker-toggle"]')
    if (btn) {
      await btn.click()
      await new Promise((r) => setTimeout(r, 500))
      const picker = await page.$('[data-testid="pair-picker"]')
      if (picker) {
        await picker.screenshot({ path: join(OUT_DIR, 'pair-picker-open.png') })
        console.log('  ✓ pair-picker-open')
      }
      // Capture full dashboard with picker overlay.
      await snapFull(page, join(OUT_DIR, '01-dashboard-with-picker.png'))
      // Close picker by clicking elsewhere.
      await page.mouse.click(10, 10)
      await new Promise((r) => setTimeout(r, 300))
    }
  } catch (e) {
    console.warn('  ✗ pair-picker capture failed:', e.message)
  }

  await browser.close()

  // Write a manifest for the docs.
  const manifest = {
    captured_at: new Date().toISOString(),
    base_url: BASE_URL,
    warm_seconds: WARM_SECONDS,
    viewport: VIEWPORT,
    files: ['00-dashboard-full.png', '01-dashboard-with-picker.png', ...PANELS.map((p) => `${p.name}.png`), 'pair-picker-open.png'],
  }
  await writeFile(join(OUT_DIR, 'manifest.json'), JSON.stringify(manifest, null, 2))

  console.log(`\nScreenshots saved to ${OUT_DIR}`)
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
