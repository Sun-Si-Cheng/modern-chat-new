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
| [website/README.md](website/README.md) | 官网结构、命令、文档同步与插件市场 | 改官网 / 文档站 |
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
| 会话 | 单聊幂等复用、群聊（建/搜/加/退，200 人上限）、**初始公共群「公共聊天室」**（首次启动加入全部用户，新注册自动加入，退群后可搜索重新加入）、会话列表（最后消息 + 未读红点 + 分组筛选：全部/未读/单聊/群聊/**归档**）、**归档会话**、**删除会话（仅从自己列表移除）**、**清空聊天记录** |
| 消息 | 文本 / **图片** / **语音** / **视频** 收发、**引用回复**（点引用块跳回原消息并高亮）、**@提醒**（群内 @ 选人、被 @ 高亮、免打扰下仍提示「有人@我」）、**消息搜索**（会话内 + 全局，FULLTEXT ngram）、**编辑**（5 分钟内可改，标「已编辑」）、**表情回应**（默认关闭，`FEATURES.reactions` 可开）、单聊发送状态（发送中 / 已发送 / 已送达 / 已读）、游标分页、**离线按已读游标增量补齐**、撤回（时限后台可配，提示显示 3 分钟后自动从聊天记录移除）、"对方正在输入"、已读回执、在线状态、`client_msg_id` 幂等、断线重连自动补齐数据、发送失败标记与重发、**仅自己删除** |
| 内容治理 | 拉黑用户（双向拦截消息与好友申请）、消息举报（每人每条一次）、管理员 / 群主删除违规消息（各端同步移除）、后台敏感词过滤 |
| 会话体验 | 置顶（跨设备同步）、消息免打扰（红点显示为小圆点）、**多端已读同步**（同账号任意端读取后其他端同步清未读）、未发送草稿保留并在列表标 `[草稿]`；图片右键可查看大图 / 保存 / 收藏；**桌面通知**（页面在后台时提醒，免打扰会话不提示）、**PWA**（可安装到桌面，静态资源离线缓存） |
| 媒体 | 图片发送前 canvas 压缩（最大边 1600 / webp）、点击放大灯箱、服务端生成缩略图；语音录制（点击开始/结束，60s 上限）自定义气泡（宽度随时长、进度条、同时只播一条）；视频选本地文件直接发送（mp4/webm/mov/m4v，原生播放器 16:9 内联播放）；文件按**文件头**判真实类型，默认图片 ≤8 MB、语音 ≤5 MB、视频 ≤30 MB、群文件 ≤20 MB（后台可调） |
| 收藏 | 右键消息收藏 / 取消收藏，收藏夹抽屉按时间倒序、可跳回原会话；空态有引导 |
| 表情 / 转发 | 个人表情库（上传 ≤2 MB、点一下即发、右键删除）；右键消息转发到任意会话（单条，气泡标注「转发」） |
| 群协作 | 群公告（群主发布，聊天区顶部折叠条 + 详情面板编辑/撤销）、群文件（≤20 MB，成员可传，上传者或群主可删）、拉好友入群、**群邀请链接**（任意成员可生成，0–30 天有效，落地页免登录可看群信息） |
| 群管理 | 改群名、**群头像**（群主 / 管理员上传，可恢复默认）、**管理员**（群主可设置 / 取消）、**禁言**（群主与管理员可操作，到期自动解除）、**全员禁言**（群主/管理员仍可发言）、拉好友入群、踢人、转让群主（`owner_id` 与成员角色事务同步；群主须先转让才能退群） |
| 插件 | 上传 / URL 安装（SSRF 防护）、权限清单确认、启停、卸载、同名上传即升级、自定义消息类型渲染、站长全局下架 |
| 界面 | 飞书式四栏（图标栏 / 分组栏 / 会话列表 / 聊天区）、统一设置页（外观 / 账号 / 布局 / 插件 / 站长 / 关于）、**全站公告弹窗提醒**（在线即时弹出，离线期间的公告下次进入聊天页补弹，关闭即标记已读；系统通知会话仅作存储，不在会话列表展示）、登录页背景色与特效（光晕/波纹/网格，访客可自选，本地持久化）、全站页面背景（预设色 / 自定义色 / 本地图片 + 网格 / 圆点 / 斜纹；图片自动压缩后存本机，全站生效；**入口只在设置页「外观」**）、连续消息分组、头像/昵称实时同步、中 / 英文切换、亮 / 暗 / 跟随系统主题 |
| 布局 | 分组栏 / 会话列表 / 详情面板均可**拖拽调宽**、双击分隔条在「常驻 / 抽屉」间切换，宽度与形态跨刷新保持，设置页可一键恢复默认 |
| 站长后台 | 概览统计、用户管理（搜索 / 筛选 / 封禁 / 强制下线 / 重置密码）、插件上下架、**全站公告**、操作审计、**注册邀请码**（批量生成 / 停用 / 原子核销）、系统设置（SMTP / Redis / 对象存储，密钥加密落库不回显；**站点名 / 注册开关 / 维护模式 / 消息长度 / 撤回时限 / 群上限 / 上传体积 / 登录锁定**即时生效） |
| 安全与运营 | **维护模式**（非管理员只见维护页，管理员仍可进后台关闭）、**注册邀请码**（后台批量生成 / 停用，原子核销）、**登录失败锁定**（按账号计数 + 临时锁定，与 IP 限流互补） |
| 邮件 | 找回密码邮件（SMTP，支持 465 隐式 TLS / 587 STARTTLS，QQ / 163 授权码），未配置时链接打印到服务端日志 |
| 运维 | 结构化日志（轮转 + 保留天数）、健康检查 `/api/health`、版本 `/api/version`、Prometheus `/api/metrics`（`METRICS_TOKEN` 可选保护）、三级限流、WS Origin 白名单、数据库备份（后台手动 / 定时）、附件孤儿清理、发行版一键安装与在线更新、覆盖率脚本（单测 + 集成合并统计） |

**本期不做（二期）**：视频通话、合并转发（聊天记录卡片，本期是「多条拼成一条文本」）、群管理员禁言之外的进阶权限、插件市场（本期只做手动安装）、水平扩展（Redis 限流已支持）、图片多选预览发送、语音波形与拖动进度、表情回应默认入口（代码已实现，默认开关关闭）。

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
├── docs/                       # 设计文档 / 后端规格 / 前端方案 / 运维手册 / UI 方案 / 插件开发 / 协议正文
├── deploy/                     # nginx 配置示例（裸机 / compose 两版）
├── website/                    # 官网（Vue 3 + Vite + TDesign）：落地页 / 文档站 / 插件市场
│   ├── public/docs/            # 由仓库文档同步生成的文档站内容（website/scripts/sync-docs.*）
│   └── scripts/sync-docs.*     # 把 README / docs / server / web 文档同步到官网
├── plugins/examples/           # 示例插件（weather / dice / word-count / conv-stats）
├── scripts/                    # install.sh（发行版一键安装）/ package-release.sh（构建机打包）
│                               # update.sh（热更新 / 发行版升级）/ update-release.sh / uninstall.sh / coverage.sh
├── server/                     # Go 后端
│   ├── cmd/server/main.go      # 入口：config → db → hub → router → 优雅停机
│   ├── internal/
│   │   ├── config/             # 环境变量解析与默认值
│   │   ├── version/            # 构建版本（ldflags 注入）
│   │   ├── model/              # GORM 模型 + 响应视图
│   │   ├── store/              # 数据访问 + migrations 执行器 + AES-GCM secretbox + 系统设置
│   │   ├── handler/            # auth / user / friend / conversation / moderation / plugin /
│   │   │                       # admin / admin_settings / upload / file / sticker / push / ops / site
│   │   ├── middleware/         # logger / jwt / ban / admin / limiter / maintenance / guard
│   │   ├── ws/                 # hub(64 分片) / serve(读写 pump) / dispatch / presence
│   │   ├── plugin/             # manifest 校验 / esbuild TS 转译 / URL 拉取（SSRF 防护）
│   │   ├── service/            # mailer / storage(本地+S3) / thumbnail / push / backup / updater
│   │   ├── logx/ metrics/      # 日志轮转 / Prometheus 指标
│   │   └── router/             # 全部路由与中间件装配
│   ├── migrations/             # 0001_init … 0022_message_fulltext（版本化 SQL）
│   ├── test/                   # 集成测试（-tags integration；需 MySQL，详见 server/README）
│   └── Dockerfile
├── web/                        # Vue 前端
│   └── src/
│       ├── api/                # REST 客户端（401 统一处理）
│       ├── stores/             # auth / ws / conversations / friends / favorites / plugins / prefs / layout / site
│       ├── composables/        # useWebSocket、usePluginHost、notifications、announcement、localCache、useOverlay
│       ├── components/
│       │   ├── ui/             # 13 个自研控件（UiButton/UiInput/UiDialog/UiDrawer/…）
│       │   ├── chat/           # 图标栏 / 分组栏 / 会话列表 / 消息流 / 输入区 / 详情面板 /
│       │   │                   # 插件抽屉 / 收藏抽屉 / 通知抽屉 / 语音气泡 / 资料卡 / 头像
│       │   ├── common/         # AuthScene / QuickPrefs / AppBackgroundPicker / PaneResizer / ContextMenu
│       │   └── declarative/    # 描述式 UI 白名单渲染器（11 组件）+ 插件面板
│       ├── types/proto.ts      # WS 帧与业务契约（前后端共用口径）
│       └── views/              # 登录注册 / 找回密码 / 设置 / 协议 / 聊天主界面 / 群邀请 / 维护页 / 站长后台
├── docker-compose.yml          # MySQL + 后端一键环境
└── bin/ dist-release/          # 本地构建产物 / 发行包输出（gitignore）
```

## 快速开始

### 方式零：生产服务器一键安装（宝塔面板风格）

全新 Linux 服务器（root）**一条命令**完成「装环境 → 下载发行版 → 建库 → 部署 → 给出访问地址」：

```bash
curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh | bash
```

脚本自动检测服务器架构（amd64/arm64）与发行版（apt/dnf/yum），下载对应的预编译发行包——服务器**无需 Go / Node / git**，全程不编译。交互问答都有默认值，直接回车即可全自动；也可环境变量预设（`RELEASE_SOURCE=gitee|github` 选源、`RELEASE_TAG` 指定版本、`DOMAIN`/`DB_MODE` 等）。详见 [运维手册 §3.1](docs/运维手册.md)。

发版流程：更新 `CHANGELOG.md` 并同步 `web/`、`website/` 的版本号后，先运行 `sh scripts/check-version.sh` 确认一致（以 CHANGELOG 最新正式版本为基准），再打 tag 并在构建机运行 `scripts/package-release.sh`，把 `dist-release/` 下的 tar.gz 与 .sha256 上传为 Release 附件（Gitee / GitHub 都可；后台图形化更新与安装脚本按 `RELEASE_SOURCE` 自动下载新版本）。

### 方式一：Docker 一键起后端（本地开发）

```bash
docker compose up -d --build           # MySQL 8 + 后端（:8080），首次启动自动建表
cd web && npm install && npm run dev   # 前端 :5173
```

需要自定义密钥时修改 `docker-compose.yml` 中的 `JWT_SECRET`（如 `openssl rand -hex 32`）。

镜像构建时通过 build args 注入 `VERSION` / `COMMIT` / `BUILD_TIME`（`/api/version` 可查）；不传则回退 `dev` / `unknown`，构建上下文没有 `.git` 也不会失败。带版本构建并核对：

```bash
VERSION=$(git describe --tags --always) COMMIT=$(git rev-parse --short HEAD) \
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) docker compose up -d --build
curl http://127.0.0.1:8080/api/version
# 直接构建镜像：docker build --build-arg VERSION=v0.10.3 -t modernchat-server ./server
```

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
- **邮箱用途**：注册邮箱验证（默认开启，可后台关闭；未配 SMTP 自动降级）、找回密码、换绑邮箱验证。首个注册账号豁免验证码但仍必须填邮箱。
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
- **WebSocket**：`GET /ws?token=<JWT>`（Origin 白名单校验）。上行帧 `chat / typing / read / delivered / recall / edit / reaction / ping`，下行帧 `message / message_update / recall / reaction / message_delivered / message_admin_deleted / read / typing / presence / user_change / conv_change / kicked / pong / error`。消息以 `clientMsgId` 幂等去重，单帧上行上限 64 KB。详见 [设计文档 §7](docs/设计文档.md)。
- **版本**：`GET /api/version` → `{version, commit, buildTime}`，用于核对升级是否生效。

## 更新与升级

在部署机上**一条命令**完成升级；脚本会自动识别部署形态：

```bash
./scripts/update.sh                          # 发行版：下载最新 Release；源码：git pull + 编译；两者都热更新
./scripts/update.sh dev                      # 源码模式指定分支
./scripts/update.sh --no-restart             # 只准备产物，稍后自己挑时间切换
./scripts/update.sh --cold                   # 强制旧式重启（有秒级中断）
RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
```

- **发行版部署（install.sh）**：从 `RELEASE_API_URL` 查最新 tag → 下载对应 tar.gz + `.sha256` 校验 → 原子替换二进制 / 前端产物 → 优雅重启（蓝绿双实例，在线用户自动重连并补齐消息）。
- **源码部署（git clone）**：`git pull --ff-only`（本地有分叉会直接报错，不自动合并）→ `go build`（注入版本号）→ `npm ci && npm run build` → 蓝绿切换。

Windows 本地开发不走升级脚本：拉取后直接重新构建运行即可。

**核对是否生效**：`curl http://127.0.0.1:8080/api/version`，或看站长后台右上角的构建信息（`版本 · 提交 · 构建时间`）。

> **两种升级方式**：
> - **图形化（推荐日常使用）**：站长后台 → 在线更新页签，一键升级 + 自动重启。发行版安装（install.sh）走 Release 下载（sha256 校验，无需本机编译）；源码部署走 git pull + 本地编译。安全边界：仅站长可用、目标版本来自 Release 服务端 / origin/main（不接收用户输入）、操作写审计、仅 install.sh 的 systemd 部署可用、`WEB_UPDATE=false` 可整体关闭。风险与脚本升级本质相同（站长账号可触发代码更新），请保管好管理员账号。
> - **脚本（推荐生产 / 无人值守）**：安装目录里的 `scripts/update.sh`（发行版模式：下载新 Release 校验替换后重启；源码模式：git pull + 编译），详见运维手册 §4。

## 部署

- **一键安装**：`scripts/install.sh`（宝塔面板风格：环境自检补装 → 下载发行版（无需 Go / Node / git）→ 建库 → systemd + nginx → 输出站点地址，详见运维手册 §3.1）
- **Docker**：`docker compose up -d --build` 起 MySQL + 后端；前端构建后可由 `deploy/nginx.compose.conf` 的 nginx 容器托管（用法见 `docker-compose.yml` 末尾注释）
- **裸机**：`go build -o modernchat-server ./cmd/server`；前端 `npm run build` 产物交 Nginx，`/api` 反代 `:8080`，`/ws` 加 `Upgrade` / `Connection` 头（`deploy/nginx.conf.example`）
- 生产必须设置：`GIN_MODE=release`、`JWT_SECRET`（≥32 字节，缺省拒绝启动）、`FRONTEND_URL`（WS Origin 白名单 + 找回密码链接）、`MIGRATE=true`
- 配置（DB DSN、JWT_SECRET、SMTP）走环境变量，不进仓库
- 本期为**单实例**架构（在线状态与 Hub 路由在内存），水平扩展列入二期

## 仓库

- 远端：<https://gitee.com/Sun-Si-Cheng/modern-chat-new>（分支 `main`）
- 提交约定：每完成一个模块 `git add -A` → commit → push

## 许可证

本项目采用 [Apache License 2.0](LICENSE) 授权，Copyright 2026 Sun-Si-Cheng。

在遵守许可证的前提下可自由使用、修改与分发（含商业用途）；再分发时需保留版权与许可声明，修改过的文件需注明变更。完整条款见 [LICENSE](LICENSE)。

## 协议与合规

站内提供[用户协议](/terms)与[隐私协议](/privacy)页面（中英双语，登录页与个人资料弹窗均有入口），正文维护在 [`web/src/content/legal.ts`](web/src/content/legal.ts)。

自建实例对外运营前，务必替换 `legalMeta` 中的运营主体、联系邮箱与域名，并参阅 [docs/legal/README.md](docs/legal/README.md)（含协议条款与实现代码的对应表、不同法域的合规提示）。
