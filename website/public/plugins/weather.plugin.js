/* {
  "name": "weather-demo",
  "version": "1.0.0",
  "description": "示例插件：侧边栏天气面板 + 自定义 weather 消息类型",
  "permissions": ["message:read", "message:send", "message:render", "ui:panel", "storage"],
  "allowedHosts": []
} */

/**
 * ModernChat 示例插件
 *
 * 安装方式：聊天页 → 侧栏「插件」→ 上传本文件 → 确认权限清单 → 启用。
 * 演示的能力：
 *   1. addSidePanel     侧边栏面板（描述式 UI，白名单组件渲染）
 *   2. ui:action        面板按钮动作回传
 *   3. storage          插件独立命名空间存储（记住上次数据）
 *   4. registerMessageRenderer  自定义 weather 消息类型在气泡中渲染
 *   5. api.send         发送自定义类型消息
 *
 * 说明：为了开箱即测，天气数据是模拟的，不发起网络请求。
 * 接入真实 API 时：在 manifest 的 allowedHosts 里放行域名，
 * 补上 "net" 权限后调用 api.request('https://api.example.com/...')。
 */

var currentConvId = 0
var weather = { city: '杭州', sky: '晴', temp: 22 }

function __mcActivate(api) {
  // 恢复上次保存的数据（每插件独立命名空间）
  api.storage.get('last').then(function (raw) {
    if (raw) {
      try {
        weather = JSON.parse(raw)
      } catch (e) {
        /* 数据损坏则用默认值 */
      }
    }
    renderPanel(api)
  })

  // 记录当前打开的会话，用于「发送到当前聊天」
  api.on('conv:open', function (p) {
    if (p && p.convId) currentConvId = p.convId
  })

  // 面板按钮动作
  api.on('ui:action', function (p) {
    if (!p) return

    if (p.action === 'refresh') {
      var skies = ['晴', '多云', '小雨', '阴']
      weather.temp = 18 + Math.floor(Math.random() * 12)
      weather.sky = skies[Math.floor(Math.random() * skies.length)]
      api.storage.set('last', JSON.stringify(weather))
      renderPanel(api)
      return
    }

    if (p.action === 'send') {
      if (!currentConvId) return
      // 对象 payload 会由主站序列化进消息 content，msgType 为 weather
      api.send(currentConvId, weather, 'weather').catch(function () {
        /* 失败静默：也可在此调用 api.addSidePanel 展示错误 */
      })
    }
  })

  // 自定义消息类型渲染：收到的 weather 消息显示为卡片
  api.registerMessageRenderer('weather', function (payload) {
    return {
      title: '',
      body: [
        {
          component: 'card',
          props: {},
          children: [
            { component: 'text', props: { content: '天气 · ' + (payload.city || '未知城市') } },
            { component: 'text', props: { content: (payload.sky || '') + ' ' + (payload.temp || '') + '℃' } }
          ]
        }
      ]
    }
  })

  renderPanel(api)
}

function renderPanel(api) {
  api.addSidePanel({
    title: '天气（示例插件）',
    body: [
      { component: 'text', props: { content: weather.city + ' · ' + weather.sky + ' ' + weather.temp + '℃' } },
      { component: 'divider', props: {} },
      { component: 'button', props: { text: '刷新数据' }, action: 'refresh' },
      { component: 'button', props: { text: '发送到当前聊天' }, action: 'send' }
    ]
  }).catch(function () {
    /* 面板提交失败：忽略 */
  })
}
