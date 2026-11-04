/** REST 客户端：统一前缀 /api、Bearer 鉴权、错误归一。 */

const BASE = '/api'
const TOKEN_KEY = 'mc_token'

/**
 * 请求超时。
 * 没有超时的话，代理/扩展/后端任一环节挂住时 fetch 会永久 pending，
 * 页面上的 loading 转圈也就永远不会停。超时后统一抛出可读错误，让调用方的
 * finally 能正常收尾。
 */
const REQUEST_TIMEOUT = 15_000
const UPLOAD_TIMEOUT = 60_000

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setToken(token: string) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  query?: Record<string, string | number | boolean | undefined>
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  if (!query) return BASE + path
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== '') params.set(k, String(v))
  }
  const qs = params.toString()
  return BASE + path + (qs ? `?${qs}` : '')
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const token = getToken()
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), REQUEST_TIMEOUT)
  try {
    const res = await fetch(buildUrl(path, opts.query), {
      method: opts.method ?? 'GET',
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: ctrl.signal,
    })

    // token 失效/被封禁：清登录态回登录页
    if (res.status === 401) {
      setToken('')
      if (!location.pathname.startsWith('/login')) {
        location.href = '/login'
      }
      throw new ApiError(401, '登录已失效，请重新登录')
    }

    const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
    if (!res.ok) {
      throw new ApiError(
        res.status,
        typeof data.error === 'string' ? data.error : `请求失败（${res.status}）`,
      )
    }
    return data as T
  } catch (e) {
    if (ctrl.signal.aborted) throw new ApiError(0, '请求超时，请稍后重试')
    throw e
  } finally {
    clearTimeout(timer)
  }
}

/**
 * multipart 上传。不能复用 request()：那里固定写死 Content-Type: application/json，
 * 而 FormData 必须让浏览器自己补 boundary（手工设会导致后端解析失败）。
 */
async function upload<T>(path: string, form: FormData): Promise<T> {
  const token = getToken()
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), UPLOAD_TIMEOUT)
  try {
    const res = await fetch(BASE + path, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: ctrl.signal,
    })

    if (res.status === 401) {
      setToken('')
      if (!location.pathname.startsWith('/login')) location.href = '/login'
      throw new ApiError(401, '登录已失效，请重新登录')
    }

    const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
    if (!res.ok) {
      throw new ApiError(
        res.status,
        typeof data.error === 'string' ? data.error : `上传失败（${res.status}）`,
      )
    }
    return data as T
  } catch (e) {
    if (ctrl.signal.aborted) throw new ApiError(0, '上传超时，请稍后重试')
    throw e
  } finally {
    clearTimeout(timer)
  }
}

export const http = {
  get: <T>(path: string, query?: RequestOptions['query']) => request<T>(path, { query }),
  upload,
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
