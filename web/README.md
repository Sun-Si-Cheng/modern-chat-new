# ModernChat Web

ModernChat 前端（Vue 3 + Vite + TypeScript + Pinia + tdesign-vue-next）。

项目定位、插件开发与部署说明见根目录 [README](../README.md)；完整设计见 [docs/设计文档.md](../docs/设计文档.md)。

## 命令

```sh
npm install        # 安装依赖
npm run dev        # 开发服务器 :5173（/api 与 /ws 代理到 :8080）
npm run build      # 类型检查 + 生产构建
npm run type-check # 仅 vue-tsc 类型检查
npm run lint       # oxlint + eslint
```

## 结构速览

```
src/
├── api/                 # REST 客户端（client.ts 含 401 统一处理）与接口定义
├── stores/              # auth / ws（心跳重连）/ conversations / friends / plugins
├── composables/
│   ├── useWebSocket.ts      # WS 生命周期与上行出口绑定
│   └── usePluginHost.ts     # 插件沙箱宿主（postMessage 桥 + 权限白名单）
├── components/
│   ├── chat/            # 会话列表 / 消息流（虚拟滚动）/ 输入区 / 详情面板
│   └── declarative/     # 描述式 UI 白名单渲染器（8 组件）+ 插件面板
├── types/proto.ts       # WS 帧 / 业务实体 / UI 描述契约
└── views/               # 登录注册 / 找回密码 / ChatLayout / admin
```
