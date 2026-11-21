/**
 * 内置平台歌词：直接调用各平台的**公开歌词接口**，不经过音源脚本。
 *
 * 为什么要内置：此前歌词完全依赖音源脚本的 lyric 动作，实测大部分音源要么
 * 不实现它、要么实现依赖各自的私有后端（未授权 IP 直接 403），表现就是
 * 「在线歌曲能播、歌词永远空白」。各平台的公开歌词接口稳定且免鉴权
 * （与搜索同一个思路，见 builtin.js），所以这里内置一份，音源脚本降为兜底。
 *
 * 五路实测（2026-11-20，服务器出口 IP 为 US，全部通过）：
 *   tx  c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg   返回 base64 的 lrc + trans
 *   wy  music.163.com/api/song/lyric                        lrc / tlyric
 *   kg  krcs.kugou.com/search → lyrics.kugou.com/download   fmt=lrc 服务端已把 KRC 转好
 *   kw  www.kuwo.cn/openapi/v1/www/lyric/getlyric           lrclist 逐行拼回标准 LRC
 *   mg  搜索结果自带 lrcUrl，直接取文本
 *
 * 汽水（qs）没有公开歌词接口，走音源 MusicFree 插件兜底。
 *
 * 注意：u.y.qq.com 的 musicu.fcg（GetLyric / GetPlayLyricInfo）实测对本机出口
 * 返回 500003/10006（需要登录态或风控放行），所以 QQ 走 c.y.qq.com 的老接口。
 */
import { createHash, randomUUID } from 'node:crypto'
import { Buffer } from 'node:buffer'

const UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
const TIMEOUT = 10000

/** 同一首歌一小时内不重复打上游（只缓存成功的，失败下次还要再试）。 */
const CACHE_TTL = 60 * 60 * 1000
const CACHE_MAX = 500
const cache = new Map()

function cacheGet(key) {
  const hit = cache.get(key)
  if (!hit) return null
  if (Date.now() - hit.at > CACHE_TTL) {
    cache.delete(key)
    return null
  }
  return hit.value
}

function cacheSet(key, value) {
  if (cache.size >= CACHE_MAX) {
    const drop = Math.ceil(CACHE_MAX * 0.2)
    let i = 0
    for (const k of cache.keys()) {
      cache.delete(k)
      if (++i >= drop) break
    }
  }
  cache.set(key, { at: Date.now(), value })
}

async function getText(url, headers = {}) {
  const resp = await fetch(url, {
    headers: { 'User-Agent': UA, ...headers },
    signal: AbortSignal.timeout(TIMEOUT),
    redirect: 'follow',
  })
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return await resp.text()
}

async function getJSON(url, headers = {}) {
  const text = await getText(url, headers)
  const trimmed = text.trim()
  let body = trimmed
  // jsonp 包装（cb({...})）剥掉
  const m = /^[a-zA-Z_$][\w$]*\(([\s\S]*)\)\s*;?\s*$/.exec(trimmed)
  if (m) body = m[1]
  try {
    return JSON.parse(body)
  } catch {
    throw new Error('响应不是 JSON')
  }
}

const md5hex = (s) => createHash('md5').update(s).digest('hex')

/** 有 [mm:ss] 时间标签才算歌词，避免把错误页/纯文本当歌词返回。 */
const hasTimeTag = (s) => /\[\d{1,3}:\d{2}/.test(s || '')

/** QQ 的歌词字段可能是 base64，也可能已是明文。 */
function decodeMaybeB64(s) {
  if (!s) return ''
  if (hasTimeTag(s)) return s
  const d = Buffer.from(s, 'base64').toString('utf8')
  return hasTimeTag(d) ? d : ''
}

// ---------- 各平台实现 ----------

async function lyricTx(mi) {
  const mid = String(mi.songmid || mi.mid || '')
  if (!mid) throw new Error('缺少 songmid')
  const u =
    'https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg' +
    `?songmid=${encodeURIComponent(mid)}&g_tk=5381&loginUin=0&hostUin=0` +
    '&format=json&inCharset=utf8&outCharset=utf-8&notice=0&platform=yqq.json&needNewCode=0'
  const j = await getJSON(u, { Referer: 'https://y.qq.com/' })
  if (Number(j.retcode ?? j.code ?? 0) !== 0) throw new Error(`QQ 歌词接口返回 ${j.retcode ?? j.code}`)
  return { lrc: decodeMaybeB64(j.lyric), tlyric: decodeMaybeB64(j.trans) }
}

async function lyricWy(mi) {
  const id = String(mi.songmid ?? mi.songId ?? mi.id ?? '')
  if (!id) throw new Error('缺少歌曲 id')
  const j = await getJSON(`https://music.163.com/api/song/lyric?id=${encodeURIComponent(id)}&lv=1&kv=1&tv=-1`, {
    Referer: 'https://music.163.com/',
  })
  return { lrc: j?.lrc?.lyric || '', tlyric: j?.tlyric?.lyric || '' }
}

// 酷狗两段式：search 拿候选 id/accesskey，download 拿正文（fmt=lrc，服务端已转好）。
// signature 实测用 md5(hash + token) 即可（token 与 lx-music-desktop 同款常量）。
const KG_TOKEN = 'hc6xxztzqjok16l0t7uajm1be4vl5lgw'

async function lyricKg(mi) {
  const hash = String(mi.hash || '')
  if (!hash) throw new Error('缺少 hash')
  let dur = Number(mi._interval || mi.duration || 0)
  if (!dur) {
    const m = /^(\d+):(\d+)$/.exec(String(mi.interval || ''))
    dur = m ? Number(m[1]) * 60 + Number(m[2]) : 0
  }
  const sj = await getJSON(
    `http://krcs.kugou.com/search?ver=1&man=yes&client=mobi&keyword=&duration=${dur}&hash=${encodeURIComponent(hash)}`,
  )
  const cand = Array.isArray(sj?.candidates) ? sj.candidates[0] : null
  if (!cand?.id || !cand?.accesskey) throw new Error('无歌词候选')
  const signature = md5hex(`${hash}${KG_TOKEN}`)
  const dj = await getJSON(
    `http://lyrics.kugou.com/download?ver=1&client=pc&id=${cand.id}&accesskey=${encodeURIComponent(cand.accesskey)}` +
      `&fmt=lrc&hash=${encodeURIComponent(hash)}&signature=${signature}`,
  )
  if (Number(dj?.error_code ?? 0) !== 0) throw new Error(`酷狗歌词下载失败 ${dj?.error_code}`)
  // 酷狗返回的 LRC 带 UTF-8 BOM，剥掉再交给前端解析
  const lrc = Buffer.from(dj?.content || '', 'base64').toString('utf8').replace(/^\uFEFF/, '')
  return { lrc: hasTimeTag(lrc) ? lrc : '', tlyric: '' }
}

async function lyricKw(mi) {
  const rid = String(mi.songmid || mi.RID || mi.id || '')
  if (!rid) throw new Error('缺少歌曲 id')
  const j = await getJSON(
    `https://www.kuwo.cn/openapi/v1/www/lyric/getlyric?musicId=${encodeURIComponent(rid)}` +
      `&httpsStatus=1&reqId=${randomUUID()}&plat=web&lib=ko`,
    { Referer: 'https://www.kuwo.cn/' },
  )
  if (Number(j?.code ?? 0) !== 200) throw new Error(`酷我歌词接口返回 ${j?.code}`)
  const list = Array.isArray(j?.data?.lrclist) ? j.data.lrclist : []
  if (!list.length) throw new Error('无歌词')
  const lrc = list
    .map((l) => {
      const t = Math.max(0, Number(l.time || 0))
      const mm = String(Math.floor(t / 60)).padStart(2, '0')
      const ss = (t % 60).toFixed(2).padStart(5, '0')
      return `[${mm}:${ss}]${l.lineLyric ?? ''}`
    })
    .join('\n')
  return { lrc, tlyric: '' }
}

async function lyricMg(mi) {
  const url = String(mi.lrcUrl || '')
  if (!url) throw new Error('搜索结果未带歌词地址')
  const text = await getText(url)
  return { lrc: hasTimeTag(text) ? text : '', tlyric: '' }
}

const impl = { tx: lyricTx, kw: lyricKw, kg: lyricKg, wy: lyricWy, mg: lyricMg }

export function hasBuiltinLyric(source) {
  return Object.prototype.hasOwnProperty.call(impl, source)
}

/**
 * 取某平台歌曲的公开歌词。
 * @returns {Promise<{lrc:string, tlyric:string}>} 失败抛错，由调用方降级到音源脚本
 */
export async function fetchBuiltinLyric(source, musicInfo) {
  const f = impl[source]
  if (!f) throw new Error(`平台无内置歌词：${source}`)
  const key = `${source}:${String(musicInfo?.songmid ?? musicInfo?.hash ?? '')}`
  const hit = cacheGet(key)
  if (hit) return hit
  const r = await f(musicInfo || {})
  const out = { lrc: r.lrc || '', tlyric: r.tlyric || '' }
  if (out.lrc) cacheSet(key, out)
  return out
}
