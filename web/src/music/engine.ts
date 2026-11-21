/**
 * 播放引擎：加密拉流 → 解密 → 解码 → AudioBufferSourceNode 播放。
 *
 * 与源站一致的关键点：
 * - 不生成 Blob URL（不给浏览器留下可下载的媒体文件）
 * - 播放前把密文 + token 存入 IndexedDB，下次命中直接本地解密（不再走网络）
 * - 解码后的 AudioBuffer 进内存 LRU，重播秒开
 *
 * 播放流程：
 *   1) 命中 IndexedDB → 读密文 → 解密 → 解码 → 播
 *   2) 未命中 → 申请一次 token → 拉密文 → 写入缓存 → 解密 → 解码 → 播
 *
 * 特别注意：AudioContext 必须由用户手势创建/恢复，否则浏览器会挂起。
 */

import {
  cacheGet,
  cacheIds,
  cacheSet,
  decryptParallel,
  decodedDelete,
  decodedGet,
  decodedSet,
  decodeAudio,
} from './cryptoCache'
import {
  fetchEncrypted,
  getLrcText,
  getSong,
  issueStreamToken,
} from './playerApi'
import { onlineCacheGet, onlineCacheSet } from './onlineCache'

export type PlayerStatus =
  | 'idle'
  | 'loading' // 申请 token / 拉流
  | 'buffering' // 下载中
  | 'decrypting' // 解密 + 解码
  | 'playing'
  | 'paused'
  | 'ended'
  | 'error'

/** 播放器内部的统一曲目（本地加密曲目 / 在线曲目共用） */
export interface TrackView {
  /** 本地曲目为正 id；在线曲目用负数 id（由调用方按 source:songmid 生成） */
  id: number
  title: string
  artist: string
  album?: string
  duration: number
  cover_url?: string
  /** 在线曲目的缓存键（`source:songmid`），本地曲目为空 */
  cacheKey?: string
  /** 在线曲目的取直链回调（本地曲目为空）。有它就走 playOnline，否则走本地 play */
  resolve?: () => Promise<string>
}

export interface PlayerSnapshot {
  status: PlayerStatus
  song: TrackView | null
  /** 当前播放位置（秒） */
  position: number
  /** 总时长（秒），未知时为 0 */
  duration: number
  /** 0..1 缓冲进度（仅从网络拉取时有意义） */
  buffered: number
  /** 本条歌曲是否命中了本地缓存 */
  fromCache: boolean
  /** 错误信息 */
  error: string
  /** 音量 0..1 */
  volume: number
}

type Listener = (s: PlayerSnapshot) => void

const INITIAL: PlayerSnapshot = {
  status: 'idle',
  song: null,
  position: 0,
  duration: 0,
  buffered: 0,
  fromCache: false,
  error: '',
  volume: 0.8,
}

export class MusicEngine {
  private ctx: AudioContext | null = null
  private source: AudioBufferSourceNode | null = null
  private buffer: AudioBuffer | null = null
  private gain: GainNode | null = null
  /** 无 CORS 音源的兜底播放元素（见 playByElement） */
  private mediaEl: HTMLAudioElement | null = null
  /** 记录当前音量：切到 audio 兜底时把同一个音量应用过去 */
  private volumeLevel = 1

  /** 播放起点在 AudioContext 时间轴上的时刻 */
  private startedAt = 0
  /** 暂停时已播过的秒数 */
  private offset = 0

  private rafId = 0
  private listeners = new Set<Listener>()
  private snap: PlayerSnapshot = { ...INITIAL }

  /** 当前是否走 <audio> 兜底（mediaEl 模式）——它的进度时钟是 el.currentTime 而非 AudioContext */
  private usingElement = false

  /** 连播队列 & 循环 */
  queue: TrackView[] = []
  private index = -1
  repeat: 'off' | 'one' | 'all' = 'all'

  /** 设置播放模式：all=列表循环（默认） off=列表播放（播完停） one=单曲循环 */
  setRepeat(r: 'off' | 'one' | 'all') {
    this.repeat = r
  }

  /** 当前请求的取消标记（切歌时让在途请求作废） */
  private gen = 0

  private cached = new Set<number>()

  // ============ 订阅 ============

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn)
    fn(this.snap)
    return () => this.listeners.delete(fn)
  }

  private emit(patch: Partial<PlayerSnapshot> = {}) {
    this.snap = { ...this.snap, ...patch }
    for (const fn of this.listeners) fn(this.snap)
  }

  get snapshot(): PlayerSnapshot {
    return this.snap
  }

  async refreshCacheIds() {
    this.cached = await cacheIds()
  }

  isCached(id: number) {
    return this.cached.has(id)
  }

  // ============ AudioContext ============

  private ensureCtx(): AudioContext {
    if (!this.ctx) {
      const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
      this.ctx = new Ctor()
      this.gain = this.ctx.createGain()
      this.gain.gain.value = this.snap.volume
      this.gain.connect(this.ctx.destination)
    }
    return this.ctx
  }

  /** 必须由用户手势调用一次，浏览器才允许出声 */
  async unlock() {
    const ctx = this.ensureCtx()
    if (ctx.state === 'suspended') await ctx.resume()
  }

  setVolume(v: number) {
    const vol = Math.min(1, Math.max(0, v))
    this.volumeLevel = vol
    if (this.gain) this.gain.gain.value = vol
    // 走 <audio> 兜底时音量在元素上，不同步会出现"拖了音量没反应"
    if (this.mediaEl) this.mediaEl.volume = vol
    this.emit({ volume: vol })
  }

  // ============ 播放控制 ============

  /** 播放指定歌曲（本地或在线自动分派） */
  async play(song: TrackView, opts: { fromCacheOnly?: boolean } = {}) {
    // 在线曲目（带取直链回调）走 playOnline
    if (song.resolve) return this.playOnline(song, song.cacheKey ?? `online:${song.id}`, song.resolve)
    return this.playLocal(song, opts)
  }

  async playLocal(song: TrackView, opts: { fromCacheOnly?: boolean } = {}) {
    const myGen = ++this.gen
    this.stopSource()

    await this.unlock()

    this.emit({
      status: 'loading',
      song,
      position: 0,
      duration: song.duration || 0,
      buffered: 0,
      fromCache: false,
      error: '',
    })

    this.startTicker()

    try {
      let cipher: ArrayBuffer | null = null
      let token = ''
      let fromCache = false

      // 1) 先看 IndexedDB 缓存
      const hit = await cacheGet(song.id)
      if (hit?.data && hit.token) {
        try {
          // 缓存里有密文 + token，直接本地解密
          cipher = hit.data
          token = hit.token
          fromCache = true
        } catch {
          cipher = null
        }
      }

      // 2) 未命中 → 走网络
      if (!cipher) {
        if (opts.fromCacheOnly) throw new Error('本地未缓存，已跳过')
        this.emit({ status: 'buffering', buffered: 0 })
        const ticket = await issueStreamToken(song.id)
        token = ticket.token
        cipher = await fetchEncrypted(token, (loaded, total) => {
          if (myGen !== this.gen) return
          this.emit({ buffered: total > 0 ? Math.min(1, loaded / total) : 0 })
        })
        if (myGen !== this.gen) return

        // 写入缓存：存密文 + token（和源站同一套结构）
        void cacheSet({
          songId: song.id,
          data: cipher,
          token,
          duration: song.duration || 0,
          title: song.title,
          artist: song.artist,
          cover: song.cover_url || '',
          timestamp: Date.now(),
        })
        this.cached.add(song.id)
      }

      if (myGen !== this.gen) return

      // 3) 解密（大文件分块并行）+ 解码
      this.emit({ status: 'decrypting', fromCache })

      let decoded = decodedGet(song.id)
      if (!decoded) {
        const plain = await decryptParallel(cipher, token, 4)
        if (myGen !== this.gen) return
        const ctx = this.ensureCtx()
        decoded = await decodeAudio(ctx, plain)
        decodedSet(song.id, decoded)
      }

      if (myGen !== this.gen) return

      this.buffer = decoded
      this.startPlayback(0)
      this.emit({
        status: 'playing',
        duration: decoded.duration,
        position: 0,
        fromCache,
        buffered: 1,
      })
    } catch (e) {
      if (myGen !== this.gen) return
      this.stopTicker()
      this.emit({ status: 'error', error: (e as Error)?.message || '播放失败' })
    }
  }

  /**
   * 在线播放：直链下载 + 本地缓存。
   *
   * ===== 两种播放路径（由音源是否允许跨域决定）=====
   *
   * 实测各平台音频直链的 CORS 差别很大：
   *   - QQ / 酷狗 回 `Access-Control-Allow-Origin: *`  → fetch 能读字节
   *   - 酷我 / 网易 / 咪咕 **不回 ACAO**              → fetch 直接抛 TypeError
   *
   * 所以这里分两条路：
   *   A) 可跨域 → fetch 下载整首 → IndexedDB 缓存 → Web Audio 解码播放
   *      （能缓存、能精确 seek、能显示下载进度）
   *   B) 不可跨域 → 用 <audio> 元素直接播直链
   *      （媒体元素不受 CORS 限制，只是"被污染"不能读字节，也就无法缓存）
   *
   * 无论走哪条，音频都是从**浏览器直连音源**，不经过本服务器。
   *
   * @param song      曲目视图
   * @param cacheKey  缓存键（`source:songmid`）
   * @param resolve   取直链的回调（懒调用：缓存命中时不解析）
   */
  async playOnline(song: TrackView, cacheKey: string, resolve: () => Promise<string>) {
    const myGen = ++this.gen
    this.stopSource()
    await this.unlock()

    this.emit({
      status: 'loading',
      song,
      position: 0,
      duration: song.duration || 0,
      buffered: 0,
      fromCache: false,
      error: '',
    })
    this.startTicker()

    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 120000)

    try {
      // ---- 1) 缓存优先 ----
      const hit = await onlineCacheGet(cacheKey)
      if (myGen !== this.gen) return
      if (hit?.data && hit.data.byteLength > 0) {
        this.emit({ status: 'decrypting', buffered: 1, fromCache: true })
        const ctx = this.ensureCtx()
        const decoded = await decodeAudio(ctx, hit.data.slice(0))
        if (myGen !== this.gen) return
        this.buffer = decoded
        this.startPlayback(0)
        this.emit({ status: 'playing', duration: decoded.duration, position: 0, buffered: 1, fromCache: true })
        return
      }

      // ---- 2) 未命中：解析直链 ----
      this.emit({ status: 'buffering', buffered: 0 })
      const url = await resolve()
      if (myGen !== this.gen) return
      if (!/^https?:\/\//i.test(url)) throw new Error('音源返回的地址无效')

      // ---- 3) 尝试直连下载（可跨域时能拿到字节）----
      let raw: ArrayBuffer | null = null
      try {
        const res = await fetch(url, { mode: 'cors', credentials: 'omit', signal: ctrl.signal })
        if (myGen !== this.gen) return
        if (res.ok) {
          const total = Number(res.headers.get('Content-Length') || 0)
          raw = await this.readWithProgress(res, total, myGen)
        }
      } catch (e) {
        // 典型原因就是没有 ACAO —— 不是错误，走 <audio> 路径即可
        if ((e as { name?: string })?.name === 'AbortError') throw e
        raw = null
      }
      if (myGen !== this.gen) return

      if (raw && raw.byteLength > 1024) {
        this.emit({ status: 'decrypting', buffered: 1 })
        const ctx = this.ensureCtx()
        let decoded: AudioBuffer
        try {
          decoded = await decodeAudio(ctx, raw.slice(0))
        } catch {
          throw new Error('音频解码失败，可能是音源返回了错误内容')
        }
        if (myGen !== this.gen) return

        void onlineCacheSet({
          key: cacheKey,
          data: raw,
          by: '',
          mime: 'audio/mpeg',
          title: song.title,
          artist: song.artist,
          cover: song.cover_url || '',
          duration: decoded.duration || song.duration || 0,
          bytes: raw.byteLength,
          timestamp: Date.now(),
          usedAt: Date.now(),
        })

        this.buffer = decoded
        this.startPlayback(0)
        this.emit({ status: 'playing', duration: decoded.duration, position: 0, buffered: 1 })
        return
      }

      // ---- 4) 兜底：<audio> 直接播（无 CORS 的音源走这里，不可缓存）----
      await this.playByElement(url, song, myGen)
    } catch (e) {
      if (myGen !== this.gen) return
      this.stopTicker()
      const aborted = (e as { name?: string })?.name === 'AbortError'
      this.emit({
        status: 'error',
        error: aborted
          ? '音频下载超时（2 分钟），请换一首或换个平台试试'
          : (e as Error)?.message || '在线播放失败',
      })
    } finally {
      window.clearTimeout(timer)
    }
  }

  /**
   * 用 <audio> 元素播放直链。
   *
   * 这是「音源不返回 ACAO」时的唯一可行路径：媒体元素加载媒体资源时
   * 不要求 CORS（只是把资源标记为跨域受污染，不能读字节）。
   * 代价是无法缓存、也无法用 Web Audio 做精确 seek，但至少**能播**。
   */
  private async playByElement(url: string, song: TrackView, myGen: number): Promise<void> {
    const el = this.mediaEl ?? new Audio()
    this.mediaEl = el
    this.usingElement = true
    el.preload = 'auto'
    el.src = url
    el.crossOrigin = null

    // 与 Web Audio 链路共用音量
    el.volume = Math.min(1, Math.max(0, this.volumeLevel))

    const onTime = () => {
      if (myGen !== this.gen) return
      this.emit({
        position: el.currentTime || 0,
        duration: Number.isFinite(el.duration) ? el.duration : song.duration || 0,
      })
    }
    const onPlaying = () => {
      if (myGen !== this.gen) return
      this.emit({
        status: 'playing',
        buffered: 1,
        fromCache: false,
        duration: Number.isFinite(el.duration) ? el.duration : song.duration || 0,
      })
    }
    const onEnded = () => {
      if (myGen !== this.gen) return
      this.emit({ status: 'ended', position: 0 })
      // 播完自动切下一首（mediaEl 路径没有 source.onended，这里补上）
      if (this.repeat === 'all' || this.repeat === 'one' || this.queue.length > 1) {
        void this.next(true)
      }
    }
    const onError = () => {
      if (myGen !== this.gen) return
      this.stopTicker()
      this.emit({ status: 'error', error: '音源无法播放（地址可能已过期），请换一首或换个平台' })
    }
    const onWaiting = () => {
      if (myGen !== this.gen) return
      this.emit({ status: 'buffering', buffered: 0.3 })
    }

    el.addEventListener('timeupdate', onTime)
    el.addEventListener('playing', onPlaying)
    el.addEventListener('ended', onEnded)
    el.addEventListener('error', onError)
    el.addEventListener('waiting', onWaiting)

    try {
      await el.play()
    } catch (e) {
      // play() 被拒绝通常是没有用户手势，或者地址真的播不了
      el.removeEventListener('timeupdate', onTime)
      el.removeEventListener('playing', onPlaying)
      el.removeEventListener('ended', onEnded)
      el.removeEventListener('error', onError)
      el.removeEventListener('waiting', onWaiting)
      throw new Error(`音源无法播放：${(e as Error)?.message || '未知原因'}`)
    }
  }

  /** 带进度的读流，用于展示「正在下载 x%」。 */
  private async readWithProgress(
    res: Response,
    total: number,
    myGen: number,
  ): Promise<ArrayBuffer> {
    if (!res.body) return await res.arrayBuffer()

    const reader = res.body.getReader()
    const chunks: Uint8Array[] = []
    let loaded = 0
    for (;;) {
      const { done, value } = await reader.read()
      if (myGen !== this.gen) {
        void reader.cancel().catch(() => {})
        return new ArrayBuffer(0)
      }
      if (done) break
      if (value) {
        chunks.push(value)
        loaded += value.byteLength
        this.emit({ buffered: total > 0 ? Math.min(1, loaded / total) : 0 })
      }
    }
    const out = new Uint8Array(loaded)
    let off = 0
    for (const c of chunks) {
      out.set(c, off)
      off += c.byteLength
    }
    return out.buffer
  }

  private startPlayback(fromSec: number) {
    const ctx = this.ensureCtx()
    if (!this.buffer) return

    this.stopSource()
    this.usingElement = false // 走 Web Audio，时钟切回 AudioContext

    const src = ctx.createBufferSource()
    src.buffer = this.buffer
    src.connect(this.gain!)

    const start = Math.min(Math.max(0, fromSec), Math.max(0, this.buffer.duration - 0.01))
    src.start(0, start)
    this.startedAt = ctx.currentTime - start
    this.offset = start
    this.source = src

    src.onended = () => {
      // 被打断（切歌/暂停）时不当作自然结束
      if (src !== this.source) return
      this.source = null
      if (this.snap.status === 'playing') this.onTrackEnded()
    }
  }

  private stopSource() {
    // 兜底的 <audio> 也要停：否则切歌后旧音频还在响
    if (this.mediaEl) {
      try {
        this.mediaEl.pause()
        this.mediaEl.removeAttribute('src')
        this.mediaEl.load()
      } catch {
        /* 忽略 */
      }
    }
    if (this.source) {
      try {
        this.source.onended = null
        this.source.stop()
      } catch {
        /* 已停止 */
      }
      this.source.disconnect()
      this.source = null
    }
    // 让正在播放的曲目进入可重播状态
    if (this.buffer && this.snap.status === 'playing') {
      this.offset = this.currentPosition()
    }
  }

  async pause() {
    if (this.snap.status !== 'playing') return
    this.offset = this.currentPosition()
    if (this.usingElement && this.mediaEl) {
      // <audio> 兜底：元素没有"暂停来源"概念，直接 pause 元素即可
      this.mediaEl.pause()
      this.stopTicker()
      this.emit({ status: 'paused', position: this.offset })
      return
    }
    this.stopSource()
    this.emit({ status: 'paused', position: this.offset })
  }

  async resume() {
    if (this.snap.status !== 'paused') return
    if (this.usingElement && this.mediaEl) {
      await this.unlock()
      await this.mediaEl.play().catch(() => {})
      this.startTicker()
      this.emit({ status: 'playing' })
      return
    }
    if (!this.buffer) return
    await this.unlock()
    this.startPlayback(this.offset)
    this.emit({ status: 'playing' })
  }

  async toggle() {
    if (this.snap.status === 'playing') await this.pause()
    else if (this.snap.status === 'paused') await this.resume()
    else if (this.snap.song) await this.play(this.snap.song)
  }

  async seek(sec: number) {
    const target = Math.min(Math.max(0, sec), this.snap.duration || 0)
    if (this.usingElement && this.mediaEl) {
      // <audio> 兜底：直接设 currentTime 即可（无需重新解码）
      try {
        this.mediaEl.currentTime = target
      } catch {
        /* ignore */
      }
      this.emit({ position: target })
      return
    }
    if (!this.buffer) return
    const t2 = Math.min(Math.max(0, sec), this.buffer.duration)
    if (this.snap.status === 'playing') {
      await this.unlock()
      this.startPlayback(t2)
      this.emit({ position: t2 })
    } else {
      this.offset = t2
      this.emit({ position: t2 })
    }
  }

  stop() {
    this.gen++
    this.stopSource()
    this.stopTicker()
    this.buffer = null
    this.offset = 0
    this.usingElement = false
    this.emit({ status: 'idle', position: 0, buffered: 0, error: '' })
  }

  private currentPosition(): number {
    // <audio> 兜底模式的进度以元素为准（el.currentTime），不要再拿 AudioContext 时钟算
    if (this.snap.status === 'playing' && this.usingElement && this.mediaEl) {
      return Math.max(0, this.mediaEl.currentTime || 0)
    }
    if (this.snap.status === 'playing' && this.ctx) {
      return Math.max(0, this.ctx.currentTime - this.startedAt)
    }
    return this.offset
  }

  // ============ 连播 ============

  setQueue(songs: TrackView[], startIndex = -1) {
    this.queue = songs
    this.index = startIndex
  }

  setIndexBySongId(id: number) {
    const i = this.queue.findIndex((s) => s.id === id)
    if (i >= 0) this.index = i
  }

  async next(auto = false) {
    if (!this.queue.length) return
    if (auto && this.repeat === 'one') {
      const cur = this.queue[this.index] ?? this.queue[0]
      if (cur) await this.play(cur)
      return
    }
    const curIdx = this.index
    // 手动「下一首」：即使只剩一首也重播它；自动播放到底且 repeat=off 时停下
    if (curIdx >= this.queue.length - 1) {
      if (this.repeat === 'off' && auto) {
        this.emit({ status: 'ended', position: this.snap.duration })
        return
      }
      this.index = 0
    } else {
      this.index = curIdx + 1
    }
    const target = this.queue[this.index]
    if (target) await this.play(target)
  }

  async prev() {
    if (!this.queue.length) return
    let i = this.index - 1
    if (i < 0) i = this.queue.length - 1
    this.index = i
    const target = this.queue[i]
    if (target) await this.play(target)
  }

  private onTrackEnded() {
    this.emit({ status: 'ended', position: this.snap.duration })
    // 列表循环（all）即使只有一首也要重播；单曲循环（one）同样重播；列表播放（off）播完即停
    if (this.repeat === 'all' || this.repeat === 'one' || this.queue.length > 1) {
      void this.next(true)
    }
  }

  // ============ 进度驱动 ============

  private startTicker() {
    this.stopTicker()
    const tick = () => {
      if (this.snap.status === 'playing') {
        const pos = this.currentPosition()
        const dur = this.buffer?.duration ?? this.snap.duration
        this.emit({ position: Math.min(pos, dur || pos) })
      }
      this.rafId = requestAnimationFrame(tick)
    }
    this.rafId = requestAnimationFrame(tick)
  }

  private stopTicker() {
    if (this.rafId) {
      cancelAnimationFrame(this.rafId)
      this.rafId = 0
    }
  }

  // ============ 歌词 ============

  async loadLyric(songId: number): Promise<string> {
    try {
      return await getLrcText(songId)
    } catch {
      return ''
    }
  }

  async loadSongDetail(id: number): Promise<TrackView | null> {
    try {
      return await getSong(id)
    } catch {
      return null
    }
  }

  // ============ 缓存管理 ============

  forgetCache(songId: number) {
    this.cached.delete(songId)
    decodedDelete(songId)
  }
}
