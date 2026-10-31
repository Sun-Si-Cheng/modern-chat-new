// TEMP：验证后台新增的公告 / 角色 / 强制卸载 / 审计（用完即删）
import { Browser } from 'file:///C:/Users/a1304/.codebuddy/skills/design-taste-frontend/scripts/lib/cdp.mjs'

const browser = await Browser.launch({ width: 1440, height: 900 })
const page = await browser.newPage({ width: 1440, height: 900 })

await page.goto('http://localhost:5173/login')
const login = await page.eval(async () => {
  const r = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'act-admin', password: 'secret123' }),
  })
  const j = await r.json()
  if (j.token) localStorage.setItem('mc_token', j.token)
  return { ok: !!j.token, status: r.status }
})

await page.goto('http://localhost:5173/admin')
await new Promise((r) => setTimeout(r, 3000))

const usersTab = await page.eval(() => ({
  announce: !!document.querySelector('.mc-admin__announce'),
  opsText: [...document.querySelectorAll('.mc-admin__ops a')].map((a) => a.textContent.trim()).slice(0, 6),
  tabs: [...document.querySelectorAll('.t-tabs__nav-item')].map((t) => t.textContent.trim()),
}))
await page.screenshot({ path: '.taste-skill/admin-v2-users.jpg', fullPage: false })

// 切到审计 tab
const auditTab = await page.eval(async () => {
  const tab = [...document.querySelectorAll('.t-tabs__nav-item')].find((t) =>
    (t.textContent || '').includes('审计'),
  )
  if (!tab) return 'no-tab'
  tab.click()
  await new Promise((r) => setTimeout(r, 1500))
  return {
    rows: document.querySelectorAll('.t-table tbody tr').length,
    firstAction: document.querySelector('.t-table tbody tr .t-tag')?.textContent?.trim() ?? null,
  }
})
await page.screenshot({ path: '.taste-skill/admin-v2-audits.jpg', fullPage: false })

// 切到插件 tab 看强制卸载
const pluginTab = await page.eval(async () => {
  const tab = [...document.querySelectorAll('.t-tabs__nav-item')].find((t) =>
    (t.textContent || '').includes('插件'),
  )
  if (!tab) return 'no-tab'
  tab.click()
  await new Promise((r) => setTimeout(r, 1500))
  return [...document.querySelectorAll('.mc-admin__ops a')].map((a) => a.textContent.trim()).slice(0, 4)
})

console.log(JSON.stringify({ login, usersTab, auditTab, pluginTab }, null, 2))
await browser.close()
