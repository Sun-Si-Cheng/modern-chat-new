/**
 * musicd —— ModernChat 自定义音源运行时。
 *
 * 职责边界（很重要）：
 *   ✅ 搜索、解析直链、取歌词、取封面
 *   ❌ 不代理音频字节 —— 浏览器拿到直链自己下载、自己缓存、自己播放
 *
 * 为什么这样切：之前音频经服务器中转，上游一限流（实测 429）整站播放就卡死，
 * 而且白白吃掉自己服务器的带宽。改成直链后，播放链路只剩「浏览器 → 音源」。
 *
 * 监听 127.0.0.1，只允许本机（Go 后端）访问，靠 X-Music-Key 做一层校验。
 */
import http from 'node:http'
import path from 'node:path'
import fs from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

import { SourceRegistry, resolveWithFallback } from './lib/registry.js'
import { searchBuiltin, BUILTIN_SOURCES, hasBuiltinSearch } from './lib/builtin.js'
import { fetchBuiltinLyric, hasBuiltinLyric } from './lib/lyric.js'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const PORT = Number(process.env.MUSICD_PORT || 8310)
const HOST = process.env.MUSICD_HOST || '127.0.0.1'
const KEY = process.env.MUSICD_KEY || ''
const DATA_DIR = process.env.DATA_DIR || path.join(__dirname, 'data')
const DEBUG = process.env.MUSICD_DEBUG === '1'

const log = (...a) => console.log(new Date().toISOString(), '[musicd]', ...a)

const registry = new SourceRegistry({
  builtinDir: path.join(__dirname, 'sources'),
  userDir: path.join(DATA_DIR, 'musicd', 'sources'),
  // 解析优先级：能拿到真直链的排前面。实测聚合类（juheapi）最快，
  // 星海后端对未授权 IP 回 403，所以排在后面。
  preferOrder: ['juheapi', 'xinghai', 'K×H', 'LX', 'lx-', '长青', 'HYW'],
  log: (...a) => DEBUG && log(...a),
})

// ---------- 小工具 ----------

function send(res, status, obj) {
  const body = Buffer.from(JSON.stringify(obj))
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': body.length,
    'Cache-Control': 'no-store',
  })
  res.end(body)
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    let size = 0
    req.on('data', (c) => {
      size += c.length
      if (size > 2 << 20) {
        reject(new Error('请求体过大'))
        req.destroy()
        return
      }
      chunks.push(c)
    })
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    req.on('error', reject)
  })
}

async function readJSON(req) {
  const raw = await readBody(req)
  if (!raw) return {}
  try {
    return JSON.parse(raw)
  } catch {
    throw new Error('请求体不是合法 JSON')
  }
}

function cleanKeyword(s) {
  return String(s || '').trim().slice(0, 80)
}

// ---------- 业务 ----------

/** 搜索：内置 SDK（五个主流平台）优先，其余平台（如汽水）走音源插件。 */
async function doSearch({ source, name, page = 1, limit = 20 }) {
  const kw = cleanKeyword(name)
  if (!kw) throw new Error('请输入关键词')

  if (hasBuiltinSearch(source)) {
    try {
      const r = await searchBuiltin(source, kw, Number(page) || 1, Number(limit) || 20, log)
      if (r.list.length) return { ...r, via: 'builtin' }
    } catch (e) {
      log(`内置搜索失败 ${source}: ${e.message}`)
    }
  }

  // 内置没有覆盖的平台（汽水 / B站等）：交给音源脚本自己的 search
  const cands = registry.candidates(source)
  if (cands.length === 0) throw new Error(`不支持的平台：${source}`)
  const errors = []
  for (const src of cands) {
    if (src.kind !== 'musicfree') continue
    try {
      const raw = await src.search(kw, Number(page) || 1)
      const list = Array.isArray(raw?.data) ? raw.data : Array.isArray(raw?.list) ? raw.list : []
      if (list.length) {
        return {
          source,
          total: raw.total ?? list.length,
          allPage: 1,
          via: src.id,
          list: list.map((s) => ({
            ...s,
            source,
            songmid: String(s.songmid ?? s.id ?? s._songId ?? s._rid ?? ''),
            name: s.name || s.title || '',
            singer: s.singer || s.artist || '',
            albumName: s.albumName || s.album || '',
            interval: s.interval || '',
            img: s.img || s.artwork || '',
          })),
        }
      }
      errors.push(`${src.id}: 无结果`)
    } catch (e) {
      errors.push(`${src.id}: ${e.message}`)
      log(`音源搜索失败 ${src.id}/${source}: ${e.message}`)
    }
  }
  if (errors.length) log(`搜索全部失败 ${source}: ${errors.join('；')}`)
  return { source, total: 0, allPage: 0, via: 'none', list: [] }
}

/** 解析直链。返回的是**第三方真实地址**，前端直接去下载。 */
async function doResolve({ source, musicInfo, quality = '128k' }) {
  if (!musicInfo || typeof musicInfo !== 'object') throw new Error('缺少 musicInfo')
  const platform = String(source || musicInfo.source || '')
  if (!platform) throw new Error('缺少 source')
  const r = await resolveWithFallback(registry, platform, musicInfo, quality, log)
  return { url: r.url, by: r.by, cors: r.cors, direct: true }
}

/**
 * 歌词。
 *
 * 顺序：**内置公开接口优先，音源脚本兜底**。
 * 之前只走音源脚本的 lyric 动作，实测大部分音源不实现它、或依赖需要卡密的
 * 私有后端，结果就是「能播、没歌词」。各平台的公开歌词接口稳定免鉴权，
 * 所以内置一份（见 lib/lyric.js），音源脚本退化为补充来源：内置拿不到时
 * 再逐个问音源（有些音源的逐字歌词比公开接口更全）。
 */
async function doLyric({ source, musicInfo }) {
  const platform = String(source || musicInfo?.source || '')
  const errors = []

  if (hasBuiltinLyric(platform)) {
    try {
      const r = await fetchBuiltinLyric(platform, musicInfo)
      if (r.lrc) return { lrc: r.lrc, tlyric: r.tlyric || '', lxlyric: '', via: 'builtin' }
    } catch (e) {
      errors.push(`builtin: ${e.message}`)
      log(`内置歌词失败 ${platform}: ${e.message}`)
    }
  }

  for (const src of registry.candidates(platform)) {
    try {
      let r
      if (src.kind === 'musicfree') {
        r = await src.lyric(musicInfo)
        if (r && (r.rawLrc || r.lrc)) {
          return { lrc: r.rawLrc || r.lrc, tlyric: r.translation || '', lxlyric: '', via: src.id }
        }
      } else {
        r = await src.call('lyric', platform, { musicInfo })
        if (r && (r.lyric || r.lrc)) {
          return { lrc: r.lyric || r.lrc, tlyric: r.tlyric || '', lxlyric: r.lxlyric || '', via: src.id }
        }
      }
    } catch (e) {
      errors.push(`${src.id}: ${e.message}`)
    }
  }
  if (errors.length) log(`歌词全部失败 ${platform}: ${errors.slice(0, 4).join('；')}`)
  return { lrc: '', tlyric: '', lxlyric: '', via: 'none' }
}

// ---------- HTTP ----------

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`)
  const p = url.pathname

  // 只允许本机 + 共享密钥
  const ip = req.socket.remoteAddress || ''
  const loopback = ip === '127.0.0.1' || ip === '::1' || ip === '::ffff:127.0.0.1'
  if (!loopback) return send(res, 403, { ok: false, error: '仅允许本机访问' })
  if (KEY && req.headers['x-music-key'] !== KEY) return send(res, 401, { ok: false, error: '密钥无效' })

  try {
    if (p === '/health') {
      return send(res, 200, { ok: true, sources: registry.sources.length, errors: registry.loadErrors.length })
    }

    if (p === '/sources') {
      return send(res, 200, { ok: true, data: { ...registry.summary(), builtin: BUILTIN_SOURCES } })
    }

    if (p === '/sources/reload' && req.method === 'POST') {
      const s = await registry.load()
      return send(res, 200, { ok: true, data: s })
    }

    if (p === '/search') {
      const data = await doSearch({
        source: url.searchParams.get('source') || 'tx',
        name: url.searchParams.get('name') || url.searchParams.get('keyword') || '',
        page: url.searchParams.get('page') || 1,
        limit: url.searchParams.get('limit') || 20,
      })
      return send(res, 200, { ok: true, data })
    }

    if (p === '/resolve' && req.method === 'POST') {
      const body = await readJSON(req)
      const data = await doResolve(body)
      return send(res, 200, { ok: true, data })
    }

    if (p === '/lyric' && req.method === 'POST') {
      const body = await readJSON(req)
      const data = await doLyric(body)
      return send(res, 200, { ok: true, data })
    }

    return send(res, 404, { ok: false, error: 'not found' })
  } catch (e) {
    return send(res, 400, { ok: false, error: e.message || '处理失败' })
  }
})

async function main() {
  await fs.mkdir(path.join(DATA_DIR, 'musicd', 'sources'), { recursive: true })
  const summary = await registry.load()
  log(`已加载 ${summary.count} 个音源，失败 ${summary.errors.length} 个`)
  for (const e of summary.errors) log(`  ✗ ${e.file}: ${e.error}`)
  server.listen(PORT, HOST, () => log(`listening on http://${HOST}:${PORT}`))
}

main().catch((e) => {
  log('fatal:', e)
  process.exit(1)
})

process.on('SIGTERM', () => server.close(() => process.exit(0)))
process.on('SIGINT', () => server.close(() => process.exit(0)))