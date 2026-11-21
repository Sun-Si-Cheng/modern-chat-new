/**
 * 在线曲目的本地缓存（IndexedDB）。
 *
 * 与 `cryptoCache` 的区别：
 *   - cryptoCache 存的是**加密流**（本地曲库，密钥由 token 派生）；
 *   - 这里存的是**已解析出的直链音频原始字节**（在线曲目，公开音频，无需解密）。
 *
 * 为什么要缓存：在线音乐每次播放都要重新解析地址 + 重新下载整首（几 MB）。
 * 直链通常 1~2 小时就过期，但音频内容不变 —— 所以缓存的是**音频本体**，
 * 命中缓存时连解析都省了，直接解码播放。
 *
 * 容量按字节数控制（默认 300MB），超出按「最久未访问」淘汰。
 * 存的是 ArrayBuffer，IndexedDB 直接以二进制存储，不占内存。
 */

const DB_NAME = 'mc-music-cache'
const DB_VERSION = 1
const STORE = 'online'
const MAX_BYTES = 300 * 1024 * 1024

export interface CachedOnlineTrack {
  /** 缓存键：`source:songmid` 的哈希（同一首歌跨会话稳定） */
  key: string
  /** 原始音频字节 */
  data: ArrayBuffer
  /** 解析来源音源 id，仅用于排查 */
  by: string
  mime: string
  title: string
  artist: string
  cover: string
  duration: number
  bytes: number
  timestamp: number
  /** 最近一次命中时间，用于 LRU 淘汰 */
  usedAt: number
}

let dbPromise: Promise<IDBDatabase> | null = null

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) {
        const store = db.createObjectStore(STORE, { keyPath: 'key' })
        store.createIndex('usedAt', 'usedAt')
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

/** 把「平台:id」压成一个稳定短键。 */
export function onlineCacheKey(source: string, id: string | number): string {
  return `${source}:${id}`
}

export async function onlineCacheGet(key: string): Promise<CachedOnlineTrack | null> {
  try {
    const db = await openDB()
    const row = await new Promise<CachedOnlineTrack | null>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).get(key)
      req.onsuccess = () => resolve((req.result as CachedOnlineTrack) ?? null)
      req.onerror = () => reject(req.error)
    })
    if (row) {
      // 异步刷新 LRU 时间，不阻塞播放
      void touch(key)
    }
    return row
  } catch {
    return null
  }
}

async function touch(key: string): Promise<void> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    const req = store.get(key)
    req.onsuccess = () => {
      const row = req.result as CachedOnlineTrack | undefined
      if (!row) return
      row.usedAt = Date.now()
      store.put(row)
    }
  } catch {
    /* 忽略 */
  }
}

export async function onlineCacheSet(track: CachedOnlineTrack): Promise<void> {
  try {
    const db = await openDB()
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readwrite')
      tx.objectStore(STORE).put(track)
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
    })
    void evictIfNeeded()
  } catch {
    /* 缓存失败不影响播放 */
  }
}

async function evictIfNeeded(): Promise<void> {
  try {
    const db = await openDB()
    const rows = await new Promise<CachedOnlineTrack[]>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).getAll()
      req.onsuccess = () => resolve((req.result as CachedOnlineTrack[]) ?? [])
      req.onerror = () => reject(req.error)
    })
    let total = rows.reduce((n, r) => n + (r.bytes || 0), 0)
    if (total <= MAX_BYTES) return

    // 最久未访问的排在前面
    rows.sort((a, b) => (a.usedAt || 0) - (b.usedAt || 0))
    const drop: string[] = []
    for (const r of rows) {
      if (total <= MAX_BYTES) break
      drop.push(r.key)
      total -= r.bytes || 0
    }
    if (!drop.length) return
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    for (const k of drop) store.delete(k)
  } catch {
    /* 忽略 */
  }
}

export async function onlineCacheClear(): Promise<void> {
  try {
    const db = await openDB()
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readwrite')
      tx.objectStore(STORE).clear()
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
    })
  } catch {
    /* 忽略 */
  }
}

export async function onlineCacheStats(): Promise<{ count: number; bytes: number }> {
  try {
    const db = await openDB()
    const rows = await new Promise<CachedOnlineTrack[]>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).getAll()
      req.onsuccess = () => resolve((req.result as CachedOnlineTrack[]) ?? [])
      req.onerror = () => reject(req.error)
    })
    return { count: rows.length, bytes: rows.reduce((n, r) => n + (r.bytes || 0), 0) }
  } catch {
    return { count: 0, bytes: 0 }
  }
}

/** 已缓存条目数，供「是否已下载」这种 UI 状态用。 */
export async function onlineCacheIds(): Promise<Set<string>> {
  try {
    const db = await openDB()
    const keys = await new Promise<string[]>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).getAllKeys()
      req.onsuccess = () => resolve((req.result as string[]) ?? [])
      req.onerror = () => reject(req.error)
    })
    return new Set(keys)
  } catch {
    return new Set()
  }
}