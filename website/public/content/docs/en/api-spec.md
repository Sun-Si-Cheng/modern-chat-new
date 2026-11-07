# ModernChat Backend Spec (server/)

> This document is the implementation spec of `docs/设计文档.md`; on conflict **this document wins** (field names are aligned with the implemented frontend).
>
> **Status: fully implemented (2026-10-24)**. Both ends implement this contract; integration tests run the full chain on real MySQL (register→friends→direct→WS send/receive→idempotency→read→recall→unread→groups→ban kick→plugin install/upgrade/SSRF/global takedown).
> Verify with: `cd server && go test ./... && go test -tags integration ./test/`; frontend `cd web && npm run type-check && npm run lint && npm run build`.

---

## 1. Target Features

Single-instance Go service, module list:

| Module | Contents |
|---|---|
| Auth | Register / login / forgot / reset password; JWT + `token_version` active revocation; ban & forced logout |
| User | Current profile, nickname/email change, user search |
| Friends | Search & apply / accept / reject / delete; incoming requests list |
| Conversations | Direct chat (idempotent reuse) / group chat (create / search / join / leave), group management (rename / kick / transfer), conversation list (last message + unread) |
| Messages | History cursor pagination, send (WS), recall (default 2 minutes, 0–60 configurable in the admin console), edit within 5 minutes (text only), read receipts, typing, presence, message content search (in-conversation / global) |
| Realtime | WebSocket pipeline (heartbeat, reconnect tolerance, fan-out, presence) |
| Plugins | Upload / URL install (SSRF protection), enable/disable / uninstall, code serving, global takedown |
| Admin | User pagination, ban / unban / reset password |
| Ops | Health check `/api/health`, version `/api/version`, structured logs, three rate-limit tiers, one-command upgrade script |

**Interface types**: REST (JSON, prefix `/api`) + WebSocket (`/ws`). File storage is optional — local disk or S3-compatible object storage (images / audio / video / group files); avatars are user-uploaded data URLs, falling back to an id-derived color when not uploaded. No third-party dependencies (SMTP / Redis optional).

## 2. Tech Stack & Versions

| Item | Requirement |
|---|---|
| Go | **1.26+** (`go.mod` is `go 1.26.0`, required by the dependency chain) |
| HTTP | `github.com/gin-gonic/gin` v1.10+ |
| WS | `github.com/gorilla/websocket` v1.5+ |
| ORM | `gorm.io/gorm` v1.25+ + `gorm.io/driver/mysql` v1.5+ |
| JWT | `github.com/golang-jwt/jwt/v5` v5.2+, HS256 |
| Password | `golang.org/x/crypto/bcrypt` (cost 10) |
| Rate limit | `golang.org/x/time/rate` (in-memory token buckets) |
| Migrations | Dev: AutoMigrate; prod: `github.com/golang-migrate/migrate/v4` |
| DB | MySQL 8.0.x, utf8mb4 / InnoDB, time columns `DATETIME(3)` |

**Free choices** (preferences, not mandates): logging via stdlib `log/slog` (JSON to stdout); config via `os.Getenv` or a lightweight env library; assertions via `stretchr/testify`. **Do not** import framework suites (go-zero, kratos, etc.) or data-access layers beyond the ORM.

## 3. API Contract

Global conventions:
- JSON fields are **camelCase** (aligned with the frontend `proto.ts`); times are ISO8601 UTC (`2026-10-24T08:30:00.123Z`)
- Error responses are `{"error": "中文消息"}` (see §5); success returns the business body directly, no `{code,data}` envelope
- Everything except auth/register, auth/login, auth/forgot, auth/reset, health requires `Authorization: Bearer <JWT>`

### 3.1 Entity shapes (matching `web/src/types/proto.ts`)

```
User            { id, username, nickname, avatarColor, avatar?, email?, role:"user"|"admin",
                  bannedUntil?, banReason? }            // email/banned only returned to self or admin
User summary    { id, username, nickname, avatarColor, avatar? }
Conversation    { id, type:1|2|3, name(group name, null for direct), avatar?, peer(direct peer summary, null for group),
                  ownerId, memberCount, lastMessageId, lastMessageAt, lastMessagePreview,
                  lastMessageType, lastReadMessageId, unreadCount, pinned, muted, archived?,
                  muteAll?, mentionUnread, announcement, announcementAt, announcementBy }
                  // type=3 is the system announcement conversation, not shown in the conversation list
                  // lastMessageType: type of the last message; media and recall are replaced with placeholder text by the frontend
                  // (image/audio/sticker/video show the matching placeholder; recall is "recalled" with an empty preview)
ConversationMember { userId, role:"owner"|"admin"|"member", mutedUntil?, joinedAt, user:User summary }
Message         { id, conversationId, senderId, clientMsgId, msgType, content, isRecalled, recalledAt, editedAt,
                  replyToId?, replyTo?, mentions?, forwarded?, reactions?, adminDeletedAt?, createdAt }
                // recalledAt: server-side recall time (RFC3339); the WS recall broadcast frame carries the same field;
                // the client uses it to keep the recall notice visible for 3 minutes, consistent across refreshes/devices.
                // editedAt: edit time (RFC3339), NULL means never edited; the WS message_update broadcast frame carries the same field.
FriendItem      { id(relation row id), friend:User summary, status:0|1|2, createdAt }
FriendRequestItem { id(request row id), from:User summary, createdAt }
Plugin          { id, name, version, description, permissions:string[], allowedHosts:string[],
                  enabled, globalDisabled, installedBy, createdAt }
VersionInfo     { version, commit, buildTime }
SearchHit       { messageId, conversationId, convType, convName, senderId, senderNickname, msgType, content, createdAt }
AdminPluginRow  { name, version, installCount, enabledCount, globalDisabled, createdAt }  // aggregated by name
```

### 3.2 REST details

| Method & path | Body / query | Success response |
|---|---|---|
| POST `/api/auth/register` | `{username, password, nickname, inviteCode?, email?, emailCode?}` | `{token, user}` (first registrant role=admin). Subject to maintenance mode (503), the registration switch (403), invite code (400) and email verification (400/409/422); if invite-code consumption fails the quota is refunded automatically. When email verification is enabled, `email` + `emailCode` are required; a failed code consumption returns 422 "Verification code is wrong or expired" |
| POST `/api/auth/email-code` | `{email}` | `{ok:true, cooldownSeconds}`; email verification disabled→400, email already bound→409, SMTP unavailable→503; same email has a 60-second cooldown (still 200 during the cooldown, returning the remaining seconds) |
| POST `/api/auth/login` | `{username, password}` | `{token, user}`, and sets the `mc_auth` cookie |
| POST `/api/auth/logout` | — | `{ok:true}`; **no auth required** (available in maintenance mode), only clears the `mc_auth` cookie; does not revoke the JWT, does not kick WS |
| POST `/api/auth/forgot` | `{email}` | `{ok:true}` (unknown emails also return success; dev prints the reset link to the log) |
| POST `/api/auth/reset` | `{token, newPassword}` | `{ok:true}`; `token_version+1` kills all sessions |
| GET `/api/me` | — | `User` (with email) |
| PATCH `/api/me` | `{nickname?, email?, avatar?, emailCode?}`. **Changing the bound email requires `emailCode`** (sent to the new address; stored and `email_verified=true` only after successful consumption; an empty `email` string means unbind and needs no verification); if the email is unchanged the verification state is untouched; with SMTP unconfigured the change returns 400. avatar is `data:image/(png\|jpeg\|webp);base64,…`, frontend compresses to 256×256, server validates prefix & ≤256KB, empty string clears | updated `User` |
| POST `/api/me/email-code` | `{email}` (new address) | `{ok:true, cooldownSeconds}`; same as the current email→400, taken by someone else→409, SMTP unavailable→503; 5 times/min per user, 60-second cooldown per email |
| POST `/api/me/logout-others` | — | `{ok:true}`; `token_version+1` invalidates all old JWTs; also actively `KickUser`s all established WS connections of that account (sending `kicked` frames), the current device must log in again too |
| GET `/api/users/search` | `?kw=` (≤50) | `User summary[]` (no email, ≤20 rows) |
| GET `/api/friends` | — | `FriendItem[]` (accepted friends; both-direction rows deduped per peer, one row per person) |
| GET `/api/friends/requests` | — | `FriendRequestItem[]` (status=0 pointing at me) |
| POST `/api/friends/apply` | `{toUsername}` | `{ok:true}`; existing 0/1 relation→409 |
| POST `/api/friends/handle` | `{requestId, accept:boolean}` | `{ok:true}`; on accept both sides get status=1 |
| DELETE `/api/friends/:friendId` | path param = **peer user_id** | `{ok:true}`; deletes both status=1 rows, direct conversations kept |
| GET `/api/conversations` | — | `Conversation[]` (with peer assembly, unread) |
| GET `/api/conversations/search` | `?kw=` | `Conversation[]` (type=2 name fuzzy match, ≤20 rows) |
| POST `/api/conversations/single` | `{toUserId}` (must be friends) | `Conversation`; an existing two-person type=1 conversation is reused if found |
| POST `/api/conversations/group` | `{name}` | `Conversation` (creator becomes owner and joins) |
| GET `/api/conversations/:id/messages` | `?beforeId=&afterId=&limit=` (limit default 30, ≤50; with `afterId` it returns an ascending incremental backfill, limit≤200) | `{messages: Message[] (id descending for beforeId / ascending for afterId), hasMore: bool}` |
| DELETE `/api/conversations/:id/messages/:messageId` | — | `{ok:true}`; self-only deletion, writes `message_deletions`, still visible to the peer |
| GET `/api/conversations/:id/members` | — | `ConversationMember[]` (must be a member) |
| POST `/api/conversations/:id/join` | — | `{ok:true}` (type=2; already joined→409) |
| POST `/api/conversations/:id/invite` | `{userIds:number[]}` | `{ok:true, added}`; only your own friends can be invited, at most 50; the whole batch over the cap→422 |
| POST `/api/conversations/:id/invite-link` | `{expiresDays:0-30}` | `{token, path, expiresAt?}`; any member can generate it, 0=permanent |
| GET `/api/invite/:token` | — | `{convId,name,avatar,memberCount}`; **no login required** |
| POST `/api/invite/:token/join` | — | `{ok:true, convId, alreadyMember?}` |
| POST `/api/conversations/:id/leave` | — | `{ok:true}` (owner can't leave→422) |
| DELETE `/api/conversations/:id` | — | `{ok:true}`; deletes only your own inbox row, does not leave the group, does not delete messages |
| POST `/api/conversations/:id/clear` | `{upToId?}` | `{ok:true, clearedBeforeId}`; clears my history (message bodies are kept) |
| POST `/api/conversations/:id/read` | `{upToId}` | `{ok:true}`; clears unread and `mention_unread`, writes last_read_message_id, pushes `read` frames to online members |
| PATCH `/api/conversations/:id/prefs` | `{pinned?:bool, muted?:bool, archived?:bool}` (at least one) | `{ok:true, pinned, muted, archived}`; **personal preferences**, affecting only the caller's own inbox row; omitted fields stay unchanged |
| PUT `/api/conversations/:id/announcement` | `{content}` (≤500 chars, empty string=revoke) | `{ok:true}`; group owner only, `type=2` only; pushes `conv_change` to all members after the change |
| GET `/api/conversations/:id/files` | — | `ConversationFile[]` (`{id,conversationId,uploaderId,uploaderName,name,url,size,createdAt}`, newest→oldest, ≤100, `[]` when empty) |
| POST `/api/conversations/:id/files` | `{name(1-120), url, size}` | `ConversationFile`; `url` must belong to this site's storage |
| DELETE `/api/files/:id` | — | `{ok:true}`; uploader or group owner |
| GET `/api/stickers` | — | `Sticker[]` (`{id,url,createdAt}`, `[]` when empty) |
| POST `/api/stickers` | `{url}` | `Sticker`; idempotent per URL; cap 200; URL must belong to this site's storage |
| DELETE `/api/stickers/:id` | — | `{ok:true}`; only your own |
| PATCH `/api/conversations/:id` | `{name}` | `{ok:true}` (owner only) |
| PUT `/api/conversations/:id/avatar` | `{avatar}` (empty string=restore default) | `{ok:true}`; group owner / admin; URL must belong to this site's storage |
| POST `/api/conversations/:id/kick` | `{userId}` | `{ok:true}` (group owner / admin; can't kick self / the owner / another admin→403/422) |
| POST `/api/conversations/:id/admin` | `{userId, admin:boolean}` | `{ok:true}`; group owner only, the owner's role can't be changed |
| POST `/api/conversations/:id/mute` | `{userId, minutes:0-43200}` | `{ok:true, mutedUntil?}`; group owner / admin; 0=unmute; can't mute the owner / another admin |
| POST `/api/conversations/:id/mute-all` | `{muted:boolean}` | `{ok:true}`; group owner / admin; after mute-all the owner and admins can still speak |
| POST `/api/conversations/:id/transfer` | `{userId}` (must be a member) | `{ok:true}`; roles swapped |
| POST `/api/upload?kind=image\|audio\|sticker\|file\|video` | multipart `file` (image ≤8 MB / audio ≤5 MB / sticker ≤2 MB / group file ≤20 MB / **video ≤30 MB, all configurable in the admin console**) | `{kind, size, url}`; **the real type is determined from the file header** (spoofed extensions → 400); **streamed processing** (multipart segments go straight to the storage layer, the file never lands in memory whole); 10 times/min rate limit per user. For `kind=image` an extra ≤480px JPEG thumbnail is generated (`_t.jpg`, skipped for GIF / too-small images / decode failures; transparent images get a white background), the response shape is unchanged — the frontend derives the thumbnail URL from `url` by convention and falls back to the original if missing |
| GET `/uploads/<year>/<month>/<file>` | — | Static file (always mounted, compatible with legacy URLs). **Login required**: `Authorization: Bearer` or same-origin `mc_auth` cookie (`UploadAuth`); not logged in 401, banned 403; directory listing 404; `Cache-Control: private, max-age=3600`, `X-Content-Type-Options: nosniff`; in maintenance mode non-admins can't access it. In S3 mode new files are served by the bucket/CDN and do not go through this auth |
| GET `/api/favorites` | `?limit=1..200` (default 100) | `FavoriteItem[]` (by favorite time descending); **returns `[]` rather than `null` when empty** |
| POST `/api/messages/:messageId/favorite` | — | `{ok:true}`; must be a member of the conversation the message belongs to; idempotent |
| DELETE `/api/messages/:messageId/favorite` | — | `{ok:true}` |
| GET `/api/search/messages` | `?kw=(1–50 chars)&convId=&limit=(1–100, default 30)` | `SearchHit[]` (`[]` when empty). With `convId` it is an in-conversation search (must be a member), without it a global search (only conversations I have joined); **keywords of ≥2 characters use FULLTEXT ngram phrase matching** (migration `0022`, index created idempotently at startup), single characters and index failures fall back to LIKE; recalled, admin-deleted and self-deleted messages are excluded, ordered by id descending; for direct chats `convName` is filled with the peer's nickname |
| GET `/api/users/:id/block` | — | `{blocked:boolean}`; whether I have blocked that user |
| POST `/api/users/:id/block` | — | `{ok:true}`; idempotent; can't block yourself |
| DELETE `/api/users/:id/block` | — | `{ok:true}` |
| POST `/api/messages/:messageId/report` | `{reason:1-200 chars}` | `{ok:true, duplicate?:boolean}`; must be a conversation member; recalled→422 |
| POST `/api/messages/:messageId/admin-delete` | — | `{ok:true, duplicate?:boolean}`; site admin or the group owner / admin of that group; broadcasts `message_admin_deleted` |
| GET `/api/push/key` | — | `{enabled:boolean, key:string}`; `enabled=false` when VAPID is unconfigured |
| POST `/api/push/subscribe` | `{endpoint,p256dh,auth}` | `{ok:true}` |
| DELETE `/api/push/subscribe` | `?endpoint=` | `{ok:true}` |
| GET `/api/plugins` | — | `Plugin[]` (installedBy=me, createdAt desc) |
| GET `/api/plugins/active` | — | `Plugin[]` (enabled=1 and global_disabled=0) |
| GET `/api/plugins/:id/code` | — | `text/javascript` source (must be own install; global_disabled→403) |
| POST `/api/plugins/upload` | body = plugin file bytes (`Content-Type: application/octet-stream`, ≤1MB); `?lang=ts\|js` selects the language, omitted means inferred from source features | `Plugin` (default enabled=false); hitting "own + same name" upgrades in place, response header `X-Plugin-Updated: true` |
| POST `/api/plugins/from-url` | `{url}` | `Plugin`; §6 SSRF checks run before fetching; same-name upgrade semantics as above |
| PATCH `/api/plugins/:id` | `{enabled:boolean}` | updated `Plugin` |
| DELETE `/api/plugins/:id` | — | `{ok:true}` |
| GET `/api/admin/stats` | — | `{users:{total,today,banned,admins}, conversations:{single,group}, messages:{total,today}, plugins:{installed,enabled,disabled}, online, uptimeSeconds, goVersion}` |
| GET `/api/admin/users` | `?page=&pageSize=&kw=&role=&status=` (`kw` matches username/nickname, `role`=user\|admin, `status`=active\|banned) | `{users: AdminUserRow[], total, page, pageSize}`; AdminUserRow = User + `createdAt` |
| GET `/api/admin/users/:id` | — | `{user: AdminUserRow, stats:{conversations,messages,plugins}, online}` |
| POST `/api/admin/users/:id/kick` | — | `{ok:true, wasOnline}`; disconnects only, no ban (the user can log in again immediately) |
| POST `/api/admin/users/:id/ban` | `{days:0-3650, reason?}` (0=permanent) | `{ok:true}`; tv+1 and pushes `kicked` to all their connections |
| POST `/api/admin/users/:id/unban` | — | `{ok:true}` |
| POST `/api/admin/users/:id/reset-password` | — | `{newPassword}` (random 12 chars; tv+1 force logout) |
| GET `/api/admin/plugins` | `?page=&pageSize=` | `{plugins: AdminPluginRow[], total, page, pageSize}`; **aggregated by name**: `{name, version, installCount, enabledCount, globalDisabled, createdAt}` |
| POST `/api/admin/plugins/global-disable` | `{name, disabled:boolean}` | `{ok:true, affected}`; updates all installs of that name, effective for all users immediately |
| POST `/api/admin/plugins/purge` | `{name, userId?:0}` | `{ok:true, affected}`; **physically deletes** the install records of that name (`userId=0` means all users); 404 when there are no records |
| POST `/api/admin/users/:id/role` | `{role:"user"\|"admin"}` | `{ok:true, changed}`; demoting the last site admin returns 403; role changes take effect immediately (the auth middleware queries the DB every time) |
| POST `/api/admin/announce` | `{content}` (1-1000 chars) | `{ok:true, convId, messageId, recipients}`; writes into the system conversation (`conversations.type=3`) and pushes to all users via WS |
| GET `/api/admin/audits` | `?page=&pageSize=` | `{audits: AdminAudit[], total, page, pageSize}`; `AdminAudit = {id, adminId, action, targetType, targetId, targetName, detail, ip, createdAt}`, descending |
| GET `/api/site` | **no login required** | `{name, allowRegister, maintenanceMode, maintenanceMessage, requireInviteCode, requireEmail, requireEmailVerify, maxMessageLength, recallMinutes, maxGroupMembers, maxImageMB, maxAudioMB, maxFileMB, maxVideoMB}`; public fields only, still available in maintenance mode (the frontend maintenance page gets its copy from it). `requireEmail` = whether email is required at registration (true whenever the switch is on, including the first account); `requireEmailVerify` = whether a code is also needed (switch && SMTP available && accounts already exist; the first account is exempt). Both are the **effective values** |
| GET `/api/admin/settings` | — | `{smtp, redis, storage, site:{name,allowRegister,maintenanceMode,maintenanceMessage,requireInviteCode,requireEmailVerify}, chat:{maxMessageLength,recallMinutes,maxGroupMembers,blockedWords}, upload:{maxImageMB,maxAudioMB,maxFileMB,maxVideoMB,maxStorageMB}, security:{loginMaxAttempts,loginLockMinutes}, smtpEnabled, redisEnabled, storageApplied, redisNeedsRestart}`; **secrets are only exposed as hasXxx, never echoed back**; the site / chat / upload / security parts show the **currently effective values** (including defaults) |
| PUT `/api/admin/settings` | `{smtp?, redis?, storage?, site?, chat?, upload?, security?}` | `{ok:true, redisNeedsRestart, storageApplied}`; empty secret fields = unchanged; SMTP takes effect immediately after saving, Redis needs a restart; site / chat / upload / security **take effect immediately**; out-of-range values return 400 (no silent clamping); writes the audit `save_settings` |
| GET `/api/admin/invites` | — | `InviteCode[]` (`{id,code,createdBy,creator,maxUses,uses,expiresAt,disabled,createdAt}`, newest→oldest) |
| POST `/api/admin/invites` | `{maxUses:1-1000, expiresDays:0-365, count:1-20}` | `InviteCode[]`; the code is a crypto/rand 12-byte hex string (24 chars) |
| POST `/api/admin/invites/:id/disable` | `{disabled}` | `{ok:true}`; disable / re-enable, never delete (keeps the audit trail) |
| POST `/api/admin/settings/smtp/test` | `{to}` | `{ok:true}`; failure returns 400 with the mail server's raw error in `error`; writes the audit `test_smtp` |
| GET `/api/admin/storage/stats` | — | `{files,bytes,referenced,orphans,orphanBytes}`; local storage only |
| POST `/api/admin/storage/gc` | `{dryRun?:boolean}` | `{ok:true,dryRun,deleted,freedBytes}`; deletes files not referenced by messages / stickers / group files / avatars |
| GET `/api/admin/backups` | — | `{backups: BackupFile[], dir}`; `BackupFile = {name,size,createdAt}` |
| POST `/api/admin/backups` | — | `{ok:true,file}`; requires `mysqldump` (`MYSQLDUMP_PATH`) |
| GET `/api/admin/update/status` | — | `{available,busy,phase,lines,error?,version,commit,mode?}`; polled by the frontend |
| GET `/api/admin/update/check` | — | `{upToDate,local,remote,remoteTag,remoteMsg}` |
| POST `/api/admin/update/start` | — | `{started:true}`; systemd deployments only; unavailable when `WEB_UPDATE=false`; writes the audit `web_update` |
| GET `/api/health` | no auth | `{"status":"ok"}` (Nginx/container probes) |
| GET `/api/version` | no auth | `{version, commit, buildTime}` (injected at build via ldflags; `go run` yields `dev/unknown`) |
| GET `/api/metrics` | `Authorization: Bearer` required when `METRICS_TOKEN` is non-empty | Prometheus text format |

### 3.3 WebSocket (`/ws`)

- Connection: `GET /ws?token=<JWT>`; the handshake validates signature, tv, ban; failures close directly (401/403 responses)
- Heartbeat: client sends `{"type":"ping"}` every 25s, server replies `{"type":"pong"}` immediately; server read timeout 60s disconnects
- Uplink & downlink frames **match `web/src/types/proto.ts` field for field** (the `chat` frame's `msgType` is a string; plugin custom types pass through and are stored)
- Every uplink frame first checks "is the sender a member of `convId`", otherwise replies `{"type":"error","reason":"..."}`
- Send pipeline (design doc §9.1): member/ban/rate checks → `client_msg_id` unique-index idempotency (duplicates **replay the original message frame**) → write messages → batch upsert user_conversations (unread+1 for non-senders) → fan-out `message` to online members → push `conv_change` to receivers with unread
- Recall: only by the sender, within `chat.recallMinutes` minutes (configurable in the admin console, **0 = recall disabled**, returning "Recall is disabled on this site"); if a receiver's `last_read_message_id < recalled id`, unread-1; persisted together with `recalled_at`, and the broadcast frame `{"type":"recall","messageId","convId","recalledAt"}` carries the server time (the client uses it to keep the notice visible for 3 minutes, consistent across devices)
- Edit: uplink `{"type":"edit","messageId":<id>,"content":"<new body>"}`; **only the sender, within 5 minutes of creation, on a non-recalled `text` message**; content length and sensitive-word validation are the same as sending; on success `edited_at` is written and `{"type":"message_update","id":<id>,"convId":<id>,"content":"<new body>","editedAt":"<RFC3339>"}` is broadcast to all members of the conversation
- Delivered: after a direct-chat receiver gets a message it uplinks `{"type":"delivered","messageId":<id>}`, the server forwards it only to the sender as `{"type":"message_delivered","messageId","convId","userId"}`
- Emoji reactions: uplink `{"type":"reaction","messageId","emoji","action":"set"|"remove"}`; at most one emoji per person per message, broadcasting the delta `{"type":"reaction","messageId","convId","emoji","before?","userId","action"}`
- Admin delete: after REST `POST /api/messages/:id/admin-delete` succeeds, `{"type":"message_admin_deleted","messageId","convId"}` is broadcast and each client removes it from the chat history
- Image/audio/sticker/video: the `chat` frame's `content` must be a URL uploaded to this site, otherwise an `error` frame is returned and the message dropped (no external links)
- Length: `content` is capped at `chat.maxMessageLength` (configurable 100–5000 in the admin console); over the limit returns an `error` frame "Message must be 1-N characters"; **uplink frame byte cap 64 KB** (`ws/serve.go` `maxMsgSize`, must cover 5000 UTF-8 chars + JSON overhead)
- Sensitive words: a text message matching `chat.blockedWords` is rejected (degenerating to an `error` frame); edits are validated the same way
- Maintenance mode: non-admin handshakes get 503 `{"error":"站点维护中","maintenance":true}` — REST is blocked but existing connections could still send/receive, which would mean no blocking at all, so the handshake must block too
- Replies & @: the `chat` frame may carry `replyToId` (must be in the same conversation) and `mentions` (an array of group member ids); the downlink `message` frame carries back the `replyTo` summary and `mentions` so receivers can render the quote block and highlights without extra lookups
- Forced logout: tv change / ban / admin reset → push `{"type":"kicked","reason":"..."}` to all the user's connections, then close
- Profile changes: after `PATCH /api/me` changes nickname or avatar, push `{"type":"user_change","userId","nickname","avatar","avatarColor"}` to friends, co-members and the user's other connections; the frontend updates conversation list, friend/request lists and group member avatars in place, no refresh

## 4. Data Model & Validation

All tables (users / conversations / conversation_members / messages / user_conversations / message_deletions / message_favorites / message_reactions / user_blocks / message_reports / friends / plugins / password_resets / admin_audits / system_settings / stickers / conversation_files / conversation_invites / invite_codes / login_attempts / push_subscriptions / email_codes) — structures in design doc §5; here only validations and key indexes.

| Field | Rule |
|---|---|
| username | 3–20, `^[a-zA-Z0-9_-]+$`, unique, unique index |
| password | 6–72 chars (bcrypt caps at 72 bytes, over-length → 400) |
| nickname | 1–20, non-empty after trim |
| email | ≤254, nullable; unique when non-empty |
| avatar_color | server derives from a 10-color palette by id modulo |
| group name | 1–30 |
| content | `text`: 1–`chat.maxMessageLength` chars (by rune, configurable 100–5000 in the admin console, default 1000); `image`/`audio`/`sticker`/`video`: a URL of ≤1000 chars that must fall under an address recognized by `Storage.Owns()` (no external links) |
| msg_type | ≤32, `^[a-z0-9_-]+$`; defaults to `text`; built-in `text`/`image`/`audio`/`sticker`/`video`, everything else is a plugin custom type |
| uploaded file | size caps configurable in the admin console (defaults 8 / 5 / 2 / 20 / **30** MB for image / audio / sticker / group file / video, video 1–100); **the real type comes from the file header** (image/jpeg,png,webp,gif; audio/webm,ogg,mpeg,mp4,wav; video/mp4,webm,quicktime,x-m4v — the mp4/mov family is detected by `ftyp` brand fallback, pure-audio m4a is rejected); filename is a 128-bit random value |
| recall | only the sender, within `chat.recallMinutes` minutes (configurable 0–60, **0 = recall disabled**); persisted together with `recalled_at` (server time) and broadcast in the `recall` frame |
| edit | only the sender, within **5 minutes** of creation, on a non-recalled `text` message; the new body is validated against `chat.maxMessageLength` and sensitive words; `edited_at` is written and broadcast in the `message_update` frame |
| registration | governed by the `site.allowRegister` switch (off → 403) and `site.requireInviteCode` (on → a valid invite code is required, consumed atomically with a conditional UPDATE, and the quota is refunded automatically if registration fails); the first registered account automatically becomes site admin |
| email change | separate from registration verification: `POST /api/me/email-code` sends a code to the new address (purpose=`change_email`), and `PATCH /api/me` stores it and sets `email_verified=true` only after successful consumption. Storing on a uniqueness check alone would let anyone grab someone else's address (the real owner's later registration/binding gets 409, and reset links go to the wrong person) |
| email verification | a code is **required** only when all three hold: `site.requireEmailVerify` (on by default) + SMTP available + not the first account: registration must carry `email` + a 6-digit `emailCode` (valid 10 minutes, one-time, 60-second cooldown per email, invalidated after 5 consecutive wrong attempts). **The email itself is required and stored as soon as the switch is on** (the first account stores it too, with `email_verified=false`), otherwise that account could never use "forgot password". **When SMTP is unconfigured there is no silent degradation any more**: except for the first account (deployment bootstrap exemption) it returns 503 "Site requires email verification but mail service is not configured, please contact the site admin", preventing bulk registration that bypasses the site policy; enabling the switch in the admin console while SMTP is unconfigured is rejected with 400 |
| login lockout | accumulates failures per account according to `security.loginMaxAttempts` (0 = disabled); reaching the threshold locks for `security.loginLockMinutes` minutes (429); correct credentials clear it; **does not reveal whether the account exists** (the error text is constant) |
| favorites | must be a member of the conversation the message belongs to; favoriting the same message repeatedly is idempotent |
| group size | `chat.maxGroupMembers` (configurable 2–2000, default 200); the capacity check and the insert happen in the same transaction |
| reply `replyToId` | the target message must exist and **belong to the same conversation** (otherwise another conversation's content could be quoted, leaking it); the summary is truncated to 60 chars server-side |
| forward `forwarded` | a marker flag only; content and msgType are copied from the original message, and `image/audio/sticker/video` must still pass the "this site's storage" check |
| upload `kind` | `image`(≤8MB) / `audio`(≤5MB) / `sticker`(≤2MB) / `file`(≤20MB) / `video`(≤30MB); **the real type is always determined from the file header**, MIME has `; charset=` stripped before the whitelist lookup; when video container sniffing is inconclusive it falls back to the `ftyp` brand / Matroska magic |
| group file type whitelist | pdf / zip(docx,xlsx,pptx containers) / txt / json / jpeg / png / webp / gif. **html and svg are excluded** — browsers render them as pages, which amounts to same-origin execution of the uploader's script |
| @ `mentions` | keep only "real members + deduplicated + excluding the sender + at most 20"; the mentioned user's inbox gets `mention_unread=1`, cleared on read |
| block / report | can't block yourself; blocking blocks direct chats and friend requests in both directions. Reporting requires being a conversation member, reason 1–200 chars, once per person per message, recalled→422 |
| self-only delete | writes `message_deletions`, affecting only my own queries / pagination / favorites; if unread and not yet read, unread-1 |
| group invite link | token = crypto/rand 16-byte hex (32 chars); validity 0–30 days (0=permanent); expired / revoked means 404 |
| Web Push | when VAPID is unconfigured `enabled=false` and all pushes are silently skipped; endpoints returning 410/404 have their subscription deleted |
| online update | `role=admin` only; `WEB_UPDATE=false` disables it entirely; the commands and URLs are fixed, no user input accepted; every trigger writes the audit `web_update` |
| client_msg_id | UUID (≤36), unique index `(conversation_id, client_msg_id)` |
| messages indexes | `conversation_id` (cursor pagination), `(conversation_id, client_msg_id)` unique, `sender_id` (admin stats by sender, migration `0021`); `content` is widened to `TEXT` in migration `0023`, supporting up to 5000 chars in the admin console |
| friends | unique index `uk_friends_pair (user_id, friend_id)` (migration 0025); the same pair may hold two opposite-direction rows, but the same direction can't repeat. On accepting a friend the reverse row uses an upsert, so mutual applications don't collide on the index |
| email_codes | unique index `uk_email_codes_email_purpose (email, purpose)` (migration 0025); sending a code locks the row inside a transaction and resets the single row in place, so the cooldown can't be bypassed |
| plugin file | ≤1MB; manifest is the code's first `/* {...} */` JSON comment: name 1–30, version `^\d+\.\d+\.\d+$`, permissions ⊆ the ten in §SDK (`message:read/send/render`, `ui:panel/actions/host/theme`, `conv:read`, `storage`, `net`), allowedHosts each a valid domain (supports `*.suffix`), ≤20 entries. **TypeScript is supported**: with `?lang=ts` or a URL ending in `.ts` it is transpiled to ES2020 with esbuild before storing, **syntax errors return 400 with the line number**; the manifest is parsed from the source |
| pagination | page≥1, pageSize 1–100 default 20; limit 1–50 default 30; beforeId/upToId positive integers |
| ban days | 0–3650 integer |

The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method.

Idempotency & consistency:
- Direct-chat creation goes through `EnsureSingle`: duplicate check / conversation creation / member add / inbox initialization inside one transaction; the `conversations.direct_key` unique index guarantees only one side wins under concurrency, and the losing side rolls back and reuses the existing conversation (migration 0024, legacy direct chats have `direct_key=NULL`)
- `user_conversations` is initialized on conversation creation / group join (last_message synced to the latest, unread=0, no retroactive history)
- The DB is the single source of truth for unread counts; failed WS pushes never roll back

## 5. Error Codes

HTTP status + `{"error": "<Chinese message>"}`. Messages are shown to users directly — human-readable, no stack traces.

| Status | Meaning | Fixed/common messages (English rendering / original Chinese shown to users) |
|---|---|---|
| 400 | Validation failure | Specific, e.g. "Group name can't exceed 30 chars" (`群名称不能超过 30 个字`) |
| 401 | Not logged in / token invalid | "Session expired, please log in again" (`登录已失效，请重新登录`) — **the frontend clears the token and jumps to login on any 401** |
| 403 | Logged in but no permission | "Not allowed" (`无权执行此操作`), "Plugin taken down by the admin" (`插件已被站长下架`) |
| 404 | Not found | "Conversation not found" (`会话不存在`), "User not found" (`用户不存在`), "Plugin not found" (`插件不存在`) |
| 409 | State conflict | "Username already taken" (`用户名已被占用`), "Already friends or pending" (`对方已是好友或申请中`), "Already in this group" (`你已在群中`), "Email already bound" (`邮箱已被绑定`) |
| 413 | Payload too large | "Plugin file exceeds 1MB" (`插件文件超过 1MB`) |
| 422 | Business rule violation | "Can't add yourself as a friend" (`不能添加自己为好友`), "Over 2 minutes, can't recall" (`超过 2 分钟，无法撤回`), "Owners must transfer before leaving" (`群主不能退群，请先转让`), "Can't remove the owner" (`不能移除群主`), "Reset token invalid or expired" (`重置令牌无效或已过期`) |
| 429 | Rate limited | "Too many requests, try again later" (`操作过于频繁，请稍后再试`) |
| 500 | Internal error | "Server hiccup, try again later" (`服务器开小差了，请稍后再试`) — the real cause only goes to logs |

> The backend returns these messages in Chinese; the frontend's `i18n/errors.ts` maps them to English by exact message text, unmapped ones pass through.

## 6. Auth & Permissions

- JWT: HS256, secret ≥32 bytes (`JWT_SECRET`), claims `{uid, tv, iat, exp}`, valid 7 days
- Middleware order: Recover → Logger → RateLimit(per route) → JWT → Ban
- Ban middleware: checks `banned_until`; unexpired returns 403 `账号已被封禁：<原因>` (Account banned: <reason>)
- Roles: `/api/admin/*` checks `role=admin` (403); group admin ops check member-table `role=owner` (403)
- Operations that bump `tv` +1: password change (no dedicated entry this phase), password reset, admin reset, ban → all old tokens 401, WS pushes `kicked`
- WS auth: same rules as REST at handshake (`?token=` preferred, falling back to the `mc_auth` cookie when absent); **every frame** re-checks ban and `token_version` before routing (`Dispatch` → `checkConnState`); failures return an `error` frame and disconnect, no longer only checking `chat` frames
- WS Origin whitelist: the handshake checks `Origin` against `FRONTEND_URL` (comma-separated, trailing-slash & case normalized); non-browser clients without `Origin` pass (still token-protected); misses are refused (anti cross-site WebSocket hijacking)

## 7. Performance & Concurrency

- **Single-instance targets**: ≥10,000 concurrent WS; single-message end-to-end P99 <100ms (same machine); history pagination <20ms
- **Query constraints for list endpoints**: conversation-list member counts may only come from `COUNT(*) ... GROUP BY conversation_id` (never materializing member rows); group search fetches all member counts with a single `GROUP BY` (no per-group COUNT); the "does an account exist" check in `/api/site` uses a `LIMIT 1` probe rather than `COUNT(*)`
- **Config reads**: `SettingStore.Ops()` returns a parsed cached snapshot (`atomic.Pointer`); `Save/Invalidate` must invalidate that cache too, otherwise config changes don't take effect immediately
- **Streaming uploads + quota cache**: multipart segments stream straight to the storage layer (local: temp file + atomic rename; S3: write a temp file first, then PutObject with the known length); the local quota check uses a **60s TTL cache + incremental accumulation on successful uploads**, no longer `filepath.Walk`ing the whole directory on every upload
- Hub uses 64 shard locks by **userID** (`shards[uid%64]`); `userID -> []*Client` multi-device online, reference-counted offline. Sharding by user keeps one user's devices in the same shard, so each delivery locks only one shard
- Per-connection send channel 256 buffered + 10s write timeout; full buffer = disconnect (slow consumers kicked); read/write pumps separated
- Group member cap `chat.maxGroupMembers` (2–2000, default 200; validated atomically with `SELECT ... FOR UPDATE` inside the join transaction)
- Rate limiting (in-memory token buckets or Redis, per-key buckets): login/register/forgot **10/min per IP**; messages **20/min per user**; plugin installs **5/min per user**; media uploads **10/min per user**; email-change verification codes **5/min per user**
- History pagination only via `(conversation_id, id)` cursors, **OFFSET forbidden**
- Compression: REST `gin-contrib/gzip` (optional); WS `permessage-deflate` toggleable (default off)

## 8. Logs & Monitoring

- `slog` JSON **dual channel**: stdout + `LOG_DIR/app-YYYYMMDD.log` (default `logs/`, daily rotation, append-only); level `LOG_LEVEL` (default info); fmt.Println forbidden
- **Retention**: `LOG_RETAIN_DAYS` (default 15); cleaned once at startup then checked hourly; only deletes `app-*.log` files whose filename date expired — never anything else
- Request logs (middleware): `method, path, status, duration_ms, uid, ip`; leveled by result (5xx→error, 4xx→warn, slow >500ms→warn with `slow`)
- Security event logs (warn): login failures (username+IP, never passwords), banned login refusals, rate-limit hits (key/path/ip), WS Origin refusals, WS handshake failures (bad/expired tokens/bans), admin ban/reset/global-takedown
- Key business logs (info): register, login success, friend apply/accept/reject/delete, conversation create/join/leave/rename/kick/transfer, plugin install/upgrade/enable/uninstall, message recall, WS connect/disconnect
- Message persistence (debug): conv/msg/sender/type/length, **never the content**
- Slow queries: GORM `SlowThreshold=200ms` logs a warning
- panics: Recover middleware → 500 + error-level with stack
- Health check `GET /api/health`, no auth
- Prometheus: `GET /api/metrics` exports per-route request counts and online count; `Authorization: Bearer <token>` is required when `METRICS_TOKEN` is non-empty

## 9. Config & Deployment

`.env.example` (every key commented):

```
PORT=8080
GIN_MODE=release            # debug|release
DB_DSN=user:pass@tcp(127.0.0.1:3306)/modernchat?charset=utf8mb4&parseTime=True&loc=Local
JWT_SECRET=                 # ≥32 bytes; refuses to start unset in release mode; dev generates randomly with a warning
FRONTEND_URL=http://localhost:5173   # comma-separated; WS Origin whitelist + recovery-link prefix
TRUSTED_PROXIES=127.0.0.1,::1        # trusted reverse proxies; explicit empty string = trust no proxy
LOG_LEVEL=info
LOG_DIR=logs
LOG_RETAIN_DAYS=15
DATA_DIR=data               # upload directory; BACKUP_DIR defaults to <DATA_DIR>/backups
MIGRATE=false               # true = run migrations/ at startup (prod)
PLUGIN_MAX_SIZE=1048576
METRICS_TOKEN=              # non-empty makes /api/metrics require Bearer
BACKUP_INTERVAL_HOURS=0     # 0 = scheduled backups off
BACKUP_RETAIN=7
MYSQLDUMP_PATH=mysqldump
WEB_UPDATE=true             # site-admin online update switch
RELEASE_API_URL=            # release mode (written by install.sh)
RELEASE_DL_URL=
VAPID_PUBLIC_KEY= VAPID_PRIVATE_KEY= VAPID_SUBJECT=mailto:admin@localhost
# Redis optional: when set, rate limits share a quota (multi-instance); on failure falls back to memory
REDIS_ADDR= REDIS_PASSWORD= REDIS_DB=0
# SMTP optional; without it, forgot logs the link (release doesn't print the full token by default)
SMTP_HOST= SMTP_PORT= SMTP_USER= SMTP_PASS= SMTP_FROM= LOG_RESET_LINK=false
```

- Startup: read env (JWT/frontend-origin validation) → connect DB (10 retries × 2s) → run `migrations/` if `MIGRATE=true` else AutoMigrate → start WS Hub → listen
- Graceful shutdown: on SIGTERM drain WS writes within 10s, close HTTP
- Nginx: `/api` proxies to `:8080`; `/ws` with `Upgrade/Connection` headers, `proxy_read_timeout ≥ 300s` (`deploy/nginx.conf.example`; container version `deploy/nginx.compose.conf`)
- One-command environment: repo-root `docker-compose.yml` (MySQL 8 + backend, starts after healthcheck, `MIGRATE=true`)
- Upgrade: `scripts/update.sh` auto-detects the deployment form — release builds download and verify the replacement, source builds `git pull` then build locally; defaults to blue-green hot swap; builds inject the version for `/api/version` verification

## 10. Deliverables

### 10.1 Layout

```
server/
├── cmd/server/main.go     # entry: config → db → hub → router → graceful shutdown
├── internal/
│   ├── config/            # env parsing & defaults (incl. JWT/frontend-origin validation)
│   ├── version/           # build version (ldflags, GET /api/version)
│   ├── model/             # GORM models + response views
│   ├── store/             # data access + RunMigrations (golang-migrate) + secretbox + Redis
│   ├── handler/           # HTTP (auth/user/friend/conversation/moderation/plugin/admin/
│   │                      #       admin_settings/upload/file/sticker/push/ops/site/health/version)
│   ├── middleware/        # logger / jwt / ban / admin / limiter / maintenance / guard
│   ├── ws/                # hub(64 shards) / serve(handshake+pumps) / dispatch / presence
│   ├── plugin/            # manifest parsing & validation / esbuild TS transpile / from-url fetch + SSRF protection
│   ├── service/           # mailer / storage / thumbnail / push / backup / updater
│   ├── logx/ metrics/     # log rotation / Prometheus
│   └── router/            # all routes and middleware wiring
├── migrations/            # 0001_init … 0022_message_fulltext (up/down SQL)
├── test/                  # integration tests (//go:build integration)
├── Dockerfile             # multi-stage build (incl. migrations)
├── .env.example
└── README.md              # runbook (deps/config/migrations/tests/upgrades)
```

Repo-root `deploy/` (dual nginx configs), `scripts/` (install / package-release / update / uninstall / coverage), `plugins/examples/` (example plugin), `docker-compose.yml`, `website/` (official site) — see the root README.

Tests share directories with the code under test as `*_test.go`; cross-process integration tests carry `//go:build integration`.

### 10.2 Test requirements

- **Unit tests** (covering service / store key queries / handler validation & permission branches / plugin manifest & SSRF rejection rules): core packages (service, handler, plugin, ws shard routing) line coverage **≥70%** (`go test -cover`)
- **Integration tests** (`-tags integration`, Docker MySQL): register→login→create conversation→send→WS frames→read→recall→unread consistency, full chain
- **Concurrency cases**: 100 concurrent clients lose no messages; concurrent resends of one `client_msg_id` persist exactly one row
- `go vet ./...` and `gofmt` zero warnings

### 10.3 Documentation requirements

- `server/README.md`: deps, Dockerized MySQL, migrations, run, integration steps, version injection & upgrades
- `.env.example`: every key commented
- `migrations/`: header notes the rollback path
- User custom avatars: `migrations/0002_user_avatar` (`users.avatar` MEDIUMTEXT, stores the frontend-compressed data URL)
- `scripts/update.sh`: auto-detects release/source mode (supports `--cold`, `--no-restart`, `RESTART_CMD`)

### 10.4 Acceptance criteria (Definition of Done)

1. Every endpoint called in `web/src/api/index.ts` works and its response shape matches §3 of this document item by item (integration checklist checked off)
2. WS frames match `web/src/types/proto.ts` field for field; heartbeat / reconnect / kicked verified on the real frontend
3. First registered user role=admin; after a ban their REST is all 403 and WS receives kicked
4. `client_msg_id` resends create no duplicates; unread & read receipts behave consistently on both ends
5. Recall is sender-only, within `chat.recallMinutes` (default 2 minutes, 0=disabled); recalling an unread message decrements the peer's unread
6. §7's rate limits are effective and return 429
7. URL install rejects private / loopback / non-http(s) / oversized bodies (with tests)
8. `go test ./...` all green (incl. `-tags integration`)
9. Same-name upload upgrades in place (`id` unchanged, `X-Plugin-Updated: true`); after a site-admin global takedown, code serving for that plugin returns 403
10. WS handshake refuses non-whitelisted Origins; `GET /api/version` returns the injected build info
