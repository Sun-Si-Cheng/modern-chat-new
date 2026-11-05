# ModernChat

一个**前后端分离、插件优先**的网页聊天平台。开箱即可用：账号、好友、单聊/群聊、**文本 + 图片 + 语音消息**；用户还可以自己写 JS 插件并安装，给网站加表情、主题、机器人、小游戏等任意能力。

> 状态：前端（`web/`）与后端（`server/`）均已实现并通过测试，可直接运行。

---

## 文档索引

| 文档 | 内容 | 什么时候看 |
|---|---|---|
| [README.md](README.md) | 定位、快速开始、插件开发、升级部署 | 第一次接触项目 |
| [docs/设计文档.md](docs/设计文档.md) | 产品定位、架构、数据库、协议、插件系统、性能边界 | 想理解设计取舍 |
| [docs/后端需求规格.md](docs/后端需求规格.md) | 接口契约（请求/响应字段）、校验规则、错误码、验收标准 | 改接口 / 做联调 / 验收 |
| [docs/前端技术方案.md](docs/前端技术方案.md) | 前端结构、状态管理、WS 实时链路、性能约定、易踩的坑 | 改前端 |
| [docs/运维手册.md](docs/运维手册.md) | 环境变量全表、后台可配置项、对象存储、部署、备份升级、日志、排障 FAQ | 部署与运维 |
| [docs/UI设计方案.md](docs/UI设计方案.md) | 设计令牌、动效规范、组件手感、验收标准与实施记录 | 改样式 / 加动效 / 新增控件 |
| [server/README.md](server/README.md) | 后端运行手册（配置、迁移、测试、结构） | 部署与运维后端 |
| [web/README.md](web/README.md) | 前端结构速览与命令 | 改前端 |
| [docs/legal/README.md](docs/legal/README.md) | 用户协议 / 隐私协议的维护方式与合规提示 | 部署上线前替换运营主体信息 |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更记录 | 了解某个功能何时加入、修了什么 |

## 核心理念：内核 vs 插件

**内核（不可禁用）**
- 账号：注册 / 登录 / 找回密码、JWT（含 `token_version` 主动失效）、封禁
- 好友、会话（单聊 / 群聊同构）、纯文本消息收发、消息漫游、WebSocket 管道
- 插件运行时：沙箱加载器、权限模型、事件总线、描述式 UI 渲染器
- 站长后台、插件启停 / 全局下架

**插件（可装可卸，卸掉即回到「文本 + 图片 + 语音」）**
- 表情、Markdown 渲染、提醒音效、自动回复机器人、天气面板、小游戏……全部由插件实现

> 图片与语音属于 IM 基本盘，因此放在内核；插件专注增强与玩法，而不是补齐基础能力。

## 已实现功能

| 模块 | 能力 |
|---|---|
| 账号 | 注册（首注册者=站长）、登录、找回/重置密码（`token_version` 全端失效）、改昵称/邮箱、**自定义头像**（前端裁切压缩至 256px，data URL 存储，全站显示）、封禁强制下线 |
| 好友 | 按用户名/昵称搜索添加、申请/同意/拒绝/删除（拒绝后可重新申请） |
| 会话 | 单聊幂等复用、群聊（建/搜/加/退，200 人上限）、**初始公共群「公共聊天室」**（首次启动加入全部用户，新注册自动加入，退群后可搜索重新加入）、会话列表（最后消息 + 未读红点 + 分组筛选：全部/未读/单聊/群聊） |
| 消息 | 文本 / **图片** / **语音** 收发、**引用回复**（点引用块跳回原消息并高亮）、**@提醒**（群内 @ 选人、被 @ 高亮、免打扰下仍提示「有人@我」）、游标分页、2 分钟撤回（提示显示 3 分钟后自动从聊天记录移除）、"对方正在输入"、已读回执、在线状态、`client_msg_id` 幂等、断线重连自动补齐数据、发送失败标记与重发 |
| 会话体验 | 置顶（跨设备同步）、消息免打扰（红点显示为小圆点）、未发送草稿保留并在列表标 `[草稿]`；图片右键可查看大图 / 保存 / 收藏 |
| 媒体 | 图片发送前 canvas 压缩（最大边 1600 / webp）、点击放大灯箱；语音录制（点击开始/结束，60s 上限）自定义气泡（宽度随时长、进度条、同时只播一条）；文件按**文件头**判真实类型，图片 ≤8 MB、语音 ≤5 MB |
| 收藏 | 右键消息收藏 / 取消收藏，收藏夹抽屉按时间倒序、可跳回原会话；空态有引导 |
| 表情 / 转发 | 个人表情库（上传 ≤2 MB、点一下即发、右键删除）；右键消息转发到任意会话（单条，气泡标注「转发」） |
| 群协作 | 群公告（群主发布，聊天区顶部折叠条 + 详情面板编辑/撤销）、群文件（≤20 MB，成员可传，上传者或群主可删） |
| 群管理 | 改群名、踢人、转让群主（`owner_id` 与成员角色事务同步；群主须先转让才能退群） |
| 插件 | 上传 / URL 安装（SSRF 防护）、权限清单确认、启停、卸载、同名上传即升级、自定义消息类型渲染、站长全局下架 |
| 界面 | 飞书式四栏（图标栏 / 分组栏 / 会话列表 / 聊天区）、统一设置页（外观 / 账号 / 布局 / 插件 / 站长 / 关于）、**全站公告弹窗提醒**（在线即时弹出，离线期间的公告下次进入聊天页补弹，关闭即标记已读；系统通知会话仅作存储，不在会话列表展示）、登录页背景色与特效（光晕/波纹/网格，访客可自选，本地持久化）、全站页面背景（预设色 / 自定义色 / 本地图片 + 网格 / 圆点 / 斜纹；图片自动压缩后存本机，全站生效；**入口只在设置页「外观」**）、连续消息分组、头像/昵称实时同步、中 / 英文切换、亮 / 暗 / 跟随系统主题 |
| 布局 | 分组栏 / 会话列表 / 详情面板均可**拖拽调宽**、双击分隔条在「常驻 / 抽屉」间切换，宽度与形态跨刷新保持，设置页可一键恢复默认 |
| 站长后台 | 概览统计、用户管理（搜索 / 筛选 / 封禁 / 强制下线 / 重置密码）、插件上下架、**全站公告**、操作审计、系统设置（SMTP / Redis / 对象存储，密钥加密落库不回显；**站点名 / 注册开关 / 消息长度 / 撤回时限 / 群上限 / 上传体积**即时生效） |
| 邮件 | 找回密码邮件（SMTP，支持 465 隐式 TLS / 587 STARTTLS，QQ / 163 授权码），未配置时链接打印到服务端日志 |
| 运维 | 结构化日志（轮转 + 保留天数）、健康检查 `/api/health`、版本 `/api/version`、三级限流、WS Origin 白名单、一键从 Gitee 升级、覆盖率脚本（单测 + 集成合并统计） |

**本期不做（二期）**：视频 / 视频通话、合并转发（聊天记录卡片）、消息搜索/编辑、群管理员与禁言、插件市场、多端已读同步、水平扩展（Redis 限流已支持）、图片多选预览发送、语音波形与拖动进度。

## 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Vue 3 + Vite + TypeScript + Pinia + Vue Router |
| UI | **UnoCSS 原子类 + 自研控件层 `components/ui/Ui*`**（13 个组件）；`tdesign-vue-next` 仅剩后台表格、页签与认证页表单。颜色/圆角/字号/动效全部走 `--td-*` 与 `--mc-*` 令牌，插件可直接换肤 |
| 大列表 | vue-virtual-scroller（消息流虚拟滚动） |
| 存储 | 本地磁盘（`DATA_DIR/uploads`）或 **S3 兼容对象存储**（MinIO / AWS S3 / OSS / COS），后台可切 |
| 实时 | 原生 WebSocket 封装（心跳 25s + 指数退避重连） |
| 后端 | Go（版本以 `server/go.mod` 为准，依赖链要求 1.26+）/ Gin / gorilla/websocket |
| 数据库 | MySQL 8.0 / GORM，utf8mb4；生产走 `migrations/` 版本化 SQL |

## 目录结构

```
modernchat-new/
├── docs/                       # 设计文档 / 后端规格 / 前端方案 / 运维手册 / 协议正文
├── deploy/                     # nginx 配置示例（裸机 / compose 两版）
├── website/                    # 官网（Vue 3 + Vite + TDesign）：落地页 / 文档站 / 插件市场
├── plugins/examples/           # 示例插件（可直接上传试用）
├── scripts/                    # update.sh / update.ps1（Gitee 升级）+ coverage.sh / coverage.ps1（覆盖率）
├── server/                     # Go 后端
│   ├── cmd/server/main.go      # 入口：config → db → hub → router → 优雅停机
│   ├── internal/
│   │   ├── config/             # 环境变量解析与默认值
│   │   ├── version/            # 构建版本（ldflags 注入）
│   │   ├── model/              # GORM 模型 + 响应视图
│   │   ├── store/              # 数据访问（含 migrations 执行器）
│   │   ├── handler/            # auth / user / friend / conversation / plugin / admin
│   │   ├── middleware/         # logger / jwt / ban / admin / limiter
│   │   ├── ws/                 # hub(64 分片) / serve(读写 pump) / dispatch / presence
│   │   ├── plugin/             # manifest 校验 + URL 拉取（SSRF 防护）
│   │   └── service/            # mailer（SMTP：465 隐式 TLS / 587 STARTTLS）
│   ├── migrations/             # 0001_init + 0002_user_avatar（用户自定义头像）
│   ├── test/                   # 集成测试（-tags integration；需 MySQL，详见 server/README）
│   └── Dockerfile
├── web/                        # Vue 前端
│   └── src/
│       ├── api/                # REST 客户端（401 统一处理）
│       ├── stores/             # auth / ws / conversations / friends / plugins / prefs
│       ├── composables/        # useWebSocket、usePluginHost（沙箱宿主）
│       ├── components/
│       │   ├── chat/           # 图标栏(AppRail) / 分组栏(GroupPane) / 会话列表 / 消息流 / 输入区 / 详情面板 / 插件抽屉 / 资料卡 / 头像
│       │   ├── common/         # AuthScene（认证页背景场景）/ QuickPrefs（认证页快捷切换）
│       │   └── declarative/    # 描述式 UI 白名单渲染器（8 组件）+ 插件面板
│       ├── types/proto.ts      # WS 帧与业务契约（前后端共用口径）
│       └── views/              # 登录注册 / 找回密码 / 设置 / 协议 / 聊天主界面 / 站长后台
└── docker-compose.yml          # MySQL + 后端一键环境
```

## 快速开始

### 方式一：Docker 一键起后端（推荐）

```bash
docker compose up -d --build           # MySQL 8 + 后端（:8080），首次启动自动建表
cd web && npm install && npm run dev   # 前端 :5173
```

需要自定义密钥时修改 `docker-compose.yml` 中的 `JWT_SECRET`（如 `openssl rand -hex 32`）。

### 方式二：手动运行

```bash
# 后端（需自备 MySQL 8.0；MIGRATE=false 时用 AutoMigrate 自动建表）
cd server
cp .env.example .env       # DB_DSN + JWT_SECRET（GIN_MODE=release 时必须设置）
go run ./cmd/server        # :8080

# 前端
cd web
npm install
npm run dev                # http://localhost:5173，已代理 /api 与 /ws 到 :8080
```

首个注册的用户自动成为站长（可访问 `/admin`：用户封禁 / 重置密码 / 插件全局下架）。

### 检查命令

```bash
# 前端
cd web && npm run type-check && npm run lint && npm run build

# 后端
cd server && go build ./... && go vet ./... && go test ./...
cd server && go test -tags integration ./test/   # 需要本机 MySQL
```

---

## 产品与运营说明（仅文档，不展示在界面）

以下规则写给部署者与开发者，界面文案保持面向用户，不出现内部规则或开发期说明：

- **首个注册账号自动成为站长**（`role=admin`），之后注册的均为普通用户。站长后台提供用户封禁 / 解封 / 随机重置密码、插件全局下架。
- **忘记用户名**：系统不提供用户名找回。站长可在后台为该用户重置密码，再线下告知。
- **找回密码**：走注册邮箱。未配置 SMTP 时，重置链接只打印到服务端日志（见 `server/README.md` 的「日志」一节），需运维从日志取值转交用户。界面统一提示"若该邮箱已注册，重置链接将发送至该邮箱"，不暴露邮箱是否存在。
- **邮箱仅用于找回密码**：本版本没有邮箱验证流程，`email_verified` 始终为 `false`；修改邮箱会在数据层重置该标记。
- **升级**：部署机执行 `scripts/update.sh`（详见「更新与升级」），界面只显示当前构建版本，不显示运维命令。

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
  api.on('ui:action', ({ action }) => {
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
| `api.request(url, opts?)` | 主站代为 fetch，仅放行 `allowedHosts` 匹配域名（受浏览器 CORS 约束） | `net` |
| `api.theme.apply(cssVars)` | 修改 `--td-*` 主题令牌，全站即时换肤 | `ui:theme` |

### Manifest

写在代码**首个块注释**里，服务端解析校验（名称/版本/权限/白名单不合法则拒绝安装）：

```js
/* {
  "name": "weather-demo",
  "version": "1.0.0",
  "description": "示例插件：侧边栏天气面板",
  "permissions": ["message:read", "message:send", "message:render", "ui:panel", "storage"],
  "allowedHosts": ["api.example.com", "*.openweathermap.org"]
} */
```

### 白名单组件

UI 描述仅接受 8 个组件：`text / image / button / input / card / list / divider / link`。props 只接受原始值；`link`/`image` 仅放行 `http(s)`；白名单外组件、`javascript:` 链接一律拒绝渲染。

### 安装 / 升级

- **上传文件**：聊天页 → 插件管理 → 上传 JS
- **URL 安装**：服务端拉取前做 SSRF 防护（拒绝私网 / 回环 / 非 http(s)，限大小与超时）
- 安装后弹出权限清单，**确认后才启用**
- **升级**：再次上传同名插件即版本升级——复用同一条记录（id 不变、启停状态保留），已启用的插件确认后立即重载新代码
- 站长可在 `/admin` → 「插件」按插件名**全局下架**（对所有用户立即生效）

### 试一试示例插件

仓库自带三个示例插件（在官网「插件市场」页也可直接下载）：

- [`plugins/examples/weather.plugin.js`](plugins/examples/weather.plugin.js)：侧边栏天气面板 + 自定义 `weather` 消息类型
- [`plugins/examples/dice.plugin.js`](plugins/examples/dice.plugin.js)：掷骰子面板 + 自定义 `dice` 消息类型
- [`plugins/examples/word-count.plugin.js`](plugins/examples/word-count.plugin.js)：会话消息统计面板（`message:new` 事件 + storage 持久化）

1. 登录后点击侧栏「插件」图标 → 上传该文件 → 确认权限清单并启用
2. 右侧详情面板出现「天气（示例插件）」卡片：点「刷新数据」看面板更新，「发送到当前聊天」把天气发到当前会话
3. 消息气泡中该消息由插件自定义渲染（白名单组件卡片），而不是裸 JSON
4. 站长账号在 `/admin` → 「插件」分区可看到按名称聚合的安装记录，并执行全局下架

示例刻意不发起网络请求；接入真实 API 的写法（`net` 权限 + `allowedHosts` + `api.request`）见文件头注释。

---

## 协议概览

- **REST**：前缀 `/api`，除注册/登录/找回/重置/健康检查/版本外均需 `Authorization: Bearer <token>`；错误统一 `{ "error": "msg" }`。全量接口与字段见 [后端需求规格 §3](docs/后端需求规格.md)。
- **WebSocket**：`GET /ws?token=<JWT>`（Origin 白名单校验）。上行帧 `chat / typing / read / recall / ping`，下行帧 `message / recall / read / typing / presence / conv_change / kicked / pong / error`。消息以 `clientMsgId` 幂等去重。详见 [设计文档 §7](docs/设计文档.md)。
- **版本**：`GET /api/version` → `{version, commit, buildTime}`，用于核对升级是否生效。

## 更新与升级

代码推到 Gitee 后，在部署机上**一条命令**从远端拉取并重建（不用手动上传文件）：

```bash
./scripts/update.sh                          # 拉 master → 构建前后端 → 重启
./scripts/update.sh dev                      # 指定分支
./scripts/update.sh --no-restart             # 只构建产物，稍后自己挑时间重启
RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
```

Windows 本地：`.\scripts\update.ps1`（支持 `-Branch` / `-NoRestart`）。

脚本做的事：`git pull --ff-only`（本地有分叉会直接报错，不自动合并）→ `go build`（注入版本号）→ `npm ci && npm run build` → 按 `RESTART_CMD` / docker compose / systemd 依次尝试重启。

**核对是否生效**：`curl http://127.0.0.1:8080/api/version`，或看站长后台右上角的构建信息（`版本 · 提交 · 构建时间`）。

> 为什么没有"后台点一下自动升级"：管理员接口触发 `git pull` + 重启等于给该账号挂上远程代码执行权限，账号一旦泄露等同丢服务器。升级始终由运维脚本执行。

## 部署

- **Docker**：`docker compose up -d --build` 起 MySQL + 后端；前端构建后可由 `deploy/nginx.compose.conf` 的 nginx 容器托管（用法见 `docker-compose.yml` 末尾注释）
- **裸机**：`go build -o modernchat-server ./cmd/server`；前端 `npm run build` 产物交 Nginx，`/api` 反代 `:8080`，`/ws` 加 `Upgrade` / `Connection` 头（`deploy/nginx.conf.example`）
- 生产必须设置：`GIN_MODE=release`、`JWT_SECRET`（≥32 字节，缺省拒绝启动）、`FRONTEND_URL`（WS Origin 白名单 + 找回密码链接）、`MIGRATE=true`
- 配置（DB DSN、JWT_SECRET、SMTP）走环境变量，不进仓库
- 本期为**单实例**架构（在线状态与 Hub 路由在内存），水平扩展列入二期

## 仓库

- 远端：<https://gitee.com/Sun-Si-Cheng/modern-chat-new>（分支 `master`）
- 提交约定：每完成一个模块 `git add -A` → commit → push

## 许可证

本项目采用 [Apache License 2.0](LICENSE) 授权，Copyright 2026 Sun-Si-Cheng。

在遵守许可证的前提下可自由使用、修改与分发（含商业用途）；再分发时需保留版权与许可声明，修改过的文件需注明变更。完整条款见 [LICENSE](LICENSE)。

## 协议与合规

站内提供[用户协议](/terms)与[隐私协议](/privacy)页面（中英双语，登录页与个人资料弹窗均有入口），正文维护在 [`web/src/content/legal.ts`](web/src/content/legal.ts)。

自建实例对外运营前，务必替换 `legalMeta` 中的运营主体、联系邮箱与域名，并参阅 [docs/legal/README.md](docs/legal/README.md)（含协议条款与实现代码的对应表、不同法域的合规提示）。
