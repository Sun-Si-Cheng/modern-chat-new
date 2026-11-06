# ModernChat Design Doc v3 (Plugin-based Web Chat Platform)

> Status: **Finalized and fully implemented (2026-10-24)**. Both `web/` and `server/` are implemented per this document; API fields and acceptance criteria defer to [`后端需求规格.md`](后端需求规格.md) (that document completes request/response fields, validation rules, error codes and DoD).
>
> Key v3 decisions:
> - Positioned as a **plugin-based chat platform**: the kernel only does connection + plain-text message pipeline + plugin container; every other capability is a plugin.
> - Plugins adopt **Option A: Declarative UI** — plugins only produce data/UI descriptions; the host renders them with whitelist components, plugins never touch the DOM.
> - A unified `conversations` table (direct/group isomorphic); JWT adds `token_version` for active revocation; SSRF protection, rate limiting, idempotency and deployment architecture added.

---

## 1. Product Positioning & Scope

### 1.1 One-liner

A **frontend-backend separated, plugin-first** web chat platform. Out of the box: the plainest accounts + plain-text chat; users write and install JS plugins to add images, emoji, themes, bots, mini-games, anything.

### 1.2 Kernel vs Plugins (everything is a plugin)

**Kernel (cannot be disabled)**
- Accounts: register/login/recover password, JWT, bans, token revocation
- Friends, conversations (direct/group), **plain-text messaging**, message roaming, WebSocket pipeline
- Plugin runtime: loader, permission model, event bus, declarative UI renderer, sandbox
- Admin console, plugin takedown

**Plugins (installable & removable — remove them and you're back to plain text)**
- Image messages, emoji/kaomoji, Markdown rendering, dark theme, notification sounds, auto-reply bots, weather panels, mini-games, etc.

### 1.3 In scope / out of scope

**In scope (IM fundamentals + plugin runtime)**
- Accounts (bcrypt + JWT + password recovery), friends (search/apply/accept/delete)
- Direct chats, groups (create/search/join/leave/members), conversation list (last message + unread badge)
- Read receipts, unread counts, 2-minute recall, "peer typing", online/offline
- Group admin (owner renames/kicks/transfers), admin console (ban/unban/reset password/global plugin takedown)
- Plugin sandbox + declarative UI + permission whitelist + upload/URL install + same-name upgrade
- UI: Chinese/English switch, light/dark/system theme (preferences persisted locally)

**Out of scope (v2)**
- Video calls, chat-record card forwarding (multi-message merge is one text), image multi-select, voice waveform/seek, default emoji-reaction entry (implemented but disabled)
- Plugin marketplace (manual install only this phase)
- Horizontal scaling (single instance this phase, see §9)

---

## 2. Tech Stack

| Layer | Choice | Notes |
|---|---|---|
| Frontend | Vue 3 + Vite + TS + Pinia + Vue Router | Fully separated from the backend |
| UI | `tdesign-vue-next` | Tencent TDesign official Vue 3 library (see §2.1) |
| Icons | `tdesign-icons-vue-next` | TDesign icon pack |
| Long lists | `vue-virtual-scroller` | Virtual scrolling for message streams |
| Realtime | Native WebSocket wrapper | Heartbeat + reconnect |
| Backend | Go 1.22+ / Gin | |
| WS | `github.com/gorilla/websocket` | |
| DB | MySQL 8.0 / GORM, utf8mb4 | |
| Auth | JWT HS256 (7 days), with `token_version` | |
| Password | `golang.org/x/crypto/bcrypt` | |
| Rate limit | `golang.org/x/time/rate` | In-memory token buckets |
| Migrations | Dev: AutoMigrate; prod: `golang-migrate` versioned SQL | |

**Single-machine performance targets**: 10k+ concurrent WebSocket connections; single-message end-to-end P99 < 100ms; history pagination < 20ms.

### 2.1 UI choice notes

We chose **tdesign-vue-next** (Tencent TDesign official Vue 3 library), reasons and constraints:

- **Official npm dependency**: `npm i tdesign-vue-next tdesign-icons-vue-next`, components & icons imported on demand; version upgrades managed by npm, no vendored source.
- **Fits the plugin whitelist**: the 8 renderers in §8.2 (`text/image/button/input/card/list/divider/link`) are controlled wrappers over TDesign components (`Typography` / `Image` / `Button` / `Input` / `Card` / `List` / `Divider` / `Link`); dangerous inputs (`javascript:` links, inline scripts) are rejected at the renderer layer — we do not rely on third-party XSS protection.
- **Theme = CSS variables**: TDesign design tokens are exposed as CSS variables; plugin `api.theme.apply(cssVars)` changes tokens directly, reskins never pollute host styles.
- **Light/dark theme**: switched via `:root[theme-mode="dark"]` (TDesign ships two token sets), tri-state "system/light/dark"; an inline script in `index.html` presets the theme to avoid a white flash; project auxiliary colors (faintest text, links, selection) converge into `--mc-*` variables, both sets meet body contrast ≥4.5:1.
- **Bilingual**: `vue-i18n`, defaults to the browser language and is switchable (persisted in `localStorage`); all UI copy lives in language packs; Chinese server errors are translated by a frontend mapping table.
- **Zero-cost admin tables**: TDesign ships `Table` (sort/filter/pagination/row selection out of the box) and `Form`, `Dialog`, `Message` etc.; the `/api/admin/*` pages need nothing custom.
- **No mixed component libraries**: a single design system site-wide, avoiding double themes and double spacing specs.

---

## 3. Architecture

```
┌──────────────────────────────────────────────────────┐
│ Browser                                                │
│  ┌────────────────────────────────────────────────┐  │
│  │ Vue host app (incl. plugin host)                 │  │
│  │  conversation/message UI  │  declarative renderer│  │
│  └───────────────┬────────────────────────────────┘  │
│  ┌───────────────┴────────────────────────────────┐  │
│  │ Plugin sandbox iframes × N (sandbox, no origin)  │  │
│  │  pure logic, postMessage carries data/UI JSON    │  │
│  └────────────────────────────────────────────────┘  │
└───────────┬───────────────────────────┬──────────────┘
            │ REST /api (JSON)          │ WebSocket /ws
┌───────────┴───────────────────────────┴──────────────┐
│ Nginx: static hosting + /api reverse proxy + /ws      │
└───────────────────────────┬───────────────────────────┘
┌───────────────────────────┴───────────────────────────┐
│ Go service (single instance): Gin + Hub(sharded) +     │
│ plugin service                                         │
└───────────────────────────┬───────────────────────────┘
┌───────────────────────────┴───────────────────────────┐
│ MySQL 8.0: users / conversations / conversation_members│
│           messages / user_conversations / friends /    │
│           plugins / password_resets                    │
└───────────────────────────────────────────────────────┘
```

---

## 4. Directory Structure

```
modernchat-new/
├── docs/                   设计文档.md 后端需求规格.md
├── deploy/                 nginx.conf.example (bare metal)  nginx.compose.conf (containers)
├── plugins/examples/       weather.plugin.js (example plugin)
├── website/                Marketing site / docs / plugin marketplace
├── scripts/                install.sh / package-release.sh / update.sh / update-release.sh / uninstall.sh / coverage.sh
├── docker-compose.yml      MySQL + backend one-command environment
├── server/
│   ├── go.mod  .env.example  Dockerfile
│   ├── migrations/                 versioned SQL (0001_init.up/down.sql)
│   ├── test/                       integration tests (//go:build integration)
│   ├── cmd/server/main.go
│   └── internal/
│       ├── config/                 env parsing
│       ├── version/                build version (ldflags injection)
│       ├── model/                  GORM models + response views
│       ├── store/                  data access + migrations runner
│       ├── handler/                auth/user/friend/conversation/plugin/admin/health
│       ├── middleware/             logger/jwt/ban/admin/limiter
│       ├── ws/                     hub(64 shards)/serve(read-write pumps)/dispatch/presence
│       ├── plugin/                 manifest validation + URL fetch (SSRF protection)
│       └── service/                mailer (optional SMTP)
└── web/
    ├── vite.config.ts              # /api and /ws proxies
    └── src/
        ├── api/ stores/ router/
        ├── composables/            useWebSocket.ts usePluginHost.ts (sandbox host)
        ├── components/
        │   ├── chat/               icon rail/filter pane/conversation list/message pane/composer/detail panel/plugin drawer/friend dialog/profile dialogs
        │   └── declarative/        whitelist renderers (8 components) + plugin panels
        ├── types/proto.ts
        └── views/                  AuthView/ForgotView/ChatLayout/admin
```

---

## 5. Database Design (MySQL 8.0, utf8mb4)

### users
| Field | Notes |
|---|---|
| id | PK |
| username | unique, login name |
| password_hash | bcrypt |
| nickname | display name |
| avatar_color | default-avatar hue |
| email | nullable, unique |
| email_verified | bool |
| role | `user` / `admin` (first registrant becomes admin) |
| **token_version** | **JWT version; +1 on password change/ban/reset — old tokens die instantly** |
| banned_until / ban_reason | ban expiry (nullable) / reason |
| created_at / updated_at | |

### conversations (unified; direct & group isomorphic)
| Field | Notes |
|---|---|
| id | PK, the global `conv_id` |
| type | 1=direct 2=group |
| name | group name (empty for direct) |
| owner_id | group owner (empty for direct) |
| created_at | |

### conversation_members
| Field | Notes |
|---|---|
| id | PK |
| conversation_id | |
| user_id | |
| role | `owner` / `member` (both sides are members in direct chats) |
| last_read_message_id | read up to (read receipts) |
| joined_at | |
| unique key | `(conversation_id, user_id)` |

> Direct: one `conversation(type=1)` + 2 member rows; group: `type=2` + N rows.
> Direct-chat idempotency: before creating, check whether the two already share a type=1 conversation and reuse it; the "check-then-insert" has a small concurrency window, accepted this phase (the client dedupes by membership), no extra constraint.

### friends
`id, user_id, friend_id, status(0=pending 1=accepted 2=rejected), created_at`
> After acceptance both sides get a `status=1` row; an existing status=0/1 relation blocks duplicate requests; after rejection re-applying updates the original record.
> Deleting a friend: deletes both status=1 rows; existing direct conversations and history are preserved, only the friend relation is removed.

### messages
| Field | Notes |
|---|---|
| id | auto-increment (in-conversation cursor, guarantees order) |
| conversation_id | |
| sender_id | |
| client_msg_id | client-generated UUID, **server idempotency key**, unique index `(conversation_id, client_msg_id)`, guards duplicate sends on reconnect |
| msg_type | `text` (plugins may register custom types) |
| content | ≤1000 chars (text) |
| is_recalled | soft recall flag |
| created_at | DATETIME(3) |
| index | `(conversation_id, id)` |

### user_conversations (inbox / conversation-list redundancy, performance-critical)
| Field | Notes |
|---|---|
| user_id | |
| conversation_id | |
| last_message_id / last_message_at | redundant, for ordering |
| unread_count | unread count |
| unique key | `(user_id, conversation_id)` |

> The list query is `WHERE user_id=? ORDER BY last_message_at DESC` — never scans the message table.
> On join/conversation creation this table is initialized: last_message_* synced to the conversation's latest message, unread_count=0, last_read_message_id = latest message id (no retroactive unread).

### plugins
| Field | Notes |
|---|---|
| id | PK |
| name / version / description | from the manifest |
| code_text | plugin code (stored server-side, ≤1MB) |
| permissions | JSON permission list |
| allowed_hosts | JSON, domains/paths allowed by the net permission |
| enabled | bool (user-level enable/disable) |
| **global_disabled** | bool, admin global takedown (applies to all users, overrides enabled) |
| installed_by | |
| created_at | |

### password_resets
`id, user_id, token(unique, crypto/rand 64hex), expires_at(15 min), used(bool), created_at`

---

## 6. REST API (prefix `/api`)

Everything except register/login/forgot/reset requires `Authorization: Bearer <token>`.

| Method | Path | Notes |
|---|---|---|
| POST | `/api/auth/register` `/api/auth/login` | returns `{token, user}` |
| GET | `/api/me` | current user |
| POST | `/api/auth/forgot` `{email}` | sends reset link (printed to log in dev) |
| POST | `/api/auth/reset` `{token, newPassword}` | sets new password (token_version+1) |
| PATCH | `/api/me` `{nickname?, email?}` | edit profile |
| GET | `/api/users/search?kw=` | search people |
| GET | `/api/friends` `/api/friends/requests` | friends / incoming requests |
| POST | `/api/friends/apply` `{toUsername}` | send request |
| POST | `/api/friends/handle` `{requestId, accept}` | accept/reject |
| DELETE | `/api/friends/:friendId` | delete friend (both directions; direct conversations kept) |
| GET | `/api/conversations` | conversation list (last message, unread) |
| POST | `/api/conversations/single` `{toUserId}` | start/reuse direct chat |
| POST | `/api/conversations/group` `{name}` | create group (creator joins) |
| GET | `/api/conversations/search?kw=` | search groups (public groups by name, returns id/name/member count) |
| POST | `/api/conversations/:id/leave` | leave (owners must transfer first) |
| GET | `/api/conversations/:id/messages?beforeId=&limit=30` | history pagination |
| GET | `/api/conversations/:id/members` | members |
| POST | `/api/conversations/:id/join` | join group |
| POST | `/api/conversations/:id/read` `{upToId}` | report read |
| PATCH | `/api/conversations/:id` `{name}` | owner renames |
| POST | `/api/conversations/:id/kick` `{userId}` | owner kicks |
| POST | `/api/conversations/:id/transfer` `{userId}` | transfer ownership |
| GET | `/api/plugins` `/api/plugins/active` | my plugins / enabled list for boot |
| GET | `/api/plugins/:id/code` | serve plugin JS (own install & enabled; response `Content-Type: text/javascript`) |
| POST | `/api/plugins/upload` `/api/plugins/from-url` | upload / URL install |
| PATCH | `/api/plugins/:id` `{enabled}` | enable/disable |
| DELETE | `/api/plugins/:id` | uninstall |
| GET | `/api/admin/users?page=` | admin: user list |
| POST | `/api/admin/users/:id/ban` `{days, reason}` | ban (days=0 permanent), force logout |
| POST | `/api/admin/users/:id/unban` | unban |
| POST | `/api/admin/users/:id/reset-password` | reset password |
| GET | `/api/admin/plugins?page=` | admin: plugins aggregated by name (installs/enabled/global state) |
| POST | `/api/admin/plugins/global-disable` `{name, disabled}` | global takedown / restore by name (affects all users) |
| GET | `/api/health` | health probe (no auth) |
| GET | `/api/version` | current build `{version, commit, buildTime}` (no auth) |

Conventions: errors `{error:"msg"}`; group admin checks `owner`; `/api/admin/*` checks `role=admin`.
`PATCH /api/me` resets `email_verified` to false after an email change (no email verification flow this phase; email is only for password recovery).
Plugin upload / URL install hitting "same user + same name" performs an **upgrade overwrite** (reuses the record, keeps enable state, response header `X-Plugin-Updated: true`).

---

## 7. WebSocket Protocol

Connection: `GET /ws?token=<JWT>`; validates login, `token_version`, ban status; 25s heartbeat (plus a protocol-level ping fallback on the server).
The handshake also validates an **Origin whitelist** (`FRONTEND_URL` config, anti cross-site WebSocket hijacking; non-browser clients without Origin pass but still need a token).
All uplink frames are checked for "is this user a member of the conversation"; non-members are dropped with an `error` frame.

### Uplink
```ts
type ClientFrame =
  | { type:"chat"; convId:number; clientMsgId:string; content:string; msgType?:"text" }
  | { type:"typing"; convId:number }
  | { type:"read"; convId:number; upToId:number }
  | { type:"recall"; messageId:number }
  | { type:"ping" };
```

### Downlink
```ts
type ServerFrame =
  | { type:"message"; id:number; convId:number;
      from:{id:number;nickname:string}; content:string;
      msgType:"text"; clientMsgId?:string; createdAt:string }
  | { type:"recall"; messageId:number; convId:number }
  | { type:"read"; convId:number; userId:number; upToId:number }
  | { type:"typing"; convId:number; userId:number }
  | { type:"presence"; userId:number; online:boolean }
  | { type:"user_change"; userId:number; nickname:string; avatar:string; avatarColor:string }
  | { type:"message_update"; id:number; convId:number; content:string; editedAt:string }
  | { type:"reaction"; messageId:number; convId:number; emoji:string; before?:string; userId:number; action:"set"|"remove" }
  | { type:"message_delivered"; messageId:number; convId:number; userId:number }
  | { type:"message_admin_deleted"; messageId:number; convId:number }
  | { type:"conv_change"; convId:number }
  | { type:"kicked"; reason:string }
  | { type:"pong" } | { type:"error"; reason:string };
```

**Idempotency**: uplink `chat` carries `clientMsgId`; the server dedupes by unique index; the frontend dedupes by message `id` (by `clientMsgId` before echo), so reconnects never duplicate sends or receives.

---

## 8. Plugin System (Option A: Declarative UI)

### 8.1 Runtime model
- Each plugin = one JS file (≤1MB), code served by the backend, loaded by the frontend into a **blob/srcdoc sandbox iframe** (`sandbox="allow-scripts"`, no `allow-same-origin`), no cross-origin loading needed.
- Plugins **never touch the DOM or the host window**; they run logic in the sandbox and talk to the host bridge via `postMessage`.

### 8.2 Declarative UI protocol
When a plugin wants to show UI, it sends a **UI description JSON** to the host, which renders it with whitelist components only:

```json
{
  "kind": "panel",
  "title": "Weather",
  "body": [
    { "component": "text", "props": { "content": "Hangzhou Sunny 22°C" } },
    { "component": "button", "props": { "text": "Refresh" }, "action": "refresh" }
  ]
}
```

**Whitelist components**: `text / image / button / input / card / list / divider / link`.
- Components outside the whitelist, `javascript:` links and inline scripts are always rejected.
- Custom message types work the same way: `api.registerMessageRenderer("weather", payload => UI description)`; the host renders inside the message bubble.

### 8.3 Plugin SDK
| API | Purpose | Permission |
|---|---|---|
| `api.on(event, fn)` | listen to `message:new / message:recall / conv:open / presence` | `message:read` |
| `api.send(convId, payload, msgType?)` | send messages (custom types allowed) | `message:send` |
| `api.registerMessageRenderer(type, fn)` | custom message type → UI description | `message:render` |
| `api.addSidePanel(desc)` | send side-panel UI description | `ui:panel` |
| `api.storage.get/set` | per-plugin namespaced storage | `storage` |
| `api.request(url, opts)` | **client-side bridged execution**: the host validates against `allowed_hosts` then forwards via fetch; natural CORS constraints, the server never egresses | `net` |
| `api.theme.apply(cssVars)` | change theme | `ui:theme` |

### 8.4 Security
- The bridge enforces the manifest permissions + `allowed_hosts` double whitelist; plugins **cannot get the JWT**, cannot read host storage, cannot touch other plugins.
- Install shows the permission list; installed only after confirmation; the admin can globally take down a plugin by name (`plugins.global_disabled=1`, all users stop it immediately).
- **Upgrade**: installing the same name again upgrades in place — reuses the record (same id), keeps enable/takedown state, updates code & permissions; enabled plugins reload the new code after frontend confirmation.
- Plugin network requests execute in the browser via the host bridge (§8.3); the server never egresses and there is no plugin-side SSRF surface; SSRF protection only guards URL install.
- **URL install SSRF protection**: before fetching, the server resolves the host IP and rejects private/reserved/loopback addresses and non-http(s) schemes, with response-size and timeout limits.

---

## 9. Backend Core: Send Pipeline, Performance & Scaling

### 9.1 Send pipeline
1. Validate: conversation membership, ban status, rate limit; dedupe by `client_msg_id`.
2. Write `messages` (get id).
3. Batch upsert members' `user_conversations`: update last_message; `unread_count+1` for non-senders.
4. Fan-out `message` frames to online members; push `conv_change` to receivers with unread.
5. When a read is reported on an open conversation: clear `unread_count`, write `last_read_message_id`, push `read` frames to other online members.

> Recall: only by the sender, within 2 minutes; if a receiver hasn't read it yet (their `last_read_message_id < recalled id`), their `unread_count` decrements accordingly.

### 9.2 Performance
- Hub shards by `conversation_id` into 64 locks; broadcast only locks the target shard; multi-device per user (`userID->[]*Client`) with reference-counted offline.
- Per-connection send channel (256) + write timeout; slow consumers are kicked; read/write pumps separated.
- **Group cap 200 members** (bounds fan-out write amplification and single-shard hotspots); mega-groups move to a "pull model" in v2 (no write fan-out).
- History uses cursor pagination (`beforeId`), OFFSET deep paging forbidden.
- Compression: REST `gin-contrib/gzip`; WS `permessage-deflate` (trade-offs in v2, can be disabled under connection pressure); frontend build outputs gzip/brotli.

### 9.3 Rate limiting
- Login/register/forgot: 10/min per IP.
- Sending messages: 20/min per user.
- Plugin upload/URL install: 5/min per user.

### 9.4 Scaling boundary (explicit, not vague)
- **This phase is single-instance**: presence and Hub routing live in memory; no horizontal scaling.
- Multi-instance in v2 requires: stateless Go + Redis (presence, connection routing, cross-instance pub/sub); the Hub would need a rewrite — not in this phase.

---

## 10. Deployment

- Frontend `npm run build` → static files served by Nginx; Nginx reverse-proxies `/api` to `:8080`, `/ws` with `Upgrade`/`Connection` headers (examples: `deploy/nginx.conf.example` bare-metal / `deploy/nginx.compose.conf` containers).
- Local one-command environment: `docker-compose.yml` (MySQL 8 + backend, first start runs `MIGRATE=true` to create tables).
- Production creates tables with versioned SQL from `migrations/`, not AutoMigrate.
- Production must set `JWT_SECRET` (release mode refuses to start without it) and `FRONTEND_URL` (WS Origin whitelist + password-recovery links).
- **Upgrades**: `scripts/update.sh` auto-detects the install mode — release deployments download and verify the latest package, source deployments `git pull` and build locally; both default to blue-green hot swap. Verify with `GET /api/version` or the admin console. The admin "online update" tab is restricted to systemd installs, writes an audit entry, and can be disabled with `WEB_UPDATE=false`.
- Config (DB DSN, JWT_SECRET, SMTP) goes through environment variables, never committed.

## 11. Local Run
```bash
# Backend (either)
docker compose up -d --build        # one command: MySQL 8 + backend :8080
# or manually: cd server && cp .env.example .env && go run ./cmd/server

# Frontend
cd web && npm install && npm run dev  # :5173, /api and /ws proxied to :8080
```
The first registered account becomes the site admin (`/admin`: ban users / reset passwords / global plugin takedown).

## 12. Delivery & Git Conventions
- Remote: Gitee `https://gitee.com/Sun-Si-Cheng/modern-chat-new.git`, branch `master`.
- After each finished module, automatically `git add -A` → commit → push, no reminders needed.
- `.gitignore` ignores `node_modules/`, `dist/`, `.env`, build artifacts, logs.

## 13. Review Conclusions & Implementation Log

### 13.1 v3 review additions (2026-10-24)
Group search/leave/delete-friend APIs (§6), `client_msg_id` unique-key scope (§5), join initialization (§5), recall unread rollback (§9), plugin global takedown field (§5/§8), `api.request` client-bridged execution (§8), direct-chat concurrency race accepted (§5), WS frame membership check (§7).

### 13.2 Implementation complete (2026-10-24)
- **Frontend `web/`**: auth (login/register/recovery), four-column chat (icon rail / filter pane / conversation & contact list / chat area + detail panel), grouped consecutive messages with read/recall folding, add-friend dialog (username/nickname search, inline accept), group admin, plugin sandbox host + declarative renderers, plugin drawer, admin console (users + plugins), responsive (tablet folds detail / phone single panel), design polish (contrast, tabular numerals, focus reachability).
- **Backend `server/`**: all §6 APIs, §7 WS protocol (64-shard Hub / read-write pumps / presence / kicked), plugins (manifest validation / SSRF / upgrade overwrite / global takedown), admin console, three rate-limit tiers, structured logs, graceful shutdown, `migrations/0001_init`.
- **Tests**: `go build` / `go vet` / `gofmt` / unit tests / integration tests (real MySQL full chain: auth→friends→direct→WS send/receive→idempotency→read→recall→unread→groups→ban kick→plugin install/upgrade/SSRF/takedown) all green; frontend `vue-tsc` / `oxlint+eslint` / `vite build` all green.
- **Security hardening** (after code review): WS Origin whitelist, `JWT_SECRET` enforced in production, configurable recovery-link base + constant-time compare, certificate files gitignored.
- **Delivery assets**: `docker-compose.yml`, `server/Dockerfile`, dual nginx configs, example plugin, `scripts/update.sh|ps1` one-command upgrade, `/api/version` verification.

### 13.3 Deviations from the initial design (adjusted during implementation, written back into this doc)
- The send pipeline & presence live in `internal/ws` (coupled with the Hub to avoid a ws ↔ service dependency cycle); `internal/service` keeps only the mailer.
- Added `internal/version` (build version injection), the admin plugin aggregation API, same-name plugin upgrade semantics, `/api/health` and `/api/version`.
- The spec document `后端需求规格.md` unified `before_id` / `new_password` to camelCase (`beforeId` / `newPassword`), matching the frontend.
