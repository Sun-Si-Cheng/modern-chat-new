# ModernChat Web

ModernChat 前端（Vue 3 + Vite + TypeScript + Pinia + Vue Router + UnoCSS + 自研 `Ui*` 控件）。

项目定位、插件开发、升级部署见根目录 [README](../README.md)；完整设计见 [docs/设计文档.md](../docs/设计文档.md)；接口字段见 [docs/后端需求规格.md](../docs/后端需求规格.md)；前端结构/交互约定见 [docs/前端技术方案.md](../docs/前端技术方案.md)。

> 前端需要后端在 `:8080` 才能登录与收发消息（Vite 已代理 `/api` / `/uploads` / `/ws`）。

## 命令

```sh
npm install        # 安装依赖
npm run dev        # 开发服务器 :5173（/api 与 /ws 代理到 :8080）
npm run build      # 类型检查 + 生产构建（dist/），并生成 .gz / .br
npm run type-check # 仅 vue-tsc 类型检查
npm run lint       # oxlint + eslint
npm run format     # prettier 格式化 src/
```

> ⚠️ `lint` 的两个子脚本（`lint:oxlint` / `lint:eslint`）都带 `--fix`，会**直接改写源码**。
> 只想检查不想改文件时，请用 `npx oxlint .` / `npx eslint .`（不带 `--fix`）。

## 结构速览

```text
src/
├── api/
│   ├── client.ts            # fetch 封装：/api 前缀、Bearer、超时、401 清 token 跳登录
│   └── index.ts             # 分域接口（auth/user/friend/conv/sticker/media/favorite/search/
│                            #   moderation/push/invite/plugin/admin/system）
├── i18n/                    # index.ts / zh-CN.ts / en-US.ts / errors.ts（中文错误 → 英文）
├── stores/
│   ├── auth.ts              # token、当前用户、isAdmin
│   ├── ws.ts                # WS 连接、心跳/重连、在线集合、帧分发（含 user_change）
│   ├── conversations.ts     # 会话列表、消息缓存、未读、乐观消息、reply/edit/reaction
│   ├── friends.ts           # 好友与申请
│   ├── favorites.ts         # 收藏 id 集合与抽屉
│   ├── plugins.ts           # 插件清单
│   ├── prefs.ts             # 语言 / 主题 / 认证页背景 / 全站页面背景
│   ├── layout.ts            # 三块区域宽度 / 常驻 / 抽屉形态
│   └── site.ts              # GET /api/site 公开配置（30s 轮询）
├── composables/
│   ├── useWebSocket.ts      # 挂载连接、卸载断开、绑定上行出口
│   ├── usePluginHost.ts     # 插件沙箱宿主：iframe + postMessage 桥 + 权限白名单 + UI 描述校验
│   ├── notifications.ts      # 桌面通知 / Web Push
│   ├── announcement.ts      # 全站公告本地已读位置
│   ├── localCache.ts        # Cache Storage 统计与清理
│   ├── useOverlay.ts        # 浮层焦点圈定 / body 滚动锁
│   └── useAppI18n.ts        # t（文案）+ te（错误翻译）
├── components/
│   ├── ui/                  # 13 个自研控件：UiButton/UiInput/UiTag/UiBadge/UiSelect/UiSlider/
│   │                        #   UiSwitch/UiCheckbox/UiInputNumber/UiTooltip/UiPopup/UiDialog/UiDrawer
│   ├── chat/
│   │   ├── AppRail.vue            # 最左图标栏：头像 / 聊天 / 联系人 / 设置
│   │   ├── GroupPane.vue          # 分组筛选：全部 / 未读 / 单聊 / 群聊 / 归档
│   │   ├── ConvSidebar.vue        # 会话与联系人列表、搜索、右键菜单（置顶/免打扰/归档/清空/删除）
│   │   ├── MessagePane.vue        # 虚拟滚动消息流、引用/转发/@/编辑/撤回/表情/举报/灯箱/回到底部
│   │   ├── MessageComposer.vue    # 自动增高、Enter 发送、输入法保护、typing、@ 选人、图片/语音/视频/表情
│   │   ├── DetailPanel.vue        # 群管理 / 群文件 / 群公告 / 会话内搜索 / 插件侧栏
│   │   ├── PluginDrawer.vue       # 上传/URL 安装、权限确认、启停、卸载、升级重载
│   │   ├── FavoriteDrawer.vue     # 收藏夹
│   │   ├── NotificationDrawer.vue # 通知（好友申请 / 公告）
│   │   ├── AnnouncementDialog.vue # 全站公告弹窗
│   │   ├── VoiceMessage.vue       # 语音气泡（宽度随时长、进度条、同时只播一条）
│   │   ├── ProfileCard.vue        # 个人资料卡：头像上传、设置、退出
│   │   ├── AddFriendDialog.vue    # 添加好友（用户名/昵称搜索、就地同意）
│   │   └── AvatarBadge.vue        # 头像（自定义图 / 默认图 / 首字母兜底）+ 在线小圆点
│   └── common/                    # AuthScene / QuickPrefs / AppBackgroundPicker / PaneResizer / ContextMenu
│   └── declarative/               # 描述式 UI 白名单渲染（11 组件）+ 插件面板
├── types/proto.ts           # WS 帧 / 业务实体 / UI 描述契约（与后端共用口径）
├── views/                   # AuthView/ForgotView/SettingsView/LegalView/JoinView/MaintenanceView/
│                            #   ChatLayout/admin/AdminView
├── router/index.ts          # 路由 + 登录态/站长守卫
└── assets/main.css          # 辅助令牌 --mc-* + 工具类 + 动效关键帧
```

## 已实现能力（前端侧）

- **四栏布局**：图标栏 → 分组栏 → 会话/联系人列表 → 聊天区 + 详情面板；三块区域可拖拽调宽、双击分隔条切换常驻 / 抽屉。
- **消息**：文本 / 图片 / 语音 / 视频 / 表情；引用回复、@ 提醒、转发、合并转发（多条拼成一条文本，≤20 条）、消息编辑（5 分钟内）、撤回（时限由后台配置）、表情回应（默认关闭）、收藏、仅自己删除、举报。
- **会话**：置顶 / 免打扰 / 归档 / 删除 / 清空；草稿保留；`lastMessageType` 驱动列表占位文案。
- **群**：群公告、群文件、群头像、管理员、单人禁言、全员禁言、群邀请链接、拉好友入群、踢人、转让。
- **实时**：心跳 + 指数退避重连；断线恢复 `resync()`；单聊送达 / 已读；多端已读同步；`user_change` 实时同步昵称/头像。
- **体验**：桌面通知、PWA、全站公告弹窗、标签页未读标题、图片压缩 + 缩略图、视频 16:9 内联播放、登录页背景特效、全站页面背景、中英双语、明暗主题。
- **插件**：沙箱 iframe + 描述式 UI + 权限清单 + TypeScript（服务端转译）+ 自定义消息类型 / 右侧面板 / 右键动作。

## 国际化与主题

**语言**：`src/i18n/`，默认跟随浏览器语言（`zh` 开头为中文，其余英文），切换后写入 `localStorage.mc_lang`。
新增文案时在 `zh-CN.ts` / `en-US.ts` 同步加键；服务端返回的中文错误由 `errors.ts` 按消息映射为英文（未收录的透传）。

**主题**：`src/stores/prefs.ts` 以 `:root[theme-mode="dark"|"light"]` 驱动 TDesign 令牌，支持「跟随系统 / 亮 / 暗」三态，持久化在 `localStorage.mc_theme`。
`index.html` 内联脚本在样式生效前预置主题，避免暗色首屏闪白。

自定义颜色一律走令牌：`--td-*` 或 `assets/main.css` 的 `--mc-*`（字号 / 圆角 / 时长 / 缓动 / 阴影 / 材质），不要写死色值。

## 设置页

`/settings` 未登录时仅可改外观；账号与其它分区需登录。

- **外观**：语言、主题、全站页面背景（预设色 / 自定义色 / 本地图片 + 网格 / 圆点 / 斜纹）
- **账号**：昵称、邮箱（换绑需验证码）、退出登录 / 退出所有设备
- **布局**：三块区域宽度、常驻 / 抽屉、恢复默认
- **插件**：上传 / URL 安装 / 启停 / 卸载（回到聊天页 `?panel=plugins` 打开面板）
- **通知**：桌面通知、Web Push（需后端配置 VAPID）
- **本地缓存**：Cache Storage 用量与一键清空（保留登录与偏好）
- **站长**（仅 `isAdmin`）：跳转 `/admin`
- **关于**：当前构建版本

偏好存 `localStorage`（`mc_lang` / `mc_theme` / `mc_layout` / `mc_scene_*` / `mc_app_*`），仅作用于当前浏览器。

## 登录页背景

登录 / 找回密码页共用 `components/common/AuthScene.vue`（`AuthView` / `ForgotView` 挂载；协议页只有 `QuickPrefs`，没有背景层）：

- **基色**：默认 `#123a5c`，由 `--mc-scene-color` 驱动
- **特效**：`aurora` 光晕（默认）/ `waves` 波纹 / `grid` 网格 / `none` 纯色
- **访客可自选**：6 个预设色 + 自定义取色器 + 4 种特效 + 恢复默认，偏好存 `localStorage`
- **无障碍 / 性能**：`prefers-reduced-motion` 下停动画；只用 `transform` / `opacity`（网格用 `background-position`）

## 设计约定

- **UI 以 UnoCSS + 自研 `Ui*` 为主**；`tdesign-vue-next` 仅保留后台表格/页签、认证页表单与少量全局配置。插件 UI 一律走描述式白名单组件。
- 消息正文用 `{{ }}` 文本插值（Vue 自动转义），全项目不使用 `v-html`。
- 数字（时间、未读数、计数）使用 `tabular-nums`。
- 动效只做反馈（hover / focus / 切换），统一使用 `--mc-dur-*` / `--mc-ease-*`，只动 `transform` / `opacity`，并尊重 `prefers-reduced-motion`。
- **UnoCSS 的 `/alpha` 对 CSS 变量无效**：淡色用 `*-soft` / `*-line` 档位。
- **自研按钮必须 `border-none`**：浏览器默认边框在暗色下会成为白圈。
- **布局类不要穿过多根组件**：抽屉的定位 / z-index 由父容器承载（多根组件不会接收父级 class）。
- **图片不能加 `loading="lazy"`**：`DynamicScroller` 依赖测量高度，会导致永远不加载。
- `MessagePane` 的 `min-item-size`（当前 88）接近真实行高，改动气泡 padding / meta 时要同步调整。

## 测试现状

前端暂无自动化测试，质量门禁依赖 `vue-tsc` + `oxlint` + `eslint` + 构建。建议优先补：

1. `stores/conversations.ts`（乐观消息 / `clientMsgId` 幂等 / 帧去重 / markRead）
2. `stores/ws.ts`（重连与 `resync` 触发条件 / `user_change` 分发）
3. `AvatarBadge` / `ProfileCard`（头像兜底与上传分支）
4. i18n 中英键集合一致性（可用脚本比对）
