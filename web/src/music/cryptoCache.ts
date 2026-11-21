/**
 * 加密音频的解密与 IndexedDB 缓存。
 *
 * 这里必须与播放器源站保持 **逐字节一致**，否则解出来的音频是噪音：
 *
 *   源站 api/stream_get.php：
 *     key   = SHA256(token . "|stream")          (32 字节)
 *     nonce = SHA256(token . "|iv")[0..12]       (12 字节)
 *     计数器块 = nonce(12) ++ 大端 uint32 块序号   (16 字节)
 *     算法 = AES-256-CTR
 *
 *   源站前端 assets/js/audio-cache.js / sw-audio.js 用的也是这一套。
 *
 * CTR 是可分段并行的（各块计数器独立），所以这里额外做了分块并行解密，
 * 把 10MB 级别的音频解密摊到多核上。
 *
 * 缓存策略也与源站一致：
 * - IndexedDB 里存 **密文 + 解密所需的 token**（开发者工具翻出来也只是密文）
 * - 命中缓存时：读密文 → 解密 → decodeAudioData → AudioBufferSourceNode 播放
 * - 全程不生成 Blob URL，磁盘上不会出现可双击的 mp3
 * - 上限 50 首，超出淘汰最旧
 * - 另外对"已解码的 AudioBuffer"做内存 LRU（默认 3 首），重播几乎零等待
 */

const DB_NAME = 'ModernChatMusicCache'
const DB_VERSION = 1
const STORE = 'songs'
const CACHE_MAX = 50

/** 已解码音频的内存 LRU 上限（一首 4 分钟立体声 f32 约 80MB，不能多） */
const DECODED_MAX = 3
const DECODED_BYTES_MAX = 512 * 1024 * 1024

let dbPromise: Promise<IDBDatabase> | null = null

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'songId' })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

export interface CachedTrack {
  songId: number
  /** 密文 */
  data: ArrayBuffer
  /** 解密该密文所需的 token */
  token: string
  duration: number
  title: string
  artist: string
  cover: string
  timestamp: number
}

// ============ 密钥派生 ============

const enc = new TextEncoder()

async function deriveKey(token: string): Promise<CryptoKey> {
  const raw = await crypto.subtle.digest('SHA-256', enc.encode(token + '|stream'))
  return await crypto.subtle.importKey('raw', raw, 'AES-CTR', false, ['decrypt'])
}

async function deriveNonce(token: string): Promise<Uint8Array> {
  const h = await crypto.subtle.digest('SHA-256', enc.encode(token + '|iv'))
  // 复制前 12 字节到独立缓冲，确保类型是 Uint8Array<ArrayBuffer> 而非 ArrayBufferLike
  return new Uint8Array(h.slice(0, 12))
}

function counterBlock(nonce: Uint8Array, blockIndex: number): ArrayBuffer {
  const ab = new ArrayBuffer(16)
  const view = new Uint8Array(ab)
  view.set(nonce, 0)
  // 计数器是大端 uint32，位于最后 4 字节
  new DataView(ab).setUint32(12, blockIndex, false)
  return ab
}

/**
 * 单段 AES-256-CTR 解密（与源站 audio-cache.js 的 ac_decrypt 等价）。
 * 注意：WebCrypto 的 length:32 表示"计数器只用低 32 位并回绕"，与 PHP 的 pack('N', $counter) 对应。
 */
export async function decryptAll(cipher: ArrayBuffer, token: string): Promise<ArrayBuffer> {
  const key = await deriveKey(token)
  const nonce = await deriveNonce(token)
  return await crypto.subtle.decrypt(
    { name: 'AES-CTR', counter: counterBlock(nonce, 0), length: 32 },
    key,
    cipher,
  )
}

/**
 * 分块并行解密（CTR 特性：每段只要计数器初值对得上就能独立解）。
 * 小文件走单段，避免并行调度开销。
 */
export async function decryptParallel(
  cipher: ArrayBuffer,
  token: string,
  parts = 4,
): Promise<ArrayBuffer> {
  const total = cipher.byteLength
  const MIN_PARALLEL = 512 * 1024
  if (!total || total < MIN_PARALLEL) return await decryptAll(cipher, token)

  const n = Math.max(1, Math.min(parts, 8))
  const key = await deriveKey(token)
  const nonce = await deriveNonce(token)

  const BLOCK = 16
  const chunkSize = Math.ceil(Math.ceil(total / n) / BLOCK) * BLOCK
  const out = new Uint8Array(total)
  const tasks: Promise<void>[] = []

  let offset = 0
  for (let i = 0; i < n && offset < total; i++) {
    const len = Math.min(chunkSize, total - offset)
    const slice = cipher.slice(offset, offset + len)
    const blockIndex = offset / BLOCK
    const dest = offset
    tasks.push(
      crypto.subtle
        .decrypt(
          { name: 'AES-CTR', counter: counterBlock(nonce, blockIndex), length: 32 },
          key,
          slice,
        )
        .then((plain) => {
          out.set(new Uint8Array(plain), dest)
        }),
    )
    offset += len
  }

  await Promise.all(tasks)
  return out.buffer
}

/** 解码成 AudioBuffer（可交给 AudioBufferSourceNode 播放） */
export async function decodeAudio(
  ctx: AudioContext,
  plain: ArrayBuffer,
): Promise<AudioBuffer> {
  // decodeAudioData 在部分浏览器上有 Promise 也有回调两种签名，这里统一用 Promise
  return await ctx.decodeAudioData(plain)
}

// ============ IndexedDB 缓存（存密文） ============

export async function cacheGet(songId: number): Promise<CachedTrack | null> {
  try {
    const db = await openDB()
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).get(songId)
      req.onsuccess = () => {
        const row = req.result as CachedTrack | undefined
        resolve(row ?? null)
      }
      req.onerror = () => reject(req.error)
    })
  } catch {
    return null
  }
}

export async function cacheSet(track: CachedTrack): Promise<void> {
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
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    const countReq = store.count()
    countReq.onsuccess = () => {
      const over = countReq.result - CACHE_MAX
      if (over <= 0) return
      const cursorReq = store.openCursor()
      let deleted = 0
      cursorReq.onsuccess = () => {
        const cursor = cursorReq.result
        if (cursor && deleted < over) {
          cursor.delete()
          deleted++
          cursor.continue()
        }
      }
    }
  } catch {
    /* ignore */
  }
}

export async function cacheDelete(songId: number): Promise<void> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).delete(songId)
  } catch {
    /* ignore */
  }
}

export async function cacheClear(): Promise<void> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).clear()
  } catch {
    /* ignore */
  }
  decodedClear()
}

/** 缓存里已有哪些歌（用于列表上显示"已缓存"标记） */
export async function cacheIds(): Promise<Set<number>> {
  try {
    const db = await openDB()
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).getAllKeys()
      req.onsuccess = () => resolve(new Set(req.result as number[]))
      req.onerror = () => reject(req.error)
    })
  } catch {
    return new Set()
  }
}

export async function cacheStats(): Promise<{ count: number; bytes: number }> {
  try {
    const db = await openDB()
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const store = tx.objectStore(STORE)
      const req = store.getAll()
      req.onsuccess = () => {
        const rows = req.result as CachedTrack[]
        const bytes = rows.reduce((n, r) => n + (r.data?.byteLength ?? 0), 0)
        resolve({ count: rows.length, bytes })
      }
      req.onerror = () => reject(req.error)
    })
  } catch {
    return { count: 0, bytes: 0 }
  }
}

// ============ 已解码 AudioBuffer 的内存 LRU ============

interface DecodedEntry {
  buffer: AudioBuffer
  bytes: number
  timestamp: number
}

const decodedMap = new Map<number, DecodedEntry>()
let decodedBytes = 0

export function decodedGet(songId: number): AudioBuffer | null {
  const rec = decodedMap.get(songId)
  if (!rec) return null
  rec.timestamp = Date.now()
  return rec.buffer
}

export function decodedSet(songId: number, buffer: AudioBuffer): void {
  const bytes = (buffer.numberOfChannels || 2) * (buffer.length || 0) * 4
  if (bytes > DECODED_BYTES_MAX) return

  const old = decodedMap.get(songId)
  if (old) {
    decodedBytes -= old.bytes
    decodedMap.delete(songId)
  }
  decodedMap.set(songId, { buffer, bytes, timestamp: Date.now() })
  decodedBytes += bytes

  while (decodedMap.size > DECODED_MAX || decodedBytes > DECODED_BYTES_MAX) {
    let oldestKey: number | null = null
    let oldestTime = Infinity
    for (const [k, v] of decodedMap) {
      if (v.timestamp < oldestTime) {
        oldestTime = v.timestamp
        oldestKey = k
      }
    }
    if (oldestKey === null) break
    decodedBytes -= decodedMap.get(oldestKey)?.bytes ?? 0
    decodedMap.delete(oldestKey)
  }
}

export function decodedDelete(songId: number): void {
  const rec = decodedMap.get(songId)
  if (rec) {
    decodedBytes -= rec.bytes
    decodedMap.delete(songId)
  }
}

export function decodedClear(): void {
  decodedMap.clear()
  decodedBytes = 0
}

export function decodedStats(): { count: number; bytes: number } {
  return { count: decodedMap.size, bytes: decodedBytes }
}
