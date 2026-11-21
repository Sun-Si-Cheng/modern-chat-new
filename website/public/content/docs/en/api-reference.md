# ModernChat API Reference

> The integrator's manual: every REST + WebSocket endpoint, parameters, responses and errors.
> This is the integration view of the contract in `docs/后端需求规格.md` §3 — semantics and validation rules defer to the requirements spec;
> field types match `web/src/types/proto.ts` exactly (shared front/back contract).
> Last updated: 2026-11-21

---

## 1. Conventions

| Item | Convention |
|---|---|
| Base URL | Same-origin `/api` (the SPA may sit under `/app/` but the API stays at the site root); WS at same-origin `/ws` |
| Field naming | JSON is always **camelCase** |
| Time | ISO8601 UTC (e.g. `2026-10-24T08:30:00.123Z`) |
| Success responses | Return the business body directly — **no `{code, data}` envelope** |
| Error responses | Uniform `{"error": "<Chinese message>"}` (see §9 error codes) |
| Auth | `Authorization: Bearer <JWT>`; login / WS handshake also set a same-origin `mc_auth` HttpOnly cookie (static `/uploads` relies on it; REST may use it too) |
| JWT | HS256, claims `{uid, tv, iat, exp}`, 7-day validity; a `tv` (token_version) change invalidates every device |
| Empty lists | Return `[]`, **never `null`** |
| Pagination | Message history uses the `(conversation_id, id)` cursor (`beforeId`/`afterId`), never OFFSET; admin lists use `page`/`pageSize` |

**Unauthenticated endpoints**: `auth/register`, `auth/login`, `auth/forgot`, `auth/reset`, `auth/logout`, `auth/migrate-login`, `auth/sso/*`, `site`, `invite/:token` (GET), `health`, `version`; `metrics` requires a Bearer token when `METRICS_TOKEN` is set.

**Rate limits** (in-memory token buckets or Redis, bucketed by key; hits return 429):

| Scope | Limit |
|---|---|
| Login / register / forgot / SSO | 10/min per IP |
| Sending messages (WS `chat` frames) | 20/min per user |
| Media uploads | 10/min per user |
| Plugin installs | 5/min per user |
| Change-email codes | 5/min per user |
| Legacy-site migration endpoints | 120/min per IP |

---

## 2. Quick start

```bash
# 1) Sign in and get a token
curl -X POST https://site/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret"}'
# → {"token":"eyJhbGciOi...","user":{"id":1,"username":"alice","nickname":"Alice","avatarColor":"#0052d9",...}}

# 2) Call APIs with the token
curl https://site/api/conversations -H 'Authorization: Bearer <token>'
# → [ { id:7, type:2, name:"公共聊天室", unreadCount:0, ... } ]

# 3) Messages go over WebSocket (§8); uploads are multipart (§6)
```

---

## 3. Auth & account

| Method & path | Request | Response / notes |
|---|---|---|
| POST `/api/auth/register` | `{username, password, nickname, inviteCode?, email?, emailCode?}` | `{token, user}`; the first registered account becomes the owner. Register switch off→403; invite required→400; email verification required → `email`+`emailCode` mandatory, wrong code→422; verification required but SMTP unset→503 (first account exempt) |
| POST `/api/auth/login` | `{username, password}` | `{token, user}`, plus the `mc_auth` cookie. Threshold lockout returns 429; error copy is constant and never reveals whether the account exists |
| POST `/api/auth/logout` | — | `{ok:true}`; **no auth**, clears the cookie only — does not revoke the JWT or kick WS |
| POST `/api/auth/forgot` | `{email}` | `{ok:true}` (succeeds even for unknown emails, anti-enumeration) |
| POST `/api/auth/reset` | `{token, newPassword}` | `{ok:true}`; `token_version+1` invalidates every device |
| POST `/api/auth/email-code` | `{email}` | `{ok:true, cooldownSeconds}`; same-email 60s cooldown (still 200 during cooldown, returning the remaining seconds); email already bound→409 |
| GET `/api/auth/sso/status` | — | `{enabled}`; **no auth**. When unified login is disabled, every sso endpoint below answers 404 |
| GET `/api/auth/sso/start` | `?redirect=` (site-relative) | 302 → the aggregated login center's authorize page |
| GET `/api/auth/sso/callback` | center redirect `?code&state` | verifies → links/creates the account → 302 to `/sso/callback?ticket&redirect` |
| POST `/api/auth/sso/exchange` | `{ticket}` | `{token, user}`; **no auth**, single-use ticket valid 5 minutes |
| POST `/api/auth/qr/start` | — | `{token, expiresAt}`; **no auth**. The desktop creates a QR session (5 minutes) and renders a QR pointing at `/app/qr/<token>` |
| POST `/api/auth/qr/status` | `{token}` | `{status, token?, user?}`; **no auth**, desktop polls every 2.5s (rate limit 60/min). `confirmed` hands out the desktop JWT once |
| POST `/api/auth/qr/confirm` | `{token}` | `{ok:true}`; **requires the phone's JWT**. Authorizes the desktop sign-in; invalid/expired QR→400 |
| POST `/api/auth/migrate-login` | `{token, key}` | `{token, user}`; **no auth**, one-time legacy-site migration ticket (10 minutes); on failure use the original password |
| GET `/api/me` | — | `User` (own record includes email / emailVerified) |
| PATCH `/api/me` | `{nickname?, email?, avatar?, emailCode?}` | updated `User`. Changing email requires `emailCode` (sent to the new address); empty-string `email` unbinds; avatar is a dataURL (≤256KB) or empty string to clear. Nickname/avatar changes broadcast `user_change` |
| POST `/api/me/email-code` | `{email}` (new email) | `{ok:true, cooldownSeconds}`; taken by someone else→409 |
| POST `/api/me/logout-others` | — | `{ok:true}`; `token_version+1` invalidates all devices and kicks them (incl. WS `kicked`) |
| GET `/api/version` | — | `{version, commit, buildTime}`; **no auth** |

---

## 4. Users & friends

| Method & path | Request | Response / notes |
|---|---|---|
| GET `/api/users/search` | `?kw=` (≤50) | `User摘要[]` (≤20, no email) |
| GET `/api/friends` | — | `FriendItem[]` (accepted friends, deduplicated per counterpart) |
| GET `/api/friends/requests` | — | `FriendRequestItem[]` (pending requests pointing at me) |
| POST `/api/friends/apply` | `{toUsername}` | `{ok:true}`; existing relation/request→409; blocks stop both sides |
| POST `/api/friends/handle` | `{requestId, accept}` | `{ok:true}`; accepting sets status=1 on both rows |
| DELETE `/api/friends/:friendId` | path param = **the other user's user_id** | `{ok:true}`; deletes both directions; the single conversation is kept |
| GET `/api/users/:id/block` | — | `{blocked}`; whether I blocked this user |
| POST `/api/users/:id/block` | — | `{ok:true}`; idempotent; cannot block yourself; blocks single chats and friend requests both ways |
| DELETE `/api/users/:id/block` | — | `{ok:true}` |

---

## 5. Conversations & messages

### 5.1 Conversations

| Method & path | Request | Response / notes |
|---|---|---|
| GET `/api/conversations` | — | `Conversation[]` (with peer, unread, pinned/muted/archived, mention unread) |
| GET `/api/conversations/search` | `?kw=` | `Conversation[]` (fuzzy match on type=2 group names, ≤20) |
| POST `/api/conversations/single` | `{toUserId}` (must be friends) | `Conversation`; reuses the existing pair chat (`direct_key` unique constraint prevents concurrent duplicates) |
| POST `/api/conversations/group` | `{name}` (1-30 chars) | `Conversation`; creator becomes owner |
| PATCH `/api/conversations/:id` | `{name}` | `{ok:true}`; owner only |
| PUT `/api/conversations/:id/avatar` | `{avatar}` | `{ok:true}`; owner/admin; URL must belong to this site's storage; empty string restores the default |
| DELETE `/api/conversations/:id` | — | `{ok:true}`; deletes only my inbox row — does not leave the group nor delete messages |
| PATCH `/api/conversations/:id/prefs` | `{pinned?, muted?, archived?}` | `{ok:true, pinned, muted, archived}`; personal settings, affecting only my own inbox row |
| POST `/api/conversations/:id/read` | `{upToId}` | `{ok:true}`; clears unread & mention marks, pushes a `read` frame to online members; the cursor only moves forward (GREATEST) |
| POST `/api/conversations/:id/clear` | `{upToId?}` | `{ok:true, clearedBeforeId}`; clears my history, message bodies are kept |

### 5.2 Members & group management (type=2 only)

| Method & path | Request | Permission / notes |
|---|---|---|
| GET `/api/conversations/:id/members` | — | `ConversationMember[]`; must be a member |
| POST `/api/conversations/:id/join` | — | `{ok:true}`; already a member→409; group full→422 |
| POST `/api/conversations/:id/invite` | `{userIds}` | `{ok:true, added}`; can only invite own friends, ≤50 per batch |
| POST `/api/conversations/:id/invite-link` | `{expiresDays:0-30}` | `{token, path, expiresAt?}`; any member may generate; 0 = permanent |
| POST `/api/conversations/:id/leave` | — | `{ok:true}`; owner cannot leave→422 |
| POST `/api/conversations/:id/kick` | `{userId}` | owner/admin; cannot kick self / the owner / other admins |
| POST `/api/conversations/:id/admin` | `{userId, admin}` | owner only |
| POST `/api/conversations/:id/mute` | `{userId, minutes:0-43200}` | owner/admin; 0 = unmute; cannot mute the owner / other admins |
| POST `/api/conversations/:id/mute-all` | `{muted}` | owner/admin; with mute-all the owner and admins can still speak |
| POST `/api/conversations/:id/transfer` | `{userId}` | `{ok:true}`; the two roles swap |
| PUT `/api/conversations/:id/announcement` | `{content}` (≤500 chars) | owner only; empty string = revoke; pushes `conv_change` to everyone |

### 5.3 Invite links (landing page)

| Method & path | Notes |
|---|---|
| GET `/api/invite/:token` | `{convId,name,avatar,memberCount}`; **no login**; expired/revoked→404 |
| POST `/api/invite/:token/join` | `{ok:true, convId, alreadyMember?}` |

### 5.4 Messages

| Method & path | Request | Response / notes |
|---|---|---|
| GET `/api/conversations/:id/messages` | `?beforeId=&afterId=&limit=` | `{messages: Message[], hasMore}`; limit defaults to 30, ≤50, ≤200 with `afterId`; history pagination is cursor-only |
| DELETE `/api/conversations/:id/messages/:messageId` | — | `{ok:true}`; "delete for me" (the counterpart still sees it); unread is decremented if not yet read |
| GET `/api/search/messages` | `?kw=&convId=&limit=` | `SearchHit[]`; without `convId` = global search (my conversations); keywords ≥2 chars use FULLTEXT ngram; recalled/deleted messages excluded |
| POST `/api/messages/:messageId/favorite` | — | `{ok:true}`; must be a member of the conversation; idempotent |
| DELETE `/api/messages/:messageId/favorite` | — | `{ok:true}` |
| GET `/api/favorites` | `?limit=1..200` | `FavoriteItem[]` (newest first) |
| POST `/api/messages/:messageId/report` | `{reason:1-200 chars}` | `{ok:true, duplicate?}`; recalled→422; one report per user per message |
| POST `/api/messages/:messageId/admin-delete` | — | `{ok:true, duplicate?}`; site admin or group owner/admin; broadcasts `message_admin_deleted` |

> Sending / recall / edit / read receipts / reactions go over **WebSocket** (§11); REST only provides queries and deletion.

### 5.5 Stickers

| Method & path | Notes |
|---|---|
| GET `/api/stickers` | `Sticker[]` (`{id,url,createdAt}`) |
| POST `/api/stickers` | `{url}`; URL must belong to this site's storage; cap 200; same URL is idempotent |
| DELETE `/api/stickers/:id` | only your own |

---

## 6. Files & upload

| Method & path | Request | Response / notes |
|---|---|---|
| POST `/api/upload?kind=image\|audio\|sticker\|file\|video` | multipart `file` | `{kind, size, url}`. Caps: image 8MB / audio 5MB / sticker 2MB / group file 20MB / video 30MB (configurable). **Real type judged from the file header** (disguised extensions→400); streamed processing; 10/min per user |
| GET `/uploads/<year>/<month>/<file>` | — | static file. **Login required** (Bearer or same-origin `mc_auth` cookie); unauthenticated→401, banned→403; unavailable to non-admins in maintenance mode; `Cache-Control: private`. In S3 mode new files are served by the bucket/CDN |
| GET `/api/conversations/:id/files` | — | `ConversationFile[]` (≤100, newest first) |
| POST `/api/conversations/:id/files` | `{name, url, size}` | `ConversationFile`; the URL must belong to this site's storage |
| DELETE `/api/files/:id` | — | `{ok:true}`; the uploader or the group owner |

Group-file type whitelist: pdf / zip / txt / json / jpeg / png / webp / gif — **no html or svg** (prevents same-origin script execution).

---

## 7. Web Push

| Method & path | Request | Notes |
|---|---|---|
| GET `/api/push/key` | — | `{enabled, key}`; `enabled=false` when VAPID is unset |
| POST `/api/push/subscribe` | `{endpoint, p256dh, auth}` | `{ok:true}` |
| DELETE `/api/push/subscribe` | `?endpoint=` | `{ok:true}`; scoped by `user_id + endpoint` |

---

## 8. Plugins

The sandbox model and SDK are described in the plugin dev guide; below is the REST surface.

| Method & path | Request | Response / notes |
|---|---|---|
| GET `/api/plugins` | — | `Plugin[]` (my installs, newest first) |
| GET `/api/plugins/active` | — | `Plugin[]` (enabled and not globally disabled) |
| GET `/api/plugins/:id/code` | — | the `text/javascript` source; global_disabled→403 |
| POST `/api/plugins/upload` | byte stream (`application/octet-stream`, ≤1MB), `?lang=ts\|js` | `Plugin` (enabled=false by default); same-name install by me = upgrade overwrite (header `X-Plugin-Updated: true`); TS is transpiled server-side by esbuild, syntax errors→400 with the line number |
| POST `/api/plugins/from-url` | `{url}` | `Plugin`; SSRF checks run before the server-side fetch |
| PATCH `/api/plugins/:id` | `{enabled}` | updated `Plugin` |
| DELETE `/api/plugins/:id` | — | `{ok:true}` |

---

## 9. Admin console (`role=admin`, otherwise 403)

### 9.1 Overview & users

| Method & path | Request | Notes |
|---|---|---|
| GET `/api/admin/stats` | — | `{users, conversations, messages, plugins, online, uptimeSeconds, goVersion}` |
| GET `/api/admin/users` | `?page=&pageSize=&kw=&role=&status=` | `{users: AdminUserRow[], total, page, pageSize}` |
| GET `/api/admin/users/:id` | — | `{user, stats:{conversations,messages,plugins}, online}` |
| POST `/api/admin/users/:id/kick` | — | `{ok:true, wasOnline}`; disconnects without banning |
| POST `/api/admin/users/:id/ban` | `{days:0-3650, reason?}` | `{ok:true}`; 0 = permanent; tv+1 and pushes `kicked` |
| POST `/api/admin/users/:id/unban` | — | `{ok:true}` |
| POST `/api/admin/users/:id/reset-password` | — | `{newPassword}` (random 12 chars; tv+1 forces re-login) |
| POST `/api/admin/users/:id/role` | `{role:"user"\|"admin"}` | `{ok:true, changed}`; demoting the last owner→403; effective immediately |

### 9.2 Announcements / audits / settings / invites

| Method & path | Request | Notes |
|---|---|---|
| POST `/api/admin/announce` | `{content}` (1-1000 chars) | `{ok:true, convId, messageId, recipients}`; writes the system conversation and pushes over WS to everyone |
| GET `/api/admin/audits` | `?page=&pageSize=` | `{audits, total, page, pageSize}`; operation audit, newest first |
| GET `/api/admin/settings` | — | per-domain effective values; **secrets appear only as hasXxx, never echoed** |
| PUT `/api/admin/settings` | `{smtp?, redis?, storage?, site?, chat?, upload?, security?}` | `{ok:true, redisNeedsRestart, storageApplied}`; empty secrets = unchanged; SMTP hot-applies, Redis needs a restart; out-of-range values→400; writes an audit |
| POST `/api/admin/settings/smtp/test` | `{to}` | `{ok:true}`; failure→400 with the mail server's original error; writes an audit |
| GET `/api/admin/invites` | — | `InviteCode[]` |
| POST `/api/admin/invites` | `{maxUses:1-1000, expiresDays:0-365, count:1-20}` | `InviteCode[]` |
| POST `/api/admin/invites/:id/disable` | `{disabled}` | `{ok:true}`; disables without deleting |

### 9.3 Storage / backups / online update

| Method & path | Request | Notes |
|---|---|---|
| GET `/api/admin/storage/stats` | — | `{files, bytes, referenced, orphans, orphanBytes}`; local storage only |
| POST `/api/admin/storage/gc` | `{dryRun?}` | `{ok:true, dryRun, deleted, freedBytes}`; cleans orphan attachments |
| GET `/api/admin/backups` | — | `{backups: BackupFile[], dir}` |
| POST `/api/admin/backups` | — | `{ok:true, file}`; requires mysqldump |
| GET `/api/admin/update/status` | — | `{available, busy, phase, lines, error?, version, commit, mode?, repo?}` |
| GET `/api/admin/update/check` | — | `{upToDate, local, remote, remoteTag, remoteMsg, remoteNotes?}` (notes truncated to 8KB) |
| POST `/api/admin/update/start` | — | `{started:true}`; systemd deployments only; writes the `web_update` audit |
| GET `/api/admin/plugins` | `?page=&pageSize=` | aggregated by name: `{name, version, installCount, enabledCount, globalDisabled, createdAt}` |
| POST `/api/admin/plugins/global-disable` | `{name, disabled}` | `{ok:true, affected}`; effective for every user immediately |
| POST `/api/admin/plugins/purge` | `{name, userId?:0}` | `{ok:true, affected}`; physically deletes install records |

---

## 10. Public endpoints

| Method & path | Notes |
|---|---|
| GET `/api/health` | `{"status":"ok"}`; liveness probe |
| GET `/api/site` | public site config: `{name, allowRegister, maintenanceMode, maintenanceMessage, requireInviteCode, requireEmail, requireEmailVerify, maxMessageLength, recallMinutes, maxGroupMembers, maxImageMB, maxAudioMB, maxFileMB, maxVideoMB}`; still available in maintenance mode |
| GET `/api/metrics` | Prometheus text format; requires a Bearer token when `METRICS_TOKEN` is set |

---

## 11. WebSocket (`/ws`)

### 11.1 Connection & lifecycle

- Handshake: `GET /ws?token=<JWT>` (`?token=` first, falling back to the same-origin `mc_auth` cookie); validates the signature / tv / ban / **Origin allowlist** (`FRONTEND_URL`; non-browser clients without an Origin are allowed); failures close with 401/403/503
- Maintenance mode: non-admin handshakes get 503 `{"error":"站点维护中","maintenance":true}`
- Heartbeat: the client sends `{"type":"ping"}` every 25s → the server answers `{"type":"pong"}` immediately; the server closes after a 60s read timeout
- **Every frame** is re-checked for ban & tv before routing (failures get an `error` frame and disconnect)
- Uplink frame byte cap 64 KB; tv changes / bans / owner password resets push `kicked` then close

### 11.2 Uplink frames (client → server, 8 kinds)

| type | payload | notes |
|---|---|---|
| `chat` | `{convId, clientMsgId, content, msgType?, replyToId?, mentions?, forwarded?}` | send a message; 20/min per user; `client_msg_id` has a unique index for idempotency (duplicates replay the original frame); member/ban/length checks; media content must be this site's storage address |
| `typing` | `{convId}` | typing indicator (forwarded to online members) |
| `read` | `{convId, upToId}` | read receipt (clears unread; cursor only moves forward) |
| `recall` | `{messageId}` | recall; sender only, within `recallMinutes` (configurable, 0 = disabled); unread rolls back, broadcasts `recall` |
| `edit` | `{messageId, content}` | edit; sender only, within 5 minutes of creation, un-recalled `text`; length & blocked-word checks like sending |
| `reaction` | `{messageId, emoji, action:"set"\|"remove"}` | one reaction per user per message; broadcasts the delta |
| `delivered` | `{messageId}` | single-chat delivery receipt (forwarded only to the sender) |
| `ping` | — | heartbeat |

### 11.3 Downlink frames (server → client, 13 kinds)

| type | payload | when |
|---|---|---|
| `message` | full message body (with the `replyTo` brief and `mentions`, no lookups needed) | new message (fan-out to members; unread recipients also get `conv_change`) |
| `conv_change` | `{convId, lastMessage…, unread…}` | conversation changed (new message / announcement / membership) |
| `typing` | `{convId, userId}` | the counterpart is typing |
| `read` | `{convId, userId, upToId}` | a member read up to a point |
| `recall` | `{messageId, convId, recalledAt}` | recall (server time; clients show the notice for 3 minutes) |
| `message_update` | `{id, convId, content, editedAt}` | edit succeeded |
| `reaction` | `{messageId, convId, emoji, userId, action}` | reaction delta |
| `message_delivered` | `{messageId, convId, userId}` | single-chat delivery |
| `message_admin_deleted` | `{messageId, convId}` | admin deletion |
| `presence` | `{userId, online}` | friend online/offline |
| `user_change` | `{userId, nickname, avatar, avatarColor}` | profile change (update in place, no refresh) |
| `kicked` | `{reason}` | forced sign-out (ban / password reset / logout-others) |
| `error` | `{reason, clientMsgId?}` | business rejection (validation failures etc.; with clientMsgId the client marks the optimistic message failed) |

---

## 12. Error codes

| Status | Meaning | Typical `error` copy |
|---|---|---|
| 400 | validation / business rule | `请输入关键词`, `内容需为 1-N 个字符`, `文件类型不符合` |
| 401 | not signed in / token expired | `登录已过期，请重新登录` (clients redirect to login) |
| 403 | forbidden / banned / maintenance | `账号已被封禁：<原因>`, `没有权限执行此操作` |
| 404 | not found | `会话不存在，无法发送消息` |
| 409 | state conflict | `用户名已被占用`, `对方已是好友或申请中`, `你已在群中`, `邮箱已被绑定` |
| 413 | too large | `插件文件超过 1MB` |
| 422 | business rule disallows | `不能添加自己为好友`, `超过 2 分钟，无法撤回`, `群主不能退群，请先转让` |
| 429 | rate limited | `操作过于频繁，请稍后再试` |
| 500 | internal error | `服务器开小差了，请稍后再试` (the real cause only goes to logs) |
| 503 | dependency unavailable / maintenance | `站点维护中`, `站点要求邮箱验证但邮件服务未配置` |

Rate limiting always returns 429; login failures never reveal whether the account exists (constant copy); 500 causes are logged, never returned.

---

## 13. Entity shapes at a glance

Identical to `web/src/types/proto.ts`; full definitions live there and in `docs/后端需求规格.md` §3.1:

```
User                { id, username, nickname, avatarColor, avatar?, email?, role:"user"|"admin", bannedUntil?, banReason? }
User brief          { id, username, nickname, avatarColor, avatar? }
Conversation        { id, type:1|2|3, name, avatar?, peer?, ownerId, memberCount, lastMessageId, lastMessageAt,
                      lastMessagePreview, lastMessageType, lastReadMessageId, unreadCount, pinned, muted,
                      archived?, muteAll?, mentionUnread, announcement, announcementAt, announcementBy }
ConversationMember  { userId, role:"owner"|"admin"|"member", mutedUntil?, joinedAt, user:User brief }
Message             { id, conversationId, senderId, clientMsgId, msgType, content, isRecalled, recalledAt,
                      editedAt, replyToId?, replyTo?, mentions?, forwarded?, reactions?, adminDeletedAt?, createdAt }
FriendItem          { id, friend:User brief, status:0|1|2, createdAt }
Plugin              { id, name, version, description, permissions[], allowedHosts[], enabled, globalDisabled, installedBy, createdAt }
SearchHit           { messageId, conversationId, convType, convName, senderId, senderNickname, msgType, content, createdAt }
```

> `Conversation.type`: 1 = single chat, 2 = group, 3 = system announcement conversation (not shown in the list).
> `Message.msgType`: built-in `text` / `image` / `audio` / `sticker` / `video`; anything else is a plugin-defined type (≤32 chars, `^[a-z0-9_-]+$`).
