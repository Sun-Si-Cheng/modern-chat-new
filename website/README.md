# ModernChat 官网（website/）

项目官网：落地页 / 文档站 / 插件市场。**Vue 3 + Vite + TypeScript + TDesign**，构建产物是纯静态文件，可托管在任意静态服务器（Nginx / Caddy / GitHub Pages / 对象存储）。

## 命令

```bash
cd website
npm install
npm run dev        # http://localhost:5174
npm run build      # 产物在 website/dist
npm run preview    # 预览构建产物
npm run type-check # vue-tsc 类型检查
```

## 页面

| 路由 | 说明 |
|---|---|
| `/` | 落地页：Hero 交互演示（时间轴播放：纯文本 → 插件消息卡片 → 插件切换暗色主题）、在线演示（可交互的迷你聊天，纯前端模拟）、功能特性、插件系统（安全模型 + 代码示例 + SDK 表）、快速开始（Docker / 手动 / 验证三 Tab + 技术栈） |
| `/docs/:file?` | 文档站：左侧分组导航，渲染 `public/docs/` 下的 Markdown，上一页 / 下一页；英文模式读 `public/docs/en/`，缺失时回退中文并提示 |
| `/plugins` | 插件市场：搜索 + 分类 + 卡片（权限标签、下载 JS、源码链接）+ 三步安装指引 |

## 结构

```
website/
├── index.html                  # Vite 入口（含 favicon 与 meta）
├── vite.config.ts              # base './'、别名 @ → src、dev 端口 5174
├── tsconfig.json
├── public/                     # 原样拷贝到 dist（不做打包）
│   ├── docs/                   # 中文文档副本（由 scripts/sync-docs 生成）
│   │   └── en/                 # 英文译本（人工维护）
│   └── plugins/                # 可下载的示例插件 JS
├── scripts/
│   ├── sync-docs.ps1           # 从仓库根同步文档到 public/docs（Windows）
│   └── sync-docs.sh            # 同上（Linux / macOS）
└── src/
    ├── main.ts                 # 装配 router / i18n / 全局样式
    ├── App.vue                 # 导航（含语言切换）+ RouterView + 页脚
    ├── router.ts               # history 路由（首页锚点需 history 模式）
    ├── i18n/
    │   ├── messages.clean.json # 121 条中英文案（自旧版 main.js 的字典提取）
    │   ├── lists.json          # 演示用的语料数组（昵称、机器人回复）
    │   └── index.ts            # vue-i18n 实例 + 语言持久化（mc_site_lang）
    ├── components/
    │   └── DemoChat.vue        # Hero 里的时间轴聊天演示
    ├── views/
    │   ├── HomeView.vue        # 落地页（Hero / 在线演示 / 特性 / 插件 / 快速开始）
    │   ├── DocsView.vue        # 文档站（marked 渲染 + 侧边栏 + 上下篇）
    │   └── PluginsView.vue     # 插件市场
    └── styles/
        └── site.css            # 站点样式（沿用原静态站视觉）
```

## 文档同步

文档真源在仓库根（`README.md`、`docs/*.md`、`server/README.md`、`web/README.md`），官网只放副本：

```bash
# 在仓库根执行
.\website\scripts\sync-docs.ps1     # Windows
./website/scripts/sync-docs.sh      # Linux / macOS
```

脚本会把中文源覆盖到 `website/public/docs/`，并检查 `public/docs/en/` 的英文译本是否落后于中文源（落后只提示，不覆盖——英文需人工修订）。

## 部署

```bash
cd website && npm run build
# 把 website/dist 交给 nginx
```

nginx 需要为 history 路由加 fallback（否则刷新 `/docs` 会 404）：

```nginx
location / {
    root /var/www/modernchat-site;
    try_files $uri $uri/ /index.html;
}
```

## 与主站视觉保持一致

官网与产品界面（`web/`）共用同一套设计语言，改样式时请遵守：

| 项 | 约定 |
|---|---|
| 颜色 | 一律走令牌：`src/styles/site.css` 顶部的 `--brand` / `--ink` / `--ink-2` / `--ink-3` / `--line` / `--bg` / `--bg-alt` 已全部映射到 TDesign 的 `--td-*`，**不要在组件里写死色值** |
| 主题 | 与主站共用 `localStorage.mc_theme` 与 `<html theme-mode>`：支持亮 / 暗 / 跟随系统，导航右上角可切换；在应用里选过暗色的用户，打开官网也是暗色 |
| 字体 | `'PingFang SC', 'Microsoft YaHei', 'Helvetica Neue', Arial`（与主站一致） |
| 圆角 | 卡片 `12px`（`--radius`）、控件 `8px`（`--radius-sm`） |
| 表面 | 纯色 + 1px 细描边，**不用大面积渐变**（主站也是纯色；渐变在暗色下会与文字色冲突） |
| 代码块 | 固定深色（`--code-bg`），不随主题变化 |
| 图标 | `public/favicon.ico` 是 `web/public/favicon.ico` 的副本，**换图标时两处一起换**（`copy web/public/favicon.ico website/public/favicon.ico`）；导航栏的品牌块仍用内联 SVG（图标 ico 是白底，直接放深色导航会糊成白方块） |

> 排查暗色问题的经验：如果某块内容在暗色下"看不见"，通常是这里写了硬编码的浅色背景或深色文字。把它换成 `var(--bg)` / `var(--bg-alt)` / `var(--ink*)` 即可。

## 说明

- **路由用 history 模式**：首页的 `#features` 等页内锚点才能正常工作；代价是需要上面的 nginx fallback。若只能纯静态托管（无 rewrite），把 `src/router.ts` 的 `createWebHistory` 改为 `createWebHashHistory`，同时 `vite.config.ts` 的 `base` 可保持 `'./'`。
- **TDesign 组件按需引入**（`import { Button as TButton } from 'tdesign-vue-next'`），不写 `app.use(TDesign)`；样式全量引入。注意 `Drawer` / `Dialog` / `Popup` 只有 `visible` prop，必须用 `v-model:visible`。
- **体积**：JS 776 kB（gzip 234 kB）、CSS 200 kB（gzip 25 kB），其中 TDesign 占大头。官网是低频访问页面，暂未做进一步拆分。
