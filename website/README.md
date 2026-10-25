# ModernChat 官网（website/）

纯静态站点，无需构建：任何静态文件服务器（Nginx / Caddy / GitHub Pages / 对象存储）都能直接托管。

## 页面

| 文件 | 说明 |
|---|---|
| `index.html` | 落地页：Hero 交互演示（纯文本消息 → 插件消息卡片 → 插件切换暗色主题）、功能特性、插件系统讲解（安全模型 + SDK 表）、快速开始（Docker / 手动 / 验证）、技术栈 |
| `docs.html` | 文档阅读页：渲染 `assets/docs/` 下的 Markdown，侧边栏分组导航、上一篇/下一篇、仓库内相对链接自动路由；英文模式自动切换到 `assets/docs/en/` 译本（缺失时回退中文并提示） |

## 目录

```
website/
├── index.html
├── docs.html
└── assets/
    ├── style.css            # 样式（品牌色与主站一致：#0052d9）
    ├── main.js              # 双语切换 / 演示播放器 / Tabs / 文档加载
    ├── vendor/marked.min.js # Markdown 渲染（本地化，无 CDN 依赖）
    └── docs/                # 仓库文档副本（与仓库文档同步维护）
        ├── getting-started.md   # ← 根 README.md
        ├── changelog.md         # ← CHANGELOG.md
        ├── design.md            # ← docs/设计文档.md
        ├── api-spec.md          # ← docs/后端需求规格.md
        ├── frontend-guide.md    # ← docs/前端技术方案.md
        ├── ops-guide.md         # ← docs/运维手册.md
        ├── server-readme.md     # ← server/README.md
        ├── web-readme.md        # ← web/README.md
        └── en/                  # 上述 8 份文档的完整英文译本（同名文件）
```

## 本地预览

```bash
cd website
python -m http.server 8090    # 或 npx serve .
# 打开 http://localhost:8090
```

## 双语

- 右上角按钮切换中 / 英文，偏好存 `localStorage.mc_site_lang`，默认跟随浏览器语言
- 站点 UI 双语；**文档正文双语**：中文模式读 `assets/docs/`，英文模式读 `assets/docs/en/` 译本；某份文档缺英文译本时回退中文原文并显示提示
- 英文译本为人工对照翻译，非机翻；修改仓库文档后需同步更新两份副本

## 同步文档

版本约定：**中文原文 1 份（`assets/docs/`，与仓库文档同步）+ 英文译本 1 份（`assets/docs/en/`）**，仓库中文文档是唯一真源。

修改仓库文档后，在仓库根目录执行一键同步：

```bash
# Linux / macOS / Git Bash
bash website/scripts/sync-docs.sh

# Windows PowerShell
.\website\scripts\sync-docs.ps1
```

脚本行为：
- 自动把 8 份中文文档覆盖拷贝到 `assets/docs/`（对应关系见脚本内映射表）
- 英文译本无法自动生成：若检测到某份 en/ 译本缺失或比中文源旧，会列出清单提醒人工修订
- 修订 en/ 下同名文件后随代码一起提交

## 部署（Nginx 示例）

```nginx
server {
    listen 80;
    server_name modernchat.example.com;
    root /var/www/modernchat-website;
    index index.html;

    location / { try_files $uri $uri/ /index.html; }
}
```

> `docs.html` 使用 hash 路由（`#/design.md`），无需服务端重写规则。
