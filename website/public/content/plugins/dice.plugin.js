/* {
  "name": "dice",
  "version": "1.0.0",
  "description": "示例插件：掷骰子面板，把结果以自定义 dice 消息发到当前会话",
  "permissions": ["message:read", "message:send", "message:render", "ui:panel"],
  "allowedHosts": []
} */

/**
 * ModernChat 示例插件：掷骰子
 *
 * 安装方式：聊天页 → 侧栏「插件」→ 上传本文件 → 确认权限清单 → 启用。
 * 演示的能力：
 *   1. addSidePanel            侧边栏面板
 *   2. ui:action               面板按钮动作回传
 *   3. api.send                发送自定义 msgType='dice' 消息
 *   4. registerMessageRenderer dice 消息在气泡内渲染为结果卡片
 */

var currentConvId = 0

function __mcActivate(api) {
  api.on('conv:open', function (p) {
    if (p && p.convId) currentConvId = p.convId
  })

  api.on('ui:action', function (p) {
    if (!p || p.action !== 'roll') return
    if (!currentConvId) return

    var sides = 20
    var value = 1 + Math.floor(Math.random() * sides)
    api.send(currentConvId, { sides: sides, value: value }, 'dice').catch(function () {
      /* 发送失败静默 */
    })
  })

  api.registerMessageRenderer('dice', function (payload) {
    var sides = payload.sides || 20
    var value = payload.value || 0
    var face = value === sides ? '🎉 自然最大！' : value === 1 ? '😅 自然最小…' : '🎲'
    return {
      title: '',
      body: [
        {
          component: 'card',
          props: {},
          children: [
            { component: 'text', props: { content: face + ' 1d' + sides + ' → ' + value } }
          ]
        }
      ]
    }
  })

  renderPanel(api)
}

function renderPanel(api) {
  api.addSidePanel({
    title: '掷骰子（示例插件）',
    body: [
      { component: 'text', props: { content: '在当前会话掷一个 1d20。' } },
      { component: 'divider', props: {} },
      { component: 'button', props: { text: '🎲 掷骰子' }, action: 'roll' }
    ]
  }).catch(function () {
    /* 面板提交失败：忽略 */
  })
}
