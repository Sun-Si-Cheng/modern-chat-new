/// <reference path="../modernchat-sdk.d.ts" />
/* {
  "name": "conv-stats",
  "version": "1.0.0",
  "description": "示例（TypeScript）：当前会话消息统计 + 消息右键菜单动作，演示 conv:read / ui:actions 与 table/progress/badge 组件",
  "permissions": ["ui:panel", "conv:read", "ui:actions", "ui:host"],
  "allowedHosts": []
} */

/**
 * TypeScript 插件示例
 *
 * 安装：聊天页 → 插件 → 上传本文件（.ts 直接上传，服务端用 esbuild 去类型后入库，
 * 无需本地构建）。启用后：
 *   1. 详情面板侧边栏出现「本会话统计」面板（table + progress + badge）
 *   2. 任意消息右键 → 「统计到这条为止」，统计到该消息为止的数据
 *
 * 演示的 API：
 *   conv.current / conv.members / messages.recent  只读数据
 *   registerMessageAction + on('message:action')   右键菜单扩展点
 *   addSidePanel                                   描述式 UI
 *   toast                                          轻提示
 */

interface Stats {
  total: number
  mine: number
  textLen: number
  types: Record<string, number>
  top: Array<{ name: string; count: number }>
}

function emptyStats(): Stats {
  return { total: 0, mine: 0, textLen: 0, types: {}, top: [] }
}

function computeStats(msgs: McMessageLite[], meId: number, names: Map<number, string>): Stats {
  const s = emptyStats()
  const perSender = new Map<number, number>()
  for (const m of msgs) {
    if (m.recalled) continue
    s.total += 1
    if (m.senderId === meId) s.mine += 1
    if (m.msgType === 'text') s.textLen += (m.content || '').length
    s.types[m.msgType] = (s.types[m.msgType] ?? 0) + 1
    perSender.set(m.senderId, (perSender.get(m.senderId) ?? 0) + 1)
  }
  s.top = [...perSender.entries()]
    .map(([id, count]) => ({ name: names.get(id) ?? `用户 ${id}`, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5)
  return s
}

function render(api: McApi, stats: Stats, note: string): void {
  const rows = stats.top.map((t) => `["${t.name}","${t.count}"]`).join(',')
  const typeRows = Object.entries(stats.types)
    .map(([type, n]) => `["${type}","${n}"]`)
    .join(',')

  const desc: McPanelDesc = {
    title: '本会话统计',
    body: [
      { component: 'badge', props: { text: note, tone: 'brand' } },
      { component: 'text', props: { content: `共 ${stats.total} 条 · 我发言 ${stats.mine} 条` } },
      {
        component: 'progress',
        props: {
          label: '我的发言占比',
          value: stats.total ? Math.round((stats.mine / stats.total) * 100) : 0,
        },
      },
      { component: 'divider', props: {} },
      { component: 'text', props: { content: '发言排行' } },
      {
        component: 'table',
        props: { headers: '成员,条数', rows: `[${rows}]` },
      },
      { component: 'text', props: { content: '消息类型分布' } },
      {
        component: 'table',
        props: { headers: '类型,条数', rows: `[${typeRows}]` },
      },
      {
        component: 'button',
        props: { text: '刷新', theme: 'primary' },
        action: 'refresh',
      },
    ],
  }
  void api.addSidePanel(desc)
}

async function runStats(api: McApi, upToId = 0): Promise<void> {
  const conv = await api.conv.current()
  if (!conv) {
    void api.toast('先打开一个会话', 'warning')
    return
  }
  const [me, msgs, members] = await Promise.all([
    api.me(),
    api.messages.recent(50),
    api.conv.members(),
  ])
  const names = new Map<number, string>(members.map((m) => [m.userId, m.nickname]))
  const sliced = upToId ? msgs.filter((m) => m.id <= upToId) : msgs
  const stats = computeStats(sliced, me?.id ?? 0, names)
  render(api, stats, upToId ? `统计到消息 #${upToId}` : `最近 ${sliced.length} 条`)
}

function __mcActivate(api: McApi): void {
  // 右键菜单扩展点：任意消息右键多出这一项
  void api.registerMessageAction({ id: 'conv-stats', label: '统计到这条为止' })

  api.on('message:action', (payload) => {
    const p = payload as { actionId?: string; message?: McActionMessage }
    if (p.actionId === 'conv-stats') void runStats(api, p.message?.id ?? 0)
  })

  api.on('ui:action', (payload) => {
    const p = payload as { action?: string }
    if (p.action === 'refresh') void runStats(api)
  })

  void runStats(api)
}
