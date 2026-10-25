# ModernChat 官网（website/）

纯静态站点，无需构建：任何静态文件服务器（Nginx / Caddy / GitHub Pages / 对象存储）都能直接托管。

## 页面

| 文件 | 说明 |
|---|---|
| `index.html` | 落地页：Hero 交互演示（纯文本消息 → 插件消息卡片 → 插件切换暗色主题）、功能特性、插件系统讲解（安全模型 + SDK 表）、快速开始（Docker / 手动 / 验证）、技术栈 |
| `docs.html` | 文档阅读页：渲染 `assets/docs/` 下的 Markdown，侧边栏分组导航、上一篇/下一篇、仓库内相对链接自动路由 |

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
        └── web-readme.md        # ← web/README.md
```

## 本地预览

```bash
cd website
python -m http.server 8090    # 或 npx serve .
# 打开 http://localhost:8090
```

## 双语

- 右上角按钮切换中 / 英文，偏好存 `localStorage.mc_site_lang`，默认跟随浏览器语言
- 站点 UI 双语；**文档正文为仓库中文原文**，英文模式下侧边栏标题为英文、正文保持中文并显示提示

## 同步文档

`assets/docs/` 是仓库文档的**副本**，修改 `docs/` 或 README 后需重新拷贝：

```bash
# 在仓库根目录执行
cp README.md website/assets/docs/getting-started.md
cp CHANGELOG.md website/assets/docs/changelog.md
cp docs/设计文档.md website/assets/docs/design.md
cp docs/后端需求规格.md website/assets/docs/api-spec.md
cp docs/前端技术方案.md website/assets/docs/frontend-guide.md
cp docs/运维手册.md website/assets/docs/ops-guide.md
cp server/README.md website/assets/docs/server-readme.md
cp web/README.md website/assets/docs/web-readme.md
```

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
