// TEMP：验证后台交互（详情弹窗 / 搜索筛选 / 踢下线），用完即删
import { Browser } from 'file:///C:/Users/a1304/.codebuddy/skills/design-taste-frontend/scripts/lib/cdp.mjs'

const browser = await Browser.launch({ width: 1440, height: 900 })
const page = await browser.newPage({ width: 1440, height: 900 })

await page.goto('http://localhost:5173/login')
await page.eval(async () => {
  const r = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'overview-admin', password: 'secret123' }),
  })
  const j = await r.json()
  if (j.token) localStorage.setItem('mc_token', j.token)
  return !!j.token
})

await page.goto('http://localhost:5173/admin')
await new Promise((r) => setTimeout(r, 3000))

const rowCount = () => page.eval(() => document.querySelectorAll('.t-table tbody tr').length)
const before = await rowCount()

// 1) 搜索筛选：只留 bob
const search = await page.eval(async () => {
  const input = document.querySelector('.mc-admin__search input')
  if (!input) return 'no-input'
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set
  setter.call(input, 'overview-bob')
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await new Promise((r) => setTimeout(r, 1200))
  return document.querySelectorAll('.t-table tbody tr').length
})

// 重置
await page.eval(async () => {
  const btn = [...document.querySelectorAll('.mc-admin__filters button')].find((b) =>
    (b.textContent || '').includes('重置'),
  )
  btn?.click()
  await new Promise((r) => setTimeout(r, 1200))
})
const afterReset = await rowCount()

// 2) 详情弹窗
const detail = await page.eval(async () => {
  const link = [...document.querySelectorAll('.mc-admin__ops a')].find(
    (a) => a.textContent.trim() === '详情',
  )
  if (!link) return 'no-link'
  link.click()
  await new Promise((r) => setTimeout(r, 1200))
  const dlg = document.querySelector('.t-dialog')
  return {
    dialog: !!dlg,
    text: dlg ? dlg.innerText.replace(/\s+/g, ' ').slice(0, 160) : null,
  }
})
await page.screenshot({ path: '.taste-skill/admin-detail.jpg', fullPage: false })

// 关闭弹窗
await page.eval(() => {
  const close = document.querySelector('.t-dialog__close')
  close?.click()
})
await new Promise((r) => setTimeout(r, 600))

// 3) 踢下线：点第一个用户的「踢下线」
const kick = await page.eval(async () => {
  const link = [...document.querySelectorAll('.mc-admin__ops a')].find(
    (a) => a.textContent.trim() === '踢下线',
  )
  if (!link) return 'no-link'
  link.click()
  await new Promise((r) => setTimeout(r, 1500))
  const msg = document.querySelector('.t-message')
  return { toast: msg ? msg.innerText.trim() : null }
})

console.log(JSON.stringify({ before, search, afterReset, detail, kick }, null, 2))
await browser.close()
