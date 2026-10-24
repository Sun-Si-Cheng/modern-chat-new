# ModernChat Web

ModernChat 前端（Vue 3 + Vite + TypeScript + Pinia + tdesign-vue-next）。

项目定位、插件开发、升级部署见根目录 [README](../README.md)；完整设计见 [docs/设计文档.md](../docs/设计文档.md)；接口字段见 [docs/后端需求规格.md](../docs/后端需求规格.md)。

> 前端需要后端在 `:8080` 才能登录与收发消息（Vite 已代理 `/api` 与 `/ws`）。

## 命令

```sh
npm install        # 安装依赖
npm run dev        # 开发服务器 :5173（/api 与 /ws 代理到 :8080）
npm run build      # 类型检查 + 生产构建（产物 dist/）
npm run type-check # 仅 vue-tsc 类型检查
npm run lint       # oxlint + eslint
```

## 结构速览

```
src/
├── api/
│   ├── client.ts            # fetch 封装：/api 前缀、Bearer、401 统一清 token 跳登录
│   └── index.ts             # 按域划分的接口（auth/user/friend/conv/plugin/admin/system）
├── i18n/
│   ├── index.ts             # createI18n（默认跟随浏览器语言，localStorage 持久化）
│   ├── zh-CN.ts / en-US.ts  # 全量 UI 文案
│   └── errors.ts            # 服务端中文错误 → 当前语言的映射
├── stores/                  # auth / ws / conversations / friends / plugins / prefs（语言+主题）
├── composables/
│   ├── useWebSocket.ts      # 挂载连接、卸载断开、上行出口绑定
│   ├── useAppI18n.ts        # t（文案）+ te（错误翻译）
│   └── usePluginHost.ts     # 插件沙箱宿主：srcdoc 注入、postMessage 桥、权限白名单、UI 描述校验
├── components/
│   ├── common/
│   │   └── QuickPrefs.vue       # 认证页快捷切换：语言 + 明暗 + 登录页背景(颜色/特效)
│   ├── chat/
│   │   ├── AppRail.vue          # 最左图标栏：头像(资料卡) + 新建菜单 + 消息/联系人/插件/后台 + 设置
│   │   ├── GroupPane.vue        # 分组筛选：全部 / 未读 / 单聊 / 群聊（计数按会话数）
│   │   ├── ConvSidebar.vue      # 会话与联系人列表：搜索(人/群)、在线状态、好友申请处理
│   │   ├── AddFriendDialog.vue  # 添加好友：用户名/昵称搜索、发送申请、已是好友/已申请、就地同意
│   │   ├── MessagePane.vue      # 虚拟滚动消息流、5 分钟消息分组、已读/撤回、回到底部、插件消息渲染
│   │   ├── MessageComposer.vue  # 1-6 行自动增高、Enter 发送 / Shift+Enter 换行、输入法保护、typing 节流
│   │   ├── DetailPanel.vue      # 群名/人数与群管理、单聊资料 + 插件侧栏
│   │   ├── PluginDrawer.vue     # 上传/URL 安装、权限确认、启停、卸载、升级重载
│   │   ├── ProfileCard.vue      # 个人资料卡：头像上传(hover 相机)、设置/退出/后台
│   │   └── AvatarBadge.vue      # 头像(自定义图/默认卡通图/首字母兜底) + 在线小圆点
│   └── declarative/
│       ├── index.ts             # 8 个白名单组件的渲染函数（仅原始值 props）
│       └── PluginPanel.vue      # 插件面板 + 动作回传
├── types/proto.ts           # WS 帧 / 业务实体 / UI 描述契约（与后端共用口径）
├── views/
│   ├── AuthView.vue         # 登录/注册（沉浸背景 + 居中卡片）
│   ├── ForgotView.vue       # 找回密码（两步 + 进度指示）
│   ├── SettingsView.vue     # 设置：外观（语言/主题/背景）/ 账号 / 关于
│   ├── LegalView.vue        # 用户协议 / 隐私协议
│   ├── ChatLayout.vue       # 飞书式四栏布局 + 响应式（平板收详情、手机单面板）
│   └── admin/AdminView.vue  # 站长后台：用户管理 / 插件聚合下架 / 构建版本
├── router/index.ts          # 路由 + 登录态/站长守卫
└── assets/main.css          # 辅助令牌（--mc-*）+ 全局工具类
```

## 聊天界面

四栏布局：图标栏（56px）→ 分组栏（148px）→ 会话/联系人列表 → 聊天区，右侧为当前会话的详情面板。平板（≤1024px）隐藏分组栏与详情面板，手机（≤768px）列表与聊天区互斥显示。

- **消息流**：同一发送者 5 分钟内的连续消息合并为一组，头像和昵称只在组首出现；时间与已读回执只在组尾展示，单聊已读只标在最新一条自己发出的消息上；群聊消息使用成员接口返回的真实头像与昵称；撤回、重发收进消息下方的元信息行，悬停或键盘聚焦时出现；离开底部时显示「回到最新消息」按钮
- **输入区**：1–6 行自动增高；Enter 发送、Shift+Enter 换行；中文输入法组合期间按 Enter 不会误发送；输入面板整块聚焦，字数接近上限时提示
- **联系人**：联系人栏顶部、新建菜单、空会话页均可打开「添加好友」弹窗，按用户名或昵称搜索；已是好友显示状态，对方已发来申请时可直接同意，发送成功显示「已申请」
- **空状态**：无会话时展示产品名、定位语与「添加好友 / 插件管理」两个入口

## 国际化与主题

**语言**：`src/i18n/`，默认跟随浏览器语言（`zh` 开头为中文，其余英文），切换后写入 `localStorage.mc_lang`。
新增文案时在 `zh-CN.ts` / `en-US.ts` 同步加键；服务端返回的中文错误由 `errors.ts` 按消息映射为英文（未收录的原样透传）。

**主题**：`src/stores/prefs.ts` 以 `:root[theme-mode="dark"|"light"]` 驱动 TDesign 令牌，支持「跟随系统 / 亮 / 暗」三态，持久化在 `localStorage.mc_theme`。
`index.html` 内联脚本在样式生效前预置主题，避免暗色首屏闪白。
自定义颜色一律走令牌：`--td-*`（TDesign）或 `--mc-text-faint` / `--mc-link` / `--mc-selection`（本项目辅助），不要写死色值。

## 设置页

`/settings` 是登录后的设置入口（聊天页图标栏底部齿轮、站长后台头部）：

- **外观**：语言（中/英）、主题（跟随系统/亮/暗）
- **账号**（需登录）：昵称、邮箱、退出登录
- **关于**：当前构建版本

> 登录 / 注册 / 找回密码 / 协议页右上角有三个快捷按钮：语言、明暗、背景（颜色 + 特效）。它们与设置页共享同一份偏好（`localStorage`），背景只作用于认证页（`AuthScene.vue`），聊天界面不受影响。

偏好存 `localStorage`（`mc_lang` / `mc_theme` / `mc_scene_color` / `mc_scene_effect`），仅作用于当前浏览器。

## 登录页背景（颜色与特效）

登录 / 找回密码页共用 `components/common/AuthScene.vue`：

- **基色**：默认 `#123a5c`（深蓝），由 CSS 变量 `--mc-scene-color` 驱动
- **特效**（三种，纯 CSS，无图片、无外部依赖）：
  - `aurora` 光晕：三团柔光（`filter: blur` 的实心圆）缓慢漂移，默认
  - `waves` 波纹：同心圆环由内向外扩散
  - `grid` 网格：极淡网格缓慢流动 + 中央光晕呼吸
  - `none` 纯色
- **访客可自选**：右上角调色板 → 6 个预设色 + 自定义取色器 + 4 种特效 + 恢复默认；偏好存 `localStorage`（`mc_scene_color` / `mc_scene_effect`），只影响本人浏览器
- **改默认值**：`stores/prefs.ts` 里的 `DEFAULT_SCENE_COLOR` 与 `detectSceneEffect()` 的返回值
- **无障碍**：`prefers-reduced-motion` 下自动停止所有动画
- **性能**：动画只用 `transform` / `opacity`（`grid` 用 `background-position`），不触发重排

### 用图片替换

把图片放到 `web/public/login-bg.jpg` 即自动覆盖基色（构建会复制到站点根目录），并在其上叠加按主题变化的暗化层，保证卡片可读。建议 ≥1920×1080 横图、主体避开画面正中（卡片会盖住中间约 420×360 区域）。用 `.png` / `.webp` 时改 `AuthScene.vue` 里 `.auth-scene__img` 的 `url()` 一行。

## 设计约定

- 单一组件库（TDesign），不混用第二套设计体系；插件 UI 一律走描述式白名单组件
- 消息正文用 `{{ }}` 文本插值（Vue 自动转义），全项目不使用 `v-html`
- 数字（时间、未读数、计数）使用 `tabular-nums`
- 动效只做反馈（hover / focus / 切换），并尊重 `prefers-reduced-motion`
