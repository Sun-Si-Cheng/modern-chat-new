/**
 * 播放器账号联动 API（经聊天室后端代理）。
 *
 * 三种状态由后端判定：
 *   bound   —— 已绑定，直接用
 *   found   —— 播放器里已有同用户名/邮箱账号，提示输入密码完成绑定
 *   missing —— 播放器里没有该账号，提示去 player 登录或新建
 */

export type LinkState = 'bound' | 'found' | 'missing'

export interface LinkPlayer {
  username: string
  is_admin?: boolean
}

export interface ProbeResult {
  ok: boolean
  state: LinkState
  player?: LinkPlayer | null
  suggest_username?: string
}

class LinkErr extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const token = localStorage.getItem('mc_token') ?? ''
  const res = await fetch(`/api/music/link/${path}`, {
    ...init,
    headers: {
      ...(init?.headers as Record<string, string>),
      Authorization: `Bearer ${token}`,
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
    },
  })
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    throw new LinkErr(res.status, '响应格式错误')
  }
  if (!res.ok) {
    throw new LinkErr(res.status, (data as { error?: string })?.error ?? `HTTP ${res.status}`)
  }
  return data as T
}

/** 查当前聊天室用户的播放器绑定情况 */
export async function probeLink(): Promise<ProbeResult> {
  return await call<ProbeResult>('probe')
}

/** 用已有播放器账号 + 密码完成绑定 */
export async function bindPlayer(identifier: string, password: string): Promise<ProbeResult> {
  return await call<ProbeResult>('bind', {
    method: 'POST',
    body: JSON.stringify({ identifier, password }),
  })
}

/** 取一次性会话票据（前端拿它去 player 域换 session）。
 *  未绑定时后端返回 409 not_bound —— 这不是错误，只是"还没有票据"，
 *  这里吞掉并返回空票据，避免控制台刷 409 红字。 */
export async function getLinkTicket(): Promise<{ ok: boolean; ticket: string; exp: number }> {
  try {
    return await call<{ ok: boolean; ticket: string; exp: number }>('ticket')
  } catch (e) {
    if (e instanceof LinkErr && (e.status === 409 || e.status === 404)) {
      return { ok: false, ticket: '', exp: 0 }
    }
    throw e
  }
}

/** 兑换票据，在播放器域建立会话（直接跨域调播放器的 chat_session.php） */
export async function redeemTicket(ticket: string): Promise<boolean> {
  try {
    const res = await fetch(
      `https://player.modern-chat.top/api/chat_session.php?ticket=${encodeURIComponent(ticket)}`,
      { credentials: 'include' },
    )
    if (!res.ok) return false
    // 会话建立后记录播放器身份，供前端做"管理员看隐藏 / 只看自己的私有歌单"的轻量判断
    const data = (await res.json().catch(() => null)) as
      | { user?: { id?: number; is_admin?: boolean; is_system?: boolean } }
      | null
    const u = data?.user
    if (u) {
      try {
        localStorage.setItem('mc_player_uid', String(u.id ?? 0))
        localStorage.setItem('mc_player_admin', u.is_admin || u.is_system ? '1' : '0')
      } catch {
        /* 隐私模式忽略 */
      }
    }
    return true
  } catch {
    return false
  }
}

/** 一步到位：拿到票据并在播放器域建立会话 */
export async function ensurePlayerSession(): Promise<boolean> {
  try {
    const { ticket } = await getLinkTicket()
    if (!ticket) return false
    return await redeemTicket(ticket)
  } catch {
    return false
  }
}
