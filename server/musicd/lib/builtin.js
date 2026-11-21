/**
 * lx 内置平台搜索（不依赖音源脚本）。
 *
 * 为什么要有它：音源脚本只保证「给 songmid 能换出直链」，搜索常常走的是
 * 各自的私有后端（需要卡密/白名单，实测星海后端对未授权 IP 直接 403）。
 * 而搜索这一步各平台的公开接口是稳定的、也不需要鉴权 —— 所以搜索走内置
 * SDK，直链交给音源脚本。两条路都留在自己手里，任何一边挂了都还能用。
 *
 * 各模块来自 lx-music-desktop（MIT），已做去依赖化改造：
 *   request.js 换成原生 fetch、iconv-lite 换成 TextDecoder。
 *
 * ===== 三个稳定性措施（都是被实测逼出来的）=====
 *
 * 1) **按平台串行**：lx 的 musicSearch 是「带可变状态的单例」
 *    （this.total / this.page / this.allPage 挂在对象上），并发调用会互相
 *    覆盖状态，表现就是「同样的关键词时好时坏」。用一条 per-platform 队列
 *    把它序列化。
 * 2) **短 TTL 缓存**：翻回上一页、或连点两次搜索都会重复打同一批请求；
 *    上游对这些重复请求很敏感（实测连续请求会突然返回空）。
 * 3) **空结果重试 + 退避**：上游偶发返回空（不是错误码，就是空列表），
 *    隔几百毫秒再试一次基本就正常了。
 */
import txSearch from '../sdk/musicSdk/tx/musicSearch.js'
import kwSearch from '../sdk/musicSdk/kw/musicSearch.js'
import kgSearch from '../sdk/musicSdk/kg/musicSearch.js'
import wySearch from '../sdk/musicSdk/wy/musicSearch.js'
import mgSearch from '../sdk/musicSdk/mg/musicSearch.js'

const searchImpl = {
  tx: txSearch,
  kw: kwSearch,
  kg: kgSearch,
  wy: wySearch,
  mg: mgSearch,
}

export const BUILTIN_SOURCES = [
  { id: 'tx', name: 'QQ音乐' },
  { id: 'kw', name: '酷我音乐' },
  { id: 'kg', name: '酷狗音乐' },
  { id: 'wy', name: '网易云音乐' },
  { id: 'mg', name: '咪咕音乐' },
]

/** 同关键词的重复搜索直接走缓存（包含空结果，避免空结果被反复打出上游）。 */
const CACHE_TTL = 5 * 60 * 1000
const CACHE_MAX = 300
const cache = new Map()

function cacheKey(source, keyword, page, limit) {
  return `${source}\u0000${keyword}\u0000${page}\u0000${limit}`
}

function cacheGet(key) {
  const hit = cache.get(key)
  if (!hit) return null
  if (Date.now() - hit.at > (hit.ttl ?? CACHE_TTL)) {
    cache.delete(key)
    return null
  }
  return hit.value
}

function cacheSet(key, value, ttl = CACHE_TTL) {
  if (cache.size >= CACHE_MAX) {
    // 简单淘汰：删掉最早插入的一批
    const drop = Math.ceil(CACHE_MAX * 0.2)
    let i = 0
    for (const k of cache.keys()) {
      cache.delete(k)
      if (++i >= drop) break
    }
  }
  cache.set(key, { at: Date.now(), value, ttl })
}

/** 每个平台一条串行队列：前一个请求结束才发下一个。 */
const queues = new Map()

function enqueue(source, task) {
  const prev = queues.get(source) || Promise.resolve()
  const next = prev.then(task, task)
  // 队列本身不传播错误，否则一次失败会让后续请求全部被拒绝
  queues.set(
    source,
    next.then(
      () => {},
      () => {},
    ),
  )
  return next
}

/** 归一化成前端统一结构。 */
function normalize(source, raw) {
  const list = Array.isArray(raw?.list) ? raw.list : []
  return {
    source,
    total: raw?.total ?? list.length,
    allPage: raw?.allPage ?? 1,
    list: list.map((s) => ({
      // 保留原始字段：音源脚本解析直链时要 songmid/hash/albumId 等
      ...s,
      source: s.source || source,
      name: s.name || '',
      singer: s.singer || '',
      albumName: s.albumName || '',
      interval: s.interval || '',
      img: s.img || s.picUrl || '',
    })),
  }
}

/**
 * 搜索并归一化成前端统一结构。
 * @param {string} source 平台 id（tx/kw/kg/wy/mg）
 * @param {string} keyword
 * @param {number} page
 * @param {number} limit
 */
export async function searchBuiltin(source, keyword, page = 1, limit = 20, log) {
  const impl = searchImpl[source]
  if (!impl) throw new Error(`不支持的平台：${source}`)

  const key = cacheKey(source, keyword, page, limit)
  const cached = cacheGet(key)
  if (cached) return { ...cached, cached: true }

  const result = await enqueue(source, async () => {
    let lastErr = null
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const raw = await impl.search(keyword, page, limit)
        const out = normalize(source, raw)
        if (out.list.length > 0) return out
        // 空结果不一定是真的没有：上游偶发返回空，退避后再试
        if (attempt < 2) {
          await new Promise((r) => setTimeout(r, 400 + attempt * 600))
          continue
        }
        return out
      } catch (e) {
        lastErr = e
        if (log) log(`[search ${source}] 第 ${attempt + 1} 次失败: ${e.message}`)
        if (attempt < 2) await new Promise((r) => setTimeout(r, 400 + attempt * 600))
      }
    }
    if (lastErr) throw lastErr
    return { source, total: 0, allPage: 0, list: [] }
  })

  // 只缓存「明确有结果」的（5 分钟）；空结果只短缓存 30 秒防抖，
  // 避免把上游「偶发返回空」冻成 5 分钟恒空（曾实测 kg/tx 第一页被冻空，
  // 表现为搜出来的第一页是空的、翻页才有歌）。
  if (result.list.length > 0) {
    cacheSet(key, result, CACHE_TTL)
  } else {
    cacheSet(key, result, 30 * 1000)
  }
  return result
}

export function hasBuiltinSearch(source) {
  return Object.prototype.hasOwnProperty.call(searchImpl, source)
}

/** 供状态页展示：当前缓存了多少条搜索结果。 */
export function builtinStats() {
  return { cacheEntries: cache.size }
}