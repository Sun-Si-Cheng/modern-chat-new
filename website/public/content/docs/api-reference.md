# ModernChat API 文档

> 面向对接者的接口手册：REST + WebSocket 全量端点、参数、响应与错误。
> 本文是 `docs/后端需求规格.md` §3 契约的对接视图——语义与校验规则的完整口径以需求规格为准；
> 字段类型与 `web/src/types/proto.ts` 完全一致（前后端共用口径）。
> 最后更新：2026-11-21

---

## 1. 通用约定

| 项 | 约定 |
|---|---|
| Base URL | 同源 `/api`（前端部署在 `/app/` 时 API 仍在站点根）；WS 为同源 `/ws` |
| 字段命名 | JSON 一律 **camelCase** |
| 时间 | ISO8601 UTC（如 `2026-10-24T08:30:00.123Z`） |
| 成功响应 | 直接返回业务体，**不包 `{code, data}` 壳** |
| 错误响应 | 统一 `{"error": "<中文消息>"}`（见 §9 错误码） |
| 鉴权 | `Authorization: Bearer <JWT>`；另登录/WS 握手会下发同源 `mc_auth` HttpOnly cookie（静态资源 `/uploads` 依赖它，REST 也可用） |
| JWT | HS256，claims `{uid, tv, iat, exp}`，有效期 7 天；`tv`（token_version）变化即全端失效 |
| 空集合 | 列表为空时返回 `[]`，**不返回 `null`** |
| 分页 | 消息分页走 `(conversation_id, id)` 游标（`beforeId`/`afterId`），禁止 OFFSET；管理列表用 `page`/`pageSize` |

**免鉴权端点**：`auth/register`、`auth/login`、`auth/forgot`、`auth/reset`、`auth/logout`、`auth/migrate-login`、`auth/sso/*`、`site`、`invite/:token`（GET）、`health`、`version`；`metrics` 配置了 `METRICS_TOKEN` 时需 Bearer。

**限流**（内存令牌桶或 Redis，按 key 分桶，命中返回 429）：

| 维度 | 限额 |
|---|---|
| 登录 / 注册 / 找回 / SSO | 每 IP 10 次/分 |
| 发消息（WS chat 帧） | 每用户 20 条/分 |
| 媒体上传 | 每用户 10 次/分 |
| 插件安装 | 每用户 5 次/分 |
| 换绑邮箱验证码 | 每用户 5 次/分 |
| 旧站迁移接口 | 每 IP 120 次/分 |

---

## 2. 快速上手

```bash
# 1) 登录拿 token
curl -X POST https://站点/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret"}'
# → {"token":"eyJhbGciOi...","user":{"id":1,"username":"alice","nickname":"Alice","avatarColor":"#0052d9",...}}

# 2) 带 token 调接口
curl https://站点/api/conversations -H 'Authorization: Bearer <token>'
# → [ { id:7, type:2, name:"公共聊天室", unreadCount:0, ... } ]

# 3) 发消息走 WebSocket（见 §8）；上传走 multipart（见 §6）
```

---

## 3. 认证与账号

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| POST `/api/auth/register` | `{username, password, nickname, inviteCode?, email?, emailCode?}` | `{token, user}`；首个注册者自动成为站长。注册开关关闭→403；需邀请码→400；需邮箱验证时 `email`+`emailCode` 必填，验证码错→422；SMTP 未配置且要求验证→503（首账号豁免） |
| POST `/api/auth/login` | `{username, password}` | `{token, user}`，并下发 `mc_auth` cookie。连续失败达阈值锁定（429），错误文案恒定不泄露账号是否存在 |
| POST `/api/auth/logout` | — | `{ok:true}`；**免鉴权**，只清 cookie，不吊销 JWT、不踢 WS |
| POST `/api/auth/forgot` | `{email}` | `{ok:true}`（无此邮箱也返回成功，防枚举） |
| POST `/api/auth/reset` | `{token, newPassword}` | `{ok:true}`；`token_version+1` 全端失效 |
| POST `/api/auth/email-code` | `{email}` | `{ok:true, cooldownSeconds}`；同邮箱 60s 冷却（冷却期内仍 200 并返回剩余秒数）；邮箱已绑定→409 |
| GET `/api/auth/sso/status` | — | `{enabled}`；**免鉴权**。统一登录未启用时下列 sso 端点一律 404 |
| GET `/api/auth/sso/start` | `?redirect=`（站内相对路径） | 302 → 聚合登录中心授权页 |
| GET `/api/auth/sso/callback` | 中心回跳 `?code&state` | 校验 → 关联/建号 → 302 回 `/sso/callback?ticket&redirect` |
| POST `/api/auth/sso/exchange` | `{ticket}` | `{token, user}`；**免鉴权**，票据一次性、5 分钟有效 |
| POST `/api/auth/migrate-login` | `{token, key}` | `{token, user}`；**免鉴权**，旧站迁移一次性票据（10 分钟），失败提示用原密码登录 |
| GET `/api/me` | — | `User`（本人含 email / emailVerified） |
| PATCH `/api/me` | `{nickname?, email?, avatar?, emailCode?}` | 更新后 `User`。换绑邮箱必须带 `emailCode`（发往新邮箱）；空串 `email`=解绑；avatar 为 dataURL（≤256KB）或空串清除。昵称/头像变更后广播 `user_change` |
| POST `/api/me/email-code` | `{email}`（新邮箱） | `{ok:true, cooldownSeconds}`；被他人占用→409 |
| POST `/api/me/logout-others` | — | `{ok:true}`；`token_version+1` 全端失效并踢线（含 WS `kicked`） |
| GET `/api/version` | — | `{version, commit, buildTime}`；**免鉴权** |

---

## 4. 用户与好友

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| GET `/api/users/search` | `?kw=`（≤50） | `User摘要[]`（≤20 条，不含 email） |
| GET `/api/friends` | — | `FriendItem[]`（已通过好友，按对方去重） |
| GET `/api/friends/requests` | — | `FriendRequestItem[]`（指向我的待处理申请） |
| POST `/api/friends/apply` | `{toUsername}` | `{ok:true}`；已有关系/申请中→409；拉黑双向拦截 |
| POST `/api/friends/handle` | `{requestId, accept}` | `{ok:true}`；同意后双向 status=1 |
| DELETE `/api/friends/:friendId` | 路径参数=**对方 user_id** | `{ok:true}`；双向删除，单聊会话保留 |
| GET `/api/users/:id/block` | — | `{blocked}`；查询我是否拉黑了该用户 |
| POST `/api/users/:id/block` | — | `{ok:true}`；幂等；不能拉黑自己；拉黑后双向拦截单聊与申请 |
| DELETE `/api/users/:id/block` | — | `{ok:true}` |

---

## 5. 会话与消息

### 5.1 会话

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| GET `/api/conversations` | — | `Conversation[]`（含 peer、未读、置顶/免打扰/归档、@未读） |
| GET `/api/conversations/search` | `?kw=` | `Conversation[]`（type=2 群名模糊，≤20 条） |
| POST `/api/conversations/single` | `{toUserId}`（须好友） | `Conversation`；已有双人会话则复用（`direct_key` 唯一约束防并发重复） |
| POST `/api/conversations/group` | `{name}`（1-30 字） | `Conversation`；创建者为 owner |
| PATCH `/api/conversations/:id` | `{name}` | `{ok:true}`；仅群主 |
| PUT `/api/conversations/:id/avatar` | `{avatar}` | `{ok:true}`；群主/管理员；URL 须属本站存储；空串恢复默认 |
| DELETE `/api/conversations/:id` | — | `{ok:true}`；只删自己的收件箱行，不退群不删消息 |
| PATCH `/api/conversations/:id/prefs` | `{pinned?, muted?, archived?}` | `{ok:true, pinned, muted, archived}`；个人设置，只影响自己的收件箱行 |
| POST `/api/conversations/:id/read` | `{upToId}` | `{ok:true}`；清未读与 @ 标记，向在线成员推 `read` 帧；游标只前进（GREATEST） |
| POST `/api/conversations/:id/clear` | `{upToId?}` | `{ok:true, clearedBeforeId}`；清空我的记录，消息本体保留 |

### 5.2 成员与群管理（仅 type=2）

| 方法 路径 | 请求 | 权限 / 说明 |
|---|---|---|
| GET `/api/conversations/:id/members` | — | `ConversationMember[]`；须为成员 |
| POST `/api/conversations/:id/join` | — | `{ok:true}`；已在群→409；群满→422 |
| POST `/api/conversations/:id/invite` | `{userIds}` | `{ok:true, added}`；只能邀请自己的好友，单批 ≤50 |
| POST `/api/conversations/:id/invite-link` | `{expiresDays:0-30}` | `{token, path, expiresAt?}`；任意成员可生成，0=永久 |
| POST `/api/conversations/:id/leave` | — | `{ok:true}`；群主不可退→422 |
| POST `/api/conversations/:id/kick` | `{userId}` | 群主/管理员；不能踢自己/群主/其他管理员 |
| POST `/api/conversations/:id/admin` | `{userId, admin}` | 仅群主 |
| POST `/api/conversations/:id/mute` | `{userId, minutes:0-43200}` | 群主/管理员；0=解除；不能禁言群主/其他管理员 |
| POST `/api/conversations/:id/mute-all` | `{muted}` | 群主/管理员；全员禁言后群主与管理员仍可发言 |
| POST `/api/conversations/:id/transfer` | `{userId}` | `{ok:true}`；双方 role 互换 |
| PUT `/api/conversations/:id/announcement` | `{content}`（≤500 字） | 仅群主；空串=撤销；变更推全员 `conv_change` |

### 5.3 邀请链接（落地页）

| 方法 路径 | 说明 |
|---|---|
| GET `/api/invite/:token` | `{convId,name,avatar,memberCount}`；**免登录**；过期/撤销 404 |
| POST `/api/invite/:token/join` | `{ok:true, convId, alreadyMember?}` |

### 5.4 消息

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| GET `/api/conversations/:id/messages` | `?beforeId=&afterId=&limit=` | `{messages: Message[], hasMore}`；limit 默认 30 ≤50，`afterId` 增量补齐时 ≤200；历史只走游标分页 |
| DELETE `/api/conversations/:id/messages/:messageId` | — | `{ok:true}`；仅自己删除（对方仍可见），未读且未读到时 unread-1 |
| GET `/api/search/messages` | `?kw=&convId=&limit=` | `SearchHit[]`；`convId` 缺省=全局（本人会话）；≥2 字符走 FULLTEXT ngram；撤回/删除消息不计入 |
| POST `/api/messages/:messageId/favorite` | — | `{ok:true}`；须为会话成员；幂等 |
| DELETE `/api/messages/:messageId/favorite` | — | `{ok:true}` |
| GET `/api/favorites` | `?limit=1..200` | `FavoriteItem[]`（倒序） |
| POST `/api/messages/:messageId/report` | `{reason:1-200 字}` | `{ok:true, duplicate?}`；已撤回→422；每人每条一次 |
| POST `/api/messages/:messageId/admin-delete` | — | `{ok:true, duplicate?}`；站点管理员或群主/管理员；广播 `message_admin_deleted` |

> 发消息 / 撤回 / 编辑 / 已读回执 / 表情回应都走 **WebSocket**（见 §11）；REST 只提供查询与删除。

### 5.5 表情包

| 方法 路径 | 说明 |
|---|---|
| GET `/api/stickers` | `Sticker[]`（`{id,url,createdAt}`） |
| POST `/api/stickers` | `{url}`；URL 须属本站存储；上限 200；同 URL 幂等 |
| DELETE `/api/stickers/:id` | 只能删自己的 |

---

## 6. 文件与上传

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| POST `/api/upload?kind=image\|audio\|sticker\|file\|video` | multipart `file` | `{kind, size, url}`。上限：图片 8MB / 语音 5MB / 表情 2MB / 群文件 20MB / 视频 30MB（后台可配）。**按文件头判真实类型**（伪装扩展名→400）；流式处理；每用户 10 次/分 |
| GET `/uploads/<年>/<月>/<文件名>` | — | 静态文件。**必须登录**（Bearer 或同源 `mc_auth` cookie）；未登录 401、封禁 403；维护模式非管理员不可访问；`Cache-Control: private`。S3 模式新文件由桶/CDN 直出 |
| GET `/api/conversations/:id/files` | — | `ConversationFile[]`（≤100，新→旧） |
| POST `/api/conversations/:id/files` | `{name, url, size}` | `ConversationFile`；`url` 须属本站存储 |
| DELETE `/api/files/:id` | — | `{ok:true}`；上传者本人或群主 |

群文件类型白名单：pdf / zip / txt / json / jpeg / png / webp / gif——**不含 html 与 svg**（防同源脚本执行）。

---

## 7. Web Push

| 方法 路径 | 请求 | 说明 |
|---|---|---|
| GET `/api/push/key` | — | `{enabled, key}`；未配置 VAPID 时 `enabled=false` |
| POST `/api/push/subscribe` | `{endpoint, p256dh, auth}` | `{ok:true}` |
| DELETE `/api/push/subscribe` | `?endpoint=` | `{ok:true}`；按 `user_id + endpoint` 限定 |

---

## 8. 插件

沙箱模型与 SDK 见 `docs/插件开发.md`；以下为 REST 面。

| 方法 路径 | 请求 | 响应 / 说明 |
|---|---|---|
| GET `/api/plugins` | — | `Plugin[]`（本人安装记录，倒序） |
| GET `/api/plugins/active` | — | `Plugin[]`（enabled 且未全局下架） |
| GET `/api/plugins/:id/code` | — | `text/javascript` 源码；global_disabled→403 |
| POST `/api/plugins/upload` | 字节流（`application/octet-stream`，≤1MB），`?lang=ts\|js` | `Plugin`（默认 enabled=false）；本人同名=升级覆盖（响应头 `X-Plugin-Updated: true`）；TS 由服务端 esbuild 转译，语法错误→400 含行号 |
| POST `/api/plugins/from-url` | `{url}` | `Plugin`；服务端拉取前做 SSRF 检查 |
| PATCH `/api/plugins/:id` | `{enabled}` | 更新后 `Plugin` |
| DELETE `/api/plugins/:id` | — | `{ok:true}` |

---

## 9. 管理后台（`role=admin`，403 拦截）

### 9.1 概览与用户

| 方法 路径 | 请求 | 说明 |
|---|---|---|
| GET `/api/admin/stats` | — | `{users, conversations, messages, plugins, online, uptimeSeconds, goVersion}` |
| GET `/api/admin/users` | `?page=&pageSize=&kw=&role=&status=` | `{users: AdminUserRow[], total, page, pageSize}` |
| GET `/api/admin/users/:id` | — | `{user, stats:{conversations,messages,plugins}, online}` |
| POST `/api/admin/users/:id/kick` | — | `{ok:true, wasOnline}`；仅断线不封禁 |
| POST `/api/admin/users/:id/ban` | `{days:0-3650, reason?}` | `{ok:true}`；0=永久；tv+1 并推 `kicked` |
| POST `/api/admin/users/:id/unban` | — | `{ok:true}` |
| POST `/api/admin/users/:id/reset-password` | — | `{newPassword}`（随机 12 位；tv+1 强制下线） |
| POST `/api/admin/users/:id/role` | `{role:"user"\|"admin"}` | `{ok:true, changed}`；降级最后一个站长→403；即时生效 |

### 9.2 公告 / 审计 / 设置 / 邀请码

| 方法 路径 | 请求 | 说明 |
|---|---|---|
| POST `/api/admin/announce` | `{content}`（1-1000 字） | `{ok:true, convId, messageId, recipients}`；写系统会话并 WS 全员推送 |
| GET `/api/admin/audits` | `?page=&pageSize=` | `{audits, total, page, pageSize}`；操作审计倒序 |
| GET `/api/admin/settings` | — | 分域生效值；**密钥只给 hasXxx，永不回显** |
| PUT `/api/admin/settings` | `{smtp?, redis?, storage?, site?, chat?, upload?, security?}` | `{ok:true, redisNeedsRestart, storageApplied}`；密钥留空=不改；SMTP 热生效，Redis 需重启；数值越界 400；写审计 |
| POST `/api/admin/settings/smtp/test` | `{to}` | `{ok:true}`；失败 400 含原始错误；写审计 |
| GET `/api/admin/invites` | — | `InviteCode[]` |
| POST `/api/admin/invites` | `{maxUses:1-1000, expiresDays:0-365, count:1-20}` | `InviteCode[]` |
| POST `/api/admin/invites/:id/disable` | `{disabled}` | `{ok:true}`；停用不删除 |

### 9.3 存储 / 备份 / 在线更新

| 方法 路径 | 请求 | 说明 |
|---|---|---|
| GET `/api/admin/storage/stats` | — | `{files, bytes, referenced, orphans, orphanBytes}`；仅本地存储 |
| POST `/api/admin/storage/gc` | `{dryRun?}` | `{ok:true, dryRun, deleted, freedBytes}`；清理孤儿附件 |
| GET `/api/admin/backups` | — | `{backups: BackupFile[], dir}` |
| POST `/api/admin/backups` | — | `{ok:true, file}`；依赖 mysqldump |
| GET `/api/admin/update/status` | — | `{available, busy, phase, lines, error?, version, commit, mode?, repo?}` |
| GET `/api/admin/update/check` | — | `{upToDate, local, remote, remoteTag, remoteMsg, remoteNotes?}`（说明截断 8KB） |
| POST `/api/admin/update/start` | — | `{started:true}`；仅 systemd 部署；写审计 `web_update` |
| GET `/api/admin/plugins` | `?page=&pageSize=` | 按 name 聚合 `{name, version, installCount, enabledCount, globalDisabled, createdAt}` |
| POST `/api/admin/plugins/global-disable` | `{name, disabled}` | `{ok:true, affected}`；立即对所有用户生效 |
| POST `/api/admin/plugins/purge` | `{name, userId?:0}` | `{ok:true, affected}`；物理删除安装记录 |

---

## 10. 公开端点

| 方法 路径 | 说明 |
|---|---|
| GET `/api/health` | `{"status":"ok"}`；探活 |
| GET `/api/site` | 站点公开配置：`{name, allowRegister, maintenanceMode, maintenanceMessage, requireInviteCode, requireEmail, requireEmailVerify, maxMessageLength, recallMinutes, maxGroupMembers, maxImageMB, maxAudioMB, maxFileMB, maxVideoMB}`；维护模式下仍可用 |
| GET `/api/metrics` | Prometheus 文本格式；`METRICS_TOKEN` 非空时需 Bearer |

---

## 11. WebSocket（`/ws`）

### 11.1 连接与生命周期

- 握手：`GET /ws?token=<JWT>`（`?token=` 优先，缺失回退同源 `mc_auth` cookie）；校验签名 / tv / 封禁 / **Origin 白名单**（`FRONTEND_URL`，无 Origin 的非浏览器客户端放行）；失败 401/403/503 直接关闭
- 维护模式：非管理员握手直接 503 `{"error":"站点维护中","maintenance":true}`
- 心跳：客户端 25s 发 `{"type":"ping"}` → 服务端立即回 `{"type":"pong"}`；读超时 60s 断开
- **每一帧**路由前统一复核封禁与 tv（不通过回 `error` 帧并断开）
- 上行帧字节上限 64 KB；tv 变化 / 封禁 / 站长重置密码 → 推 `kicked` 后关闭

### 11.2 上行帧（客户端 → 服务端，8 种）

| type | 载荷 | 说明 |
|---|---|---|
| `chat` | `{convId, clientMsgId, content, msgType?, replyToId?, mentions?, forwarded?}` | 发消息；每用户 20 条/分；`client_msg_id` 唯一索引幂等（重复回放原帧）；成员/封禁/敏感词/长度校验；媒体类 content 须为本站存储地址 |
| `typing` | `{convId}` | 正在输入（转发给在线成员） |
| `read` | `{convId, upToId}` | 已读回执（清未读，游标只前进） |
| `recall` | `{messageId}` | 撤回；仅本人、`recallMinutes` 内（后台可配 0=关闭）；unread 回退、广播 `recall` |
| `edit` | `{messageId, content}` | 编辑；仅本人、创建后 5 分钟内、未撤回的 `text`；敏感词与长度同发消息 |
| `reaction` | `{messageId, emoji, action:"set"\|"remove"}` | 每人每条最多一个表情；广播增量 |
| `delivered` | `{messageId}` | 单聊送达回执（只转发给发送者） |
| `ping` | — | 心跳 |

### 11.3 下行帧（服务端 → 客户端，13 种）

| type | 载荷 | 时机 |
|---|---|---|
| `message` | 完整消息体（含 `replyTo` 摘要与 `mentions`，无需回查） | 新消息（成员 fan-out；未读接收者另推 `conv_change`） |
| `conv_change` | `{convId, lastMessage…, unread…}` | 会话变更（新消息/公告/成员变化） |
| `typing` | `{convId, userId}` | 对方正在输入 |
| `read` | `{convId, userId, upToId}` | 成员已读 |
| `recall` | `{messageId, convId, recalledAt}` | 撤回（服务端时间，客户端提示展示 3 分钟后移除） |
| `message_update` | `{id, convId, content, editedAt}` | 编辑成功 |
| `reaction` | `{messageId, convId, emoji, userId, action}` | 表情增量 |
| `message_delivered` | `{messageId, convId, userId}` | 单聊送达 |
| `message_admin_deleted` | `{messageId, convId}` | 管理删除 |
| `presence` | `{userId, online}` | 好友上下线 |
| `user_change` | `{userId, nickname, avatar, avatarColor}` | 资料变更（就地更新，无需刷新） |
| `kicked` | `{reason}` | 强制下线（封禁/重置密码/登出其他设备） |
| `error` | `{reason, clientMsgId?}` | 业务拒绝（校验失败等；带 clientMsgId 时前端把乐观消息标失败） |

---

## 12. 错误码汇总

| 状态码 | 语义 | 典型 `error` 文案 |
|---|---|---|
| 400 | 参数/业务校验失败 | `请输入关键词`、`内容需为 1-N 个字符`、`文件类型不符合` |
| 401 | 未登录 / token 失效 | `登录已过期，请重新登录`（前端统一跳登录页） |
| 403 | 越权 / 封禁 / 维护 | `账号已被封禁：<原因>`、`没有权限执行此操作` |
| 404 | 资源不存在 | `会话不存在，无法发送消息` |
| 409 | 状态冲突 | `用户名已被占用`、`对方已是好友或申请中`、`你已在群中`、`邮箱已被绑定` |
| 413 | 体积超限 | `插件文件超过 1MB` |
| 422 | 业务规则不允许 | `不能添加自己为好友`、`超过 2 分钟，无法撤回`、`群主不能退群，请先转让` |
| 429 | 限流 | `操作过于频繁，请稍后再试` |
| 500 | 内部错误 | `服务器开小差了，请稍后再试`（真实原因只进日志） |
| 503 | 依赖不可用 / 维护 | `站点维护中`、`站点要求邮箱验证但邮件服务未配置` |

限流统一返回 429；登录失败不泄露账号是否存在（文案恒定）；500 的真实原因只写日志，不进响应体。

---

## 13. 实体形状速查

与 `web/src/types/proto.ts` 完全一致，完整定义见该文件与 `docs/后端需求规格.md` §3.1：

```
User                { id, username, nickname, avatarColor, avatar?, email?, role:"user"|"admin", bannedUntil?, banReason? }
User 摘要            { id, username, nickname, avatarColor, avatar? }
Conversation        { id, type:1|2|3, name, avatar?, peer?, ownerId, memberCount, lastMessageId, lastMessageAt,
                      lastMessagePreview, lastMessageType, lastReadMessageId, unreadCount, pinned, muted,
                      archived?, muteAll?, mentionUnread, announcement, announcementAt, announcementBy }
ConversationMember  { userId, role:"owner"|"admin"|"member", mutedUntil?, joinedAt, user:User摘要 }
Message             { id, conversationId, senderId, clientMsgId, msgType, content, isRecalled, recalledAt,
                      editedAt, replyToId?, replyTo?, mentions?, forwarded?, reactions?, adminDeletedAt?, createdAt }
FriendItem          { id, friend:User摘要, status:0|1|2, createdAt }
Plugin              { id, name, version, description, permissions[], allowedHosts[], enabled, globalDisabled, installedBy, createdAt }
SearchHit           { messageId, conversationId, convType, convName, senderId, senderNickname, msgType, content, createdAt }
```

> `Conversation.type`：1=单聊、2=群聊、3=系统公告会话（不入会话列表展示）。
> `Message.msgType`：内置 `text` / `image` / `audio` / `sticker` / `video`，其余为插件自定义类型（≤32 字符，`^[a-z0-9_-]+$`）。
