/**
 * 全站公告的本地已读位置。
 * 公告以系统会话（type=3）最后一条消息的形式存在，前端用最后一条消息 id
 * 判断是否有新公告：在线时由 WS 帧触发，离线期间产生的公告在下次拉会话列表时补弹。
 */
export const ANNOUNCEMENT_SEEN_KEY = 'mc_last_announcement_id'

export function lastSeenAnnouncementId(): number {
  const raw = localStorage.getItem(ANNOUNCEMENT_SEEN_KEY)
  const n = raw ? Number(raw) : 0
  return Number.isFinite(n) ? n : 0
}

export function markAnnouncementSeen(id: number) {
  if (!Number.isFinite(id) || id <= 0) return
  localStorage.setItem(ANNOUNCEMENT_SEEN_KEY, String(id))
}
