import puppeteer from 'puppeteer'

const URL = 'https://brecha-engine-ch-mexico.fly.dev/'

const browser = await puppeteer.launch({
  headless: 'new',
  args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
})
const page = await browser.newPage()
await page.setViewport({ width: 1600, height: 1100, deviceScaleFactor: 1 })
await page.goto(URL, { waitUntil: 'networkidle2', timeout: 60000 })
await new Promise(r => setTimeout(r, 15000))

const info = await page.evaluate(() => {
  const panel = document.querySelector('#bx-trades')
  if (!panel) return { error: 'panel not found' }
  const tbody = panel.querySelector('tbody')
  const rows = tbody ? tbody.querySelectorAll('tr').length : 0
  const rect = panel.getBoundingClientRect()
  return {
    panelHeight: rect.height,
    rowsRendered: rows,
    bodyScrollHeight: document.body.scrollHeight,
  }
})
console.log(JSON.stringify(info, null, 2))
await page.screenshot({ path: '/tmp/prod-trades.png', fullPage: true })
await browser.close()
