/** 桌面通知：开关存本地，权限由浏览器控制 */
const KEY = 'mc_notify_enabled'

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
