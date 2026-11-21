/**
 * 音源注册表：扫描目录、加载脚本、维护「平台 → 音源」映射。
 *
 * 目录约定：
 *   <musicd>/sources/*.js          内置（随仓库分发，管理员可改）
 *   <DATA_DIR>/musicd/sources/*.js 外置（管理员/用户上传，覆盖同名内置）
 *
 * 同一个平台可以有多个音源，解析直链时按顺序尝试：任一成功即返回。
 * 这样「星海挂了换聚合 API」这类降级不用改代码。
 */
import fs from 'node:fs/promises'
import path from 'node:path'

import { loadLxSource, parseLxMeta } from './lx.js'
import { loadMusicFreeSource, parseMusicFreeMeta } from './musicfree.js'

/** 判断脚本类型：出现 module.exports 视为 MusicFree 插件，否则是 lx 音源。 */
export function detectKind(script) {
  return /module\.exports/.test(script) ? 'musicfree' : 'lx'
}

async function readDir(dir) {
  try {
    const names = await fs.readdir(dir)
    return names.filter((n) => n.endsWith('.js')).sort()
  } catch {
    return []
  }
}

export class SourceRegistry {
  constructor(opts) {
    this.builtinDir = opts.builtinDir
    this.userDir = opts.userDir
    this.log = opts.log || (() => {})
    /** @type {Array<object>} 已加载音源 */
    this.sources = []
    /** 平台 id → 音源列表 */
    this.byPlatform = new Map()
    this.loadErrors = []
    /** 解析直链的尝试顺序（按音源 id 前缀排序，可控优先级） */
    this.preferOrder = opts.preferOrder || []
  }

  async load() {
    this.sources = []
    this.byPlatform = new Map()
    this.loadErrors = []

    const files = []
    for (const n of await readDir(this.builtinDir)) files.push({ name: n, file: path.join(this.builtinDir, n), scope: 'builtin' })
    for (const n of await readDir(this.userDir)) files.push({ name: n, file: path.join(this.userDir, n), scope: 'user' })

    // 并行加载：脚本初始化要各自等网络（有的要 5 秒以上），串行会拖到十几秒
    const loaded = await Promise.all(
      files.map(async (f) => {
        try {
          const script = await fs.readFile(f.file, 'utf8')
          // id 用文件名（稳定、可定位），展示名用脚本里的 @name
          const id = path.basename(f.name, '.js')
          const kind = detectKind(script)
          const src =
            kind === 'musicfree'
              ? await loadMusicFreeSource({ id, script, log: this.log })
              : await loadLxSource({ id, script, log: this.log })
          src.scope = f.scope
          src.file = f.file
          this.log(`音源已加载 [${f.scope}] ${id} (${kind}) 平台=${src.platforms.join(',')}`)
          return src
        } catch (e) {
          this.loadErrors.push({ file: f.name, scope: f.scope, error: e.message })
          this.log(`音源加载失败 ${f.name}: ${e.message}`)
          return null
        }
      }),
    )

    // 外置音源优先（用户自己加的应该先被尝试）
    const ok = loaded.filter(Boolean)
    ok.sort((a, b) => {
      if (a.scope !== b.scope) return a.scope === 'user' ? -1 : 1
      const ia = this.preferOrder.findIndex((p) => a.id.startsWith(p))
      const ib = this.preferOrder.findIndex((p) => b.id.startsWith(p))
      return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib)
    })

    for (const src of ok) {
      this.sources.push(src)
      for (const p of src.platforms) {
        if (!this.byPlatform.has(p)) this.byPlatform.set(p, [])
        this.byPlatform.get(p).push(src)
      }
    }
    return this.summary()
  }

  summary() {
    const platforms = {}
    for (const [p, list] of this.byPlatform) {
      platforms[p] = list.map((s) => ({ id: s.id, name: s.name, kind: s.kind, scope: s.scope }))
    }
    return {
      count: this.sources.length,
      errors: this.loadErrors,
      platforms,
      sources: this.sources.map((s) => ({
        id: s.id,
        name: s.name,
        kind: s.kind,
        scope: s.scope,
        version: s.version,
        author: s.author,
        platforms: s.platforms,
      })),
    }
  }

  /** 取某平台的音源候选（按优先级）。 */
  candidates(platform) {
    return this.byPlatform.get(platform) || []
  }

  find(id) {
    return this.sources.find((s) => s.id === id)
  }
}

/**
 * 探测一个地址是不是**可播放的音频**，以及它是否允许浏览器跨域读取。
 *
 * 为什么光看 URL 格式不够：网易的 `music.163.com/song/media/outer/url?id=` 这种
 * 老接口永远返回 200，但实际会 302 到 /404 页面（HTML）。只判断 http:// 前缀
 * 会让前端拿到一个 HTML 地址然后「播放失败」，还不知道为什么。
 *
 * 为什么要探测 CORS：实测酷我与咪咕的音频直链**不返回 Access-Control-Allow-Origin**，
 * 浏览器里 `fetch()` 会直接抛 TypeError。但同一个地址 `<audio src>` 却能正常播放
 * （媒体元素不受 CORS 限制，只是"被污染"不可读）。所以前端需要知道走哪条路：
 *   cors=true  → fetch 下载字节，走 Web Audio 解码（可缓存、可精确 seek）
 *   cors=false → 用 <audio> 直接播（不能读字节，也就无法缓存）
 *
 * 判定：能拿到响应、Content-Type 是音频类，且（带 Origin 探测时）回显了 ACAO。
 */
async function probeAudio(url, timeout = 8000) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)
  try {
    // 带上 Origin 才能看出上游会不会回 ACAO（服务端探测默认没有 Origin）
    const resp = await fetch(url, {
      method: 'GET',
      headers: {
        'User-Agent': 'Mozilla/5.0',
        Range: 'bytes=0-1023',
        Origin: 'https://newchat.modern-chat.top',
      },
      signal: controller.signal,
      redirect: 'follow',
    })
    const ct = String(resp.headers.get('content-type') || '').toLowerCase()
    const acao = resp.headers.get('access-control-allow-origin')
    const finalUrl = resp.url || url
    try {
      await resp.body?.cancel()
    } catch {
      /* ignore */
    }
    let playable = false
    if (/\/404(\b|\/|\?|$)/.test(new URL(finalUrl).pathname)) playable = false
    else if (resp.status >= 400) playable = false
    else if (ct.startsWith('audio/') || ct.includes('octet-stream') || ct.includes('mpegurl')) playable = true
    else if (resp.status === 206) playable = true

    return { playable, cors: acao === '*' || !!acao }
  } catch {
    return { playable: false, cors: false }
  } finally {
    clearTimeout(timer)
  }
}

/**
 * 从某平台的多个音源里解析直链。
 *
 * ===== 为什么要「竞速」而不是「排队试」=====
 * 现在一个平台可能挂着十几个音源，逐个等超时的话用户要等半分钟以上。
 * 改成：同时发起，谁先给出**可播放**的地址就用谁，其余立刻取消。
 *
 * 三档节奏：
 *   - 第一档：并发全部候选（上限 8 个），任一成功即返回 —— 正常情况下就是最快的；
 *   - 失败的会记下来，只有全部失败才把错误汇总抛给前端；
 *   - 探测（probeAudio）在拿到地址后立刻做，探不过是 HTML 就直接丢弃、继续等别的。
 *
 * @returns {Promise<{url:string, by:string, cors:boolean}>}
 */
export async function resolveWithFallback(registry, platform, musicInfo, quality, log) {
  const cands = registry.candidates(platform)
  if (cands.length === 0) throw new Error(`没有可用于 ${platform} 的音源脚本`)

  const CONCURRENCY = 8
  const errors = []
  const queue = cands.slice()

  // 每拿到一个候选就开工，谁先成功就 resolve 整个 Promise
  const attempt = async (src) => {
    let url
    try {
      if (src.kind === 'musicfree') {
        const r = await src.mediaSource(musicInfo, quality)
        url = typeof r === 'string' ? r : r?.url
      } else {
        url = await src.call('musicUrl', platform, { musicInfo, type: quality })
        if (url && typeof url === 'object') url = url.url || url.play_url
      }
    } catch (e) {
      errors.push(`${src.id}: ${e.message}`)
      log(`解析失败 ${src.id}/${platform}: ${e.message}`)
      return null
    }

    const trimmed = typeof url === 'string' ? url.trim() : ''
    if (!/^https?:\/\//i.test(trimmed)) {
      errors.push(`${src.id}: 未返回有效地址`)
      return null
    }

    const probe = await probeAudio(trimmed)
    if (!probe.playable) {
      errors.push(`${src.id}: 地址不可播放`)
      log(`地址不可播放 ${src.id}/${platform}: ${trimmed.slice(0, 90)}`)
      return null
    }
    log(`解析成功 ${src.id}/${platform} cors=${probe.cors}`)
    return { url: trimmed, by: src.id, cors: probe.cors }
  }

  const result = await new Promise((resolve, reject) => {
    let running = 0
    let finished = 0
    let settled = false
    const total = queue.length

    const pump = () => {
      if (settled) return
      if (finished === total) {
        settled = true
        reject(new Error(errors.join('；') || '所有音源都未能解析出播放地址'))
        return
      }
      while (running < CONCURRENCY && queue.length > 0) {
        const src = queue.shift()
        running++
        attempt(src)
          .then((ok) => {
            if (settled) return
            if (ok) {
              settled = true
              resolve(ok)
              return
            }
            running--
            finished++
            pump()
          })
          .catch(() => {
            running--
            finished++
            pump()
          })
      }
    }
    pump()
  })

  return result
}

export { parseLxMeta, parseMusicFreeMeta }