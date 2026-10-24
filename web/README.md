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
├── stores/                  # auth / ws（心跳+重连+帧路由）/ conversations / friends / plugins
├── composables/
│   ├── useWebSocket.ts      # 挂载连接、卸载断开、上行出口绑定
│   └── usePluginHost.ts     # 插件沙箱宿主：srcdoc 注入、postMessage 桥、权限白名单、UI 描述校验
├── components/
│   ├── chat/
│   │   ├── ConvSidebar.vue      # 产品名 + 用户行 + 搜索(人/群) + 会话/联系人
│   │   ├── MessagePane.vue      # 虚拟滚动消息流、已读回执、撤回、插件消息渲染
│   │   ├── MessageComposer.vue  # Enter 发送、typing 节流
│   │   ├── DetailPanel.vue      # 群成员/群管理、单聊资料 + 插件侧栏
│   │   ├── PluginDrawer.vue     # 上传/URL 安装、权限确认、启停、卸载、升级重载
│   │   ├── ProfileDialog.vue    # 昵称/邮箱修改
│   │   └── AvatarBadge.vue      # 取色头像（在线小圆点）
│   └── declarative/
│       ├── index.ts             # 8 个白名单组件的渲染函数（仅原始值 props）
│       └── PluginPanel.vue      # 插件面板 + 动作回传
├── types/proto.ts           # WS 帧 / 业务实体 / UI 描述契约（与后端共用口径）
├── views/
│   ├── AuthView.vue         # 登录/注册（左右分栏）
│   ├── ForgotView.vue       # 找回密码（两步）
│   ├── ChatLayout.vue       # 三栏布局 + 响应式（平板收详情、手机单面板）
│   └── admin/AdminView.vue  # 站长后台：用户管理 / 插件聚合下架 / 构建版本
├── router/index.ts          # 路由 + 登录态/站长守卫
└── assets/main.css          # 全局令牌与工具类（插件通过 --td-* 换肤）
```

## 设计约定

- 单一组件库（TDesign），不混用第二套设计体系；插件 UI 一律走描述式白名单组件
- 消息正文用 `{{ }}` 文本插值（Vue 自动转义），全项目不使用 `v-html`
- 数字（时间、未读数、计数）使用 `tabular-nums`
- 动效只做反馈（hover / focus / 切换），并尊重 `prefers-reduced-motion`
