// TEMP：强制指定配色方案截一张图（用完即删）
import { Browser } from 'file:///C:/Users/a1304/.codebuddy/skills/design-taste-frontend/scripts/lib/cdp.mjs'

const scheme = process.argv[2] === 'dark' ? 'dark' : 'light'
const out = process.argv[3] ?? `.taste-skill/site-${scheme}.jpg`

const browser = await Browser.launch({ width: 1440, height: 900 })
const page = await browser.newPage({ width: 1440, height: 900 })
await page.send('Emulation.setEmulatedMedia', {
  features: [{ name: 'prefers-color-scheme', value: scheme }],
})
await page.goto('http://localhost:4174/')
await new Promise((r) => setTimeout(r, 2600))
await page.screenshot({ path: out, fullPage: false })
console.log('saved', out, 'scheme=', scheme)
await browser.close()
