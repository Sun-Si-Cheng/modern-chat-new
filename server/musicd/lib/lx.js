/**
 * lx 格式音源加载器。
 *
 * 协议（lx-music-desktop / lxserver 通用）：
 *   脚本读 globalThis.lx.{request,on,send,EVENT_NAMES,utils,env,version}
 *   on(EVENT_NAMES.request, handler) 注册「取数据」回调
 *   send(EVENT_NAMES.inited, { sources }) 宣告支持哪些平台
 *
 * 运行时做的事：
 *   1. 建 vm 上下文，注入白名单 API，不注入 require/process/fs
 *   2. 执行脚本，等 send('inited')（最多 3 秒）
 *   3. 之后每次请求都通过 handler({action, source, info}) 拿结果
 */
import vm from 'node:vm'
import crypto from 'node:crypto'
import zlib from 'node:zlib'

import { decontextify } from './decontextify.js'
import { createLxRequest } from './request.js'

const INIT_TIMEOUT = 12000
const RUN_TIMEOUT = 15000

/** 从脚本头部注释解析 @name/@version/@author/@description。 */
export function parseLxMeta(script) {
  const meta = {}
  const head = script.slice(0, 4096)
  const comment = /\/\*[*!]([\s\S]*?)\*\//.exec(head)
  if (!comment) return meta
  for (const line of comment[1].split(/\r?\n/)) {
    const m = /^\s*\*?\s*@(\w+)\s+(.+)$/.exec(line)
    if (m) meta[m[1]] = m[2].trim()
  }
  return meta
}

/**
 * 加载一个 lx 音源。
 * @param {{id:string, script:string, name?:string, log?:Function}} opts
 * @returns {Promise<{id, name, version, author, platforms: string[], handler: Function}>}
 */
export async function loadLxSource(opts) {
  const { id, script } = opts
  const log = opts.log || (() => {})
  const meta = parseLxMeta(script)

  let initResolve
  let initReject
  const initPromise = new Promise((resolve, reject) => {
    initResolve = resolve
    initReject = reject
  })

  const handlers = new Map()
  let registeredSources = {}

  const lxUtils = {
    buffer: {
      from: (d, e) => Buffer.from(d, e),
      bufToString: (b, f) => (Buffer.isBuffer(b) ? b.toString(f) : Buffer.from(b, 'binary').toString(f)),
    },
    crypto: {
      md5: (s) => crypto.createHash('md5').update(String(s ?? '')).digest('hex'),
      aesEncrypt: (buf, mode, key, iv) => {
        const k = Buffer.from(key)
        const cipher = crypto.createCipheriv(`aes-${k.length * 8}-${mode}`, k, Buffer.from(iv))
        return Buffer.concat([cipher.update(Buffer.from(buf)), cipher.final()])
      },
      rsaEncrypt: (buf, key) => crypto.publicEncrypt(String(key), Buffer.from(buf)),
      randomBytes: (n) => crypto.randomBytes(n),
    },
    zlib: {
      inflate: (buf) => zlib.inflateSync(Buffer.from(buf)),
      deflate: (buf) => zlib.deflateSync(Buffer.from(buf)),
    },
  }

  const lx = {
    version: '2.0.0',
    env: 'desktop',
    platform: 'web',
    EVENT_NAMES: { request: 'request', inited: 'inited', updateAlert: 'updateAlert' },
    currentScriptInfo: {
      name: meta.name || id,
      description: meta.description || '',
      version: meta.version || '',
      author: meta.author || '',
      homepage: meta.homepage || '',
      rawScript: script,
    },
    utils: lxUtils,
    request: createLxRequest({ log: (m) => log(`[${id}] ${m}`) }),
    send(eventName, data) {
      const d = decontextify(data)
      if (eventName === 'inited') {
        if (d && d.sources && typeof d.sources === 'object') registeredSources = d.sources
        initResolve()
      } else if (eventName === 'updateAlert') {
        log(`[${id}] 音源提示更新: ${(d && (d.log || d.message)) || ''}`)
      }
    },
    on(eventName, handler) {
      if (typeof handler === 'function') handlers.set(eventName, handler)
    },
  }

  const noop = () => {}
  const sandbox = {
    console: { log: noop, info: noop, warn: noop, error: (...a) => log(`[${id}] ${a.join(' ')}`), debug: noop, trace: noop, table: noop, time: noop, timeEnd: noop, group: noop, groupEnd: noop, dir: noop, assert: noop, count: noop },
    setTimeout,
    clearTimeout,
    setInterval: (fn, ms) => {
      // 音源脚本里的定时器只用来做缓存清理，给个上限防止泄漏
      const t = setInterval(fn, Math.max(Number(ms) || 1000, 1000))
      if (t.unref) t.unref()
      return t
    },
    clearInterval,
    Buffer,
    URL,
    URLSearchParams,
    TextEncoder,
    TextDecoder,
    atob: (s) => Buffer.from(String(s), 'base64').toString('binary'),
    btoa: (s) => Buffer.from(String(s), 'binary').toString('base64'),
    crypto: {
      createHash: (alg) => crypto.createHash(alg),
      createHmac: (alg, key) => crypto.createHmac(alg, key),
      randomBytes: (n) => crypto.randomBytes(n),
      createCipheriv: (...a) => crypto.createCipheriv(...a),
      createDecipheriv: (...a) => crypto.createDecipheriv(...a),
      constants: crypto.constants,
    },
    lx,
    process: { nextTick: (fn, ...a) => setTimeout(() => fn(...a), 0), env: { NODE_ENV: 'production' } },
  }
  sandbox.global = sandbox
  sandbox.window = sandbox
  sandbox.globalThis = sandbox

  const context = vm.createContext(sandbox)
  vm.runInContext(script, context, { filename: `lx-source-${id}.js`, timeout: RUN_TIMEOUT })

  await Promise.race([
    initPromise,
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error('音源初始化超时：脚本没有调用 lx.send("inited", …)')), INIT_TIMEOUT),
    ),
  ])

  const handler = handlers.get('request')
  if (typeof handler !== 'function') throw new Error('音源没有注册 request 处理器')

  const platforms = Object.keys(registeredSources || {})
  if (platforms.length === 0) throw new Error('音源没有声明任何平台（sources 为空）')

  return {
    id,
    kind: 'lx',
    name: meta.name || id,
    version: meta.version || '',
    author: meta.author || '',
    description: meta.description || '',
    platforms,
    sources: registeredSources,
    async call(action, source, info) {
      const result = await handler({ action, source, info })
      return decontextify(result)
    },
  }
}