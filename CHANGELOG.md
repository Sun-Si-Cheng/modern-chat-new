# 更新日志

本文件记录 ModernChat 的重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

> 项目当前处于 `0.x` 阶段，`0.MINOR.PATCH` 中 MINOR 递增表示可能不兼容的变更。

---

## [未发布]

### 修复

- **密码重置令牌 TOCTOU、验证码尝试计数非原子（中等）**：`PasswordResetStore.Use` 只做 `Update("used", true)`，不校验令牌是否仍有效，两个并发请求拿同一链接都能改密码；邮箱验证码失败次数是「读-改-写」，并发错误提交会丢计数、5 次上限可被绕过。现在令牌核销改为事务内条件更新（`used=false AND expires_at>now`），`RowsAffected=0` 返回 `ErrResetTokenUsed`，接口回 400「重置链接已失效」；验证码改为单条原子 UPDATE（`attempts = attempts + 1`，达到上限同时写 `used_at`）。
- **Web Push 无并发上限、无超时、endpoint 可任意填（中等）**：`webpush-go` 默认使用无超时的 `http.Client`，端点不响应会长期占用连接；大群群发时每个收件人一个 goroutine、无限并发外连；订阅接口只检查了 `https://` 前缀，endpoint 可被填成内网地址（服务端 SSRF）。现在：① 推送客户端固定 10s 超时；② 全局最多 16 个并发推送，满了直接丢弃（消息本身已在库中）；③ endpoint 必须是 https、无 userinfo、域名属于主流浏览器推送网关（FCM / Mozilla / WNS / Apple），订阅接口与发送侧共用同一校验。**注意**：自建/私有推送网关不在白名单内，如需支持需在 `service.pushEndpointHosts` 增加域名。
- **发消息多步写入无事务、失败仍广播（中等）**：`Create / UpsertInbox / BumpUnread / MarkMention` 各用独立连接，失败只 `slog` 后继续 fan-out，会造成「消息已入库但收件箱 last/未读没更新」「客户端收到消息却对不上库」。现在 `MessageStore.CreateWithInbox` 用一个事务提交全部四步，任一步失败整体回滚并通过 error 帧告知客户端，不再广播。
- **并发创建单聊会生成重复会话（中等）**：单聊创建原为「查重 → 建会话 → 加成员 → 初始化收件箱」四步且无事务、无唯一约束，双击或双方同时发起时可能各建一个会话，之后消息/未读分裂到两条会话。现在新增 `conversations.direct_key`（`小ID:大ID`）唯一索引，`EnsureSingle` 把查重、建会话、加成员、收件箱初始化包进同一事务；并发插入时只有一个成功，失败方回滚后复用已有会话。**升级需执行迁移 0024**；存量单聊 `direct_key` 保持 NULL（不影响现有数据，新会话才开始写入）。
- **插件 URL 安装的 SSRF DNS rebinding（中等）**：`plugin.Fetch` 先 `LookupIP` 校验、再交给 `http.Client` 重新解析并连接，攻击者可让两次解析返回不同地址（校验时公网、连接时内网）。现在校验与连接都在自定义 `DialContext` 内完成：解析后直接拨号已校验的 IP，重定向的每一跳同样受控；并显式禁用环境代理。保留段补上 `100.64.0.0/10`（CGNAT）、`192.0.0.0/24`、`198.18.0.0/15`、`240.0.0.0/4`、文档网段、NAT64 `64:ff9b::/96` 及 IPv4-mapped 地址。
- **「退出所有设备」不踢已连 WS 连接，且多数上行帧不复核连接状态（中等）**：`LogoutOthers` 只把 `token_version+1`，已建立的 WebSocket 仍缓存旧 tv，可继续收发；`Dispatch` 里也只有 `chat` 帧复核封禁/tv，`typing/read/recall/edit/reaction/delivered/ping` 全部绕过。现在：①「退出所有设备」与邮箱重置密码成功后主动 `KickUser` 断开全部连接；②`Dispatch` 在路由任何帧之前统一复核封禁与 `token_version`，失败回 error 帧并断开。握手/接口契约不变（帧格式不变，仅新增 `kicked` 断开行为）。
- **单条消息长度上限与列宽不一致（中等）**：后台允许把「单条消息长度」调到 5000 字符，但 `messages.content` 是 `VARCHAR(1000)`——严格模式下发长消息报 1406、非严格模式静默截断。迁移 `0023` 把列扩为 `TEXT`（5000 个多字节字符约 15KB，远低于 64KB 上限），GORM 模型同步改为 `type:text`。**升级需执行迁移 0023**，回滚为 `VARCHAR(1000)` 会截断超长消息，回滚前先备份。
- **`/uploads` 未鉴权、可列目录、公开缓存一年（严重）**：上传目录原先对所有匿名请求开放，Gin 的 `StaticFS` 还会列出目录内容，私人图片/语音/视频可被枚举；响应携带 `public, max-age=31536000, immutable`，会长期留在代理/共享缓存。现在：① 目录一律 404（`onlyFilesFS`）；② 缓存改为 `private, max-age=3600`；③ 读取必须登录——`Auth` 中间件在已认证响应里顺带下发同源 `HttpOnly` cookie，`/uploads` 同时接受 `Authorization` 与 cookie，历史消息里的旧 URL 无需迁移即可继续打开；④ 维护模式下与 API 一样受控（管理员可访问）。**取舍与限制**：未做逐会话成员校验（文件 URL 不含会话信息，需新增文件归属映射表），登录用户知道 URL 仍可读取；使用 S3 对象存储时，文件由桶策略/CDN 控制，不在本中间件范围内。
- **附件 GC 误删视频与缩略图（严重）**：运维「清理孤儿附件」只把 `image/audio/sticker` 消息算作引用，前端已支持发送视频，导致仍在使用的视频被批量删除；图片派生缩略图（`xxx_t.jpg`）也会被单独判成孤儿。现改为收集所有 `content` 以 `/uploads/` 开头的消息（不再按 `msg_type` 枚举，后续新增消息类型不会漏），并让缩略图跟随原文件生命周期；补 dryRun / 真实删除两种模式的回归测试。
- **所有流式上传丢前 512 字节（严重）**：`LocalStorage.SaveStream` / `S3Storage.SaveStream` 用 `io.ReadFull` 读文件头做类型嗅探后，只把剩余内容写入临时文件，前 512 字节从未写回；S3 分支的 `PutObject` 大小也用的是剩余长度。结果是图片打不开、语音无声音、视频只剩后半段（小于 512 字节的文件甚至直接失败）。现已写回头部、上传长度改用含头总长，并补本地与 S3 分支的逐字节 round-trip 回归测试。
- **昵称/头像实时同步未生效**：服务端一直广播 `user_change` 帧，前端 `ServerFrame` 未声明该类型也没有处理分支，导致对方改了昵称/头像后必须刷新页面才更新。现由 `stores/ws.ts` 统一分发，更新单聊 peer、群成员缓存、好友/申请列表与本人多端。
- **自己发出的消息不更新会话列表**：发送者本端收不到「纯新消息」的 `conv_change`，消息回显分支漏写 `lastMessageId/At/Preview/Type`，列表预览、时间与排序会停在旧消息；同时移除「发送即记已读水位」的错误优化，避免把消息转发到仍有未读的会话后，服务端未读数永远清不掉。
- **WS 上行 4 KB 限制误伤长消息**：`chat.maxMessageLength` 可配到 5000 字符（UTF-8 下超过 4 KB），原 `maxMsgSize=4096` 会让合法长消息在 gorilla 读限制处直接断连且收不到 `error` 帧；上限改为 64 KB。
- **新成员加入群后列表显示「暂无消息」**：`initInboxes` 只插入空收件箱行，没有同步当前最新消息；而打开会话又能看到历史。现在主动加入 / 被邀请 / 注册自动进公共群时都写入最新消息预览与时间，并把 `last_read` 推到最新（不追溯历史未读）。
- **`FRONTEND_URL` 全为空白项时启动后找回密码 panic**：形如 `FRONTEND_URL=","` 或全空格时解析结果为空，WS Origin 白名单拒绝所有浏览器来源，且 `Forgot` 取 `FrontendURLs[0]` 会越界。现在启动阶段直接报错并提示至少配置一个来源（附单元测试）。
- **发版版本号漂移**：`web/package.json` / `web/package-lock.json` 停在 `0.10.2`，官网 `website/package.json`、`package-lock.json` 与首页 Hero 徽标停在 `0.9.0`，与 CHANGELOG 已定版的 `0.10.3` 不一致。已统一对齐为 `0.10.3`；新增 `scripts/check-version.sh`（以 CHANGELOG 最新正式版本为基准核对各包版本与官网徽标，并提示最新 git tag），发版流程说明已补进 README 与运维手册。
- **Docker 镜像版本号恒为 `dev`**：`server/Dockerfile` 构建时只传 `-s -w`，未注入 ldflags，`docker compose` 下 `/api/version` 永远返回 `dev`。现新增 `VERSION` / `COMMIT` / `BUILD_TIME` 三个 build arg（`docker-compose.yml` 已接好，未设置时回退 `dev` / `unknown`），构建上下文没有 `.git` 也能按需传入，不需要为了版本号挂载仓库元数据。
- **插件沙箱在 Chrome 中永不启动（严重）**：`mountRuntime` 用 `contentWindow.document.open/write` 向 `sandbox="allow-scripts"`（无 `allow-same-origin`）的 iframe 写文档，父页对该窗口的文档写入被 `SecurityError` 拒绝，插件代码从未执行——`__mcActivate` 不触发，面板、消息渲染器、侧栏与消息动作全部失效。现改用 `iframe.srcdoc = bootHtml(code)` 注入，**保留无 `allow-same-origin` 的隔离**（插件仍读不到 JWT 与 localStorage）；`ev.source !== iframe.contentWindow` 的来源校验在 `srcdoc` 下继续有效。已用最小插件桩实测：`__mcActivate` 正常执行、双向 `postMessage`、事件分发与 RPC 全部通过。
- **沙箱启动后暴露的权限越界（中等）**：`message:new` / `message:recall` / `presence` 广播未做接收方过滤，只声明 `ui:actions` 的插件也能收到整条消息正文；现按 `permissions.includes('message:read')` 过滤，`runMessageAction` 对无 `message:read` 的插件只下发消息元信息（id / 会话 / 发送者 / 类型 / 时间 / 撤回态）。
- **沙箱无 CSP，插件可裸 `fetch` 绕过 `allowed_hosts`（中等）**：启动 HTML 注入 `Content-Security-Policy`（`default-src 'none'`、脚本/样式仅内联、`connect-src` 只含 `net` 权限声明的主机、禁用 frame/object/worker/form），`net` 权限的主机白名单这才真正生效。
- **实时消息让会话历史不再拉取（中等）**：以 `messagesByConv[convId]` 是否存在判断「已加载首屏」，未打开的会话收到 WS 消息也会建数组，点开时跳过历史拉取（表现为「历史没了」）。改用独立 `loadedFirstPageByConv` 标记区分「已完整加载」与「仅有增量」。
- **首屏 REST 结果覆盖并发 WS 消息（中等）**：首屏返回直接整表赋值，`await` 期间到达的新消息会被覆盖直到重开会话；现首屏也走 `mergeServerPage` 合并。
- **断线补齐顺序错乱（中等）**：`catchUp` 补齐的缺口消息 id 小于已加载最新页，直接 append 会把旧消息排到最新消息之后；合并后统一按 id 归并排序（pending 固定在末尾），积压超一页也不会错序。
- **`fetchMe()` 失败导致聊天页半死（中等）**：非 401 失败（断网 / 502 / 超时）直接在 `onMounted` 抛出，会话列表、通知监听与插件宿主都不会初始化且只能刷新；现单独 try/catch，顶部给出可重试的「重新加载」入口，帧监听与快捷键只在引导成功后绑定且不会重复注册。
- **退出登录不联动 WS 与服务端（中等）**：本地 `logout()` 只清 localStorage，内存中的 Pinia 登录态与 WS 连接仍在收消息；现 `logout()` 通知订阅方断开 WS，`api/client` 的 401 分支同步清空 store。**说明**：后端没有「单会话登出」接口（仅 `POST /api/me/logout-others`），因此未新增服务端调用，接口契约保持不变。
- **已读水位先记后发（中等）**：`markRead` 先写 `lastReportedRead` 再发帧，WS 未连接时 `sendFrame` 返回 false、帧被丢弃，重连 `resync` 也不会补发，服务端未读永远清不掉；现仅在发送成功后记录水位，重连补齐后自动补发。
- **旧 socket 的 `onclose` 误标新连接（中等）**：快速切页时旧连接处于 CLOSING，`connect()` 新建连接后旧回调仍会把 `connected` 置 false、清空在线列表并再排一次重连；现每个回调先判断 `ws === socket`，`disconnect()` 先摘引用再关闭。
- **插件 API 裸 `fetch` 无超时、无 401 处理（中等）**：代码下载、octet-stream 上传、按 URL 安装在代理挂住时永久 pending，401 也不回登录页；现统一走新增的 `rawFetch`（15s 超时、上传 60s，带 Bearer 与 401 同步登出）。
- **消息缓存只增不减**：单会话本地缓存保留最新 800 条，裁剪后保持 `hasMore=true`，仍可继续上翻历史。
- **他端 `read` 帧无条件清零未读**：只有 `upToId` 覆盖本端最新消息时才清未读，避免误清本端尚未看到的消息。
- **桌面通知固定 tag**：改为按会话设置 tag，不同会话的通知不再互相覆盖，同一会话只保留最新一条。
- **缓存用量文案不准确**：`navigator.storage.estimate()` 减去 localStorage 得到的数值含 IndexedDB 等其他存储，而清理只删 PWA 静态缓存；文案改为「站点存储近似值，清理只删除静态缓存」。
- **fire-and-forget 无 catch**：通知点击打开会话、群成员刷新、通知权限请求、`?panel=plugins` 跳转补上 `.catch`，避免未处理的 Promise rejection。
- **上传共用布尔标志**：图片 / 视频 / 语音共用 `uploading`，并发时先完成的一条会提前解除禁用；改为并发计数器。

### 文档

- 按当前代码回写 `docs/设计文档.md`（功能范围 / 目录 / 数据表 / 完整 REST 与 WS 帧）、`docs/后端需求规格.md`、`docs/前端技术方案.md`、`docs/运维手册.md`、`web/README.md`、`server/README.md`；修复对已删除的 `update.ps1` / `coverage.ps1` 的引用与 CHANGELOG 中串行的条目。

---

## [0.10.3] — 2026-11-06

### 新增

- **发行版安装与更新（不再需要在服务器上编译）**：新增 `scripts/package-release.sh`——构建机打 tag 后一键产出 `modernchat-<版本>-linux-{amd64,arm64}.tar.gz` + `.sha256`（含后端二进制、前端 dist、迁移 SQL、nginx 模板、更新脚本），上传为 Gitee / GitHub Release 附件。`scripts/install.sh` 改为从 Release 下载安装（sha256 校验），服务器**无需 Go / Node / git**；`RELEASE_SOURCE=gitee|github` 选源、`RELEASE_TAG` 指定版本。发行包内自带 `scripts/update.sh`（命令行：查询最新 Release → 下载校验 → 原子替换 → 优雅重启）。站长后台「在线更新」同样支持发行版模式：按安装时记录的 Release 源检查新 tag、下载校验后替换重启（源码 git 部署仍走本地编译模式）。
- **站长后台图形化更新（在线更新页签）**：后台新增「在线更新」页签——检查更新（对比 `origin/main`，显示最新 tag 与提交）、一键更新（拉取最新代码 → 本机编译前后端 → 原子替换产物 → 自动重启当前实例），实时日志在页签内滚动展示（进行中每 2 秒轮询）。安全边界：仅站长可触发、命令全部固定参数（分支锁 `origin/main`，不接收用户输入）、操作写审计（`web_update`）、仅 install.sh 的 systemd 部署可用（Docker / 裸机界面提示改用脚本）、`WEB_UPDATE=false` 环境变量可整体关闭。新增接口 `GET /api/admin/update/{status,check}`、`POST /api/admin/update/start`。
- **安装/卸载体系完善**：① 全程日志——安装镜像到 `/var/log/modernchat/install-*.log`、更新到仓库 `logs/update-*.log`（均带 latest 软链）；② 断点续跑——安装失败后重跑同一命令自动跳过已完成阶段、参数不再重复询问（进度存 `/var/lib/modernchat/install-state`，600 权限），`FORCE=1` 全部重跑；③ 面板路径适配——自动探测宝塔（`/www/server/panel/vhost/nginx`）/ 1Panel / OpenResty 的 nginx 二进制与 vhost 目录并直接写配置 reload，识别不到时可手动指定；④ 完整卸载 `scripts/uninstall.sh`——停服务删单元、只删带生成标记的 nginx 配置、数据库与安装目录默认保留需确认，支持 `AUTO_CONFIRM` / `PURGE_DB` / `DELETE_FILES` 免交互。
- **热更新（升级不停机）**：`scripts/update.sh` 默认蓝绿双实例切换——新二进制原子替换（修复直接覆盖运行中文件会 `ETXTBSH` 的问题）、新实例起备用端口并通过健康检查、nginx upstream 平滑切流、老实例优雅停机，在线用户自动重连并增量补齐消息；任何一步失败自动回滚到老实例。install.sh 部署改为 `modernchat@<端口>` 模板单元；旧式单端口部署升级时自动迁移。前端产物同样原子交换（构建到 `dist.new`）。`--cold` 可强制旧式重启。
- **一键安装脚本（宝塔面板风格）**：全新 Linux 服务器一条命令完成部署——环境自检补装（Go 1.26 / Node 22 / MySQL 8 / nginx，国内镜像可切官方源）→ 从 Gitee 拉最新代码 → 本地编译前后端 → 自动建库建号（已有实例填连接信息即可）→ 生成 `JWT_SECRET` 与 `server/.env` → systemd 常驻 + nginx 配置（静态托管 / `/api` 反代 / `/ws` 升级）→ 输出可直接访问的站点地址（域名优先，无域名用公网 IP:端口）。支持 `INSTALL_DIR` / `DOMAIN` / `DB_MODE` 等环境变量免交互。用法见运维手册 §3.1。
- **安装脚本密码透明化**：应用数据库密码支持 `DB_PASS` 预设或问答中自定义，留空随机生成；安装完成摘要**明示**数据库账号密码（原来只说"密码在 server/.env 的 DB_DSN 里"，用户无从知晓）；自动安装 MySQL 时摘要说明 root 免密（auth_socket）并给出设置 root 密码的命令；安装日志收紧为 600 权限（避免明文密码被本机其他用户读取）。
- **设置页本地缓存管理**：显示 PWA 静态缓存与本地偏好占用，支持一键清空静态缓存；保留登录状态与外观偏好。
- **消息流阅读辅助**：上翻历史时右下角出现「回到底部」；期间收到新消息会显示「N 条新消息」，点击回到底部并标记已读。自己发消息时无论在哪都跳回底部，避免发出后看不见。
- **标签页未读提示**：聊天页有未读时标题显示 `(N) ModernChat`（免打扰会话不计入），切到其他页面恢复站点名。

### 修复

- **补齐 `server/.env.example`**：运维手册 §3.3 手工部署引用了 `cp server/.env.example server/.env` 但文件一直缺失，手工部署者在第 3 步就会卡住。已补齐（与 `config.go` 全部环境变量一一对应，含在线更新 `RELEASE_API_URL` / `RELEASE_DL_URL` 注释示例）；同时修复 `.gitignore` 中自相矛盾的规则（第 24 行 `!.env.example` 放行后又被第 29 行重新忽略，导致示例文件永远进不了仓库）。
- **发行包二进制无可执行位（203/EXEC）**：包在 Windows 上打包时 NTFS 文件没有 POSIX 执行位，解压出的 `bin/modernchat-server` 是 0644，systemd 启动报 `Permission denied`/`status=203` 无限重启。修复：install.sh / 发行包内 update.sh 解压后强制 `chmod 755` 二进制与更新脚本；Go 端 updater 替换二进制后同样补权限——与打包平台无关，双保险。
- **端口问答恒跳过导致部署失败**：端口问答条件写成 `[ -n "${X+x}" ]`，而变量已在开头初始化为空串，条件恒真——`SITE_PORT`/`BACKEND_PORT` 全程为空且从未询问。后果：systemd 启用的是模板单元本身 `modernchat@.service`（被 systemd 拒绝，安装中断于服务阶段）、nginx 将生成 `listen ;`、`FRONTEND_URL` 写成 `http://IP:`。修复为 `[ -n "$X" ]` 判断；`PUBLIC_URL` 不再从断点恢复（改由 `DOMAIN`/`SITE_PORT` 每次推导，避免坏值残留）；配置阶段续跑时校验 `.env` 的 `FRONTEND_URL` 与推导值一致，不一致自动重写 `.env` 修复旧断点的坏值。
- **sha256 校验被 Gitee 反盗链页击穿**：拉取 `.sha256` 附件用了不带 `-L` 的 curl（下载 tar.gz 时带了），Gitee 附件重定向后拿到的是 HTML 反盗链页，被当作校验清单比对，报「期望 `<html><body>You`」并中止安装。三处修复：install.sh 与发行包内 update.sh 的拉取补 `-L`；三处（含 Go 端 updater）都先校验清单必须是 64 位十六进制再比对，异常时降级跳过校验并显示本包哈希，不再误报"被篡改"。
- **install.sh 自动装 MySQL 后建库失败（ERROR 1698）**：预检用 `mysql -uroot`（Unix socket，auth_socket 生效）判定 root 免密通过，但建库时强制 `--protocol=TCP -h127.0.0.1`——Ubuntu 的 root@localhost 仅允许 socket 认证，TCP 直接 Access denied。修复为：免密 root 且目标是本机时改走 Unix socket（远程实例仍走 TCP）。
- **install.sh 跳过提示与 --force 参数**：脚本头部注释与进度提示一直宣称 `--force` 可全部重跑，但从未解析该参数（实际只有 `FORCE=1` 生效），已补上参数解析；所有「跳过已完成阶段」提示现在同时说明重跑方法——只重跑该阶段（删除 state 文件中对应 `STAGE_` 行）或全部重跑（`--force` / `FORCE=1`），不再让用户只看到"跳过了"却不知道怎么重来。
- **install.sh 因未定义变量中断安装**：`set -u` 模式下引用了旧版"自动装 Go/Node"时代残留的 `$MIRROR` 变量（该机制已随发行版模式移除），全新安装在数据库问答后必然报 `MIRROR: unbound variable` 中止，走不到下载发行包阶段。已删除残留的状态记录行。
- **通知抽屉文案编译失败**：`notify.mentions` / `notify.empty` 的中文文案里 `@` 未按 vue-i18n 语法转义成 `{'@'}`，整条消息编译失败、抽屉渲染抛错，loading 结束后 DOM 仍停在「加载中」。已转义并补浏览器回归验证。

### 性能

- **图片消息缩略图**：服务端上传 `kind=image` 时生成 ≤480px 的 JPEG 缩略图（`_t.jpg`，本地 / 对象存储均支持；GIF 与过小图跳过，透明图垫白底），消息气泡加载缩略图、灯箱加载原图——图片密集会话的流量与解码开销显著下降。
- **消息搜索 FULLTEXT**：`messages.content` 增加 ngram 全文索引（迁移 `0022`，启动时幂等兜底）；≥2 字符关键词走短语匹配，单字符与索引异常自动回退 LIKE。**升级需执行迁移 0022**（低峰执行，建索引不锁表）。

---

## [0.10.2] — 2026-11-06

### 性能

- **上传不再整块进内存**：multipart 分段流式交给存储层，边读边写临时文件（本地：原子改名；对象存储：先落临时文件再以已知长度上传）——视频上限调到 100 MB 时，几个并发上传也不会把小实例的内存打爆
- **本地存储配额缓存**：配额检查从「每次上传 `filepath.Walk` 整个上传目录」（5 万个历史文件 = 5 万+ 次 stat）改为 **60 秒 TTL 缓存 + 上传成功增量累加**；管理员清理孤儿文件后最多 1 分钟反映到配额判断

---

## [0.10.1] — 2026-11-06

### 修复

- **发送失败只能看不能重试**：失败消息此前只在单聊的元信息里显示「发送失败」，群聊里连提示都没有，也没有重发入口。现在所有会话类型的失败消息下方都有「发送失败 · 重发」，点击复用 store 的 `resend`（保留引用与 @）
- **断线没有任何提示**：新增顶部「网络已断开，正在重连…」提示条（首次连接时不闪、重连成功即消失），配合失败重发让用户能判断是自己网断了还是服务挂了
- **手机端详情抽屉过宽**：固定 280px 在窄屏上几乎顶满，改为 `min(88vw, 320px)`，保留少量主区可见以便点空白关闭
- **手机端视频气泡偏小**：竖屏视频宽度由 62vw 提到 70vw

### 变更

- 转发与举报浮层支持 **`Esc` 关闭**，并在打开时把焦点移入面板（此前键盘用户会被困在浮层里）
- `web/package.json` 版本对齐为 `0.10.1`（不参与界面展示，仅避免与 git tag 混淆）

---

## [0.10.0] — 2026-11-06

> **升级注意（从 0.9.x 升级）**
>
> - 新增迁移 **`0021_message_sender_index`**（为 `messages.sender_id` 建索引）。MySQL 8 默认走 Online DDL 不锁表，但消息表很大时仍会占用 IO，建议低峰执行并观察 `SHOW PROCESSLIST`。
> - 前置 nginx 的 `client_max_body_size` 需 ≥ **32 MB**：本版视频默认上限 30 MB，nginx 默认 1 MB 会直接回 413（后台调大视频上限时同步调大它）。
> - 「注册需邮箱验证」**默认开启**：升级后新注册需要邮箱 + 6 位验证码。未配 SMTP 时自动降级为不强制（`GET /api/site` 返回生效值），**首个账号始终豁免**；要回到旧行为在后台「系统设置 → 站点」关掉即可。
> - 若换过 `JWT_SECRET`，后台已加密保存的 SMTP / Redis / 对象存储密钥需重填（与本版无关，属既有约束）。

> 注：本版部分能力（消息编辑、会话管理、群管理增强、通知 / PWA、备份等）随 tag 一并合入，初版日志漏记，现已补记。

### 新增

- **视频消息**：输入区「视频」按钮，选本地文件直接发送（mp4 / webm / mov / m4v，默认 ≤30 MB，后台可调 1–100 MB）。气泡用原生播放器内联播放、固定 16:9 占位不抖动，支持引用 / 转发 / 收藏 / 撤回。后端 `upload?kind=video` **按容器特征判定真实类型**（`ftyp` brand / Matroska magic，纯音频 m4a 拒收），对象存储的 `Content-Type` 按音/视频分别给出。
- **注册邮箱验证**：注册页邮箱 + 6 位验证码（60 秒倒计时）。验证码 10 分钟有效、一次性、连错 5 次作废，库里只存 HMAC-SHA256 哈希；新增 `POST /api/auth/email-code` 与 `email_codes` 表（迁移 `0020`）。后台开关默认开、未配 SMTP 自动降级、首账号豁免。
- **换绑邮箱需验证**：`POST /api/me/email-code` 向新邮箱发码，`PATCH /api/me` 核销通过才落库并标记已验证——避免有人把邮箱改成他人地址（抢占唯一索引、让重置链接寄错人）。
- **消息内容搜索**：`GET /api/search/messages`（LIKE 匹配正文，只搜本人已加入的会话，排除已撤回 / 已删除）。两个入口：列表栏搜索框可搜全部会话的消息，详情面板新增「搜索聊天记录」分区（`Ctrl/Cmd+F` 聚焦）；点击结果跳转并高亮。
- **插件支持 TypeScript + 扩展点增强**：上传 `.ts`（或安装 `.ts` 结尾的 URL）由服务端 esbuild 逐文件转译入库，语法错误返回 400 带行号；新增 `registerMessageAction`（消息右键菜单）、`conv:read`（`me` / `conv.current` / `conv.members` / `messages.recent`）、`ui:host`（`nav.*` / `toast`）、描述式 UI 的 `table` / `progress` / `badge` 三个组件。类型声明 `plugins/modernchat-sdk.d.ts`，开发指引 `docs/插件开发.md`，示例 `plugins/examples/conv-stats.plugin.ts`。
- **媒体消息**：图片（发送前 canvas 压缩至最大边 1600、webp q0.82）与语音（`MediaRecorder`，点击开始/再点结束、60 秒上限）入口；消息只存 URL，落库前校验 URL 必须属于本站存储。
- **表情包**：个人表情库（`stickers`，只存 URL），面板点一下直接发送，支持上传（≤2 MB）与删除；消息渲染为透明贴图。
- **IM 体验增强**：引用回复（气泡上方引用块，点击跳回原消息）、`@` 提醒（成员选择器 + 高亮 + `mention_unread`）、置顶（服务端个人设置，跨设备）、免打扰（红点变圆点）、草稿（切换会话保留 + 列表显示 `[草稿]`）、图片右键菜单（查看大图 / 保存 / 收藏）。
- **消息编辑**：右键「编辑」仅对自己 **5 分钟内、未撤回的文本消息**可见；WS `edit` 上行、`message_update` 广播，`messages.edited_at` 落库并显示「已编辑」。
- **消息可靠性与多端同步**：单聊送达状态（`delivered` / `message_delivered`）、多端已读同步、断线重连增量补齐（按 `clientMsgId` 去重，不再整份重拉）。
- **会话管理**：会话归档 / 取消归档；从自己的列表移除会话（不退出群、不删消息）；清空个人聊天记录（只设个人水位，消息本体保留）。
- **内容治理**：拉黑用户（双向拦截消息与好友申请）、消息举报（同一消息每人一次、理由 1–200 字）、管理员删除违规消息（所有端同步移除）、后台敏感词过滤（命中文本拒发）。
- **群能力**：群公告（群主发布/编辑/撤销，≤500 字，实时推送）、群文件（≤20 MB，类型白名单**不含 html / svg** 防存储型 XSS，上传者或群主可删）、群管理员角色、成员禁言（10 分钟 – 30 天）、全员禁言、群头像、群邀请链接（7 天有效）与好友邀请。
- **注册邀请码**：后台批量生成（可设次数 / 有效期 / 单次最多 20 个），数据库条件 UPDATE 原子核销、并发不超发，注册失败自动退还名额；停用不删除以保留审计线索。
- **登录失败锁定**：按账号累计失败次数（原子自增），达阈值临时锁定，阈值与时长后台可配（0 = 关闭）；凭据正确即清零。
- **维护模式**：后台一键开启后非管理员只见维护页（可自定义文案）；REST 返回 503、WS 拒绝非管理员建连。管理员始终可登录与访问后台，`/api/site`、`/api/health`、`/api/version`、`/auth/login` 保持可用。
- **全站公告弹窗 + 初始公共群聊**：在线收到公告立即弹窗，离线期间的公告下次进入聊天页补弹（同一条只弹一次）；首次启动自动创建「公共聊天室」并纳入现有用户，新注册用户自动加入（主动退群后不会被重新拉回）。
- **桌面通知 + PWA**：浏览器桌面通知、通知抽屉、Service Worker 缓存静态资源（断网保留应用外壳）。
- **Web Push**：配置 VAPID 后页面关闭仍可收到推送；未配置时静默跳过。
- **站长后台增强**：概览统计、用户筛选、用户详情（在线状态 / 会话数 / 消息数）、强制下线、全站公告、角色调整（禁止降级最后一个站长）、按插件名强制卸载、操作审计页签、存储统计与孤儿文件清理。
- **站长备份**：后台「立即备份」与备份文件列表；可选定时备份与保留份数（`BACKUP_INTERVAL_HOURS` / `BACKUP_RETAIN`）。
- **可观测性**：Prometheus `GET /api/metrics`，以 `METRICS_TOKEN` 保护。
- **运营配置即时生效**：站点（名称 / 注册开关）、聊天规则（消息长度 / 撤回时限 / 群成员上限）、上传上限（图片 / 语音 / 群文件 / **视频**）在后台保存即生效，越界显式 400；新增公开接口 `GET /api/site` 供前端统一消费。
- **界面与体验基建**：四栏布局可拖拽调宽、双击分隔条切换「常驻 / 抽屉」（宽度与形态持久化）；全站页面背景自定义（预设色 / 取色 / 本地图片 + 四种纹理，入口只在设置页）；Apple 风格动效与材质令牌；TDesign → UnoCSS + 13 个自研 `Ui*` 控件；静态资源构建期生成 `.gz` / `.br`（首屏 161 KB → 约 65 KB）。
- **文件存储抽象**：`service.Storage` 接口 + 本地 / S3 兼容实现（minio-go，兼容 AWS S3 / MinIO / OSS / COS）；后台可视化配置并 AES-256-GCM 加密落库，密钥永不回显；配了对象存储时失败明确报错、不静默回退本地。
- 文档：`docs/设计文档.md`、`后端需求规格.md`、`前端技术方案.md`、`运维手册.md`、`UI设计方案.md`、`插件开发.md`。

### 变更

- **表情回应暂未开放**：消息气泡下的 emoji 表态入口已隐藏（`FEATURES.reactions = false`，代码保留，改回 `true` 即可启用）。按设计文档划在二期，本期只做自定义表情**发送**。
- **撤回提示 3 分钟后自动移除**：撤回时间由服务端下发并落库（`messages.recalled_at`），跨刷新 / 跨设备一致；历史遗留无时间的撤回消息按已过期处理。
- **信息架构重整**：图标栏 6 项精简为 4 项（头像 / 聊天 / 联系人 / 设置），加好友与建群提到列表栏顶部常驻，插件管理与后台收进设置页。
- **空会话门面标记改用产品 favicon**，与浏览器标签页图标统一。

### 修复

- **会话详情抽屉点不动**：抽屉布局类曾被挂到多根子组件 `DetailPanel` 上而静默丢失，导致面板没有 `z-index`、被全屏遮罩盖住（看得见点不了），平板上的「抽屉展开可见」也失效。改为由 `ChatLayout` 自己的容器承载布局类。
- **撤回一致性**：未读气泡数、引用块快照、编辑态、会话预览五处不同步；现随 `recall` 帧一并修正（本地未读与服务端 `RecallUnreadAdjust` 对齐，服务端不再写死中文预览），单聊视图补 `lastMessageType`（图片/语音不再显示文件地址）。
- **转发与合并转发**：已撤回消息禁止转发（发送时二次校验，防选择框停留期间被撤回导致内容泄漏）；合并转发跳过已撤回并提示条数、最多 20 条（超出当场拒绝而不是静默丢弃）。
- **消息可靠性**：断线重连合并服务端消息时未清 `pending`，已送达消息 8 秒后被误标「发送失败」；重发丢失引用与 `@`；自动已读过于激进（后台标签页/上翻历史也清红点）；「标记已读」不清「有人@我」。
- **认证链路**：登录/注册回车不提交；401 跳登录丢来源（现带 `?redirect=`）；`fetch()` 网络异常弹英文原文；`fetchMe` 遇网络抖动就登出；首账号邮箱被丢弃（导致该账号永远无法找回密码）；已登录点找回密码链接被守卫踢回聊天页；找回密码链接带 `token` 却不被读取（流程断死，现自动进第二步并在邮件正文附令牌兜底）。
- **后台设置**：加载失败时表单是默认值却仍可保存（会覆盖线上配置，现锁住保存并给重试）；密钥字段看不出是否已配置；资料保存无脏检查且「只改昵称」会清空邮箱验证状态（改为按 diff 提交）；一个插件加载失败会连累全部（逐个隔离，启停失败保留旧运行时，停用回收主题变量）。
- **群与成员**：退群后停在空白会话；群主转让后「删除违规消息」入口不刷新；成员列表加载失败无提示（现给错误与重试），成员/群文件请求补竞态保护，切换会话不再串数据；群文件上限改为跟随后台配置。
- **消息列表与搜索**：搜索结果跳转对已撤回过期/被删消息空转且 `pendingJumpId` 残留；多选模式下点图片/表情选不上；会话列表置顶样式每行 `O(n)` 查找。
- **其它**：页面背景颜色只刷第一屏（纯色层改 `repeat`、图片层 `fixed`）；背景图超配额会删掉已保存的旧图；语音录制中切会话会把录音发到上一个会话；取消编辑会清空原草稿；邀请落地页切换 token 不刷新；窄屏抽屉被 `display:none` 吃掉、平板留一根无效分隔条；插件只读接口 `messages.recent` 泄露撤回原文。

### 性能

- **会话列表少读一个数量级**：成员数改用一条 `GROUP BY`、只有单聊才取对方资料（20 个满员群从约 4000 行/次降到几十行）；群搜索同样一次 `GROUP BY` 取代逐群 `COUNT`（21 次往返 → 2 次）。
- **`/api/site` 不再全表 `COUNT`**：改为 `SELECT 1 ... LIMIT 1`（该接口每 30 秒被每个在线客户端调用）；配置解析结果用 `atomic.Pointer` 缓存，保存时同步失效。
- **补齐 `messages.sender_id` 索引**（迁移 `0021`）：后台按发送者统计不再全表扫描。
- **前端**：普通 `conv_change` 只做增量（不再每条消息重拉会话列表）、站点配置轮询后台跳过 + 浅比较、会话列表去掉 O(n²)、插件代码并发拉取、消息区昵称复用成员缓存、虚拟列表估算行高贴近真实值。

### 安全

- **请求体大小限制**（CWE-770）：新增 `BodyLimit` 中间件（默认 1 MB，插件上传按 `PLUGIN_MAX_SIZE` 放宽），超限返回 **413**；此前超大 JSON 打到登录接口即可占用大量内存。
- **安全响应头**：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`；`deploy/nginx.*.conf` 同步补齐含 `Permissions-Policy`，并给出 CSP 模板（默认注释——严格 CSP 会让插件沙箱失效）。
- **重置链接不再默认写入日志**：未配 SMTP 时 `LOG_RESET_LINK` 在 release 模式下默认 `false`（日志里的可用令牌等于账户接管入口）。
- **`/ws` 关闭 nginx 访问日志**：WebSocket token 只能走 query，默认 `access_log` 会记录完整 URI（CWE-598）。
- 压缩：REST 走 `gin-contrib/gzip`（排除 `/ws`、<1KB 不压缩），WS 开启 `permessage-deflate`。

---

## [0.9.0] — 2026-10-25

### 新增

- **用户自定义头像**：后端 `users.avatar` 字段 + `PATCH /api/me` 支持 data URL（png/jpeg/webp，≤256 KB）；前端点击头像 hover 相机上传（居中裁切 256×256 webp 压缩）；全站显示，含会话列表、消息气泡、群成员、后台表格
- **个人资料卡**：点击侧栏头像弹出（头像 / 昵称 / @用户名 / 设置 / 退出登录 / 管理后台）
- **飞书式四栏布局**：图标栏（头像、新建、消息、联系人、插件、后台、设置）+ 分组筛选栏（全部消息 / 未读 / 单聊 / 群聊）+ 会话与联系人列表栏 + 聊天区
- **认证页背景可自选**：纯色基色 + 光晕 / 波纹 / 网格三种动效，6 预设色 + 自定义取色器，偏好本地持久化
- **统一设置页**（`/settings`）：语言、主题、账号、版本
- **覆盖率工具**：`scripts/coverage.ps1` / `coverage.sh` 合并单测与集成测试覆盖率，并检测"用例被静默 SKIP"的假通过
- 后端配置 / 版本 / 邮件三个包的单元测试

### 修复

- **`text/plain` 等类型上传永远被拒**：`http.DetectContentType` 对文本返回 `text/plain; charset=utf-8`，带参数导致白名单永远匹配不上（图片/音频返回的 MIME 不带参数，所以之前一直没暴露）。改为先剥离 `;` 后的参数再查表。补回归测试 `TestDetectTypeStripsMimeParameters`
- **迁移版本号重复**：`chat_prefs_reply_mention` 与 `system_settings` 都编到 `0005`，`golang-migrate` 按版本号索引会冲突/跳过。`chat_prefs_reply_mention` 重排为 `0007`
- **同一好友重复出现两次**：`PendingToUser` 只查 `status=0 AND friend_id=我`，未排除"已经是好友"的对；互发申请的竞态或历史残留会让同一个人同时出现在「好友申请」与「我的好友」。改为 `NOT EXISTS` 排除 + 按申请人去重，同意好友时顺带清理这对的其它待处理记录，前端再兜一层去重
- **收藏夹打开即白屏**：后端 `Favorites` 用 `var rows []`，无收藏时 nil slice 序列化成 `null`，前端 `items.map()` 抛 `Cannot read properties of null`。后端初始化空切片 + handler 兜底，前端 store 用 `Array.isArray()` 收敛；同类问题的消息列表（空会话）、好友列表也一并修
- **消息时间戳行显示不全**：`.mc-msg` 用 `margin` 做间距，而 `DynamicScroller` 按 wrapper 高度测量 item，margin 不计入 → 相邻消息的 meta 行互相贴边；`opacity: 0.9` 还会创建合成层把 12px 小字渲染成模糊残影。改用 `padding`、明确行高、去掉 opacity
- **UnoCSS 透明度修饰对 CSS 变量无效**：`bg-success/12` 退化成实心色，配合同色文字变成"绿底绿字"（后台状态列、插件权限标签不可读）。改用 `color-mix` 预定义的 `*-soft` / `*-line` 档位，共修 15 处
- **有 admin 权限却进不去后台**：路由守卫直接读内存态 `auth.user`，刷新或直接访问 `/admin` 时为空 → 判定无权限弹回聊天页。改为有 token 但 user 为空时先 `fetchMe()` 再判
- **详情面板「退群」按钮被压扁成细线**：面板是 flex column 且整体滚动，子项默认 `flex-shrink: 1`，成员一多就等比压缩。给 section/footer 加 `flex-shrink: 0`
- **`<button>` 默认边框在暗色下像白圈**：浏览器默认 `2px outset`，输入框清除按钮尤其明显。所有自研按钮补 `border-none`，描边变体用 `border-1 border-solid`
- **转让群主未同步 `owner_id`**：前端读 `ownerId` 判管理权、后端读成员 `role` 判权限，两边结论相反 → 原群主仍显示管理入口（点了 403）、新群主看不到入口；现改为同一事务内同步
- **插件抽屉打不开**：TDesign `Drawer` 只有 `visible` prop，裸 `v-model` 的值落到 DOM 属性上，抽屉停在屏幕外 → 改用 `v-model:visible`
- **`FRONTEND_URL` 尾随空格导致斜杠未去除**：白名单与浏览器 Origin 不一致，WS 握手被拒
- **断线时发消息永远停在"发送中"**：`ws.send()` 现在返回布尔值，连接不可用即标记失败并支持重发
- **断线重连后不补数据**：重连时自动重拉会话列表与当前会话最新消息（按 `clientMsgId` 合并去重）
- **断线后仍显示他人"在线"**：断开即清空在线集合
- **被踢后点旧会话停在空白页**：打开会话失败回退到选择态
- **后台用户列表无法翻页**：分页总数被一个未赋值的变量覆盖
- **邮件发不出去**：SMTP 认证参数为空、不支持 465 隐式 TLS、中文主题乱码、缺少超时保护
- 好友列表按对方去重（修复添加好友后显示两条）
- 向上翻历史时不再重复播放最后一条消息的入场动画
- 头像 / 昵称变更通过 WebSocket 实时同步

### 性能

- TDesign 改为按需引入（移除 `app.use(TDesign)` 全量注册），组件随路由分包：**首屏 gzip 262 kB → 104 kB**
- `vite-plugin-vue-devtools` 仅开发环境启用

---

## [0.8.0] — 2026-10-24

### 新增

- 统一动效系统：路由 / 会话切换过渡、消息入场与微交互
- 聊天界面体验优化与「添加好友」弹窗（会话列表、新建菜单、空会话页、资料区四处入口）
- 单聊资料面板的「添加好友 / 同意申请」操作
- 会话列表分组筛选（全部 / 未读 / 单聊 / 群聊）
- 头像 / 昵称变更通过 WebSocket 实时同步到会话、好友与群成员列表

---

## [0.7.0] — 2026-10-24

### 新增

- 沉浸式登录页：背景场景组件 `AuthScene`（可换自定义背景图）、居中卡片、注册改链接切换
- 找回密码页重做：复用同一套场景、两步进度指示、保留已填邮箱
- 协议同意勾选框：登录态默认勾选、注册态要求主动勾选、状态本地记忆
- favicon 接入与移动端 `theme-color`（随亮暗主题切换）

### 变更

- 界面文案去开发化：注册副标题、找回密码提示、邮箱说明、后台版本提示改为面向用户的表述，内部规则集中到 README
- 《用户协议》《隐私协议》入口只保留在登录 / 注册页

---

## [0.6.0] — 2026-10-24

### 修复

- 撤回时未读数可能为负
- 群主转让后权限判断不一致
- 重复入群返回 500
- 登录 / 注册限流绕过（收紧可信代理网段）
- GORM 日志明文打印 SQL 参数

### 新增

- 统一动效与主题令牌体系（`--mc-*`）
- 插件面板的声明式渲染白名单

---

[0.10.3]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.10.2...v0.10.3
[0.10.2]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.10.1...v0.10.2
[0.10.1]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.10.0...v0.10.1
[0.10.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.9.0...v0.10.0
[0.9.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.8.0...v0.9.0
[0.8.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.7.0...v0.8.0
[0.7.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.6.0...v0.7.0
[0.6.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/releases/tag/v0.6.0
