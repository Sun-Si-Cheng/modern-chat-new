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

/**
 * 401 处理钩子：由 auth store 注册，用来同步清掉 Pinia 里的 token/user。
 * 只清 localStorage 会让内存中的 store 仍是「已登录」，路由守卫与页面状态会失真。
 */
let unauthorizedHandler: (() => void) | null = null

export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  query?: Record<string, string | number | boolean | undefined>
}

/**
 * 401 统一回登录页，并带上来源：
 * 不带 redirect 的话，会话中途过期后用户只能落到 /chat，
 * 正在看的 /join/:token、/settings 就回不去了。
 */
function redirectToLogin() {
  const p = location.pathname
  if (p.startsWith('/login')) return
  const from = p + location.search
  location.href = from === '/chat' ? '/login' : `/login?redirect=${encodeURIComponent(from)}`
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

/**
 * 强制超时包装。
 * 只用 AbortController 不够：第三方扩展若包装了 window.fetch 却没有把 signal
 * 透传下去，abort 不会让它的 Promise 结算，页面 loading 就会永久转圈。
 * 这里用 Promise.race 保证调用方一定在超时时间内拿到结果，并顺手 abort 清理底层请求。
 */
function withTimeout<T>(
  task: Promise<T>,
  ms: number,
  message: string,
  ctrl: AbortController,
): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      ctrl.abort()
      reject(new ApiError(0, message))
    }, ms)
  })
  return Promise.race([task, timeout]).finally(() => {
    if (timer) clearTimeout(timer)
  })
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const token = getToken()
  const ctrl = new AbortController()
  const run = async (): Promise<T> => {
    let res: Response
    try {
      res = await fetch(buildUrl(path, opts.query), {
        method: opts.method ?? 'GET',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        signal: ctrl.signal,
      })
    } catch (e) {
      // fetch 只在网络层失败时抛 TypeError("Failed to fetch")：包装成可读中文，
      // 否则 te() 会把英文原文直接弹给中文用户
      const aborted = (e as Error)?.name === 'AbortError'
      throw new ApiError(0, aborted ? '请求超时，请稍后重试' : '网络连接失败，请检查网络后重试')
    }

    // token 失效/被封禁：清登录态回登录页
    if (res.status === 401) {
      setToken('')
      unauthorizedHandler?.()
      redirectToLogin()
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
  }
  return withTimeout(run(), REQUEST_TIMEOUT, '请求超时，请稍后重试', ctrl)
}

/**
 * multipart 上传。不能复用 request()：那里固定写死 Content-Type: application/json，
 * 而 FormData 必须让浏览器自己补 boundary（手工设会导致后端解析失败）。
 */
async function upload<T>(path: string, form: FormData): Promise<T> {
  const token = getToken()
  const ctrl = new AbortController()
  const run = async (): Promise<T> => {
    let res: Response
    try {
      res = await fetch(BASE + path, {
        method: 'POST',
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        body: form,
        signal: ctrl.signal,
      })
    } catch (e) {
      const aborted = (e as Error)?.name === 'AbortError'
      throw new ApiError(0, aborted ? '上传超时，请稍后重试' : '网络连接失败，请检查网络后重试')
    }

    if (res.status === 401) {
      setToken('')
      unauthorizedHandler?.()
      redirectToLogin()
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
  }
  return withTimeout(run(), UPLOAD_TIMEOUT, '上传超时，请稍后重试', ctrl)
}

/**
 * 非 JSON 封装的请求（插件代码下载、octet-stream 上传、按 URL 安装）。
 * 这些路径原先直接裸调 fetch：没有超时（代理挂住时永久 pending），
 * 也没有 401 处理。这里与 request() 共用超时/鉴权/401 逻辑。
 */
export async function rawFetch(
  path: string,
  init: RequestInit = {},
  timeoutMs = REQUEST_TIMEOUT,
): Promise<Response> {
  const token = getToken()
  const ctrl = new AbortController()
  const run = async (): Promise<Response> => {
    let res: Response
    try {
      const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
      const extra = init.headers as Record<string, string> | undefined
      if (extra) Object.assign(headers, extra)
      res = await fetch(path, {
        ...init,
        headers,
        signal: ctrl.signal,
      })
    } catch (e) {
      const aborted = (e as Error)?.name === 'AbortError'
      throw new ApiError(0, aborted ? '请求超时，请稍后重试' : '网络连接失败，请检查网络后重试')
    }
    if (res.status === 401) {
      setToken('')
      unauthorizedHandler?.()
      redirectToLogin()
      throw new ApiError(401, '登录已失效，请重新登录')
    }
    return res
  }
  return withTimeout(run(), timeoutMs, '请求超时，请稍后重试', ctrl)
}

export const http = {
  get: <T>(path: string, query?: RequestOptions['query']) => request<T>(path, { query }),
  upload,
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
