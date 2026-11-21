/**
 * 在线音乐 API。
 *
 * 链路：浏览器 → 本站 /api/music/*（同域，带登录态）→ musicd（本机音源运行时）
 *
 * ⚠️ 关键变化：`/api/music/resolve` 现在返回的是**第三方平台的真实直链**，
 * 不再是本站的中转地址。浏览器拿到直链后自己下载、自己缓存、自己播放，
 * 音频字节完全不经本服务器 —— 这是本次改造的核心目的：
 *   1) 不吃服务器带宽；
 *   2) 不被「上游对本服务器 IP 限流（实测 429）」波及；
 *   3) 首字节就是音源的首字节，没有中转排队。
 */

export interface OnlineSong {
  name: string
  singer: string
  source: string
  songmid: string
  albumId?: string
  albumName?: string
  interval?: string
  img?: string
  types?: Array<{ type: string; size: string }>
}

export type OnlineSource = 'kw' | 'kg' | 'tx' | 'wy' | 'mg' | 'qs'

export const SOURCES: Array<{ id: OnlineSource; label: string }> = [
  { id: 'tx', label: 'QQ音乐' },
  { id: 'kw', label: '酷我' },
  { id: 'wy', label: '网易云' },
  { id: 'kg', label: '酷狗' },
  { id: 'mg', label: '咪咕' },
  { id: 'qs', label: '汽水' },
]

class ApiErr extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const token = localStorage.getItem('mc_token') ?? ''
  const res = await fetch(`/api/music/${path}`, {
    ...init,
    headers: {
      ...(init?.headers as Record<string, string>),
      Authorization: `Bearer ${token}`,
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
    },
  })
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    throw new ApiErr(res.status, '响应格式错误')
  }
  if (!res.ok) {
    const msg = (data as { error?: string })?.error ?? `HTTP ${res.status}`
    throw new ApiErr(res.status, msg)
  }
  return data as T
}

/** 在线音乐是否可用（后端音源服务是否就绪） */
export async function musicStatus(): Promise<boolean> {
  try {
    const r = await call<{ online: boolean }>('status')
    return !!r.online
  } catch {
    return false
  }
}

/** 后端当前可用的音源与平台（后台/调试用）。 */
export async function musicSources(): Promise<{
  count: number
  platforms: Record<string, Array<{ id: string; name: string; kind: string; scope: string }>>
  sources: Array<{ id: string; name: string; kind: string; scope: string; platforms: string[] }>
}> {
  const r = await call<{ ok: boolean; data: never }>('sources')
  return r.data
}

/** 搜索在线歌曲。后端返回 {ok, data:{list:[...]}}。 */
export async function searchOnline(
  name: string,
  source: OnlineSource,
  page = 1,
): Promise<OnlineSong[]> {
  const q = new URLSearchParams({ name, source, page: String(page), limit: '20' })
  const r = await call<{ ok: boolean; data: unknown; list?: OnlineSong[] }>(`search?${q}`)
  if (Array.isArray(r.list)) return r.list
  const raw = r.data as unknown
  if (Array.isArray(raw)) return raw as OnlineSong[]
  if (raw && typeof raw === 'object' && Array.isArray((raw as { list?: OnlineSong[] }).list)) {
    return (raw as { list: OnlineSong[] }).list
  }
  return []
}

/** 搜索翻页：返回歌曲 + 是否还有下一页（每页 20 条，按返回条数判断）。 */
export async function searchOnlinePage(
  name: string,
  source: OnlineSource,
  page: number,
): Promise<{ songs: OnlineSong[]; hasMore: boolean; total?: number }> {
  const songs = await searchOnline(name, source, page)
  // 少于一页说明没了；正好 20 条可能还有下一页，交给 next 按钮再试
  return { songs, hasMore: songs.length >= 20 }
}

/**
 * 解析在线歌曲的**直链**。
 *
 * 返回值可以直接给 `<audio src>` 或 `fetch()` 用 —— 它是第三方平台上
 * 带签名与时效的真实地址。同一个地址通常在 1~2 小时内有效，因此上层
 * 应当把它和音频一起缓存起来（见 onlineCache.ts），过期后重新解析即可。
 */
export async function resolveOnline(song: OnlineSong, quality = '128k'): Promise<string> {
  const r = await call<{ ok: boolean; data: { url: string; by?: string; direct?: boolean } }>(
    'resolve',
    {
      method: 'POST',
      body: JSON.stringify({ songInfo: song, quality, source: song.source }),
    },
  )
  const u = r.data?.url ?? ''
  if (!u) throw new ApiErr(502, '该歌曲暂时无法解析，换个平台或换一首试试')
  return u
}

/**
 * 取在线歌词。
 *
 * musicd 返回的是 `{lrc, tlyric, lxlyric}`（lx 的字段名），本项目的
 * `parseLyric(lrc, yrc)` 第二个参数要的是逐字歌词，所以这里把 `lxlyric`
 * 映射成 `yrc`。此前直接读 `r.data.yrc` 恒为 undefined —— 就算音源给了
 * 逐字歌词也会被丢掉，只剩整行 LRC。
 */
export async function lyricOnline(song: OnlineSong): Promise<{ lrc: string; yrc: string }> {
  const r = await call<{ ok: boolean; data: { lrc?: string; tlyric?: string; lxlyric?: string } }>('lyric', {
    method: 'POST',
    body: JSON.stringify({ songInfo: song, source: song.source }),
  })
  return { lrc: r.data?.lrc ?? '', yrc: r.data?.lxlyric ?? '' }
}