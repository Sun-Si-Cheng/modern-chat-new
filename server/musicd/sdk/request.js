/**
 * lx 内置搜索 SDK 的出网层（原 lxserver 的 src/modules/utils/request.js 精简版）。
 *
 * 原实现依赖 needle + tunnel + @renderer 别名，这里改成原生 fetch：
 *   - httpFetch(url, options) 返回 { promise, cancelHttp }，与 lx 的调用方式一致
 *   - 自动把 JSON 解析成对象（调用方普遍直接读 resp.body.xxx）
 *   - 默认 15s 超时，5MB 上限
 *
 * 为什么保留 cancelHttp：lx 的 musicSearch 内部有重试逻辑，出错时会再发一次，
 * 旧实现里通过它中止上一次请求，这里保持同样的接口以免调用方报错。
 */

const DEFAULT_TIMEOUT = 15000
const MAX_BODY = 5 * 1024 * 1024

const DEFAULT_UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'

async function doFetch(url, method, options) {
  const controller = new AbortController()
  const timeout = Number(options.timeout) > 0 ? Number(options.timeout) : DEFAULT_TIMEOUT
  const timer = setTimeout(() => controller.abort(), timeout)

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

  try {
    const resp = await fetch(url, {
      method: (method || 'get').toUpperCase(),
      headers,
      body: body,
      signal: controller.signal,
      redirect: 'follow',
    })
    const buf = Buffer.from(await resp.arrayBuffer())
    if (buf.length > MAX_BODY) throw new Error(`响应体过大（${buf.length}）`)
    const text = buf.toString('utf8')
    let parsed = text
    try {
      parsed = JSON.parse(text)
    } catch {
      /* 保留字符串 */
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
  } finally {
    clearTimeout(timer)
  }
}

export const httpFetch = (url, options = { method: 'get' }) => {
  const obj = {
    isCancelled: false,
    requestObj: null,
    promise: null,
    cancelFn: null,
    cancelHttp: () => {},
  }
  obj.cancelHttp = () => {
    obj.isCancelled = true
    if (obj.requestObj && obj.requestObj.abort) obj.requestObj.abort()
  }
  obj.promise = doFetch(url, options.method, options)
  return obj
}

export const http = (url, options, cb) => {
  if (typeof options === 'function') {
    cb = options
    options = {}
  }
  const o = { ...(options || {}), method: options?.method || 'get' }
  doFetch(url, o.method, o).then(
    (resp) => cb(null, resp, resp.body),
    (err) => cb(err, null, null),
  )
}

export const httpGet = http

export const httpPost = (url, data, options, cb) => {
  if (typeof options === 'function') {
    cb = options
    options = {}
  }
  const o = { ...(options || {}), method: 'post', body: data }
  doFetch(url, 'post', o).then(
    (resp) => cb(null, resp, resp.body),
    (err) => cb(err, null, null),
  )
}