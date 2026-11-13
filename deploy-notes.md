# ModernChat 部署与开发纪要

> 本文档记录一轮完整的部署与功能开发过程：从零部署到生产服务器，到后续十余项功能改造与问题排查。
> **目的**：把「做了什么、为什么这么做、踩了哪些坑」沉淀下来，避免下次重复踩。
>
> - 记录时间：2026-11-07
> - 服务器：`<部署机 IP>`（Ubuntu 22.04，宝塔面板）
> - 线上地址：<https://newchat.modern-chat.top>
> - 当前版本：`v0.10.3-35-g8cdf3bc`

---

## 1. 部署结果一览

### 1.1 运行环境

| 组件 | 版本 | 位置 | 说明 |
|---|---|---|---|
| Go | 1.27.3 | `/usr/local/go` | 源码编译后端 |
| Node | 22.18.0 | `/opt/node22` | **隔离安装**，不影响机器上其他站点的 Node 20 |
| MySQL | **5.7.44** | 宝塔自带 | 按要求保持 5.7，未升级 |
| Redis | 6.0.16 | `127.0.0.1:6379` | 自动探测后使用 **db=1**（db0 已被其他站点占用） |
| Nginx | 1.31.6 | 宝塔 | TLS 由 CDN 终结，回源为 HTTP |
| CDN | 语鹿云盾（luvipcdn） | — | **HTTP/2 已关闭**（见 §5.1） |

### 1.2 目录结构

```
/opt/modernchat-nc/                    <- 源码（网站根目录不暴露）
├── app/                               <- git clone，含完整历史
│   ├── server/.env                    <- 600 权限，含 DB_DSN / JWT_SECRET
│   ├── server/data/                   <- 上传文件
│   ├── server/migrations/             <- 0001…0026
│   └── web/dist/                      <- 前端构建产物
└── bin/modernchat-server              <- 51MB 二进制

/www/wwwroot/newchat.modern-chat.top/  <- 仅前端静态产物，无源码
```

**为什么源码放在 `/opt`**：如果放进网站根目录，`.env`（含数据库密码、JWT_SECRET）可被公网直接下载。网站根目录只放 `dist` 产物。

### 1.3 systemd

```ini
[Unit]
Description=ModernChat (newchat.modern-chat.top)
After=network.target mysql.service mysqld.service

[Service]
Type=simple
WorkingDirectory=/opt/modernchat-nc/app/server
ExecStart=/opt/modernchat-nc/bin/modernchat-server
Restart=on-failure
RestartSec=3
Environment=DATA_DIR=/opt/modernchat-nc/app/server/data
LimitNOFILE=65535
```

### 1.4 关键环境变量

```env
PORT=8100                       # 8080 已被其他 Node 服务占用
GIN_MODE=release
FRONTEND_URL=https://newchat.modern-chat.top
MIGRATE=true
DATA_DIR=data
LOG_DIR=logs
TRUSTED_PROXIES=127.0.0.1,::1
BACKUP_INTERVAL_HOURS=24
RESEND_FROM=ModernChat <noreply@modern-chat.top>
CHAT_MUSIC_ENDPOINT=http://127.0.0.1/api/chat_music.php
```

### 1.5 常用命令

```bash
systemctl {start|stop|restart|status} modernchat-nc
journalctl -u modernchat-nc -f
tail -f /opt/modernchat-nc/app/server/logs/app-*.log
/www/server/nginx/sbin/nginx -t && /www/server/nginx/sbin/nginx -s reload

# 重新构建
cd /opt/modernchat-nc/app/web && export PATH=/opt/node22/bin:$PATH && npm run build
cd /opt/modernchat-nc/app/server && export PATH=/usr/local/go/bin:$PATH && go build -o /opt/modernchat-nc/bin/modernchat-server ./cmd/server
rsync -a /opt/modernchat-nc/app/web/dist/ /www/wwwroot/newchat.modern-chat.top/
```

---

## 2. MySQL 5.7 兼容性（重要结论）

项目文档写的是 MySQL 8.0，实际跑在 **5.7.44** 上。逐项验证过：

| 检查项 | 结果 |
|---|---|
| 迁移 0022 `FULLTEXT ... WITH PARSER ngram` | ✅ 5.7 支持，建索引成功 |
| 迁移 0023 `content` 扩为 `TEXT` | ✅ |
| 迁移 0024 `direct_key` 唯一索引 | ✅ |
| 迁移 0025 两个唯一索引 | ✅ |
| 代码里 MySQL 8 专有语法 | ❌ 无（无 `SKIP LOCKED`、无窗口函数、无 `JSON_TABLE`、无 `utf8mb4_0900`） |
| `utf8mb4_unicode_ci` 排序规则 | ✅ 5.7 可用 |

**唯一注意点**：`innodb_large_prefix` 在 5.7 默认 ON，否则 767 字节索引前缀限制会卡住 `direct_key` 这类索引。

---

## 3. 功能改造记录

### 3.1 Resend 邮件通道（替代 SMTP）

**背景**：原项目只支持 SMTP，服务器没配 SMTP，导致「注册需邮箱验证」直接 503。

**实现**：新增 `service/resend.go`，完整 Resend API 客户端；`Mailer` 优先走 Resend，未配置时回退 SMTP。

**关键决策：配置存数据库而非环境变量**

| 键 | 说明 |
|---|---|
| `resend.apiKey` | **AES-256-GCM 加密入库**，接口只回 `hasApiKey` |
| `resend.from` | 发件人 |
| `resend.enabled` | 开关 |

优先级：**数据库 > 环境变量**。启动时从库里加载（`resend config loaded from database`），后台保存后热更新，无需重启。

**踩的坑**：早期实现是环境变量优先，导致后台改了配置不生效；且「留空不改」逻辑写错，把 `enabled` 覆盖成 `false`、`from` 覆盖成空串。修复：
- 启动时数据库优先
- `from` / `enabled` 空值时保留原值

### 3.2 后台邮件管理

新增「邮件」页签：

| 能力 | 接口 |
|---|---|
| 查看历史已发邮件 | `GET /api/admin/mail?limit=&after=` |
| 单封详情（含正文） | `GET /api/admin/mail/:id` |
| 后台直接发信（支持附件） | `POST /api/admin/mail/send` |
| 附件下载 | `GET /api/admin/mail/:id/attachments/:attId` |

**设计要点**：
- HTML 正文用 `sandbox="allow-same-origin"` 的 iframe 渲染，防止邮件里的脚本影响后台页面
- 附件**走后端中转**而不是把 `download_url` 直接给前端（Resend 的下载链接需鉴权）
- 文件名与正文由前端 `FileReader` 转 base64，后端二次校验（单个 ≤10MB，总计 ≤25MB，最多 5 个）

### 3.3 两步验证（2FA）

**两种方式，可同时开启，也可都不开（不强制）**：

| 方式 | 流程 |
|---|---|
| **TOTP 应用** | 生成密钥 → 二维码 + 手动代码 → 输入 6 位验证码 → 生成 10 条恢复码 |
| **邮箱验证码** | 提示邮箱地址 → 点发送 → 5 分钟内输入验证码 |

**技术实现**：

- `service/totp.go`：**标准 RFC 6238，未引第三方库**（HMAC-SHA1 + 动态截断，几十行）
  - 周期 30 秒，6 位，允许 ±1 窗口容忍时钟漂移
  - 二维码名称按要求：`ModernChat 2FA {用户名}`
- 恢复码：`A1B2-C3D4` 格式（剔除易混淆字符），库内只存 SHA-256 哈希，一次性
- 登录挑战：`login_2fa_challenges` 表，10 分钟过期、最多 5 次尝试、一次性
- 挑战内记录 `token_version`，验证期间改密/强制下线则作废

**两个特殊规则（按需求）**：
1. **开了 2FA 的账号不受账号级登录失败锁定约束**（2FA 本身已是更强保护；IP 限流仍生效）
2. 关闭 2FA 必须验证一次（TOTP 码 / 邮箱码 / 恢复码任一），否则等于给盗号者一键关闭入口

**关于「8 位验证码」**：标准认证器（Google/Microsoft Authenticator）只支持 6 位，强制 8 位会导致绝大多数 App 无法使用。实现按 **6 位标准**，同时兼容 8 位输入。

### 3.4 三级角色与自我保护

**角色分级**：

| 角色 | 权限 |
|---|---|
| **owner（站长）** | 全部权限；**唯一、不可转让、不可降级** |
| **admin（管理员）** | 进后台；管理普通用户 |
| **user（普通用户）** | 无后台 |

**站长专属**（管理员不可做）：设置/移除管理员、封禁/解封/踢线/重置密码。

**防呆设计（实测全部 403）**：

| 操作 | 结果 |
|---|---|
| 踢自己下线 | `不能对自己执行「踢下线」` |
| 封禁自己 | `不能对自己执行「封禁」` |
| 改自己角色 | `不能对自己执行「修改自己的角色」` |
| 把别人设为 owner | `role 只能是 admin 或 user（站长为唯一且不可指定）` |
| 重置站长密码 | `不能重置站长密码，请站长本人在「账号」设置里修改` |
| 管理员操作站长/其他管理员 | `管理员不能操作站长/其他管理员` |
| 封禁站长 | `不能封禁站长账号`（否则站长唯一，封掉就没人能解封） |

**数据迁移**：原 `role=admin` 的首个用户升级为 `role=owner`。

### 3.5 Redis 自动探测

**需求**：能连就用，连不上就静默降级。

**实现**（`store/redis_auto.go`）：

1. 未显式配置 `REDIS_ADDR` 时，依次探测 `127.0.0.1:6379` → `[::1]:6379` → `localhost:6379`
2. PING 通 → 扫描 DB 0..15，**选第一个空库**（避免与其他业务 key 冲突）
3. 全不通 → 返回空，回落内存限流

**三个设计决策**：
- **只探测回环，不扫网段**（扫内网既慢又有探测他人服务的风险）
- **自动选空库**（本机 db0 被占用，自动落到 db1）
- **不使用任何密码**（需认证的 Redis 会 PING 失败，直接放弃而非猜密码）

实测日志：
```
INFO redis auto-detected, using it automatically  addr=127.0.0.1:6379 db=1
INFO rate limiting backed by redis
```

### 3.6 音乐播放器（改为插件 + 宿主执行）

**架构约束（关键）**：插件运行在 `sandbox="allow-scripts"` 且**无 `allow-same-origin`** 的 iframe，是 **opaque origin**。按浏览器规范，**IndexedDB 在该上下文被禁用**（直接 `SecurityError`）。

而播放器核心正是「AES-CTR 加密流 + IndexedDB 密文缓存」。所以采用**分层**：

```
插件（UI / 曲库 / 搜索）
  │  api.music.*（13 个方法，权限 music:control）
  ▼
宿主 musicHost.ts
  │
  ▼
MusicEngine（AES-CTR 解密 + IndexedDB 缓存 + AudioBufferSourceNode）
```

| 侧 | 职责 |
|---|---|
| 插件 | 曲库浏览、歌单选择、在线搜索（酷我/QQ/酷狗/网易/汽水） |
| 宿主 | 拉流、解密、解码、缓存、播放控制 |

**音乐抽屉**（`MusicDrawer.vue`）分两部分：
1. **顶部播放控制条** —— 主站渲染（插件的描述式 UI 只有 `text/button/list/progress`，做不出可拖动进度条）
2. **下方插件面板** —— 插件的曲库与搜索界面

**加密方案与源站逐字节一致**：
```
key   = SHA256(token . "|stream")
nonce = SHA256(token . "|iv")[0..12]
算法  = AES-256-CTR，计数器 = nonce(12) ++ 大端 uint32
```

### 3.7 播放器账号联动

**问题**：播放器（`player.modern-chat.top`）有独立的 PHP session 登录体系，聊天室用户没有那套账号。

**三种状态**：

| 状态 | 触发 | 弹窗表现 |
|---|---|---|
| `missing` | 播放器无同名/同邮箱账号 | 「请先登录播放器」+ 去登录 / 手动绑定 |
| `found` | 已有账号 | 「已找到对应用户：XXX，请输入密码完成绑定」 |
| `bound` | 已绑定 | 不弹窗 |

**注册时自动绑定**：聊天室注册后异步在播放器建号并绑定，用户名/邮箱/密码原样复用。注册成功提示追加：
> 由于应用需要在注册的同时已为您同时注册本应用旗下的播放器，使用本账号邮箱 + 密码可以直接登录播放器，感谢您的使用！

**会话票据**：`ticket.php` 签发一次性票据（120 秒）→ `chat_session.php` 兑换播放器 session（两站同 site，SameSite=Lax 可行）。

**安全**：`chat_uid` 只取登录态，绝不信任前端传参（否则能绑定他人账号）。

### 3.8 密码规则两边统一

**问题**：聊天室 6–72 位任意字符，播放器要求 6–18 位且必须含四类字符。导致聊天室合法密码建播放器账号后，用户用原密码登不进。

**解决**：两边统一为 **6–72 位 + 必须含大写、小写、数字、特殊字符**。

### 3.9 音频播放（MP3 / WAV / FLAC）

**后端**：`detectType` 补 FLAC（`DetectContentType` 认不出，靠 `fLaC` magic 兜底）。

**前端**（`AudioMessage.vue`）：原生 `<audio>` + 自绘播放条，按扩展名区分：
- `mp3/wav/flac/m4a/aac/opus` → 标准播放条（**可拖动进度**）
- `webm/ogg` → 紧凑语音气泡

关键测试结果：`audio/mpeg` + `Accept-Ranges: bytes`（拖动只拉需要的字节）。

### 3.10 附件直链签名

**目标**：不暴露长期可用的直链，防止被刷流量。

**实现**：上传返回的 URL 带 `exp`（过期时间）+ `sig`（HMAC-SHA256 签名，密钥由 JWT_SECRET 派生）。

| 场景 | 结果 |
|---|---|
| 正常签名 URL | 200 |
| 篡改 sig | 403 `附件链接已失效` |
| exp 改成过去 | 403 |
| **不带签名（历史 URL）** | 200 兼容放行 |

有效期 6 小时。

### 3.11 秒传（内容寻址去重）

**流程**：前端算 SHA-256 → 查 `/api/upload/exists` → 命中直接复用。

**省空间原理**：同一内容只落盘一份，所有消息指向同一 URL。

**安全**：只复用「**本人**上传过」的文件（`owner_id` 过滤），否则知道别人文件哈希就能白嫖 URL 与配额。

**踩的坑**：建 `upload_dedup` 表时漏了 `owner_id` 列，而代码在读它 → 记录写不进去 → 秒传永不命中。补列 + 索引后正常。

### 3.12 上传进度条

**技术关键**：`fetch` 没有上传进度事件。改用 `XMLHttpRequest` 的 `upload.onprogress`。

```js
// 封顶 99%：最后 1% 留给服务端落盘与响应
onProgress(Math.min(0.99, ev.loaded / ev.total))
```

秒传命中时直接给 100%（没有真正传字节）。

**UI**：输入框上方显示「文件名 + 百分比 + 进度条」。

### 3.13 登录提醒邮件（用户自选）

**踩的坑**：早期实现**没有开关**，每次登录都发一封，测试期间刷屏。

**修复**：改为**用户级偏好**（新表 `user_prefs`），设置页 → 账号 → 「登录提醒邮件」开关，默认开启。

**另外**：脚本/curl 登录不发邮件（`isBrowserUA` 排除 `curl/`、`wget/`、`python-requests`、`go-http-client`、`okhttp`、`postman`、`axios` 等）。

### 3.14 消息转发

已有实现，支持全部类型（文本/图片/语音/视频/音频/文件）。转发的是 `content`（URL）+ `msgType`，**不产生新文件**——天然复用已有存储，与秒传同效。

---

## 4. 数据库变更汇总

### 4.1 新增表（迁移 0026）

```sql
user_2fa                -- 两步验证配置（每用户一行）
login_2fa_challenges    -- 登录二次验证挑战（一次性）
upload_dedup            -- 上传文件指纹（秒传）
user_prefs              -- 用户个人偏好（登录提醒等）
```

**注意**：这 4 张表最初是直接在库里手工建的，后来补成正式迁移 `0026_twofa_upload_dedup_prefs.{up,down}.sql`，库内 `schema_migrations` 已同步到 **26**。

### 4.2 新增配置键（`system_settings`）

| 键 | 说明 |
|---|---|
| `resend.apiKey` | AES-GCM 加密 |
| `resend.from` | 发件人 |
| `resend.enabled` | 开关 |
| `security.loginAlertEmail` | 登录告警（站点级，已被用户级偏好取代） |

### 4.3 角色值变化

```
user | admin  ->  user | admin | owner
```

首个注册者 = `owner`，唯一且不可变。

---

## 5. 踩坑记录（按严重程度）

### 5.1 CDN 的 HTTP/2 吞掉 WebSocket 升级头（严重）

**现象**：网络面板里 `/ws?token=...` 一直刷屏，198 个错误，前端无限重连。

**排查过程**：

| 测试 | 结果 |
|---|---|
| 浏览器直连源站 80 端口 | **101 Switching Protocols** ✅ |
| 经 CDN，HTTP/1.1 | **101** ✅ |
| 经 CDN，HTTP/2 | **400** ❌ |
| CDN 在 HTTP/2 下转发 `Upgrade` 头 | **空**（被吞） |
| CDN 在 HTTP/1.1 下转发 `Upgrade` 头 | `websocket` ✅ |

用临时 nginx 探针抓到的确凿证据：
```
HTTP/2 请求:  upgrade=[]        connection=[]      <- 头被丢弃
HTTP/1.1 请求: upgrade=[websocket] connection=[upgrade]
```

**结论**：CDN 的 HTTP/2 实现不支持 WebSocket 的 Upgrade（需要 RFC 8441 Extended CONNECT，多数中小 CDN 没实现）。浏览器对 HTTPS 默认走 HTTP/2 → 必失败。

**解决**：在 CDN 后台关闭 **HTTP2 设置**。代价仅是静态资源少一点多路复用，站点只有 7 个资源，影响可忽略。

**关闭后验证**：`/ws` 全部 200，2.5 分钟长测 `opens=1 closes=0`。

### 5.2 CDN 覆盖 CORS 响应头（严重）

**现象**：
```
Access to fetch at 'https://player.modern-chat.top/api/stream_token.php'
from origin 'https://newchat.modern-chat.top' has been blocked by CORS policy:
Request header field x-csrf-token is not allowed by Access-Control-Allow-Headers
```

**根因**：源站 nginx 配的 CORS 头是对的（明确列表），但 **CDN 把它覆盖成通配符**：

```
源站: Allow-Headers: Content-Type, X-Requested-With, X-CSRF-Token, Range   ✅
CDN:  Allow-Headers: *                                                    ❌
```

**`Access-Control-Allow-Headers: *` 在带凭据（`Allow-Credentials: true`）的请求里是非法的**，浏览器直接拒绝。

**解决（双重）**：

1. CDN 后台把 CORS 配置改成明确列表（但该面板的「允许请求方法」只接受单个方法或 `*`，无法表达多方法组合——产品缺陷）
2. **从根本上消除预检**：前端把播放器的跨域写请求改成 **CORS 简单请求**（`application/x-www-form-urlencoded` + POST），浏览器不发预检，CDN 的跨域规则完全不参与判断

第 2 条是真正的解法——不依赖 CDN 配置。

### 5.3 `c.Data()` 透传导致 gzip 声明与实际不符（中等）

**现象**：
```
GET ... net::ERR_CONTENT_DECODING_FAILED 200 (OK)
```

**根因**：后端用 `c.Data()` 透传播放器的 JSON，会写死 `Content-Length` → gin 的 gzip 中间件（`MinLength=1024`）跳过压缩 → 但 CDN 仍按 `Accept-Encoding` 加了 `Content-Encoding: gzip` → 浏览器对着明文解压必然失败。

**解决**：改为解析后重新 `c.JSON()` 输出，让 gzip 中间件统一接管。

**验证**：
| 接口 | 大小 | gzip 头 | 首字节 |
|---|---|---|---|
| probe | 77 B | 无 | `{"o` 明文 ✅ |
| /api/me | 28 KB | 有 | `1f8b08` 真 gzip ✅ |

### 5.4 vue-i18n 的 `@` 是 linked message 语法（中等）

**现象**：点「系统设置」页签直接黑屏，控制台 `SyntaxError: 10`。

**排查过程**：从 vendor 里逐字节定位：
```js
var Q={...,INVALID_LINKED_FORMAT:10,...}
function wd(e,t,n={}){...SyntaxError(String(e))...}
```
这是 **vue-i18n 的 ICU 消息解析器**，错误码 **10 = `INVALID_LINKED_FORMAT`**。

**根因**：我加的文案里有裸 `@`：
```js
resendFromHint: '格式：显示名 <邮箱@已验证域名>。...'
```

**解决**：按项目既有约定转义成 `{'@'}`：
```js
resendFromHint: "格式：显示名 <邮箱{'@'}已验证域名>。..."
```

> **这个坑项目里踩过两次**（CHANGELOG 记着「通知抽屉文案编译失败」）。新增文案时必须检查 `@`、`|`、括号配对。

### 5.5 Service Worker 返回 undefined 导致黑屏（中等）

**现象**：
```
The FetchEvent for "https://.../admin" resulted in a network error response:
the promise was rejected.
Uncaught (in promise) TypeError: Failed to convert value to 'Response'.
```

**根因**：
```js
// 导航请求：caches.match('/') 未命中时 resolve 成 undefined
event.respondWith(fetch(req).catch(() => caches.match('/')))
```
`respondWith(undefined)` 抛错 → 整页黑屏，连浏览器自带的错误页都不显示。

**解决**：保证 `respondWith` 永远拿到真正的 `Response`，缓存未命中时返回最小兜底页。

**顺带修的另一个 SW bug**：
```js
// ❌ res 返回给浏览器后再 clone，body 已被消费
const fresh = fetch(req).then((res) => {
  if (res.ok) cache.put(req, res.clone())   // 抛 "Response body is already used"
  return res
})

// ✅ 同步阶段先 clone
const forCache = res.ok ? res.clone() : null
if (forCache) caches.open(CACHE).then((c) => c.put(req, forCache))
return res
```

### 5.6 插件上传把 multipart 边界存进了代码（严重）

**现象**：`about:srcdoc:85 Uncaught SyntaxError: Invalid or unexpected token`

**根因**：插件上传接口把**整个请求体**当代码存，而前端用 `multipart/form-data` 上传：
```
--------------------------351546c35cda0b10
Content-Disposition: form-data; name="file"; filename="music-player.plugin.js"
```
下发到沙箱后 `---...` 是非法 JS。

**解决**：
- 按 `Content-Type` 分流：multipart 走 `c.FormFile("file")`，raw body 走原路径
- 加 `stripMultipartWrapper` 兜底，能剥离历史脏数据

### 5.7 插件 IIFE 包裹导致 `__mcActivate` 找不到（严重）

**现象**：插件装上了但不生效，面板不出现。

**根因**：沙箱引导代码在**自己的 IIFE 内**做：
```js
if (typeof __mcActivate === "function") { ... }
```
对**未声明标识符**，`typeof` 永远返回 `"undefined"`。而插件用外层 IIFE 把函数挂在 `window` 上——`typeof` 查的是**作用域内的绑定**，不是对象属性，所以查不到。

**解决**：插件必须用**顶层函数声明**，不要外层 IIFE：
```js
// ✅ 正确
function __mcActivate(mcApi) { ... }

// ❌ 错误
(function(){ function __mcActivate(...){}; window.__mcActivate = __mcActivate })()
```

**验证方法**：
```js
new Function(code + '; return typeof __mcActivate')()  // 应为 "function"
```

### 5.8 路由守卫与页面内二次守卫都只认 admin（严重）

**现象**：站长账号无法进入后台，一进 `/admin` 就被弹回聊天页。

**根因**：**两层**都写死 `role !== 'admin'`：
1. `router/index.ts` 的 `beforeEach` 守卫
2. `AdminView.vue` 的 `onMounted` 二次守卫（进入后被 `replace('/chat')` 弹回）

**同类问题还波及 3 处**：
- `App.vue` 维护模式判断
- `middleware/maintenance.go`
- `admin.go` 封禁保护（原来是"不能封禁 admin"，owner 反而能被封禁 → **安全漏洞**）

**教训**：改角色模型时必须全局搜索所有 `role ===` / `role !==` 判断。

### 5.9 静态资源长缓存导致发版不生效（中等）

**现象**：代码改好了但行为不变。

**根因**：`index.html` 没有 Cache-Control，而 JS 是 `max-age=2592000` + `immutable`（30 天）。浏览器一直用旧 JS。

**解决**：
```nginx
location = /index.html {
    add_header Cache-Control "no-cache, no-store, must-revalidate" always;
}
location / {
    add_header Cache-Control "no-cache, no-store, must-revalidate" always;
    try_files $uri $uri/ /index.html;
}
```

### 5.10 其他小坑

| 问题 | 原因 | 解决 |
|---|---|---|
| UnoCSS 把注释里 `[00:00.00]` 当原子类 | 扫描模板内容 | 改注释措辞 |
| Go 的 `req.Header.Set("Host")` 无效 | `Host` 是独立字段 | 赋 `req.Host` |
| `redis.Cmd` 没有 `.Scan` | `DBSIZE` 返回整数 | 用 `.Int64()` |
| CDN 拼接 CORS 头逗号重复 | 它解析多值列表出错 | 印证"不该依赖它" |
| python 替换没命中 | 模式与实际不符 | 改用行号精确定位 |

---

## 6. 安全设计要点

### 6.1 密钥管理

| 密钥 | 位置 | 保护 |
|---|---|---|
| `DB_DSN` | `server/.env`（600） | 不进仓库 |
| `JWT_SECRET` | 同上 | 同时是 `system_settings` 敏感项的加密密钥 |
| `resend.apiKey` | 数据库 | AES-256-GCM 加密，接口只回 `hasApiKey` |
| `CHAT_MUSIC_KEY` | nginx `fastcgi_param` | **不落任何静态文件** |
| 附件签名密钥 | 由 JWT_SECRET 派生 | HMAC-SHA256 |

**注意**：`JWT_SECRET` 更换后，`system_settings` 里加密的 SMTP/Resend/存储密码**无法解密**，需重填。

### 6.2 权限边界

- 插件跑在 sandbox iframe（无 same-origin），**拿不到 JWT / localStorage / 主站 DOM**
- 插件网络出口由 CSP `connect-src` 收敛到 `allowedHosts`
- 播放器内部端点（`chat_music.php` / `chat_link.php`）仅 `127.0.0.1` 可访问
- `/uploads/**` 需登录（Auth 或 `mc_auth` cookie）+ 签名

### 6.3 已知取舍

**附件签名只防「长期复用」，不防「当场分享」**。要彻底防盗链得做一次性 token，但那样 `<img>`/`<video>` 的正常重放（拖动进度、重复观看）会失效，取舍上不划算。

---

## 7. 遗留问题

| # | 问题 | 状态 |
|---|---|---|
| 1 | 邮箱发件人头像 | **未生效**：发件人是 `noreply@modern-chat.top`（根域），但根域无 DNS 记录也无站点，邮件客户端抓不到 favicon。需在 Cloudflare 给根域加 A 记录，或验证 `newchat` 子域 |
| 2 | 插件面板 UI 朴素 | 描述式 UI 只有白名单组件，做不出封面图/渐变歌词。播放控件已由主站补上，曲库列表仍是文本形式 |

---

## 8. 代码改动清单

### 8.1 后端新增文件

```
server/internal/handler/admin_mail.go     后台邮件管理
server/internal/handler/attach_sign.go    附件签名
server/internal/handler/music.go          音乐代理（在线搜索/解析/歌词）
server/internal/handler/music_link.go     播放器账号联动
server/internal/handler/rbac.go           三级角色与自我保护
server/internal/handler/twofa.go          2FA 配置
server/internal/handler/twofa_login.go    登录 2FA 校验
server/internal/handler/upload_dedup.go   秒传查询
server/internal/handler/user_prefs.go     用户偏好
server/internal/service/resend.go         Resend 客户端
server/internal/service/totp.go           TOTP 实现
server/internal/store/redis_auto.go       Redis 自动探测
server/internal/store/twofa.go            2FA 数据访问
server/internal/store/user_prefs.go       偏好数据访问
server/migrations/0026_*.sql              新增 4 张表
```

### 8.2 后端修改文件

```
cmd/server/main.go                     Redis 自动探测接线
internal/config/config.go              Resend / ChatMusic 配置
internal/handler/admin.go              权限保护
internal/handler/admin_settings.go     Resend 配置读写
internal/handler/auth.go               2FA 分支、登录告警、首用户 owner
internal/handler/common.go             密码规则四类字符
internal/handler/plugin.go             multipart 上传解析
internal/handler/site.go               mailReady 判断
internal/handler/upload.go             签名 URL、秒传记录
internal/middleware/ban.go             Admin 中间件认 owner
internal/middleware/maintenance.go     维护模式认 owner
internal/plugin/manifest.go            music:control 权限
internal/router/router.go              全部新路由
internal/service/mailer.go             Resend 优先、SendAlert
internal/service/storage.go            FLAC 支持
internal/store/conversation.go         成员数 JOIN users（防孤儿行）
internal/store/setting.go              Resend 配置键
internal/store/user.go                 owner 选举
```

### 8.3 前端新增文件

```
web/src/music/*.ts                           播放引擎 / 加密缓存 / 歌词 / API
web/src/components/chat/AudioMessage.vue     音频播放器
web/src/components/chat/MusicDrawer.vue      音乐抽屉（控件+插件面板）
web/src/components/chat/TwoFADialog.vue      2FA 弹窗
web/src/views/admin/MailPanel.vue            后台邮件管理
```

### 8.4 前端修改文件

```
web/public/sw.js                       SW 兜底与 clone 修复
web/src/App.vue                        维护模式认 owner
web/src/api/client.ts                  上传改 XHR（带进度）
web/src/api/index.ts                   秒传 / 2FA / 偏好 API
web/src/components/chat/AppRail.vue    音乐按钮
web/src/components/chat/MessageComposer.vue  上传进度条、音频上传
web/src/components/chat/MessagePane.vue      音频文件渲染
web/src/components/common/AuthScene.vue      移除 login-bg 引用
web/src/views/AuthView.vue             2FA 弹窗接线
web/src/views/ChatLayout.vue           音乐抽屉
web/src/views/SettingsView.vue         登录提醒开关
web/src/views/admin/AdminView.vue      三级角色、邮件页签、owner 判定
web/src/router/index.ts                owner 守卫
web/src/stores/auth.ts                 isOwner、login 支持 2FA
web/src/types/proto.ts                 owner 角色、2FA 挑战类型
web/src/i18n/*.ts                      全部新文案
```

---

## 9. 插件清单

当前已安装 **1 个插件**：

| 名称 | 版本 | 权限 |
|---|---|---|
| `music-player` | 1.0.0 | `ui:panel` / `ui:actions` / `music:control` / `storage` / `ui:host` |

插件源码在 `/opt/modernchat-nc/app` 之外维护，通过后台「插件管理」上传安装。

---

## 10. 验收基线

```
modernchat-nc    active
/api/health      {"status":"ok"}
/api/version     v0.10.3-35-g8cdf3bc
migrations       26 / dirty=0
MySQL            5.7.44
Redis            6.0.16（自动探测 db=1）
Go               1.27.3
Node             22.18.0
入口资源          7 个全 200
dist = site      123 个文件
```

**用户数据现状**：

```
users          1（admin / owner）
messages       2
上传目录        仅真实业务文件
upload_dedup   1 条有效记录
```
