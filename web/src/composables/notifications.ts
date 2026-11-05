import { pushApi } from '@/api'

/** 桌面通知：开关存本地，权限由浏览器控制 */
const KEY = 'mc_notify_enabled'
const PUSH_KEY = 'mc_push_enabled'

export function notificationsSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

/** 用户开关 + 浏览器权限都满足时才提示 */
export function notificationsEnabled(): boolean {
  return notificationsSupported() && localStorage.getItem(KEY) === '1' && Notification.permission === 'granted'
}

/** 打开开关时申请权限（必须由用户手势触发，浏览器才允许弹权限框） */
export async function setNotificationsEnabled(on: boolean): Promise<boolean> {
  if (!on) {
    localStorage.removeItem(KEY)
    return false
  }
  if (!notificationsSupported()) return false
  let permission = Notification.permission
  if (permission === 'default') permission = await Notification.requestPermission()
  if (permission !== 'granted') return false
  localStorage.setItem(KEY, '1')
  return true
}

// ===== Web Push（离线推送）=====

/** 浏览器与运行环境是否支持 Web Push（必须 HTTPS 或 localhost） */
export function pushSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    'serviceWorker' in navigator &&
    'PushManager' in window
  )
}

/** 本机是否已开启离线推送（仅本地标记，真实订阅在浏览器里） */
export function pushEnabled(): boolean {
  return localStorage.getItem(PUSH_KEY) === '1'
}

/** VAPID 公钥是 base64url，PushManager 需要 Uint8Array */
function b64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const s = b64.replace(/-/g, '+').replace(/_/g, '/')
  const pad = s.length % 4 ? '='.repeat(4 - (s.length % 4)) : ''
  const raw = atob(s + pad)
  // 显式用 ArrayBuffer 构造：PushManager 的类型要求 ArrayBuffer 而非 SharedArrayBuffer
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

async function swRegistration(): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null
  let reg = await navigator.serviceWorker.getRegistration()
  if (!reg) reg = await navigator.serviceWorker.register('/sw.js')
  return reg ?? null
}

/**
 * 开启 / 关闭离线推送。
 * 返回是否成功（浏览器拒绝权限、服务端未配置 VAPID、环境不支持都会返回 false）。
 */
export async function setPushEnabled(on: boolean): Promise<boolean> {
  if (!on) {
    localStorage.removeItem(PUSH_KEY)
    try {
      const reg = await swRegistration()
      const sub = await reg?.pushManager.getSubscription()
      if (sub) {
        await pushApi.unsubscribe(sub.endpoint).catch(() => undefined)
        await sub.unsubscribe()
      }
    } catch {
      /* 关闭失败也不影响本地开关 */
    }
    return false
  }
  if (!pushSupported()) return false

  const cfg = await pushApi.key()
  if (!cfg.enabled || !cfg.key) return false

  let permission = Notification.permission
  if (permission === 'default') permission = await Notification.requestPermission()
  if (permission !== 'granted') return false

  const reg = await swRegistration()
  if (!reg) return false

  let sub = await reg.pushManager.getSubscription()
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: b64ToBytes(cfg.key),
    })
  }
  const json = sub.toJSON()
  await pushApi.subscribe({
    endpoint: sub.endpoint,
    p256dh: json.keys?.p256dh ?? '',
    auth: json.keys?.auth ?? '',
  })
  localStorage.setItem(PUSH_KEY, '1')
  return true
}

/** 页面不可见时弹出通知；点击后聚焦窗口并打开对应会话 */
export function showMessageNotification(title: string, body: string, onClick: () => void) {
  if (!notificationsEnabled() || !document.hidden) return
  try {
    const n = new Notification(title, { body, icon: '/icon-192.png', tag: 'mc-message' })
    n.onclick = () => {
      window.focus()
      onClick()
      n.close()
    }
  } catch {
    /* 某些移动端浏览器只允许经 Service Worker 发通知：静默跳过 */
  }
}
