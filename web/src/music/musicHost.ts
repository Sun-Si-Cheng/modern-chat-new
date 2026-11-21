/**
 * 给插件用的音乐能力通道（宿主侧实现）。
 *
 * 背景：插件运行在 sandbox iframe（无 allow-same-origin）里，属 opaque origin，
 * 因此 **IndexedDB 不可用**（浏览器直接抛 SecurityError），Web Audio 的
 * decodeAudioData 虽能跑，但一段完整播放器的缓存/持久化都无处存放。
 *
 * 所以把"重活"留在宿主：
 *   - 拉加密流、AES-CTR 解密、decodeAudioData、IndexedDB 缓存、播放控制，
 *     全部由主站已有的 MusicEngine 执行；
 *   - 插件只负责 UI 与交互意图（放哪首歌、切歌、显示歌词），
 *     通过 postMessage → 本模块 → MusicEngine。
 *
 * 这样：
 *   - 插件可装可卸，卸掉即回到"没有音乐面板"的内核状态；
 *   - 加密与缓存逻辑一行不用重写，也不会因为沙箱限制而失效。
 */

import { MusicEngine, type PlayerSnapshot, type TrackView } from '@/music/engine'
import { activeLineIndex, parseLyric, sungCharCount, type LyricDoc } from '@/music/lyrics'
import { lyricOnline, resolveOnline, searchOnline, SOURCES, type OnlineSong, type OnlineSource } from '@/music/onlineApi'
import { ensurePlayerSession } from '@/music/linkApi'
import {
  listPlaylists,
  listSongsOfPlaylist,
  type PlayerPlaylist,
  type PlayerSong,
} from '@/music/playerApi'

/** 主站提供给插件的音乐能力接口名（插件侧通过 api.music.* 调用）。 */
export interface MusicBridgeResult {
  ok: boolean
  error?: string
  [k: string]: unknown
}

const engine = new MusicEngine()
let initialized = false
const listeners = new Set<(s: PlayerSnapshot) => void>()

/** 最近一次在线搜索结果（作为连播队列） */
let lastOnlineList: OnlineSong[] = []
/** 最近一次加载的本地歌单歌曲（作为本地曲库连播队列） */
let lastLocalSongs: PlayerSong[] = []

/** 统一的初始化（AudioContext 需要用户手势，由插件触发的动作里再 unlock）。 */
function ensureInit() {
  if (initialized) return
  initialized = true
  let lastSongKey: number | string | null = null
  engine.subscribe((s) => {
    // 歌曲变化（含自动连播切歌）时，自动加载新歌的歌词
    const cur = s.song
    const key = cur ? (cur.cacheKey ?? cur.id) : null
    if (cur && key !== lastSongKey) {
      lastSongKey = key
      if (cur.cacheKey) {
        const song = lastOnlineList.find((x) => `${x.source}:${x.songmid}` === cur.cacheKey)
        if (song) {
          void lyricOnline(song)
            .then((ly) => loadLyricFor(cur.cacheKey!, ly))
            .catch(() => loadLyricFor(cur.cacheKey!))
        } else {
          void loadLyricFor(cur.cacheKey)
        }
      } else if (cur.id) {
        void loadLyricFor(cur.id)
      }
    }
    for (const fn of listeners) fn(s)
  })
}

export interface MusicStatePayload {
  status: PlayerSnapshot['status']
  song: TrackView | null
  position: number
  duration: number
  volume: number
  fromCache: boolean
  error: string
  /** 当前歌词行与已唱字数（供插件高亮，不必自己解析歌词） */
  lyricLine: string
  lyricSungChars: number
  lyricKind: LyricDoc['kind']
}

let lyricDoc: LyricDoc = { kind: 'none', lines: [], chars: [], meta: {} }
/** 当前歌词对应的歌：本地歌用 songId，在线歌用 `${source}:${songmid}` 字符串 */
let currentLyricKey: number | string = -1

async function loadLyricFor(songKey: number | string, online?: { lrc: string; yrc: string }) {
  const myKey = songKey
  if (currentLyricKey === myKey) return
  currentLyricKey = myKey
  lyricDoc = { kind: 'none', lines: [], chars: [], meta: {} }
  try {
    let doc: LyricDoc
    if (online) {
      doc = parseLyric(online.lrc, online.yrc)
    } else if (typeof myKey === 'string') {
      return // 在线歌必须走 online 分支
    } else {
      const raw = await engine.loadLyric(myKey)
      doc = parseLyric(raw, '')
    }
    // 只写回「仍是当前歌曲」的结果：避免切歌后旧歌词的异步回调把新歌词覆盖掉
    if (currentLyricKey === myKey) lyricDoc = doc
  } catch {
    if (currentLyricKey === myKey) lyricDoc = { kind: 'none', lines: [], chars: [], meta: {} }
  }
}

/** 供插件轮询/订阅的完整状态快照。 */
export function musicState(): MusicStatePayload {
  const s = engine.snapshot
  const idx = activeLineIndex(lyricDoc.lines, s.position)
  const line = idx >= 0 ? lyricDoc.lines[idx] : undefined
  const next = idx >= 0 ? lyricDoc.lines[idx + 1] : undefined
  const sung =
    line && next !== undefined ? sungCharCount(lyricDoc.chars, line, next, s.position)
      : line ? sungCharCount(lyricDoc.chars, line, undefined, s.position) : 0
  return {
    status: s.status,
    song: s.song,
    position: s.position,
    duration: s.duration,
    volume: s.volume,
    fromCache: s.fromCache,
    error: s.error,
    lyricLine: line?.text ?? '',
    lyricSungChars: sung,
    lyricKind: lyricDoc.kind,
  }
}

/** 订阅播放状态变化（供插件推送状态）。返回取消函数。 */
export function onMusicState(fn: (s: PlayerSnapshot) => void): () => void {
  ensureInit()
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** 整篇歌词（供主站抽屉做滚动歌词；插件的描述式 UI 只能拿到当前行）。 */
export interface MusicLyricView {
  kind: LyricDoc['kind']
  lines: string[]
  active: number
}

export function musicLyricView(): MusicLyricView {
  return {
    kind: lyricDoc.kind,
    lines: lyricDoc.lines.map((l) => l.text),
    active: activeLineIndex(lyricDoc.lines, engine.snapshot.position),
  }
}

/** 静默播放（不弹 UI），供插件调用。 */
export async function musicPlayLocal(song: PlayerSong): Promise<MusicBridgeResult> {
  ensureInit()
  try {
    await engine.unlock()
    // 建队：本地歌单按「当前列表」播放，保证上一首/下一首/自动切歌可用
    const list = lastLocalSongs.length ? lastLocalSongs : [song]
    if (list.length) {
      engine.setQueue(list.map((s) => toTrack(s)), list.findIndex((s) => String(s.id) === String(song.id)))
    } else {
      engine.setQueue([song], 0)
    }
    await engine.play(song)
    void loadLyricFor(song.id)
    return { ok: true }
  } catch (e) {
    return { ok: false, error: (e as Error)?.message ?? '播放失败' }
  }
}

/** 播放在线歌曲（先解析直链，然后浏览器直连音源下载并缓存）。 */
export async function musicPlayOnline(song: OnlineSong): Promise<MusicBridgeResult> {
  ensureInit()
  const cacheKey = `${song.source}:${song.songmid}`
  try {
    await engine.unlock()
    // 在线播放同时建立「本次搜索结果」作为连播队列 —— 这样点一首能顺着整个列表播，
    // 上一首/下一首/自动切歌都有意义。改选歌时重建队列。
    if (lastOnlineList.length) {
      const tracks = lastOnlineList.map((s) => toTrack(s))
      const idx = tracks.findIndex((t) => t.cacheKey === cacheKey)
      engine.setQueue(tracks, idx >= 0 ? idx : 0)
    } else {
      engine.setQueue([], -1)
    }
    // playOnline 内部按缓存键判断：命中缓存时连解析都不会发生
    await engine.play(toTrack(song))
    // 歌词失败不算播放失败：音频已经在响了，不能因为取不到歌词就报「播放失败」。
    // 键必须用 cacheKey —— 此前传常量 0，第二首在线歌会因为「键没变」直接跳过，
    // 表现为换歌后歌词停留在上一首。
    void lyricOnline(song)
      .then((ly) => loadLyricFor(cacheKey, ly))
      .catch(() => {
        /* 取不到歌词就清空，界面退化为无歌词 */
      })
    return { ok: true }
  } catch (e) {
    return { ok: false, error: (e as Error)?.message ?? '播放失败' }
  }
}

export async function musicToggle(): Promise<MusicBridgeResult> {
  ensureInit()
  try {
    await engine.unlock()
    await engine.toggle()
    return { ok: true }
  } catch (e) {
    return { ok: false, error: (e as Error)?.message ?? '' }
  }
}

export async function musicNext(): Promise<MusicBridgeResult> {
  ensureInit()
  await engine.next()
  // 切歌后自动拉新歌歌词，否则歌词停在上一首（表现为"乱跳"）
  const cur = engine.snapshot.song
  if (cur?.cacheKey) {
    const song = lastOnlineList.find((s) => `${s.source}:${s.songmid}` === cur.cacheKey)
    if (song) {
      void lyricOnline(song)
        .then((ly) => loadLyricFor(cur.cacheKey!, ly))
        .catch(() => {})
    }
  } else if (cur) {
    void loadLyricFor(cur.id)
  }
  return { ok: true }
}

export async function musicPrev(): Promise<MusicBridgeResult> {
  ensureInit()
  await engine.prev()
  const cur = engine.snapshot.song
  if (cur?.cacheKey) {
    const song = lastOnlineList.find((s) => `${s.source}:${s.songmid}` === cur.cacheKey)
    if (song) {
      void lyricOnline(song)
        .then((ly) => loadLyricFor(cur.cacheKey!, ly))
        .catch(() => {})
    }
  } else if (cur) {
    void loadLyricFor(cur.id)
  }
  return { ok: true }
}

/** 设置播放模式：all=列表循环 off=列表播放 one=单曲循环 */
export function musicSetRepeat(mode: 'all' | 'off' | 'one'): MusicBridgeResult {
  ensureInit()
  engine.setRepeat(mode)
  return { ok: true }
}

/** 读取当前播放模式 */
export function musicRepeat(): 'all' | 'off' | 'one' {
  return engine.repeat
}

export async function musicSeek(sec: number): Promise<MusicBridgeResult> {
  ensureInit()
  await engine.seek(sec)
  return { ok: true }
}

export function musicVolume(v: number): MusicBridgeResult {
  ensureInit()
  engine.setVolume(v)
  return { ok: true }
}

export function musicStop(): MusicBridgeResult {
  engine.stop()
  return { ok: true }
}

/** 歌单列表（插件做选择器用）。 */
export async function musicPlaylists(): Promise<{ ok: boolean; playlists: PlayerPlaylist[] }> {
  try {
    // 先建立播放器会话：身份决定"私有歌单只看自己的"、"管理员看隐藏歌曲"
    await ensurePlayerSession()
    return { ok: true, playlists: await listPlaylists() }
  } catch (e) {
    return { ok: false, playlists: [], error: (e as Error)?.message } as never
  }
}

/** 某歌单的歌。 */
export async function musicPlaylistSongs(id: number): Promise<{ ok: boolean; songs: PlayerSong[] }> {
  try {
    await ensurePlayerSession()
    const songs = await listSongsOfPlaylist(id)
    lastLocalSongs = songs // 记录为本地连播队列
    return { ok: true, songs }
  } catch (e) {
    return { ok: false, songs: [], error: (e as Error)?.message } as never
  }
}

/** 在线搜索（经聊天室后端代理，插件不直接跨域）。 */
export async function musicSearchOnline(
  name: string,
  source: OnlineSource,
  page = 1,
): Promise<{ ok: boolean; songs: OnlineSong[]; error?: string; hasMore?: boolean }> {
  try {
    const songs = await searchOnline(name, source, page)
    // 记录最近一次在线搜索结果，作为连播队列（点一首能顺着整个列表播）
    lastOnlineList = songs
    return { ok: true, songs, hasMore: songs.length >= 20 }
  } catch (e) {
    return { ok: false, songs: [], error: (e as Error)?.message ?? '搜索失败' }
  }
}

export function musicSources() {
  return SOURCES
}

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 2147483000
  return h || 1
}

function parseInterval(iv?: string): number {
  if (!iv) return 0
  const m = /^(\d+):(\d+)$/.exec(iv.trim())
  if (!m) return 0
  return Number(m[1]) * 60 + Number(m[2])
}

/** 把在线歌曲统一成引擎的 TrackView（带缓存键 + 懒取直链回调，供队列/自动切歌用）。 */
function toTrack(song: OnlineSong | PlayerSong): TrackView {
  const isOnline = 'source' in song && 'songmid' in song
  if (isOnline) {
    const o = song as OnlineSong
    const cacheKey = `${o.source}:${o.songmid}`
    return {
      id: -Math.abs(hashStr(cacheKey)),
      title: o.name,
      artist: o.singer,
      album: o.albumName ?? '',
      duration: parseInterval(o.interval),
      cover_url: o.img ?? '',
      cacheKey,
      resolve: () => resolveOnline(o),
    }
  }
  const p = song as PlayerSong
  return {
    id: p.id,
    title: p.title,
    artist: p.artist,
    album: p.album,
    duration: p.duration,
    cover_url: p.cover_url,
  }
}
