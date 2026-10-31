// TEMP 诊断：验证插件抽屉在生产构建下能否打开（用完即删）
import { Browser } from 'file:///C:/Users/a1304/.codebuddy/skills/design-taste-frontend/scripts/lib/cdp.mjs'

const BASE = 'http://localhost:4173'

const browser = await Browser.launch({ width: 1440, height: 900 })
const page = await browser.newPage()

const logs = []
await page.send('Runtime.enable')
await page.send('Log.enable')
browser.on(page.sessionId, 'Runtime.exceptionThrown', (p) => {
  const d = p.exceptionDetails
  logs.push('EXCEPTION: ' + (d?.exception?.description || d?.text || JSON.stringify(d)))
})
browser.on(page.sessionId, 'Runtime.consoleAPICalled', (p) => {
  if (p.type === 'error' || p.type === 'warning') {
    const txt = (p.args || []).map((a) => a.value ?? a.description ?? a.type).join(' ')
    logs.push(p.type.toUpperCase() + ': ' + txt)
  }
})
browser.on(page.sessionId, 'Log.entryAdded', (p) => {
  logs.push('LOG:' + p.entry.level + ' ' + p.entry.text)
})

// 1) 登录并写入 token
await page.goto(BASE + '/login')
const loginRes = await page.eval(async () => {
  const r = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'mailtest', password: 'newpass123' }),
  })
  const j = await r.json()
  if (j.token) localStorage.setItem('mc_token', j.token)
  return { ok: !!j.token, status: r.status }
})

// 2) 进入聊天页
await page.goto(BASE + '/chat')
const rail = await page.eval(() => {
  const btns = [...document.querySelectorAll('button')].map((b) => b.getAttribute('aria-label'))
  return { buttons: btns, rail: !!document.querySelector('.app-rail') }
})

// 3) 点击插件按钮
const clicked = await page.eval(() => {
  const btn = [...document.querySelectorAll('button')].find(
    (b) => (b.getAttribute('aria-label') || '') === '插件管理',
  )
  if (!btn) return 'NOT_FOUND'
  btn.click()
  return 'CLICKED'
})

await new Promise((r) => setTimeout(r, 1500))

// 4) 检查抽屉
const drawer = await page.eval(() => {
  const d = document.querySelector('.t-drawer')
  const body = document.querySelector('.t-drawer__body')
  const mc = document.querySelector('.mc-plugins')
  return {
    drawerEl: !!d,
    hasBody: !!body,
    bodyText: body ? body.innerText.slice(0, 100) : null,
    hasMcPlugins: !!mc,
    mcText: mc ? mc.innerText.slice(0, 100) : null,
    position: (() => {
      const w = document.querySelector('.t-drawer__content-wrapper')
      if (!w) return null
      const r = w.getBoundingClientRect()
      const cs = getComputedStyle(w)
      return {
        rect: { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) },
        transform: cs.transform,
        opacity: cs.opacity,
        zIndex: cs.zIndex,
      }
    })(),
    rootClass: d ? d.className : null,
    viewport: { w: window.innerWidth, h: window.innerHeight },
    drawerCount: document.querySelectorAll('.t-drawer').length,
  }
})

console.log(JSON.stringify({ loginRes, rail, clicked, drawer, logs }, null, 2))
await browser.close()
