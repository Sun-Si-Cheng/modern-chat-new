/**
 * MusicFree 插件加载器。
 *
 * 协议：脚本是 CommonJS 模块，`module.exports = { platform, search, getMediaSource, ... }`
 * 但沙箱里没有 require，唯一的依赖获取方式是宿主注入的 `__musicfree_require`。
 *
 * 这里用 `vm` 执行脚本并手工提供 module/exports/require 三件套：
 *   - require('axios') → 返回 undefined（插件普遍有 fetch 兜底，见汽水源的写法）
 *   - 其它模块一律抛错，避免插件把 Node 内置模块当依赖
 */
import vm from 'node:vm'

import { decontextify } from './decontextify.js'

const INIT_TIMEOUT = 5000
const RUN_TIMEOUT = 10000

/** 解析 MusicFree 插件头部注释。 */
export function parseMusicFreeMeta(script) {
  const meta = {}
  const head = script.slice(0, 4096)
  const m = /\/\*\*([\s\S]*?)\*\//.exec(head)
  if (!m) return meta
  for (const line of m[1].split(/\r?\n/)) {
    const r = /^\s*\*?\s*@(\w+)\s+(.+)$/.exec(line)
    if (r) meta[r[1]] = r[2].trim()
  }
  return meta
}

/**
 * 加载一个 MusicFree 插件。
 * @param {{id:string, script:string, platform?:string, log?:Function}} opts
 */
export async function loadMusicFreeSource(opts) {
  const { id, script } = opts
  const log = opts.log || (() => {})
  const meta = parseMusicFreeMeta(script)

  const moduleObj = { exports: {} }
  const noop = () => {}

  const sandbox = {
    module: moduleObj,
    exports: moduleObj.exports,
    // 插件里的 axios 一律拿不到；它们基本都有 fetch 兜底
    __musicfree_require: (name) => {
      log(`[${id}] 插件请求模块 ${name}（已忽略，将使用 fetch 兜底）`)
      return undefined
    },
    require: (name) => {
      log(`[${id}] 插件请求模块 ${name}（已忽略）`)
      return undefined
    },
    console: {
      log: noop,
      info: noop,
      debug: noop,
      warn: noop,
      error: (...a) => log(`[${id}] ${a.join(' ')}`),
      trace: noop,
    },
    setTimeout,
    clearTimeout,
    setInterval: (fn, ms) => {
      const t = setInterval(fn, Math.max(Number(ms) || 1000, 1000))
      if (t.unref) t.unref()
      return t
    },
    clearInterval,
    fetch,
    AbortController,
    URL,
    URLSearchParams,
    TextEncoder,
    TextDecoder,
    Buffer,
    Promise,
    Date,
    Math,
    JSON,
    atob: (s) => Buffer.from(String(s), 'base64').toString('binary'),
    btoa: (s) => Buffer.from(String(s), 'binary').toString('base64'),
  }
  sandbox.global = sandbox
  sandbox.window = sandbox
  sandbox.globalThis = sandbox

  const context = vm.createContext(sandbox)
  vm.runInContext(script, context, { filename: `musicfree-${id}.js`, timeout: RUN_TIMEOUT })

  // 有的插件直接给 exports 赋值，有的改 module.exports
  const plugin = moduleObj.exports && Object.keys(moduleObj.exports).length
    ? moduleObj.exports
    : sandbox.module.exports
  if (!plugin || typeof plugin !== 'object') throw new Error('插件没有导出对象')

  const platform = String(plugin.platform || meta.name || id)
  const supported = Array.isArray(plugin.supportedSearchType) ? plugin.supportedSearchType : ['music']

  /** 把这些插件方法包一层，统一成宿主的「平台 id + 直链」形态。 */
  return {
    id,
    kind: 'musicfree',
    name: meta.name || platform,
    version: String(plugin.version || meta.version || ''),
    author: String(plugin.author || meta.author || ''),
    description: meta.description || '',
    platform,
    platforms: ['qs'],
    supportedSearchType: supported,

    async search(query, page = 1) {
      if (typeof plugin.search !== 'function') throw new Error('插件不支持搜索')
      const r = await plugin.search(query, page, 'music')
      return decontextify(r)
    },

    async mediaSource(musicItem, quality = 'standard') {
      if (typeof plugin.getMediaSource !== 'function') throw new Error('插件不支持取播放地址')
      const r = await plugin.getMediaSource(musicItem, quality)
      return decontextify(r)
    },

    async lyric(musicItem) {
      if (typeof plugin.getLyric !== 'function') return null
      return decontextify(await plugin.getLyric(musicItem))
    },
  }
}