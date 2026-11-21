/**
 * 播放器 API 客户端。
 *
 * 全部请求都指向独立部署的播放器站点（默认 player.modern-chat.top）。
 * 该站点已在 nginx 层放开 CORS（精确来源 newchat.modern-chat.top + Allow-Credentials），
 * 因此这里必须用 `credentials: 'include'` 携带播放器的 session cookie。
 *
 * 两个站同属 modern-chat.top（同 site），播放器的 SameSite=Lax session 在跨 origin
 * 的 fetch 中可以被带上（Lax 只阻止 cross-site）。
 *
 * 鉴权说明（实测）：
 * - 本地曲库列表 / 歌词 / 加密流：游客即可（无 session 也能播）
 * - 在线搜索 / 在线解析：需要播放器账号登录
 * 所以本 UI 在未登录状态下仍能完整使用本地曲库，在线功能则提示去登录。
 */

export const PLAYER_BASE = 'https://player.modern-chat.top'

/**
 * 把封面地址归一化成**绝对 http(s) 地址**。
 *
 * 插件面板用描述式 UI 的 image 组件渲染封面，而该组件出于安全考虑
 * 只放行 `http(s)` 开头的地址（相对路径会被校验丢弃，封面直接消失）。
 * 播放器接口有时返回 /uploads/xx.jpg 这种站内相对路径，
 * 所以统一在这里补全成绝对地址，让宿主 UI 与插件面板都能直接用。
 */
function absoluteCover(url: string | undefined | null): string {
  const v = (url ?? '').trim()
  if (!v) return ''
  if (/^https?:\/\//i.test(v)) return v
  if (v.startsWith('//')) return `https:${v}`
  return `${PLAYER_BASE}${v.startsWith('/') ? '' : '/'}${v}`
}

export class PlayerApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
  ) {
    super(message)
    this.name = 'PlayerApiError'
  }
}

/**
 * 播放器接口的 require_same_origin 会检查 Origin/Referer，
 * 浏览器从聊天室域发起时 Origin 是聊天室，已在播放器的白名单里，无需额外头。
 *
 * 注意：这里**不能**带 X-Requested-With —— CDN 的 CORS 预检返回的是
 * `Access-Control-Allow-Headers: *`，而通配符在带凭据的请求里不合法，
 * 带上这个自定义头会让浏览器直接拒绝整个请求。
 */
const AJAX_HEADER: Record<string, string> = {}

let csrfToken = ''

/** 播放器登录态 / CSRF 一次性拉取 */
export async function ensureCsrf(): Promise<string> {
  if (csrfToken) return csrfToken
  const res = await fetch(`${PLAYER_BASE}/api/csrf_token.php`, {
    credentials: 'include',
    headers: AJAX_HEADER,
  })
  const data = (await res.json().catch(() => ({}))) as { csrf_token?: string }
  csrfToken = data.csrf_token ?? ''
  return csrfToken
}

export function resetCsrf() {
  csrfToken = ''
}

interface Opts {
  method?: 'GET' | 'POST'
  query?: Record<string, string | number | undefined>
  body?: unknown
  /** 是否要求登录（失败时抛 unauthorized） */
  timeoutMs?: number
}

async function request<T>(path: string, opts: Opts = {}): Promise<T> {
  const { method = 'GET', query, body, timeoutMs = 20_000 } = opts

  const url = new URL(path, PLAYER_BASE)
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined) url.searchParams.set(k, String(v))
    }
  }

  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), timeoutMs)

  const init: RequestInit = {
    method,
    credentials: 'include',
    signal: ctrl.signal,
    headers: { ...AJAX_HEADER },
  }

  if (body !== undefined) {
    // 关键：这里必须组成 **CORS 简单请求**，不能触发预检。
    //
    // 原因：播放器经 CDN 后，预检响应把 Allow-Methods 限制成单个值或 `*`，
    // 且 Allow-Headers 的通配符在带凭据请求里非法，浏览器会直接拒绝请求。
    // 而 application/x-www-form-urlencoded + POST 属于"简单请求"，浏览器不发预检，
    // 于是完全不经过 CDN 的跨域规则。
    //
    // 服务端 get_input() 在非 JSON 时读 $_POST，csrf_check 也支持 body 里的 csrf 字段，
    // 因此行为与 JSON 完全一致。
    const token = await ensureCsrf()
    const params = new URLSearchParams()
    if (body && typeof body === 'object') {
      for (const [k, v] of Object.entries(body as Record<string, unknown>)) {
        if (v === undefined || v === null) continue
        params.set(k, typeof v === 'string' ? v : JSON.stringify(v))
      }
    }
    if (token) params.set('csrf', token)
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/x-www-form-urlencoded'
    init.body = params.toString()
  }

  try {
    const res = await fetch(url.toString(), init)
    // 播放器在「地址栏直连」时会返回 200 + 空 body，这里按空响应处理
    const text = await res.text()
    if (!res.ok) {
      let msg = `HTTP ${res.status}`
      let code: string | undefined
      try {
        const j = JSON.parse(text) as { error?: string; message?: string }
        msg = j.message ?? j.error ?? msg
        code = j.error
      } catch {
        /* 非 JSON 错误体，保持 HTTP 状态文本 */
      }
      throw new PlayerApiError(res.status, msg, code)
    }
    if (!text) {
      throw new PlayerApiError(res.status, '播放器返回空响应（可能未放行跨域）', 'empty')
    }
    return JSON.parse(text) as T
  } catch (e) {
    if (e instanceof PlayerApiError) throw e
    if ((e as Error)?.name === 'AbortError') {
      throw new PlayerApiError(0, '请求超时', 'timeout')
    }
    throw new PlayerApiError(0, (e as Error)?.message || '网络错误', 'network')
  } finally {
    clearTimeout(timer)
  }
}

// ============ 数据形状（与播放器返回对齐） ============
export interface PlayerSong {
  id: number
  title: string
  artist: string
  album: string
  duration: number
  cover_url: string
  hidden: number
  allow_download: number
  is_uploader: boolean
}

export interface StreamTicket {
  ok: boolean
  token: string
  exp: number
  file_size: number
  is_large: boolean
}


/**
 * 当前登录的播放器账号 id。
 * 由 ensurePlayerSession() 通过 chat_session.php 建立会话后写入 localStorage，
 * 用于前端做"私有歌单只看自己的"这类轻量过滤（服务端仍会做最终校验）。
 */
export function myPlayerUid(): number {
  try {
    return Number(localStorage.getItem('mc_player_uid') ?? 0) || 0
  } catch {
    return 0
  }
}

/** 是否以管理员身份登录播放器（管理员可看隐藏歌曲） */
export function isPlayerAdmin(): boolean {
  try {
    return localStorage.getItem('mc_player_admin') === '1'
  } catch {
    return false
  }
}

/** 歌单摘要 */
export interface PlayerPlaylist {
  id: number
  name: string
  user_id: number
  owner_name: string
  is_private: boolean
  is_default: boolean
  cover_url: string
  description: string
  song_count: number
  can_edit: boolean
}

/**
 * 拉某个歌单的歌单（用户自己选，不再把全部歌单合并）。
 *
 * 为什么改成按歌单选择：
 *   把「默认歌单 + 所有公开歌单」合并后，同一首歌可能出现在多个歌单里，
 *   列表顺序与来源都变得不可解释，用户也无法"只听某一个歌单"。
 *   现在由 UI 提供歌单下拉，选中哪个就只拉哪个。
 */
export async function listSongsOfPlaylist(playlistId: number): Promise<PlayerSong[]> {
  const detail = await request<{ ok: boolean; songs: PlayerSong[] }>('api/playlist.php', {
    query: { action: 'detail', playlist_id: playlistId },
  })
  return (detail.songs ?? []).map((x) => ({ ...x, cover_url: absoluteCover(x.cover_url) }))
}

/**
 * 拉全量曲库（旧接口，保留给搜索兜底用）。
 */
export async function listAllSongs(): Promise<PlayerSong[]> {
  const lists = await request<{ ok: boolean; playlists: PlayerPlaylist[] }>('api/playlist.php', {
    query: { action: 'list' },
  })
  const all = lists.playlists ?? []

  // 可见性规则（与播放器服务端一致）：
  //   - 公开歌单：所有人可见
  //   - 私有歌单：仅创建者本人可见
  //   - 管理员/系统用户：全部可见（含隐藏歌曲）
  // 前端这层过滤只是为了少发无谓请求；真正的权限在服务端 can_view_playlist。
  const admin = isPlayerAdmin()
  const meUid = myPlayerUid()
  const visible = all.filter((p) => !p.is_private || admin || p.user_id === meUid)
  const songs: PlayerSong[] = []
  const seen = new Set<number>()

  for (const pl of visible) {
    try {
      const detail = await request<{ ok: boolean; songs: PlayerSong[] }>('api/playlist.php', {
        query: { action: 'detail', playlist_id: pl.id },
      })
      for (const s of detail.songs ?? []) {
        if (!seen.has(s.id)) {
          seen.add(s.id)
          songs.push(s)
        }
      }
    } catch {
      /* 单个歌单失败不影响其余 */
    }
  }
  return songs
}

/** 拉歌单列表（游客可用） */
export async function listPlaylists(): Promise<PlayerPlaylist[]> {
  const res = await request<{ ok: boolean; playlists: PlayerPlaylist[] }>('api/playlist.php', {
    query: { action: 'list' },
  })
  return (res.playlists ?? []).map((p) => ({ ...p, cover_url: absoluteCover(p.cover_url) }))
}

/** 本地曲库搜索（服务端要求 q 非空） */
export async function searchSongs(keyword: string): Promise<PlayerSong[]> {
  const q = keyword.trim()
  if (!q) return listAllSongs()
  const res = await request<{ ok: boolean; songs: PlayerSong[] }>('api/song.php', { query: { q } })
  return (res.songs ?? []).map((x) => ({ ...x, cover_url: absoluteCover(x.cover_url) }))
}

/** 单曲详情 */
export async function getSong(id: number): Promise<PlayerSong> {
  const res = await request<{ ok: boolean; song: PlayerSong }>('api/song.php', { query: { id } })
  return res.song
}

/** 歌词（lrc 普通 / yrc 逐字），游客可用 */
export async function getLyric(songId: number): Promise<{ lrc: string; yrc: string }> {
  const res = await request<{ ok: boolean; lrc: string; yrc: string }>('api/songs.php', {
    query: { action: 'lyric', song_id: songId },
    // 该端点在 api/pc/ 下（带 CORS 与 Bearer），但我们没有 Bearer；
    // 主站对应端点是 lrc.php，返回纯文本，这里兜底用主站。
  }).catch(async () => {
    const txt = await fetch(`${PLAYER_BASE}/api/lrc.php?id=${songId}`, {
      credentials: 'include',
      headers: AJAX_HEADER,
    }).then((r) => r.text())
    return { ok: true, lrc: txt, yrc: '' }
  })
  return { lrc: res.lrc ?? '', yrc: res.yrc ?? '' }
}

/** 纯文本歌词（主站 lrc.php），稳定可用 */
export async function getLrcText(songId: number): Promise<string> {
  const res = await fetch(`${PLAYER_BASE}/api/lrc.php?id=${songId}`, {
    credentials: 'include',
    headers: AJAX_HEADER,
  })
  if (!res.ok) return ''
  return await res.text()
}

/** 申请一次性加密流 token（游客可用） */
export async function issueStreamToken(songId: number): Promise<StreamTicket> {
  return await request<StreamTicket>('api/stream_token.php', {
    method: 'POST',
    body: { id: songId },
  })
}

/**
 * 拉取加密音频。
 * 返回的是 AES-256-CTR 密文（application/octet-stream），前端自行解密。
 * key = SHA256(token + "|stream")，nonce = SHA256(token + "|iv")[0..12]，与源站一致。
 */
export async function fetchEncrypted(
  token: string,
  onProgress?: (loaded: number, total: number) => void,
): Promise<ArrayBuffer> {
  const url = `${PLAYER_BASE}/api/stream_get.php?tk=${encodeURIComponent(token)}`

  const res = await fetch(url, {
    credentials: 'include',
    headers: { ...AJAX_HEADER },
  })
  if (!res.ok) {
    const t = await res.text().catch(() => '')
    throw new PlayerApiError(res.status, t === 'token_expired' ? '播放凭证已过期' : `拉流失败 ${res.status}`)
  }

  const total = Number(res.headers.get('Content-Length') ?? 0)
  if (!res.body) {
    const buf = await res.arrayBuffer()
    onProgress?.(buf.byteLength, total || buf.byteLength)
    return buf
  }

  // 边读边报进度（大文件时能显示缓冲百分比）
  const reader = res.body.getReader()
  const chunks: Uint8Array[] = []
  let loaded = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    chunks.push(value)
    loaded += value.byteLength
    onProgress?.(loaded, total)
  }

  const out = new Uint8Array(loaded)
  let off = 0
  for (const c of chunks) {
    out.set(c, off)
    off += c.byteLength
  }
  return out.buffer
}

/** 在线搜索（需要播放器账号登录） */
export async function onlineSearch(
  keyword: string,
  source: 'kw' | 'kg' | 'tx' | 'wy' | 'mg' = 'kw',
): Promise<unknown> {
  return await request('api/online_music.php', {
    query: { action: 'search', name: keyword, source, type: 'song', page: 1, pages: 1 },
    timeoutMs: 40_000,
  })
}

/** 在线歌曲解析（拿播放地址，需要登录） */
export async function onlineSongUrl(payload: Record<string, unknown>): Promise<unknown> {
  return await request('api/online_music.php', {
    method: 'POST',
    query: { action: 'song_url' },
    body: payload,
    timeoutMs: 40_000,
  })
}

/** 在线歌词（需要登录） */
export async function onlineLyric(payload: Record<string, unknown>): Promise<unknown> {
  return await request('api/online_music.php', {
    method: 'POST',
    query: { action: 'lyric' },
    body: payload,
    timeoutMs: 40_000,
  })
}
