# ModernChat 前端技术方案（web/）

> 面向"要改前端"的人：讲清**结构、状态、实时链路、性能约定与易踩的坑**。
> 接口契约见 [后端需求规格.md](后端需求规格.md)，产品与架构取舍见 [设计文档.md](设计文档.md)。

---

## 1. 范围与技术栈

前端是 Vue 3 单页应用，**不依赖任何后端渲染**：所有数据来自 REST（`/api/*`）与一条长连接（`/ws`）。

| 层 | 选型 | 版本 |
|---|---|---|
| 框架 | Vue 3（`<script setup>` + Composition API） | ^3.5.42 |
| 构建 | Vite | ^8.2.2 |
| 语言 | TypeScript | ~6.0.0 |
| 状态 | Pinia | ^4.0.3 |
| 路由 | Vue Router | ^5.3.1 |
| 国际化 | vue-i18n | ^11.4.16 |
| UI | tdesign-vue-next | ^1.20.10 |
| 图标 | tdesign-icons-vue-next | ^0.4.11 |
| 大列表 | vue-virtual-scroller | ^3.0.5 |

设计原则：**内核尽量薄**。除账号 / 消息 / 会话 / 插件运行时外，一切增强（图片消息、表情、Markdown、主题皮肤、机器人）都由插件提供，前端只提供 8 个白名单声明式组件给插件渲染。

---

## 2. 目录结构

```
web/src/
├── main.ts                 # 应用装配：pinia / i18n / router；不全局注册 TDesign（见 §7）
├── App.vue                 # 根组件：RouterView + 全局样式
├── api/
│   ├── client.ts           # fetch 封装：Bearer 注入、错误归一化、401 清 token
│   └── index.ts            # 分域 API：auth/user/conv/friend/plugin/admin
├── stores/                 # Pinia（见 §3）
│   ├── auth.ts             # token + 当前用户 + isAdmin
│   ├── ws.ts               # WebSocket 连接、在线用户集合、收发出口
│   ├── conversations.ts    # 会话列表、消息缓存、未读、筛选、发送与乐观消息
│   ├── friends.ts          # 好友与申请
│   ├── plugins.ts          # 插件清单
│   └── prefs.ts            # 语言 / 主题 / 认证页背景偏好（持久化）
├── composables/
│   ├── useWebSocket.ts     # 连接生命周期：挂载连接、卸载断开、绑定上行出口
│   ├── usePluginHost.ts    # 插件宿主：iframe 沙箱、postMessage 协议、事件总线
│   └── useAppI18n.ts       # t / te（错误消息映射）
├── components/
│   ├── chat/               # AppRail / GroupPane / ConvSidebar / MessagePane /
│   │                       # MessageComposer / DetailPanel / PluginDrawer /
│   │                       # ProfileCard / AddFriendDialog / AvatarBadge
│   ├── common/             # AuthScene（认证页背景场景）/ QuickPrefs（语言·明暗·背景）
│   └── declarative/        # 插件声明式 UI 渲染器（8 个白名单组件）
├── views/                  # Auth / Forgot / Settings / Legal / ChatLayout / admin
├── router/index.ts         # 路由 + 登录态与站长守卫
├── i18n/                   # zh-CN.ts / en-US.ts / errors.ts（服务端中文错误 → 英文）
├── content/legal.ts        # 用户协议 / 隐私协议正文
├── types/proto.ts          # WS 帧与业务实体的类型契约（与后端共用口径）
└── assets/main.css         # 辅助令牌 --mc-* + 工具类 + 动效关键帧
```

---

## 3. 状态管理（Pinia）

| store | 职责 | 关键点 |
|---|---|---|
| `auth` | token、当前用户、是否站长 | token 存 `localStorage.mc_token`，由 `api/client.ts` 统一读写 |
| `ws` | 连接状态、在线用户集合、收发帧 | `send()` 返回 **boolean**：连接不可用时调用方必须把消息标记为失败，而不是静默丢弃 |
| `conversations` | 会话列表、按会话的消息缓存、未读数、发送队列 | 乐观消息 + `clientMsgId` 幂等；`resync()` 用于断线重连后补数据 |
| `friends` | 好友列表、收到的申请、申请计数 | 与 `conversations` 解耦，好友变更单独推送 |
| `plugins` | 插件清单（启用 / 停用 / 全局下架） | 与插件宿主 `usePluginHost` 配合 |
| `prefs` | 语言、主题、认证页背景色与特效 | 只写 `localStorage`，不影响他人 |

**数据流**

```
视图 ──调用──▶ store ──HTTP──▶ api/*  ──▶ 后端
  ▲                              │
  └──── WS 帧 ──▶ ws store ──────┘
```

- 只读数据走 REST，写入走 store 动作；视图不直接 `fetch`
- WS 下行帧统一在 `ws.ts` 分发：`message` / `read` / `recall` / `typing` / `presence` / `conv_change` / `user_change` / `kicked` / `error`

---

## 4. 实时链路（WebSocket）

`composables/useWebSocket.ts` 负责连接生命周期，`stores/ws.ts` 负责状态与帧处理。

- **心跳**：客户端 25s 一次 ping，服务端 pong 超时判定掉线
- **重连**：指数退避（1s 起，上限 30s），`closedByUser` 区分"主动断开"（登出）与"意外断开"
- **重连后必须补数据**：断线期间服务端不会补发帧，因此 `onopen` 时若非首次连接会调用 `conversations.resync()` → 重拉会话列表 + 当前会话最新消息（按 `clientMsgId` 与本地合并去重）
- **断开即清空在线集合**：否则会一直显示过期的"在线"绿点
- **发送失败可见**：`ws.send()` 在连接未就绪时返回 `false`，`conversations.sendText()` 立刻把乐观消息标记为 `failed`，用户可点重发（而不是永远停在"发送中"）

上行帧：`chat` / `read` / `recall` / `typing`（详见后端规格 §3.3）。

---

## 5. 路由与守卫

| 路由 | 访问 | 说明 |
|---|---|---|
| `/login` `/register` | 公开 | 已登录访问会跳回聊天页 |
| `/forgot` | 公开 | 两步：填邮箱 → 填令牌与新密码 |
| `/terms` `/privacy` | 公开 | 协议正文，仅登录页有入口（协议需可直达） |
| `/settings` | 公开 | 未登录仅可改外观；账号区需登录 |
| `/chat` | 需登录 | 主界面 |
| `/admin` | 需站长 | 站长后台 |

守卫在 `router/index.ts`：刷新时先 `auth.fetchMe()` 拿到角色，再判断是否放行 `/admin`（**不依赖 localStorage 里的角色字段**，避免改角色后旧前端仍能进后台）。

---

## 6. 国际化、主题与偏好

**国际化**：`i18n/index.ts` 默认跟随浏览器语言（`zh` 开头 → 中文，其余英文），选择写入 `localStorage.mc_lang`。新增文案必须在 `zh-CN.ts` 与 `en-US.ts` **同时**加键。服务端返回的中文错误由 `i18n/errors.ts` 映射为英文，未收录的原样透传。

**主题**：`stores/prefs.ts` 以 `:root[theme-mode="dark"|"light"]` 驱动 TDesign 令牌，三态：跟随系统 / 亮 / 暗。`index.html` 的内联脚本在样式生效前预置主题与背景色，**避免首屏闪白 / 闪蓝**。

**颜色规范**：一律用令牌，不写死色值。

- `--td-*`：TDesign 原生令牌
- `--mc-text-faint` / `--mc-link` / `--mc-selection` / `--mc-hairline` / `--mc-dur-*` / `--mc-ease-*` / `--mc-scene-color`：项目辅助令牌（见 `assets/main.css`）

---

## 7. 性能约定

| 主题 | 做法 | 现状 |
|---|---|---|
| 组件体积 | **不写 `app.use(TDesign)`**；组件在 SFC 内按需 `import { X as TX } from 'tdesign-vue-next'`，样式全量引入 | TDesign chunk 1,371 kB → 726 kB |
| 分包 | `manualChunks` 只固定框架核心为 `vendor`；TDesign 组件**跟随引用它的路由**分包 | 首屏 gzip 262 kB → **104 kB** |
| 长列表 | 消息流用 `vue-virtual-scroller` 虚拟滚动 | 已在 `MessagePane` |
| 动画 | 只改 `transform` / `opacity`（网格动效用 `background-position`），不触发重排 | 统一在 `main.css` 关键帧 |
| 无障碍 | `prefers-reduced-motion: reduce` 时停掉所有装饰动画 | 认证页背景、消息入场等均已处理 |
| devtools | `vite-plugin-vue-devtools` 仅在 `mode === 'development'` 启用 | 生产包不含 |

> **踩坑提醒**：TDesign 组件并非都支持裸 `v-model`。
> `Drawer` / `Dialog` / `Popup` 只有 `visible` prop，必须写 `v-model:visible="xxx"`；
> 写成 `v-model="xxx"` 时值会被当作**未声明属性**落到 DOM 上（如 `modelvalue="true"`），组件内部收不到，表现为"弹层打不开 / 停在屏幕外"。
> `Input` / `Checkbox` / `Tabs` / `Select` / `InputNumber` 才有 `modelValue`，可直接用 `v-model`。

---

## 8. 组件与代码约定

- 组件文件名 PascalCase，事件用 kebab-case，props 显式声明并给默认值
- 图标从 `tdesign-icons-vue-next` **按名导入**（`import { ChatIcon } from 'tdesign-icons-vue-next'`），不要 `import * as`
- 样式用 `<style scoped>`；全局仅 `assets/main.css`
- 主题相关一律用令牌，避免暗色下出现"重框"（细描边用 `--mc-hairline` 而非 `--td-component-border`）
- 认证页背景（`components/common/AuthScene.vue`）：纯色基色 + 三种纯 CSS 动效（光晕 / 波纹 / 网格），访客可在右上角调色板自选颜色与特效，偏好存 `localStorage`

---

## 9. 插件运行时

`composables/usePluginHost.ts` 是前端最复杂的部分：

- 每个插件一个**沙箱 iframe**（`sandbox="allow-scripts"`），插件代码不能直接碰主页面 DOM
- 通信走 `postMessage`，主页面校验来源与消息结构后才处理
- 插件 UI 用**声明式描述**（`components/declarative/index.ts`）：插件只描述"这里放一个按钮 / 一段文字 / 一张图"，由主页面用白名单组件渲染，插件拿不到任意 HTML
- 卸载插件时同步卸载 iframe、移除事件监听、清理状态，回到纯文本内核

---

## 10. 测试现状与建议

**现状**：前端**暂无自动化测试**（0 个 spec 文件），质量门禁依赖 `vue-tsc` + `oxlint` + `eslint` + 构建。类型检查能挡住大部分静态错误，但挡不住"绑定方向错""状态不同步"这类运行时问题（历史上 `PluginDrawer` 的 `v-model` 绑定问题就是类型检查放行、真机点击才发现的）。

**建议补充的优先级**：

1. `stores/conversations.ts` —— 乐观消息、`clientMsgId` 幂等、帧去重、`markRead` 上报
2. `stores/ws.ts` —— 断线重连与 `resync` 触发条件
3. `components/chat/AvatarBadge.vue` / `ProfileCard.vue` —— 头像兜底与上传分支
4. `i18n` —— 中英键集合一致性（可用脚本比对两个文件的键）

---

## 11. 与后端的契约边界

- **类型**：`web/src/types/proto.ts` 与 `server/internal/model/view.go` 一一对应（字段名、类型、枚举值），改一侧必须改另一侧
- **WS 帧**：`proto.ts` 的 `ClientFrame` / `ServerFrame` 对应后端 `ws/dispatch.go` 的上下行处理
- **错误**：后端返回 `{ error, code? }`，前端 `api/client.ts` 归一化成 `Error`；中文错误在 `i18n/errors.ts` 中映射为英文
- **改接口的流程**：先改 [后端需求规格.md](后端需求规格.md) → 再改后端与 `proto.ts` → 最后改前端

