/* ModernChat PWA Service Worker：只缓存静态资源，接口与 WebSocket 始终走网络 */
const CACHE = 'mc-pwa-v1'
const SHELL = ['/', '/manifest.webmanifest', '/icon-192.png']

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(SHELL))
      .catch(() => undefined),
  )
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))))
  self.clients.claim()
})

// Web Push：应用完全关闭时由浏览器唤醒 Service Worker 显示通知
self.addEventListener('push', (event) => {
  let data = {}
  try {
    data = event.data ? event.data.json() : {}
  } catch {
    data = { body: event.data ? event.data.text() : '' }
  }
  const title = data.title || 'ModernChat'
  event.waitUntil(
    self.registration.showNotification(title, {
      body: data.body || '',
      icon: '/icon-192.png',
      badge: '/icon-192.png',
      tag: data.convId ? `mc-conv-${data.convId}` : 'mc-message',
      renotify: true,
      data: { url: data.url || '/chat' },
    }),
  )
})

// 点击通知：聚焦已有窗口，没有则新开一个
self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = (event.notification.data && event.notification.data.url) || '/chat'
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      for (const client of list) {
        if ('focus' in client) {
          client.focus()
          if ('navigate' in client) client.navigate(url)
          return undefined
        }
      }
      return self.clients.openWindow(url)
    }),
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  // 接口、WebSocket、上传文件不缓存，避免读到过期数据
  if (url.pathname.startsWith('/api') || url.pathname.startsWith('/ws') || url.pathname.startsWith('/uploads')) {
    return
  }
  // 页面导航：网络优先，断网时回退到缓存的首页
  if (req.mode === 'navigate') {
    event.respondWith(fetch(req).catch(() => caches.match('/')))
    return
  }
  // 静态资源：缓存优先 + 后台更新
  event.respondWith(
    caches.match(req).then((cached) => {
      const fresh = fetch(req)
        .then((res) => {
          if (res.ok) caches.open(CACHE).then((cache) => cache.put(req, res.clone()))
          return res
        })
        .catch(() => cached)
      return cached || fresh
    }),
  )
})
