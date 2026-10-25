/* {
  "name": "word-count",
  "version": "1.0.0",
  "description": "示例插件：统计监听到的消息数与字数，侧边栏实时展示",
  "permissions": ["message:read", "ui:panel", "storage"],
  "allowedHosts": []
} */

/**
 * ModernChat 示例插件：会话统计
 *
 * 安装方式：聊天页 → 侧栏「插件」→ 上传本文件 → 确认权限清单 → 启用。
 * 演示的能力：
 *   1. api.on('message:new')  监听消息事件（不读正文之外的数据）
 *   2. storage                计数持久化（刷新页面后保留）
 *   3. addSidePanel + ui:action  面板展示与清零按钮
 *
 * 说明：仅统计插件启用之后收到的新消息（message:new 为增量事件，
 * 不回放历史消息），这正是权限模型的最小可见面。
 */

var stats = { count: 0, chars: 0 }

function __mcActivate(api) {
  api.storage.get('stats').then(function (raw) {
    if (raw) {
      try {
        stats = JSON.parse(raw)
      } catch (e) {
        /* 数据损坏则从零开始 */
      }
    }
    renderPanel(api)
  })

  api.on('message:new', function (frame) {
    if (!frame) return
    var text = typeof frame.content === 'string' ? frame.content : ''
    stats.count += 1
    stats.chars += text.length
    api.storage.set('stats', JSON.stringify(stats))
    renderPanel(api)
  })

  api.on('ui:action', function (p) {
    if (!p || p.action !== 'reset') return
    stats = { count: 0, chars: 0 }
    api.storage.set('stats', JSON.stringify(stats))
    renderPanel(api)
  })

  renderPanel(api)
}

function renderPanel(api) {
  api.addSidePanel({
    title: '会话统计（示例插件）',
    body: [
      { component: 'text', props: { content: '启用后收到消息：' + stats.count + ' 条' } },
      { component: 'text', props: { content: '累计字数：' + stats.chars + ' 字' } },
      { component: 'divider', props: {} },
      { component: 'button', props: { text: '清零' }, action: 'reset' }
    ]
  }).catch(function () {
    /* 面板提交失败：忽略 */
  })
}
