/**
 * 本地缓存管理：统计静态资源缓存（Cache Storage）与 localStorage 偏好占用，
 * 并提供清空静态缓存的能力。
 *
 * 只清理 Cache Storage。token、语言、主题、布局与插件数据都在 localStorage，
 * 属于偏好/数据而非可再生缓存，清空时保留。
 */

export interface LocalCacheUsage {
  /** Cache Storage 中静态资源的字节数（可清理） */
  cacheBytes: number
  /** localStorage 中偏好与数据的字节数（不会清理） */
  localStorageBytes: number
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

/**
 * 静态缓存字节数。
 *
 * 优先用 `navigator.storage.estimate()`：一次调用就能拿到 origin 总占用，
 * 减去 localStorage 即为 Cache Storage 的近似大小。旧实现遍历每个缓存条目
 * 并 `blob()` 读一遍，缓存多时会让设置页卡住。
 * estimate 不可用时才回退到逐条统计。
 */
async function measureCacheStorage(localStorageBytes: number): Promise<number> {
  if (!cacheSupported()) return 0
  try {
    if (navigator.storage?.estimate) {
      const { usage } = await navigator.storage.estimate()
      if (typeof usage === 'number') {
        return Math.max(0, usage - localStorageBytes)
      }
    }
  } catch {
    /* 隐私模式等场景不可用：走回退 */
  }

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
  const localStorageBytes = measureLocalStorage()
  return {
    cacheBytes: await measureCacheStorage(localStorageBytes),
    localStorageBytes,
  }
}

export async function clearStaticCache(): Promise<void> {
  if (!cacheSupported()) return
  const names = await caches.keys()
  await Promise.all(names.map((name) => caches.delete(name)))
}
