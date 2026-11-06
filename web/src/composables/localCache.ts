/**
 * 本地缓存管理：统计静态资源缓存（Cache Storage）与 localStorage 偏好占用，
 * 并按用户设置的上限清理静态缓存。
 *
 * 只清理 Cache Storage。token、语言、主题、布局与插件数据都在 localStorage，
 * 属于偏好/数据而非可再生缓存，清空时保留。
 */

export const CACHE_LIMIT_KEY = 'mc_cache_limit_mb'
export const DEFAULT_CACHE_LIMIT_MB = 50

/** 设置页的上限选项；0 = 不限制 */
export const CACHE_LIMIT_OPTIONS = [10, 20, 50, 100, 0] as const

export interface LocalCacheUsage {
  /** Cache Storage 中静态资源的字节数（可清理） */
  cacheBytes: number
  /** localStorage 中偏好与数据的字节数（不会清理） */
  localStorageBytes: number
}

export function getCacheLimitMB(): number {
  const raw = localStorage.getItem(CACHE_LIMIT_KEY)
  const n = Number(raw)
  if (!Number.isFinite(n) || n < 0) return DEFAULT_CACHE_LIMIT_MB
  return Math.floor(n)
}

export function setCacheLimitMB(mb: number): void {
  localStorage.setItem(CACHE_LIMIT_KEY, String(Math.max(0, Math.floor(mb))))
}

export function formatBytes(n: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function cacheSupported(): boolean {
  return typeof caches !== 'undefined'
}

async function measureCacheStorage(): Promise<number> {
  if (!cacheSupported()) return 0
  let total = 0
  try {
    for (const name of await caches.keys()) {
      const cache = await caches.open(name)
      for (const req of await cache.keys()) {
        const res = await cache.match(req)
        if (!res) continue
        // 读取克隆体，避免消费掉缓存里的响应体
        total += (await res.clone().blob()).size
      }
    }
  } catch {
    return total
  }
  return total
}

function measureLocalStorage(): number {
  let total = 0
  try {
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i)
      if (!key) continue
      total += new Blob([key, localStorage.getItem(key) ?? '']).size
    }
  } catch {
    /* 隐私模式等不可读场景按 0 处理 */
  }
  return total
}

export async function measureLocalCache(): Promise<LocalCacheUsage> {
  return {
    cacheBytes: await measureCacheStorage(),
    localStorageBytes: measureLocalStorage(),
  }
}

export async function clearStaticCache(): Promise<void> {
  if (!cacheSupported()) return
  const names = await caches.keys()
  await Promise.all(names.map((name) => caches.delete(name)))
}

/**
 * 超过上限时清空静态缓存；返回是否发生了清理。
 * 上限 0 = 不限制；传入 cacheBytes 可复用刚测到的值，避免重复遍历缓存。
 */
export async function applyCacheLimit(limitMB: number, cacheBytes?: number): Promise<boolean> {
  if (limitMB <= 0 || !cacheSupported()) return false
  const bytes = cacheBytes ?? (await measureCacheStorage())
  if (bytes <= limitMB * 1024 * 1024) return false
  await clearStaticCache()
  return true
}
