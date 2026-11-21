/**
 * 音源脚本的出网代理（lx.request）。
 *
 * 音源脚本运行在 vm 沙箱里，拿不到 http/fetch。所有出网必须走这里，
 * 因此这里也是唯一能施加约束的地方：
 *   - 超时（默认 15s，可被脚本 options.timeout 覆盖，上限 60s）
 *   - 响应体上限 5MB（音源接口都是小 JSON，超了必然是异常）
 *   - 方法白名单 GET/POST/HEAD
 *   - 回调签名同时兼容 (err, resp) 与 (err, resp, body)
 *
 * 注意：这里不限制目标域名。音源脚本本身就是「去别的站点取数据」，
 * 限域名会让绝大多数第三方音源直接失效。真正的边界是「只有管理员能装音源」。
 */

const MAX_BODY = 5 * 1024 * 1024
const DEFAULT_TIMEOUT = 15000
const MAX_TIMEOUT = 60000
const ALLOWED_METHODS = new Set(['GET', 'POST', 'HEAD', 'PUT', 'DELETE'])

const DEFAULT_UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'

/**
 * 创建 lx.request 实现。
 * @param {object} opts
 * @param {(msg: string) => void} [opts.log] 调试日志
 */
export function createLxRequest(opts = {}) {
  const log = opts.log || (() => {})

  return function request(url, options, callback) {
    if (typeof options === 'function') {
      callback = options
      options = {}
    }
    options = options || {}
    if (typeof callback !== 'function') throw new Error('lx.request 需要回调函数')

    const controller = new AbortController()
    const optTimeout = Number(options.timeout)
    const timeout =
      Number.isFinite(optTimeout) && optTimeout > 0
        ? Math.min(optTimeout, MAX_TIMEOUT)
        : DEFAULT_TIMEOUT
    const timer = setTimeout(() => controller.abort(), timeout)

    const method = String(options.method || 'GET').toUpperCase()
    const headers = { 'User-Agent': DEFAULT_UA, ...(options.headers || {}) }

    let body
    if (options.form) {
      body = new URLSearchParams(options.form).toString()
      headers['Content-Type'] = 'application/x-www-form-urlencoded'
    } else if (typeof options.body === 'string') {
      body = options.body
    } else if (options.body && typeof options.body === 'object') {
      body = JSON.stringify(options.body)
      headers['Content-Type'] = headers['Content-Type'] || 'application/json'
    }

    const promise = (async () => {
      const resp = await fetch(url, {
        method: ALLOWED_METHODS.has(method) ? method : 'GET',
        headers,
        body: method === 'GET' || method === 'HEAD' ? undefined : body,
        signal: controller.signal,
        redirect: 'follow',
      })

      const rawBuf = Buffer.from(await resp.arrayBuffer())
      if (rawBuf.length > MAX_BODY) {
        throw new Error(`响应体过大（${rawBuf.length} 字节）`)
      }
      const text = rawBuf.toString('utf8')
      let parsed = text
      try {
        parsed = JSON.parse(text)
      } catch {
        /* 非 JSON 就保留字符串 */
      }

      const respHeaders = {}
      resp.headers.forEach((v, k) => {
        respHeaders[k] = v
      })

      return {
        statusCode: resp.status,
        statusMessage: resp.statusText,
        headers: respHeaders,
        body: parsed,
        raw: text,
      }
    })()

    promise
      .then((resp) => {
        clearTimeout(timer)
        // 两种回调签名都喂一遍：老脚本读 resp.body，新脚本读第三个参数
        callback.call(null, null, resp, resp.body)
      })
      .catch((err) => {
        clearTimeout(timer)
        const msg = controller.signal.aborted
          ? new Error(`请求超时（${timeout}ms）`)
          : err instanceof Error
            ? err
            : new Error(String(err))
        log(`request failed: ${url} -> ${msg.message}`)
        callback.call(null, msg, null, null)
      })

    // 返回取消函数（lx 的约定）
    return () => controller.abort()
  }
}