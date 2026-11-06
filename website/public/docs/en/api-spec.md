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
| Conversations | Direct (idempotent reuse) / groups (create / search / join / leave), group admin (rename / kick / transfer), conversation list (last message + unread) |
| Messages | History cursor pagination, send (WS), 2-minute recall, read receipts, "typing", presence |
| Realtime | WebSocket pipeline (heartbeat, reconnect tolerance, fan-out, presence) |
| Plugins | Upload / URL install (SSRF protection), enable/disable / uninstall, code serving, global takedown |
| Admin | User pagination, ban / unban / reset password |
| Ops | Health check `/api/health`, version `/api/version`, structured logs, three rate-limit tiers, one-command upgrade script |

**Interface types**: REST (JSON, prefix `/api`) + WebSocket (`/ws`). No file storage (avatars are color-derived), no third-party dependencies (SMTP optional).

## 2. Tech Stack & Versions

| Item | Requirement |
|---|---|
| Go | **1.22+** (1.23.x recommended) |
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
Conversation    { id, type:1|2, name(group name, null for direct), peer(direct peer summary, null for group),
                  ownerId, memberCount, lastMessageId, lastMessageAt, lastMessagePreview, unreadCount }
ConversationMember { userId, role:"owner"|"member", joinedAt, user:User summary }
Message         { id, conversationId, senderId, clientMsgId, msgType, content, isRecalled, createdAt }
FriendItem      { id(relation row id), friend:User summary, status:0|1|2, createdAt }
FriendRequestItem { id(request row id), from:User summary, createdAt }
Plugin          { id, name, version, description, permissions:string[], allowedHosts:string[],
                  enabled, globalDisabled, installedBy, createdAt }
VersionInfo     { version, commit, buildTime }
AdminPluginRow  { name, version, installCount, enabledCount, globalDisabled, createdAt }  // aggregated by name
```

### 3.2 REST details

| Method & path | Body / query | Success response |
|---|---|---|
| POST `/api/auth/register` | `{username, password, nickname}` | `{token, user}` (first registrant role=admin) |
| POST `/api/auth/login` | `{username, password}` | `{token, user}` |
| POST `/api/auth/forgot` | `{email}` | `{ok:true}` (unknown emails also return success; dev prints the reset link to the log) |
| POST `/api/auth/reset` | `{token, newPassword}` | `{ok:true}`; `token_version+1` kills all sessions |
| GET `/api/me` | — | `User` (with email) |
| PATCH `/api/me` | `{nickname?, email?, avatar?}` (email change resets email_verified=false; avatar is `data:image/(png\|jpeg\|webp);base64,…`, frontend compresses to 256×256, server validates prefix & ≤256KB, empty string clears) | updated `User` |
| GET `/api/users/search` | `?kw=` (≤50) | `User summary[]` (no email, ≤20 rows) |
| GET `/api/friends` | — | `FriendItem[]` (accepted friends; both-direction rows deduped per peer, one row per person) |
| GET `/api/friends/requests` | — | `FriendRequestItem[]` (status=0 pointing at me) |
| POST `/api/friends/apply` | `{toUsername}` | `{ok:true}`; existing 0/1 relation→409 |
| POST `/api/friends/handle` | `{requestId, accept:boolean}` | `{ok:true}`; on accept both sides get status=1 |
| DELETE `/api/friends/:friendId` | path param = **peer user_id** | `{ok:true}`; deletes both status=1 rows, direct conversations kept |
| GET `/api/conversations` | — | `Conversation[]` (with peer assembly, unread) |
| GET `/api/conversations/search` | `?kw=` | `Conversation[]` (type=2 name fuzzy match, ≤20 rows) |
| POST `/api/conversations/single` | `{toUserId}` (must be friends) | `Conversation`; reuses an existing type=1 conversation if found |
| POST `/api/conversations/group` | `{name}` | `Conversation` (creator becomes owner and joins) |
| GET `/api/conversations/:id/messages` | `?beforeId=&limit=` (limit default 30, ≤50) | `{messages: Message[] (id descending), hasMore: bool}` |
| GET `/api/conversations/:id/members` | — | `ConversationMember[]` (must be a member) |
| POST `/api/conversations/:id/join` | — | `{ok:true}` (type=2; already joined→409) |
| POST `/api/conversations/:id/leave` | — | `{ok:true}` (owner can't leave→422) |
| POST `/api/conversations/:id/read` | `{upToId}` | `{ok:true}`; clears unread, writes last_read_message_id, pushes `read` frames to online members |
| PATCH `/api/conversations/:id` | `{name}` | `{ok:true}` (owner only) |
| POST `/api/conversations/:id/kick` | `{userId}` | `{ok:true}` (owner only; can't kick self/owner→422) |
| POST `/api/conversations/:id/transfer` | `{userId}` (must be a member) | `{ok:true}`; roles swapped |
| GET `/api/plugins` | — | `Plugin[]` (installedBy=me, createdAt desc) |
| GET `/api/plugins/active` | — | `Plugin[]` (enabled=1 and global_disabled=0) |
| GET `/api/plugins/:id/code` | — | `text/javascript` source (must be own install; global_disabled→403) |
| POST `/api/plugins/upload` | body = JS file bytes (`Content-Type: application/octet-stream`, ≤1MB) | `Plugin` (default enabled=false); hitting "own + same name" upgrades in place, response header `X-Plugin-Updated: true` |
| POST `/api/plugins/from-url` | `{url}` | `Plugin`; §6 SSRF checks run before fetching; same-name upgrade semantics as above |
| PATCH `/api/plugins/:id` | `{enabled:boolean}` | updated `Plugin` |
| DELETE `/api/plugins/:id` | — | `{ok:true}` |
| GET `/api/admin/users` | `?page=&pageSize=` (default 1/20) | `{users: AdminUserRow[], total, page, pageSize}`; AdminUserRow = User + `createdAt` |
| POST `/api/admin/users/:id/ban` | `{days:0-3650, reason?}` (0=permanent) | `{ok:true}`; tv+1 and pushes `kicked` to all their connections |
| POST `/api/admin/users/:id/unban` | — | `{ok:true}` |
| POST `/api/admin/users/:id/reset-password` | — | `{newPassword}` (random 12 chars; tv+1 force logout) |
| GET `/api/admin/plugins` | `?page=&pageSize=` | `{plugins: AdminPluginRow[], total, page, pageSize}`; **aggregated by name**: `{name, version, installCount, enabledCount, globalDisabled, createdAt}` |
| POST `/api/admin/plugins/global-disable` | `{name, disabled:boolean}` | `{ok:true, affected}`; updates all installs of that name, effective for all users immediately |
| GET `/api/health` | no auth | `{"status":"ok"}` (Nginx/container probes) |
| GET `/api/version` | no auth | `{version, commit, buildTime}` (injected at build via ldflags; `go run` yields `dev/unknown`) |

### 3.3 WebSocket (`/ws`)

- Connection: `GET /ws?token=<JWT>`; the handshake validates signature, tv, ban; failures close directly (401/403 responses)
- Heartbeat: client sends `{"type":"ping"}` every 25s, server replies `{"type":"pong"}` immediately; server read timeout 60s disconnects
- Uplink & downlink frames **match `web/src/types/proto.ts` field for field** (the `chat` frame's `msgType` is a string; plugin custom types pass through and are stored)
- Every uplink frame first checks "is the sender a member of `convId`", otherwise replies `{"type":"error","reason":"..."}`
- Send pipeline (design doc §9.1): member/ban/rate checks → `client_msg_id` unique-index idempotency (duplicates **replay the original message frame**) → write messages → batch upsert user_conversations (unread+1 for non-senders) → fan-out `message` to online members → push `conv_change` to receivers with unread
- Recall: only by the sender, within 2 minutes; if a receiver's `last_read_message_id < recalled id`, unread-1
- Forced logout: tv change / ban / admin reset → push `{"type":"kicked","reason":"..."}` to all the user's connections, then close
- Profile changes: after `PATCH /api/me` changes nickname or avatar, push `{"type":"user_change","userId","nickname","avatar","avatarColor"}` to friends, co-members and the user's other connections; the frontend updates conversation list, friend/request lists and group member avatars in place, no refresh

## 4. Data Model & Validation

7 tables (users / conversations / conversation_members / messages / user_conversations / friends / plugins + password_resets) — structures in design doc §5; here only validations and key indexes.

| Field | Rule |
|---|---|
| username | 3–20, `^[a-zA-Z0-9_-]+$`, unique, unique index |
| password | 6–72 chars (bcrypt caps at 72 bytes, over-length → 400) |
| nickname | 1–20, non-empty after trim |
| email | ≤254, nullable; unique when non-empty |
| avatar_color | server derives from a 10-color palette by id modulo |
| group name | 1–30 |
| content | 1–1000 chars (by rune) |
| msg_type | ≤32, `^[a-z0-9_-]+$`; defaults to `text` |
| client_msg_id | UUID (≤36), unique index `(conversation_id, client_msg_id)` |
| friends | index `(user_id, friend_id)`; a pair may hold two opposite-direction rows |
| plugin file | ≤1MB; manifest is the code's first `/* {...} */` JSON comment or a same-name `.json`: name 1–30, version `^\d+\.\d+\.\d+$`, permissions ⊆ the 7 in §SDK, allowed_hosts each a valid domain (supports `*.suffix`), ≤20 entries |
| pagination | page≥1, pageSize 1–100 default 20; limit 1–50 default 30; beforeId/upToId positive integers |
| ban days | 0–3650 integer |

Idempotency & consistency:
- Direct-chat creation is "check-then-insert"; concurrent duplicates are acceptable (no extra constraint)
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
- WS auth: same rules as REST at handshake; uplink frames don't re-check tv (trusted for the connection lifetime); tv changes close via the `kicked` frame
- WS Origin whitelist: the handshake checks `Origin` against `FRONTEND_URL` (comma-separated, trailing-slash & case normalized); non-browser clients without `Origin` pass (still token-protected); misses are refused (anti cross-site WebSocket hijacking)

## 7. Performance & Concurrency

- **Single-instance targets**: ≥10,000 concurrent WS; single-message end-to-end P99 <100ms (same machine); history pagination <20ms
- Hub: 64 shard locks by `conversation_id`; `userID -> []*Client` multi-device online, reference-counted offline
- Per-connection send channel 256 buffered + 10s write timeout; full buffer = disconnect (slow consumers kicked); read/write pumps separated
- Group cap 200 members (validated on join)
- Rate limiting (in-memory token buckets, per-key buckets): login/register/forgot **10/min per IP**; messages **20/min per user**; plugin installs **5/min per user**
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
- Prometheus `/metrics`: **optional**, not in this phase, comment placeholder reserved

## 9. Config & Deployment

`.env.example` (every key commented):

```
PORT=8080
GIN_MODE=release            # debug|release
DB_DSN=user:pass@tcp(127.0.0.1:3306)/modernchat?charset=utf8mb4&parseTime=True&loc=Local
JWT_SECRET=                 # ≥32 bytes; refuses to start unset in release mode; dev generates randomly with a warning
FRONTEND_URL=http://localhost:5173   # comma-separated; WS Origin whitelist + recovery-link prefix
LOG_LEVEL=info
MIGRATE=false               # true = run migrations/ at startup (prod)
PLUGIN_MAX_SIZE=1048576
# Redis optional: when set, rate limits share a quota (multi-instance); on failure falls back to memory
REDIS_ADDR= REDIS_PASSWORD= REDIS_DB=0
# SMTP optional; without it, forgot logs the link
SMTP_HOST= SMTP_PORT= SMTP_USER= SMTP_PASS= SMTP_FROM=
```

- Startup: read env (JWT/frontend-origin validation) → connect DB (10 retries × 2s) → run `migrations/` if `MIGRATE=true` else AutoMigrate → start WS Hub → listen
- Graceful shutdown: on SIGTERM drain WS writes within 10s, close HTTP
- Nginx: `/api` proxies to `:8080`; `/ws` with `Upgrade/Connection` headers, `proxy_read_timeout ≥ 300s` (`deploy/nginx.conf.example`; container version `deploy/nginx.compose.conf`)
- One-command environment: repo-root `docker-compose.yml` (MySQL 8 + backend, starts after healthcheck, `MIGRATE=true`)
- Upgrade: `scripts/update.sh` auto-detects install mode (release download+verify, or source `git pull` + local build), defaults to blue-green hot swap; builds inject the version for `/api/version` verification

## 10. Deliverables

### 10.1 Layout

```
server/
├── cmd/server/main.go     # entry: config → db → hub → router → graceful shutdown
├── internal/
│   ├── config/            # env parsing & defaults (incl. JWT/frontend-origin validation)
│   ├── version/           # build version (ldflags, GET /api/version)
│   ├── model/             # GORM models + response views
│   ├── store/             # data access + RunMigrations (golang-migrate)
│   ├── handler/           # HTTP (auth/user/friend/conversation/plugin/admin/health/version)
│   ├── middleware/        # logger / jwt / ban / admin / limiter
│   ├── ws/                # hub(64 shards) / serve(handshake+pumps) / dispatch / presence
│   ├── plugin/            # manifest parsing & validation / from-url fetch + SSRF protection
│   └── service/           # mailer (optional SMTP, logs by default)
├── migrations/            # 0001_init.up/down.sql
├── test/                  # integration tests (//go:build integration)
├── Dockerfile             # multi-stage build (incl. migrations)
├── .env.example
└── README.md              # runbook (deps/config/migrations/tests/upgrades)
```

Repo-root `deploy/` (dual nginx configs), `scripts/` (one-command upgrade), `plugins/examples/` (example plugin), `docker-compose.yml` — see the root README.

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
5. Recall is sender-only within 2 minutes; recalling an unread message decrements the peer's unread
6. §7's three rate limits are effective and return 429
7. URL install rejects private / loopback / non-http(s) / oversized bodies (with tests)
8. `go test ./...` all green (incl. `-tags integration`)
9. Same-name upload upgrades in place (`id` unchanged, `X-Plugin-Updated: true`); after a global takedown, code serving returns 403
10. WS handshake refuses non-whitelisted Origins; `GET /api/version` returns the injected build info
