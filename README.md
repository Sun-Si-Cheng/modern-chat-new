# ModernChat

一个**前后端分离、插件优先**的网页聊天平台。开箱只有最朴素的账号 + 纯文本聊天；用户可以自己写 JS 插件并安装，给网站加图片、表情、主题、机器人、小游戏等任意能力。

> 状态：前端（`web/`）已完成；后端（`server/`）按 [docs/设计文档.md](docs/设计文档.md) 实现中。

---

## 核心理念：内核 vs 插件

**内核（不可禁用）**
- 账号：注册 / 登录 / 找回密码、JWT（含 `token_version` 主动失效）、封禁
- 好友、会话（单聊 / 群聊同构）、纯文本消息收发、消息漫游、WebSocket 管道
- 插件运行时：沙箱加载器、权限模型、事件总线、描述式 UI 渲染器
- 站长后台、插件启停

**插件（可装可卸，卸掉即回到纯文本）**
- 图片消息、表情、Markdown 渲染、暗色主题、提醒音效、自动回复机器人、天气面板、小游戏……全部由插件实现

## 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Vue 3 + Vite + TypeScript + Pinia + Vue Router |
| UI | tdesign-vue-next（TDesign 设计令牌即 CSS 变量，插件可直接换肤） |
| 大列表 | vue-virtual-scroller（消息流虚拟滚动） |
| 实时 | 原生 WebSocket 封装（心跳 25s + 指数退避重连） |
| 后端（实现中） | Go 1.22+ / Gin / gorilla/websocket |
| 数据库（规划） | MySQL 8.0 / GORM，utf8mb4 |

## 目录结构

```
modernchat-new/
├── docs/设计文档.md        # 全量设计（协议、数据库、安全、部署）
├── web/                    # 前端（已完成）
│   └── src/
│       ├── api/            # REST 客户端（/api 前缀，401 统一处理）
│       ├── stores/         # Pinia：auth / ws / conversations / friends / plugins
│       ├── composables/    # useWebSocket、usePluginHost（沙箱宿主）
│       ├── components/
│       │   ├── chat/       # 会话列表 / 消息流 / 输入区 / 详情面板
│       │   └── declarative/# 描述式 UI 白名单渲染器（8 组件）
│       ├── types/proto.ts  # WS 帧与业务契约（前后端共用口径）
│       └── views/          # 登录注册 / 找回密码 / 聊天主界面 / 站长后台
└── server/                 # Go 后端（按设计文档 §4–§10 实现中）
```

## 快速开始

### 前端

```bash
cd web
npm install
npm run dev        # http://localhost:5173，已代理 /api 与 /ws 到 :8080
```

构建与检查：

```bash
npm run type-check # vue-tsc
npm run lint       # oxlint + eslint
npm run build      # 产物在 dist/
```

> 当前仅有前端：打开页面会停在登录页（接口 401 属预期）。后端就绪前可注册首个账号体验完整流程。

### 后端（实现中）

```bash
cd server
cp .env.example .env   # MySQL DSN + JWT_SECRET
go run ./cmd/server    # :8080
```

首个注册的用户自动成为站长（可访问 `/admin` 后台：封禁 / 解封 / 重置密码）。

---

## 插件开发

每个插件是一个 JS 文件（≤1MB），存于服务端，前端以 sandbox iframe（`allow-scripts`，无 same-origin）加载。插件不接触 DOM、拿不到 JWT，只通过 `postMessage` 与主站桥接层通信，UI 一律以**描述式 JSON** 交由白名单组件渲染。

### 入口约定

在插件文件中定义 `__mcActivate(api)`，宿主加载时会注入 `api` 对象：

```js
// my-plugin.js
function __mcActivate(api) {
  // 1. 监听事件（权限 message:read）
  api.on('message:new', (frame) => {
    // frame: { type:'message', convId, from, content, msgType, ... }
  });

  // 2. 侧边栏面板（权限 ui:panel），按钮动作经 'ui:action' 事件回传
  api.addSidePanel({
    title: '天气',
    body: [
      { component: 'text', props: { content: '杭州 晴 22℃' } },
      { component: 'button', props: { text: '刷新' }, action: 'refresh' },
    ],
  });
  api.on('ui:action', ({ action, value }) => {
    if (action === 'refresh') {
      // api.request(...) 拉数据后重新 addSidePanel
    }
  });

  // 3. 注册自定义消息类型的渲染（权限 message:render）
  api.registerMessageRenderer('weather', (payload) => ({
    title: '',
    body: [{ component: 'text', props: { content: `${payload.city} ${payload.sky} ${payload.temp}℃` } }],
  }));
}
```

### SDK 一览

| API | 作用 | 所需权限 |
|---|---|---|
| `api.on(event, fn)` | 订阅 `message:new` / `message:recall` / `presence` / `conv:open` / `ui:action` | `message:read` |
| `api.send(convId, payload, msgType?)` | 发消息；对象自动 JSON 序列化 | `message:send` |
| `api.registerMessageRenderer(type, fn)` | 自定义消息类型 → UI 描述，主站在气泡内渲染 | `message:render` |
| `api.addSidePanel(desc)` | 提交侧边栏面板 UI 描述 | `ui:panel` |
| `api.storage.get(key)` / `set(key, value)` | 每插件独立命名空间存储（单值 ≤64KB） | `storage` |
| `api.request(url, opts?)` | 主站代为 fetch，仅放行 `allowed_hosts` 匹配域名（受浏览器 CORS 约束） | `net` |
| `api.theme.apply(cssVars)` | 修改 `--td-*` 主题令牌，全站即时换肤 | `ui:theme` |

### 白名单组件

UI 描述仅接受 8 个组件：`text / image / button / input / card / list / divider / link`。props 只接受原始值；`link`/`image` 仅放行 `http(s)`；白名单外组件、`javascript:` 链接一律拒绝渲染。

### 安装方式

- **上传文件**：聊天页 → 插件管理 → 上传 JS
- **URL 安装**：服务端拉取前做 SSRF 防护（拒绝私网 / 回环 / 非 http(s)，限大小与超时）

安装后会弹出权限清单，**确认后才启用**；站长可在后台全局下架任意插件（`global_disabled`，对所有用户立即生效）。

---

## 协议概览

- **REST**：前缀 `/api`，除注册 / 登录 / 找回外均需 `Authorization: Bearer <token>`；错误统一 `{ "error": "msg" }`。全量接口见设计文档 §6。
- **WebSocket**：`GET /ws?token=<JWT>`。上行帧 `chat / typing / read / recall / ping`，下行帧 `message / recall / read / typing / presence / conv_change / kicked / pong / error`。消息以 `client_msg_id`（UUID）幂等去重，重连不重复收发。详见设计文档 §7。

## 部署

- 前端 `npm run build` 产物由 Nginx 托管；`/api` 反代到 `:8080`，`/ws` 需加 `Upgrade` / `Connection` 头（示例见 `deploy/nginx.conf.example`，随 server 一并交付）
- 配置（DB DSN、JWT_SECRET、SMTP）走环境变量，不进仓库
- 本期为**单实例**架构（在线状态与 Hub 路由在内存），水平扩展列入二期

## 仓库

- 远端：<https://gitee.com/Sun-Si-Cheng/modern-chat-new>（分支 `master`）
- 设计与评审记录：[docs/设计文档.md](docs/设计文档.md)
