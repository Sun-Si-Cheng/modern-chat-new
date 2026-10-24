# ModernChat

一个**前后端分离、插件优先**的网页聊天平台。开箱只有最朴素的账号 + 纯文本聊天；用户可以自己写 JS 插件并安装，给网站加图片、表情、主题、机器人、小游戏等任意能力。

> 状态：前端（`web/`）与后端（`server/`）均已完成，接口契约见 [docs/后端需求规格.md](docs/后端需求规格.md)，设计见 [docs/设计文档.md](docs/设计文档.md)。

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

### 方式一：Docker 一键起后端（推荐）

```bash
docker compose up -d --build    # MySQL 8 + 后端（:8080），首次启动自动建表
cd web && npm install && npm run dev   # 前端 :5173
```

需要自定义密钥时修改 `docker-compose.yml` 中的 `JWT_SECRET`（如 `openssl rand -hex 32`）。

### 方式二：手动运行

```bash
# 后端（需自备 MySQL 8.0，MIGRATE=false 时用 AutoMigrate 自动建表）
cd server
cp .env.example .env       # DB_DSN + JWT_SECRET（GIN_MODE=release 时必须设置）
go run ./cmd/server        # :8080

# 前端
cd web
npm install
npm run dev                # http://localhost:5173，已代理 /api 与 /ws 到 :8080
```

首个注册的用户自动成为站长（可访问 `/admin`：用户封禁 / 重置密码 / 插件全局下架）。

### 前端检查命令

```bash
npm run type-check # vue-tsc
npm run lint       # oxlint + eslint
npm run build      # 产物在 dist/
```

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

安装后会弹出权限清单，**确认后才启用**；站长可在后台「插件」分区按插件名全局下架（对所有用户立即生效）。

**升级**：再次上传同名插件即为版本升级——复用同一条安装记录（id 不变、启停状态保留）、更新代码与权限，已启用的插件确认后立即重载新代码。

### 试一试示例插件

仓库自带 [`plugins/examples/weather.plugin.js`](plugins/examples/weather.plugin.js)：侧边栏天气面板 + 自定义 `weather` 消息类型。

1. 登录后点击侧栏「插件」图标 → 上传该文件 → 确认权限清单并启用
2. 右侧详情面板出现「天气（示例插件）」卡片：点「刷新数据」看面板更新，「发送到当前聊天」把天气发到当前会话
3. 消息气泡中该消息由插件自定义渲染（白名单组件卡片），而不是裸 JSON
4. 站长账号在 `/admin` → 「插件」分区可看到按名称聚合的安装记录，并执行全局下架

示例刻意不发起网络请求，「接入真实 API」的写法（`net` 权限 + `allowedHosts` + `api.request`）见文件头注释。

---

## 协议概览

- **REST**：前缀 `/api`，除注册 / 登录 / 找回外均需 `Authorization: Bearer <token>`；错误统一 `{ "error": "msg" }`。全量接口见设计文档 §6。
- **WebSocket**：`GET /ws?token=<JWT>`。上行帧 `chat / typing / read / recall / ping`，下行帧 `message / recall / read / typing / presence / conv_change / kicked / pong / error`。消息以 `client_msg_id`（UUID）幂等去重，重连不重复收发。详见设计文档 §7。

## 更新与升级

代码推到 Gitee 后，在部署机上**一条命令**从远端拉取并重建（不用手动上传文件）：

```bash
./scripts/update.sh                          # 拉 master → 构建前后端 → 重启
./scripts/update.sh dev                      # 指定分支
./scripts/update.sh --no-restart             # 只构建产物，稍后自己挑时间重启
RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
```

Windows 本地：`.\scripts\update.ps1`（同样支持 `-Branch` / `-NoRestart`）。

脚本做的事：`git pull --ff-only`（本地有分叉会直接报错，不自动合并）→ `go build`（注入版本号）→ `npm ci && npm run build` → 按 `RESTART_CMD` / docker compose / systemd 依次尝试重启。

**核对是否生效**：`curl http://127.0.0.1:8080/api/version`，或看站长后台右上角的构建信息（`版本 · 提交 · 构建时间`）。

> 为什么没有"后台点一下自动升级"：管理员接口触发 `git pull` + 重启等于给该账号挂上远程代码执行权限，账号一旦泄露等同丢服务器。升级始终由运维脚本执行。

## 部署

- **Docker**：`docker compose up -d --build` 起 MySQL + 后端；前端可构建后由 `deploy/nginx.compose.conf` 的 nginx 容器托管（配置见 `docker-compose.yml` 末尾注释）
- **裸机**：`go build -o modernchat-server ./cmd/server`；前端 `npm run build` 产物交 Nginx，`/api` 反代 `:8080`，`/ws` 加 `Upgrade` / `Connection` 头（`deploy/nginx.conf.example`）
- 生产必须设置：`GIN_MODE=release`、`JWT_SECRET`（≥32 字节，缺省拒绝启动）、`FRONTEND_URL`（WS Origin 白名单 + 找回密码链接）、`MIGRATE=true`
- 配置（DB DSN、JWT_SECRET、SMTP）走环境变量，不进仓库
- 本期为**单实例**架构（在线状态与 Hub 路由在内存），水平扩展列入二期

## 仓库

- 远端：<https://gitee.com/Sun-Si-Cheng/modern-chat-new>（分支 `master`）
- 设计与评审记录：[docs/设计文档.md](docs/设计文档.md)
