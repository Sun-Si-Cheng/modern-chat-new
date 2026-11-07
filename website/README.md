# ModernChat 官网（website/）

项目官网：落地页 / 文档站 / 插件市场。**Vue 3 + Vite + TypeScript**，构建产物是纯静态文件，可托管在任意静态服务器（Nginx / Caddy / 对象存储）。

> 不依赖任何组件库：按钮、标签、输入框、图标、代码块全部在 `src/styles` + `src/components` 里手写。理由是官网只有 6 类控件，引一个 UI 库要多背 700 KB JS 和 200 KB CSS。

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
| `/` | 落地页：Hero（一句话定位 + 一键安装命令 + 产品界面复刻）、核心能力（三组、按内核/治理/运维分层）、插件系统（沙箱模型 + 6 项权限表 + 代码示例）、部署（安装脚本 / Docker / 手动三 Tab + 运维清单）、生态（文档 / 插件市场 / 更新日志 / 源码）、收尾 CTA |
| `/docs/:file?` | 文档站：分组侧栏 + Markdown 渲染 + 上下篇；英文模式读 `public/content/docs/en/`，缺失时回退中文并提示 |
| `/plugins` | 插件市场：搜索 + 分类 + 卡片（权限标签、下载 JS、源码链接）+ 三步安装指引 + 提交入口 |

首页的产品界面是**用 HTML/CSS 复刻的真实四栏界面**（`components/ProductPreview.vue`），不是截图：切会话、开合详情面板、切暗色都能点，字体与间距按 `web/` 的真实取值。

## 结构

```
website/
├── index.html                  # Vite 入口（title / description / 首屏主题预设）
├── vite.config.ts              # base './'、别名 @ → src、dev 端口 5174
├── public/
│   ├── favicon.ico             # 与 web/public/favicon.ico 同源，换图标两处一起换
│   └── content/                # 静态内容（刻意不放在路由同名目录下，见下文）
│       ├── docs/               # 中文文档副本（由 scripts/sync-docs 生成）
│       │   └── en/             # 英文译本（人工维护）
│       └── plugins/            # 可下载的示例插件 JS
├── scripts/
│   ├── sync-docs.ps1           # 从仓库根同步文档（Windows）
│   └── sync-docs.sh            # 同上（Linux / macOS）
└── src/
    ├── main.ts                 # 装配 router / i18n / 样式分层
    ├── App.vue                 # SiteNav + RouterView + SiteFooter + 标题/描述同步
    ├── router.ts               # history 路由（首页锚点需要）
    ├── theme.ts                # 亮 / 暗 / 跟随系统，与 web/ 共用 localStorage.mc_theme
    ├── links.ts                # 版本号与仓库地址（版本号的唯一来源）
    ├── i18n/
    │   ├── messages.clean.json # 中英文案（含产品预览里的每条消息）
    │   └── index.ts            # vue-i18n 实例 + 语言持久化（mc_site_lang）+ dev 期消息自检
    ├── components/
    │   ├── SiteNav.vue         # 顶部导航（移动端折叠）
    │   ├── SiteFooter.vue      # 页脚（产品 / 文档 / 开发者 / 仓库 四列）
    │   ├── ProductPreview.vue  # 产品界面复刻
    │   ├── CodeCard.vue        # 代码块（复制按钮）
    │   └── UiIcon.vue          # 内联 SVG 图标集
    ├── views/
    │   ├── HomeView.vue        # 落地页
    │   ├── DocsView.vue        # 文档站
    │   └── PluginsView.vue     # 插件市场
    └── styles/
        ├── tokens.css          # 设计令牌（颜色 / 字号 / 圆角 / 动效 / 层次）
        ├── base.css            # reset、排版、按钮、标签、代码块、表格
        ├── site.css            # 导航、页脚、产品界面复刻
        ├── home.css            # 落地页各区块
        ├── docs.css            # 文档站与 Markdown 排版
        └── market.css          # 插件市场
```

## 两条必须遵守的路径约定

1. **静态资源不放路由同名目录。** 文档在 `public/content/docs/`，插件在 `public/content/plugins/`——不是 `public/docs/` 与 `public/plugins/`。因为 `/docs/:file` 与 `/plugins` 是前端路由，若物理目录同名，静态服务器会优先返回真实文件：访问 `/docs/design.md` 会变成一坨裸 Markdown，`/plugins` 甚至可能 403。
2. **文档正文的首个 `h1` 会被剥掉。** 页面标题来自 `DocsView` 的 `DOCS` 表，正文从 `h2` 起，避免同一屏出现两个大标题。

## 文档同步

文档真源在仓库根（`README.md`、`docs/*.md`、`server/README.md`、`web/README.md`），官网只放副本，**不要在 `public/content/docs/` 里手改中文副本**：

```bash
# 在仓库根执行
powershell -File website/scripts/sync-docs.ps1   # Windows
bash website/scripts/sync-docs.sh                # Linux / macOS / Git Bash
```

| 源文件（唯一真源） | 官网文件 |
|---|---|
| `README.md` | `content/docs/getting-started.md` |
| `CHANGELOG.md` | `content/docs/changelog.md` |
| `docs/设计文档.md` | `content/docs/design.md` |
| `docs/后端需求规格.md` | `content/docs/api-spec.md` |
| `docs/前端技术方案.md` | `content/docs/frontend-guide.md` |
| `docs/运维手册.md` | `content/docs/ops-guide.md` |
| `docs/安全与迁移指南.md` | `content/docs/security-migration-guide.md` |
| `server/README.md` | `content/docs/server-readme.md` |
| `web/README.md` | `content/docs/web-readme.md` |

脚本会把中文源覆盖到 `public/content/docs/`，并检查 `content/docs/en/` 的英文译本是否落后于中文源（落后只提示，不覆盖——英文需人工修订）。新增文档时三处一起改：`docs/` 源文件、两个 sync 脚本的映射表、`DocsView.vue` 的 `DOCS` 表。

## 插件市场

- 可下载文件在 `public/content/plugins/`，对应源码在仓库 `plugins/examples/`（`weather` / `dice` / `word-count`）。
- 新增示例插件时三处一起改：`plugins/examples/` 源码、`public/content/plugins/` 可下载副本、`src/views/PluginsView.vue` 的列表。

## 版本号

版本号集中在 `src/links.ts` 的 `VERSION`，导航与页脚两处引用它。发版时与 `web/`、`website/package.json` 一起改，`sh scripts/check-version.sh` 会校验（基准是 `CHANGELOG.md` 最新正式版本，同时校验 `links.ts` 与两个 `package.json` / `package-lock.json`）。

## 部署

```bash
cd website && npm run build
# 把 website/dist 交给 nginx
```

history 路由需要 fallback，否则刷新 `/docs` 会 404：

```nginx
location / {
    root /var/www/modernchat-site;
    try_files $uri $uri/ /index.html;
}
```

## 设计约定

官网与产品界面（`web/`）共用同一套语言：**7 档字号、4 档圆角 + 胶囊、三档动效、三级阴影**。改样式前先读 `src/styles/tokens.css`。

| 项 | 约定 |
|---|---|
| 颜色 | 一律走令牌（`--brand` / `--ink*` / `--bg*` / `--line*`），**组件里不写死色值**。实底按钮用 `--brand-solid`，文字/图标用 `--brand`——暗色下两者取值不同，白字压在亮蓝上对比度不够 |
| 字号 | 只用 `--fs-100` … `--fs-900`（官网比产品多两档展示级） |
| 圆角 | 只用 `--r-sm/md/lg/xl/pill` |
| 动效 | 只用 `--dur-*` 与 `--ease-out`（`cubic-bezier(0.22,1,0.36,1)`），不写裸 `ease-out`；`prefers-reduced-motion` 下全部降为 1ms |
| 主题 | 与主站共用 `localStorage.mc_theme` 与 `<html theme-mode>`，导航右上角可切换；产品预览默认跟随站点亮暗，用户点预览里的主题按钮后转为手动 |
| 图标 | 全部内联 SVG（`UiIcon.vue`），**不用 emoji 当图标**——emoji 在不同系统上字形差异大，且无法随文字色变化 |
| 代码块 | 固定深色，不随主题；示例代码里的注释也要走 i18n，否则英文页里会出现中文注释 |
| 图标文件 | `public/favicon.ico` 是 `web/public/favicon.ico` 的副本，换图标时两处一起换 |

> 排查暗色问题的经验：某块内容"看不见"，通常是写了硬编码的浅色背景或深色文字；换成 `var(--bg)` / `var(--ink*)` 即可。

## 说明

- **路由用 history 模式**：首页的 `#capabilities` 等锚点才能正常工作；代价是需要上面的 nginx fallback。若只能纯静态托管（无 rewrite），把 `src/router.ts` 的 `createWebHistory` 改成 `createWebHashHistory`。
- **文案里不能出现裸 `@`**：`vue-i18n` 把 `@` 当 linked message、`|` 当 plural、`{}` 当插值，写错会在渲染期抛 `SyntaxError` 并让整页白屏。需要字面 `@`（如 `@某人`）请写 `{'@'}`。`src/i18n/index.ts` 在 dev 下会把所有消息编译一遍，出错直接打 console。
- **体积**：JS 249 KB（gzip 90 KB）、CSS 38 KB（gzip 8 KB）。
