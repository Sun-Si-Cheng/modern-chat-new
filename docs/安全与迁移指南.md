# 安全与迁移指南

> 面向**接手这个项目的人**：本轮前后端缺陷修复改动了很多“默认假设”，只读 CHANGELOG 容易知其然不知其所以然。
> 本文讲清每项改动的动机、边界和容易踩的坑，并标注代码位置，方便对照实现。
>
> 契约字段与接口清单仍以 [后端需求规格.md](后端需求规格.md) 为准；架构取舍见 [设计文档.md](设计文档.md)；
> 日常部署步骤见 [运维手册.md](运维手册.md)；插件作者请同时阅读 [插件开发.md](插件开发.md)。

---

## 0. 速查表

| 主题 | 一句话结论 | 代码位置 |
|---|---|---|
| `/uploads` 鉴权 | 必须登录；`Authorization` 或同源 `mc_auth` cookie 二选一；目录列举 404；缓存 `private, max-age=3600` | `server/internal/router/router.go` `serveUpload`、`onlyFilesFS`；`server/internal/middleware/jwt.go` `UploadAuth` |
| 退出登录 | `POST /api/auth/logout` 只清 cookie，不吊销 JWT；“退出所有设备”才 bump `token_version` 并踢 WS | `handler/auth.go` `Logout`；`handler/user.go` `LogoutOthers`；`ws/hub.go` `KickUser` |
| 实时通道 | 握手 `?token=` 保留，缺失时回退 cookie；**每一帧**先复核封禁与 tv，失败 error 帧 + 断开 | `ws/serve.go` `Serve`；`ws/dispatch.go` `Dispatch` / `checkConnState` |
| 插件隔离 | `sandbox="allow-scripts"`（无 same-origin）、`srcdoc` 启动、CSP 收敛出口；**裸 `fetch` 已失效，必须用 `mc.request`** | `web/src/composables/usePluginHost.ts` `mountRuntime` / `buildSandboxCsp` / `hostAllowed` |
| 上传链路 | 先嗅探 512 字节并**写回**；限额按后台配置；缩略图限 4000 万像素；GC 按 `/uploads/` 前缀判定 | `service/storage.go` `SaveStream` / `extFor`；`service/thumbnail.go`；`handler/ops.go` `referencedUploads` |
| 迁移 | 0023 扩 TEXT、0024 单聊唯一键、0025 好友/验证码唯一索引；升级前必须备份 | `server/migrations/0023..0025` |
| 部署 | Docker 版本号走 `VERSION`/`COMMIT`/`BUILD_TIME`，镜像源走 `GO_IMAGE`/`RUNTIME_IMAGE`/`GOPROXY`/`APK_MIRROR` | `server/Dockerfile`；`docker-compose.yml` |

---

## 1. 附件存储与访问模型

### 1.1 为什么改

改造前 `/uploads` 是匿名公开的 `StaticFS`：任何人拿到或猜到路径就能下载，Web 服务器还会列出目录；
响应还带了 `public, max-age=31536000, immutable`，私人图片会长期留在代理/浏览器缓存里。
历史消息里的 URL 不带签名，直接改成“签名 URL”会导致全部旧图片 404，所以选择了**登录态 + 同源 cookie** 的方案。

### 1.2 三条访问路径

| 场景 | 凭据 | 说明 |
|---|---|---|
| 前端 API 请求 | `Authorization: Bearer <JWT>` | `middleware.Auth` **只认** 头，不认 cookie，避免引入 CSRF 面 |
| 浏览器加载 `<img>` / `<video>` / 下载 | 同源 `mc_auth` cookie | 浏览器无法给子资源加 Authorization 头，只能靠 cookie |
| 下载器 / 旧客户端 | `Authorization` 头 | `UploadAuth` 先看头，再看 cookie，两种都支持 |

`UploadAuth`（`middleware/jwt.go`）的判定顺序：`Authorization: Bearer` → `mc_auth` cookie → 401。
解析 JWT 后还会校验 `token_version` 与封禁状态，因此**旧 cookie 在密码重置后会立刻 401**。

### 1.3 cookie 的下发、属性与失效

Cookie 名 `mc_auth`，与 localStorage 里的 JWT **是同一个字符串**（`middleware.AuthCookieName`）。

下发时机：

1. 登录 / 注册成功响应（`handler/auth.go`）；
2. WebSocket 握手成功（`ws/serve.go` `Serve`，升级前 `SetAuthCookie`）；
3. 任何带 `Authorization` 的已登录 REST 请求（`middleware.Auth` 里 `setAuthCookie`，cookie 相同则跳过）；
4. 用 `Authorization` 访问 `/uploads` 时（`UploadAuth` 补发）。

属性（`middleware.SetAuthCookie`）：

| 属性 | 值 | 原因 |
|---|---|---|
| `HttpOnly` | true | 插件/脚本读不到，降低 XSS 窃取面 |
| `SameSite` | `Lax` | 允许同站子资源携带，跨站 POST 不带 |
| `Path` | `/` | `/uploads`、`/ws` 都能用 |
| `MaxAge` | 7 天（与 `middleware.TokenTTL` 一致） | 与 JWT 生命周期对齐 |
| `Secure` | TLS 或 `X-Forwarded-Proto: https` 时为 true | HTTP 部署不会因为强制 Secure 拿不到 cookie |

失效规则：

- **JWT 的 `tv` 与库中 `token_version` 不一致 → 401**（REST、上传、WS 都会查）；
- 浏览器里的 cookie **不会自动消失**，只是校验失败；真正清除靠 `POST /api/auth/logout`（`ClearAuthCookie`，`MaxAge=-1`）；
- 只清 localStorage 不清 cookie 的话，退出后 7 天内同浏览器仍能直接打开 `/uploads`——这是新增 logout 接口的直接原因。

### 1.4 目录列举与缓存

- `onlyFilesFS.Open` 对目录直接返回错误，`http.FileServer` 的目录列表页变成 404（`router.go`）。
- 响应头固定 `Cache-Control: private, max-age=3600` + `X-Content-Type-Options: nosniff`。
  文件名是随机不可变的，短私有缓存能复用，但不会进入共享代理缓存。
- 维护模式与 API 一致：非管理员返回 `503 {"error":"站点维护中","maintenance":true}`，管理员仍可访问附件。

### 1.5 S3 / 对象存储模式的边界

静态服务**始终挂载**（`router.go` 注释：切到 S3 后历史 `/uploads/...` 仍要能打开）。
新上传文件返回 `publicBase + key` 的绝对 URL，**不经过 `UploadAuth`**。因此：

- 访问控制下沉到桶策略 / CDN：桶必须私有并配合签名 URL，或明确接受“公开可读”；
- 服务端不会为 S3 对象设置 ACL，也不会生成签名；
- 附件 GC 只扫描本地 `DATA_DIR/uploads`，**S3 上的孤儿对象不在清理范围内**，需要额外的生命周期策略；
- `S3Storage.Owns` 同时接受 `publicBase` 前缀与历史 `/uploads/` 前缀，保证切换存储后历史文件仍可转发/引用。

### 1.6 跨域部署的限制

`SameSite=Lax` 意味着：**前端与 API 不同 site 时，浏览器加载 `<img src="https://api.example.com/uploads/...">` 不会带 cookie**，结果 401。

- 推荐：前端与 API 同域（nginx 反代 `/api`、`/ws`、`/uploads`），cookie 天然同源；
- 同一主域的不同子域（`app.example.com` 与 `api.example.com`）仍属 same-site，Lax 会携带；
- 完全跨站（`app.com` 调 `api.other.com`）只能改用 S3/CDN 公开域名，或由前端带 `Authorization` 拉 blob；
- 跨域还会影响 WS：cookie 回退在跨站场景同样不生效，此时仍需 `?token=`。

### 1.7 已知边界

- **没有逐会话成员校验**：只要登录且知道 URL 就能读取，URL 本身是 128 位随机名，但不是授权模型。要做得更细需要“文件 → 会话/消息”归属表。
- 未登录一律 401，包括公开群的历史图片；这是有意的取舍。
- 没有角色级豁免：登录用户（包括管理员）都按同一规则读取，管理员不会因为身份获得额外文件权限。

---

## 2. 认证与登出

### 2.1 cookie 与 JWT 的关系

- JWT（HS256，claims `uid` / `tv` / `iat` / `exp`）是唯一身份凭据，`TokenTTL = 7 天`；
- cookie 只是同一 JWT 的副本，用于浏览器无法设置请求头的场景；
- REST 仍只认 `Authorization`，cookie **不能**直接调 API（防 CSRF）；
- 前端登出必须同时丢弃 localStorage token 并调用 `POST /api/auth/logout`。

### 2.2 `POST /api/auth/logout`

- 免鉴权、维护模式下也可用（否则维护时用户无法退出）；
- 只做 `ClearAuthCookie`，返回 `{ "ok": true }`；
- **不 bump `token_version`，也不踢 WS**：已泄露的 Bearer token 在 7 天有效期内仍可用；
- 前端 `auth.logout` 使用 `fetch(..., { method: 'POST', keepalive: true })`，页面跳转也不会中断请求。

需要“立即吊销”时使用下一节的“退出所有设备”。

### 2.3 `token_version` 的失效语义

- 登录/注册时把用户当前 `token_version` 写进 JWT 的 `tv`；
- 每次鉴权都查库比较 `u.TokenVersion == cl.TV`，不一致即 401；
- 触发 bump 的场景：
  - “退出所有设备”（`UserStore.BumpTokenVersion`）；
  - 邮箱令牌重置密码（`handler/auth.go` `Reset` 在同一事务里 `u.TokenVersion++`）；
  - 管理员封禁 / 解封与随机重置密码（`handler/admin.go` `TokenVersion++`）；
- 管理员改角色**不** bump（`handler/admin.go` 注释：鉴权中间件每次查库，角色变更立即生效）；
- 语义是“全端失效”，没有单会话 ID，无法只踢某一台设备。

### 2.4 “退出所有设备”为什么要踢 WS

只 bump `token_version` 不会影响**已经建立**的 WebSocket：连接只缓存握手时的 `tv`。
因此 `LogoutOthers` 在 bump 之后显式调用 `notifyFor(c).KickUser(uid, ...)`：

- 向该用户所有分片连接发送 `{"type":"kicked","reason":"已退出所有设备，请重新登录"}`，然后关闭；
- `KickUser` 使用非阻塞 `trySend`，慢消费者不会卡住踢线；
- 加上 `Dispatch` 的逐帧复核，形成双保险：即使 `kicked` 帧被忽略，下一次上行也会被拒绝并断开。

注意：`LogoutOthers` 会使**当前设备**也失效（当前 token 的 tv 变旧），前端随后应清本地 token 并回登录页。

---

## 3. 实时通道约束

### 3.1 握手鉴权

`ws/serve.go` `Serve`：

1. 优先 `?token=`（兼容现有前端，不改握手契约）；
2. 为空时回退 `mc_auth` cookie；
3. `ParseToken` → 查用户 → `u.TokenVersion == cl.TV` → `IsBanned` → 维护模式（非管理员 503）；
4. 成功后 `SetAuthCookie` 回授 cookie，随后才 `Upgrade`。

### 3.2 每一帧复核连接状态

`ws/dispatch.go` `Dispatch` 在 `json.Unmarshal` 之后、进入 `switch` 之前统一调用 `checkConnState(c, f.ConvID)`：

- 用户被删除 / 封禁 / `token_version` 变化 → `error` 帧 + `denyConn` 断开；
- 覆盖 `ping` / `typing` / `read` / `recall` / `edit` / `reaction` / `delivered` / `chat` **所有帧**；
- `deps.Users == nil`（单测或未注入）时跳过复核，保持兼容。

这是本轮修复的重点：此前只有 `chat` 复核，封禁用户只要不发消息就能继续 `typing/read`，甚至靠 `edit/recall` 影响数据。

### 3.3 token 传递方式与日志风险

- `?token=` 仍保留：URL 会进入 nginx / 反向代理 / 应用访问日志，**等于把 7 天有效的 JWT 写进日志**；
- cookie 回退是给“前端后续改造”留的兼容路径，当前前端仍走 query；
- 建议：
  - nginx 对 `/ws` 关闭或脱敏 `access_log`（运维手册检查清单已列出）；
  - 前端迁移到“不带 token 的握手 + 同源 cookie”（需同域部署）；
  - 日志采集侧增加 `token=` 脱敏规则；
  - 不要把完整 URL 打到应用日志。

### 3.4 帧大小与限流

- 读限制 64 KB（此前 4 KB 会误伤 5000 字符长消息，UTF-8 下超过 4 KB）；
- 发消息每用户 20 条/分（`MsgLimiter`），超限回 `error` 帧而不是断连。

---

## 4. 插件安全模型

### 4.1 隔离边界

插件运行在 `iframe sandbox="allow-scripts"`（`usePluginHost.ts` `mountRuntime`）：

- **没有 `allow-same-origin`**：iframe 的 origin 是 opaque（`null`），拿不到父页面 DOM、localStorage、JWT、cookie；
- 所有能力只能通过 `postMessage` 桥接，宿主按 `API_PERMISSION` 做权限过滤；
- 插件互相隔离，各自独立 iframe 与 storage 命名空间。

### 4.2 为什么用 `srcdoc`

没有 `allow-same-origin` 时，父页面无权访问 `iframe.contentWindow.document`，`document.open/write` 会抛 `SecurityError`，插件永远不启动。
因此必须用 `iframe.srcdoc = bootHtml(...)`，在挂载前写入引导脚本；这不会改变 opaque origin 的隔离属性。

`bootHtml` 还会转义插件代码里的 `</script` 与 `<script`，防止在 HTML 解析阶段提前闭合引导脚本。

### 4.3 CSP 策略

`buildSandboxCsp` 生成：

```
default-src 'none';
script-src 'unsafe-inline';
style-src 'unsafe-inline';
img-src data:;
connect-src <allowedHosts 或 'none'>;
base-uri 'none'; form-action 'none'; frame-src 'none'; object-src 'none'; worker-src 'none'
```

要点：

- `default-src 'none'`：外部图片、字体、媒体默认全部禁止，UI 描述里的 `image` 只接受 http(s) 由宿主渲染；
- `script-src 'unsafe-inline'` 是引导脚本内联所必需，但插件代码本身也在这段内联脚本里，**不要把它理解成“脚本可信”**；
- `connect-src` 把 `fetch` / XHR / WebSocket / beacon 收敛到 manifest 的 `allowedHosts`；未声明则 `'none'`；
- 主机名只接受 `example.com` 或 `*.example.com`，端口/路径/引号会被过滤，避免拼进 CSP 指令。

### 4.4 权限与事件过滤

`API_PERMISSION` 把每个 SDK 方法映射到权限：

| 权限 | 能力 |
|---|---|
| `message:read` | `api.on(...)` 订阅事件 |
| `message:send` | `api.send(...)` |
| `message:render` | `api.registerMessageRenderer(...)` |
| `ui:panel` | `api.addSidePanel(...)` |
| `ui:actions` | `api.registerMessageAction(...)` |
| `conv:read` | `api.me()` / `api.conv.current()` / `api.conv.members()` / `api.messages.recent()` |
| `ui:host` | `api.nav.*` / `api.toast()` |
| `storage` | `api.storage.get/set`（每插件命名空间，值 ≤64KB） |
| `net` | `api.request()`（还受 `allowedHosts` 限制） |
| `ui:theme` | `api.theme.apply()`（仅 `--td-*` 令牌） |

事件分发额外按权限过滤（`EVENT_PERMISSION`）：

- `message:new` / `message:recall` / `presence` 的 payload 含消息正文或用户列表，只发给声明 `message:read` 的插件；
- `conv:open` / `ui:action` 不含隐私正文，不额外限制；
- `message:action` 只投递给**注册了该 action 的插件**，不广播；
- `messages.recent` 只返回已落库、非 pending/failed 的可见消息，撤回消息的 `content` 置空，避免把撤回原文交给插件。

> 注意：`ui:actions` 的右键菜单回调里，如果插件**没有** `message:read`，宿主只提供 `messageMeta`（id/convId/senderId/msgType/createdAt/isRecalled），不提供正文。这是刻意设计，不是 bug。

### 4.5 `net` 与 `allowed_hosts` 的强制方式

两层：

1. **服务端 manifest 校验**（`server/internal/plugin/manifest.go`）：`allowedHosts` ≤20 项，正则只接受主机名/`*.` 子域；
2. **前端执行**（`usePluginHost.ts` `request`）：
   - 只允许 `http:` / `https:`；
   - `hostAllowed(hostname, allowedHosts)` 精确匹配或 `*.` 后缀匹配（`*.example.com` 也允许裸 `example.com`）；
   - 不满足直接返回 `host not allowed`，不会发出请求。

注意：`allowedHosts` 同时决定 CSP `connect-src`。**CSP 只按域名放行，不区分是否声明 `net`**；如果插件写了 `allowedHosts` 却没写 `net`，它无法通过 `api.request` 发请求，但理论上仍可在 iframe 内裸 `fetch` 这些域名（还受目标站点 CORS 限制）。审计插件时不要只看 `net`，要同时看 `allowedHosts`。

### 4.6 必须写明：裸 `fetch` 已失效

> **依赖裸 `fetch` 的插件已失效，必须改用 `mc.request`。**
>
> 仓库里注入的 SDK 对象名是 `api`，所以代码写法是 `api.request(...)`（`McApi` 类型）；文档和口头交流里的 `mc.request` 指的就是这个方法。
> 裸 `fetch` 失效的原因：iframe 是 opaque origin（`Origin: null`），目标站点通常不会在 CORS 里放行 `null`；同时 CSP `connect-src` 只允许 `allowedHosts`，未声明时是 `'none'`。
> `api.request` 由宿主页面代发，受同样的 CORS 与 `allowedHosts` 双重约束，但至少能带上宿主页面的真实 Origin。

### 4.7 其他约束

- 插件体积 ≤1 MB；`.ts` 上传由服务端 esbuild 去类型，不解析 `import`；
- URL 安装时 `plugin.Fetch` 在自定义 `DialContext` 内完成 DNS 解析与拨号，防 DNS rebinding，并禁用环境代理；
- 站长可全局下架/强制卸载，立即对所有用户生效。

---

## 5. 上传链路

### 5.1 类型嗅探与“写回 512 字节”

`service/storage.go` 的 `LocalStorage.SaveStream` / `S3Storage.SaveStream`：

1. `io.ReadFull` 读前 512 字节到 `head`；
2. `sniffMime` 去掉 charset；`extFor(kind, mime, head)` 按 kind 白名单决定扩展名；
3. **`tmp.Write(head)` 把头写回临时文件**；
4. `io.Copy(tmp, io.LimitReader(r, maxBytes+1-hn))`，`total = hn + n` 判超限。

这是本轮最严重的修复之一：此前只写剩余部分，**所有流式上传都丢前 512 字节**，图片打不开、语音无声音、视频只剩后半段；S3 分支的 `PutObject` 大小也用的是剩余长度。

### 5.2 限额

| 类型 | 上限 | 来源 |
|---|---|---|
| 图片 / 音频 / 群文件 / 视频 | 后台 `MaxImageMB` / `MaxAudioMB` / `MaxFileMB` / `MaxVideoMB` | `handler/upload.go` `limits` |
| 表情 | 2 MB（`MaxStickerBytes`） | `service/storage.go` |
| 请求体 | `maxBytes + 1 MB`（multipart 边框开销） | `http.MaxBytesReader` |

超限返回 413“文件过大（kind 上限 N MB）”；`MultipartReader` 因 `MaxBytesReader` 失败时也会优先识别为超限，而不是误报“缺少文件字段”。

### 5.3 允许的类型

- 图片：jpeg / png / webp / gif；
- 音频：webm / ogg / mpeg / mp4(m4a) / wav；
- 视频：mp4 / webm / quicktime(mov) / x-m4v；
- 群文件：不按 MIME 白名单，仅限大小（仍会嗅探但允许任意二进制）。

音频有一个兼容兜底：浏览器 `MediaRecorder` 录的音频常被 `http.DetectContentType` 判成 `video/webm`、`application/ogg` 或 `video/mp4`，`extFor` 在 `kind=audio` 时按容器特征 `guessAudioExt` 识别，避免合法语音被拒。

### 5.4 缩略图安全限制

`service/thumbnail.go`：

- 先用 `image.DecodeConfig` **只读头部**，像素总数超过 4000 万（约 6325×6325）直接跳过缩略图，前端回退原图；
- 整条生成路径带 `recover`，畸形图片触发解码器 panic 时只记日志，不影响上传；
- 缩略图名形如 `xxx_t.jpg`，与原文件同目录。

### 5.5 附件 GC 的判定规则

`handler/ops.go`：

- `referencedUploads` 收集五类引用：消息 `content LIKE '/uploads/%'`、表情 `url`、群文件 `url`、群头像、用户头像；
- **不按 `msg_type` 枚举**：任何 content 以上传路径开头的消息都算引用，新增消息类型不会漏；
- `addUploadRefs` 把 `xxx_t.jpg` 派生缩略图也标记为已引用，跟随原文件生命周期；
- `orphanMinAge = 24h`：刚上传但还没发消息的文件不会被误删；
- 只有本地 `DATA_DIR/uploads` 会被扫描；S3 对象不在范围内；
- `dryRun` 与真实删除共用 `gcOrphans`，先 dryRun 看数量和字节数再执行。

---

## 6. 上线与迁移

### 6.1 执行机制

- `server/internal/store/migrations.go` `RunMigrations` 使用 `golang-migrate`，源为镜像/工作目录下的 `migrations/`；
- `MIGRATE=true` 时应用启动自动 `Up()`；默认 `false`，需要手工执行；
- 版本记录在 `schema_migrations` 表；
- 迁移文件成对出现：`NNNN_name.up.sql` / `.down.sql`。

### 6.2 0023：消息正文扩为 `TEXT`

| 项 | 内容 |
|---|---|
| 影响 | `messages.content` 从 `VARCHAR(1000)` 改为 `TEXT`；后台可把单条消息上限调到 5000 字符，多字节字符约 15KB，远低于 TEXT 上限 |
| 风险 | 表大时 `ALTER` 可能锁表/耗时；`VARCHAR → TEXT` 是否支持 `ALGORITHM=INSTANT` 取决于 MySQL 版本与列定义，**不要假设一定不锁表**，应在低峰执行并观察 `SHOW PROCESSLIST` |
| 回滚 | `ALTER COLUMN content VARCHAR(1000)`；**超过 1000 字符的消息会被截断或导致 ALTER 失败** |
| 备份 | 回滚前必须备份 `messages`（至少备份超长行） |

### 6.3 0024：单聊 `direct_key` 唯一键

| 项 | 内容 |
|---|---|
| 影响 | `conversations` 新增 `direct_key`（`小ID:大ID`）唯一索引；`EnsureSingle` 把查重/建会话/加成员/初始化收件箱包进一个事务 |
| 存量 | 存量单聊 `direct_key` 保持 `NULL`（MySQL 唯一索引允许多个 NULL），不影响现有数据；只有新会话写值 |
| 风险 | 并发插入只有一个成功，失败方回滚后复用已有会话；不会产生重复单聊 |
| 回滚 | 先 `DROP INDEX idx_conversations_direct_key`，再 `DROP COLUMN direct_key`；回滚后并发重复问题会回归 |
| 备份 | 涉及 `conversations` / `conversation_members` / `user_conversations`，建议整库备份 |

### 6.4 0025：好友 / 验证码唯一索引（含重复数据清理）

| 项 | 内容 |
|---|---|
| 影响 | `friends (user_id, friend_id)` 唯一索引 `uk_friends_pair`；`email_codes (email, purpose)` 唯一索引 `uk_email_codes_email_purpose` |
| 数据清理 | 迁移先清理同方向重复好友行：保留优先级 **已通过 > 待处理 > 已拒绝**，同优先级保留 `id` 最小的一条；验证码侧同 `(email, purpose)` 只保留 `id` 最大（最新）的一条，发码在运行时改为行锁 + 原地重置 |
| 风险 | 清理是**破坏性**的，被删除的重复行不会恢复；同意好友的反向记录改为 upsert，避免互发申请撞索引 |
| 回滚 | 只删除两个索引；已清理的脏数据不会恢复 |
| 备份 | **必须备份** `friends` 与 `email_codes`，这是三支迁移里唯一会删数据的 |

### 6.5 推荐顺序

1. 停写或低峰窗口，`mysqldump` 备份相关表（0025 建议整库）；
2. 先执行 0023（列扩容，风险最低）；
3. 再执行 0024（加列 + 唯一索引）；
4. 最后执行 0025（含清理，风险最高）；
5. 启动新版本（`MIGRATE=true` 会自动按序执行；手工执行时按 0023 → 0024 → 0025）；
6. 验证：
   - 发一条接近 5000 字符的消息；
   - 双方同时发起单聊，确认只产生一个会话；
   - 并发发好友申请，确认 409 且无重复行；
   - `/api/version` 返回正确版本。

> 不要在生产库上跑 `go test -tags integration ./test/`：集成测试会先 DELETE 全部业务表。

### 6.6 回滚顺序

与升级相反：0025 → 0024 → 0023。0023 回滚前先检查是否存在 `CHAR_LENGTH(content) > 1000` 的行，否则 `ALTER` 会失败或截断。

---

## 7. 部署

### 7.1 Docker 版本注入

`server/Dockerfile` 构建阶段通过 ldflags 注入：

| build arg | 作用 | 未传时 |
|---|---|---|
| `VERSION` | 版本号 | `dev` |
| `COMMIT` | 提交号 | `unknown` |
| `BUILD_TIME` | 构建时间 | `unknown` |

`docker-compose.yml` 已透传这三个参数；`/api/version` 读取的就是这些值。构建上下文没有 `.git` 也能工作，不需要为了版本号挂载仓库元数据。

### 7.2 镜像源可配置参数

| build arg | 覆盖 | 默认 |
|---|---|---|
| `GO_IMAGE` | Go 构建镜像 | 官方 `golang` |
| `RUNTIME_IMAGE` | 运行镜像 | 官方 Alpine |
| `GOPROXY` | Go module 代理 | 官方代理 |
| `APK_MIRROR` | Alpine 软件源 | 官方源 |

默认值全部是官方源，不传参时行为与之前一致。第三方镜像/代理需自行确认可信度；国内网络示例见 [README.md](../README.md) 与 [运维手册.md](运维手册.md)。

### 7.3 容器内已验证的行为

- 启动时若 `MIGRATE=true`，会自动执行镜像内 `migrations/` 到最新；
- `/api/health` 可用于 compose healthcheck；
- `/uploads` 静态服务在容器内正常工作，但**默认 compose 没有为 `DATA_DIR` 挂载卷**：容器重建后本地附件会丢失。生产部署必须给 `DATA_DIR`（默认 `data`）挂持久卷，或改用 S3；
- 反向代理需保留 `X-Forwarded-Proto`，否则 HTTPS 部署下 `mc_auth` 不会带 `Secure`（虽仍可用，但安全性下降）。

### 7.4 常见部署坑

- 前端静态资源与 API 不同域：`mc_auth` 的 `SameSite=Lax` 会导致 `/uploads` 401，见 §1.6；
- 只挂 `nginx` 不挂 `DATA_DIR`：附件丢失；
- 用 `docker compose up -d` 重建容器但没重建镜像：`VERSION` 仍是旧值，`/api/version` 不会变；
- 维护模式：`/uploads` 与 WS 都受控，非管理员无法建连，不要误判为“服务挂了”。

---

## 8. 代码对照表

| 行为 | 文件 | 关键符号 |
|---|---|---|
| `/uploads` 路由、缓存、目录拒绝 | `server/internal/router/router.go` | `serveUpload`、`onlyFilesFS` |
| cookie 下发/清除、REST/上传鉴权 | `server/internal/middleware/jwt.go` | `AuthCookieName`、`SetAuthCookie`、`ClearAuthCookie`、`Auth`、`UploadAuth` |
| 登出接口 | `server/internal/handler/auth.go` | `Logout` |
| 退出所有设备 | `server/internal/handler/user.go` | `LogoutOthers` |
| WS 握手与 cookie 回退 | `server/internal/ws/serve.go` | `Serve` |
| 逐帧复核 | `server/internal/ws/dispatch.go` | `Dispatch`、`checkConnState` |
| 踢线 | `server/internal/ws/hub.go` | `KickUser` |
| 插件 manifest / 权限 | `server/internal/plugin/manifest.go` | `Manifest`、`ValidPermissions` |
| 插件 URL 安装防 SSRF | `server/internal/plugin/fetch.go` | `Fetch` |
| 插件沙箱 / CSP / 权限过滤 | `web/src/composables/usePluginHost.ts` | `mountRuntime`、`buildSandboxCsp`、`bootHtml`、`API_PERMISSION`、`EVENT_PERMISSION`、`hostAllowed` |
| 上传限额与流式保存 | `server/internal/handler/upload.go`、`server/internal/service/storage.go` | `Upload`、`SaveStream`、`sniffMime`、`extFor` |
| 缩略图 | `server/internal/service/thumbnail.go` | 解码尺寸上限 |
| 附件 GC | `server/internal/handler/ops.go` | `referencedUploads`、`addUploadRefs`、`gcOrphans` |
| 迁移执行 | `server/internal/store/migrations.go` | `RunMigrations` |
| Docker 参数 | `server/Dockerfile`、`docker-compose.yml` | `GO_IMAGE` / `RUNTIME_IMAGE` / `GOPROXY` / `APK_MIRROR` / `VERSION` / `COMMIT` / `BUILD_TIME` |

---

## 9. 容易踩的坑

1. **以为登出就吊销了 token**：`/api/auth/logout` 只清 cookie；真正吊销要用“退出所有设备”。
2. **以为 Cookie 会自动过期**：它只是 7 天副本；密码重置后旧 cookie 会 401，但浏览器仍保留直到 logout 或自然过期。
3. **修改 API 后忘了重新编译后端**：本地运行的是 `bin/modernchat-server.exe`，改 Go 源码后必须重新 `go build -o ../bin/modernchat-server.exe ./cmd/server` 并重启。
4. **在 S3 模式下依赖 `/uploads` 鉴权**：S3 URL 不经过 `UploadAuth`，访问控制由桶/CDN 负责。
5. **插件里继续用 `fetch`**：已失效，改 `mc.request` / `api.request`；同时确认 manifest 声明了 `net` 和 `allowedHosts`。
6. **把 `allowedHosts` 当成 `net` 的替代**：CSP 只按域名放行，权限仍需 `net`。
7. **上传后只写剩余流**：任何“先读头再转发”的实现都必须把头写回，否则文件前 512 字节丢失。
8. **GC 按消息类型枚举**：新增消息类型会漏判，必须按 `/uploads/` 前缀收集。
9. **直接跑集成测试**：会清空业务表，只能对独立测试库执行。
10. **0025 不备份**：它会删重复好友/验证码行，回滚无法恢复数据。
