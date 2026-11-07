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

A **frontend-backend separated, plugin-first** web chat platform. Usable out of the box: accounts, friends, direct/group chats, text + image + audio + video messages; users can also write and install their own JS plugins to add emoji, themes, bots, mini-games and any other capability to the site.

### 1.2 Kernel vs Plugins (everything is a plugin)

**Kernel (cannot be disabled)**
- Accounts: register/login/password recovery, JWT, bans, token revocation
- Friends, conversations (direct/group), **text / image / audio / video messaging**, message roaming, favorites, WebSocket pipeline
- File storage (local disk or S3-compatible object storage)
- Plugin runtime: loader, permission model, event bus, declarative UI renderer, sandbox
- Site admin console, plugin takedown and restore

**Plugins (installable & removable — remove them and you're back to "text + image + audio + video")**
- Emoji reactions (react to a message with an emoji), Markdown rendering, notification sounds, auto-reply bots, weather panels, mini-games, custom message-type rendering, etc.

> Custom sticker **sending** is kernel (`msgType=sticker`), not a plugin capability — like image/audio it is a basic means of communication.

> Images and audio are IM fundamentals, so they live in the kernel; this also keeps the "plugin" positioning purer — enhancement and play, rather than filling in basic capabilities.

### 1.3 In scope / out of scope

**In scope (IM fundamentals + plugin runtime)**
- Accounts (bcrypt + JWT + password recovery / email verification code), friends (search/apply/accept/delete), block and report
- Direct chats, groups (create/search/join/invite/invite link/leave/members), initial public group, conversation list (last message + unread + archive)
- Read receipts, unread counts, recall (time limit configurable in the admin console; the tip is auto-removed after 3 minutes), message editing (**within 5 minutes, text only**), "peer typing", online/offline, delivered/read status, multi-device read sync
- **Image / audio / video messages** (images compressed before send + server-side thumbnails; video picks a local file, default ≤30 MB, inline playback with the native player), message favorites, forwarding, delete-for-me, clear history
- Group admin (rename / group avatar / kick / transfer / admins / member mute / mute all / group announcement / group files), site admin console (ban/unban/reset password/global plugin takedown/system settings/backup/online update)
- Plugin sandbox + declarative UI + permission whitelist + upload/URL install + same-name upgrade + server-side TypeScript transpilation
- UI: Chinese/English switch, light/dark/system theme, drag-resizable regions and drawer form, PWA, desktop notifications / Web Push
- Ops: maintenance mode, login lockout, registration invite codes, sensitive words, Prometheus metrics, database backup, orphan file cleanup

**Out of scope (v2)**
- Video calls, **merged forwarding (chat-record cards; this phase is "several messages joined into one text")**, image multi-select, voice waveform and seek
- Advanced video capabilities: capture-and-send, frontend transcoding/compression, poster frame, playback progress memory
- Default emoji-reaction entry (implemented, `FEATURES.reactions=false` off by default)
- Plugin marketplace (manual install only this phase)
- Horizontal scaling (single instance this phase, see §9)

---

## 2. Tech Stack

| Layer | Choice | Notes |
|---|---|---|
| Frontend | Vue 3 + Vite + TS + Pinia + Vue Router | Fully separated from the backend |
| UI | **UnoCSS + in-house `components/ui/Ui*`** | 13 in-house controls are the main force; `tdesign-vue-next` remains only for admin tables/tabs and auth-page forms (see §2.1) |
| Icons | `tdesign-icons-vue-next` | Same origin as TDesign, no conflict when mixed with in-house controls |
| Long lists | `vue-virtual-scroller` | Virtual scrolling for message streams |
| Realtime | Native WebSocket wrapper | Heartbeat + reconnect |
| Backend | Go 1.26+ / Gin | `go 1.26.0` in `go.mod`; the dependency chain does not allow a lower version |
| WS | `github.com/gorilla/websocket` | |
| DB | MySQL 8.0 / GORM, utf8mb4 | |
| Auth | JWT HS256 (7 days), with `token_version` | |
| Password | `golang.org/x/crypto/bcrypt` | |
| Rate limit | `golang.org/x/time/rate` | In-memory token buckets |
| Migrations | Dev: AutoMigrate; prod: `golang-migrate` versioned SQL | |

**Single-machine performance targets**: 10k+ concurrent WebSocket connections; single-message end-to-end P99 < 100ms; history pagination < 20ms.

### 2.1 UI choice notes

**Current state: UnoCSS atomic classes + an in-house control layer as the main force, TDesign shrunk to a residue.**

Early on the whole site used `tdesign-vue-next`, but two real problems led to a gradual migration:

1. **Uncontrollable styles**: changing a button's height/radius required overriding multiple layers of TDesign internal selectors, and broke easily on upgrades.
2. **Bundle size**: with TDesign imported in full the chunk reached 1,371 kB.

The migration strategy was **batched by risk**, with per-page screenshot regression after each batch, rather than a one-shot rewrite:

| Batch | Replaced | Result |
|---|---|---|
| A | Button / Input / Tag / Badge | 12 files |
| B | Dialog / Popup / Drawer | 11 overlays |
| C | Select / Slider / Switch / Checkbox / InputNumber / Tooltip | 14 files |
| — | **Keep TDesign** | admin `TTable`/`TTabs`, auth page `TForm`/`TFormItem`, `TConfigProvider` |

**Why the remainder stays**: `Table` (sort/filter/pagination/row slots) and `Tabs` are costly to build in-house with low payoff, and only appear on low-frequency admin pages; rewriting `TForm`'s validation semantics (rules/trigger timing) is riskier than the perceived gain.

**Key constraints** (must hold both during and after the migration):

- **Colors/radii/font sizes/motion always go through tokens**: `--td-*` (variables provided by TDesign, still the color palette base) and `--mc-*` (project-owned font sizes, radii, durations, easing, shadows, materials). Both control sets consume the same variables, so the visuals don't clash.
- **UnoCSS's `/alpha` modifier does not work on CSS variables**: when a tint is needed, use the `*-soft` / `*-line` steps predefined in `uno.config.ts` with `color-mix`; do not write `bg-success/12`.
- **In-house buttons must use `border-none`**: `<button>` carries browser default borders and a gray background, which become a "white ring/white block" in dark mode.
- **Plugin whitelist renderers** (§8.2) live in `components/declarative/`, decoupled from host controls; plugins depend on no concrete component library.
- **Theme**: switched via `:root[theme-mode="dark"]`, tri-state "system/light/dark"; an inline script in `index.html` presets it to avoid a dark-mode white flash.
- **Bilingual**: `vue-i18n`, defaults to the browser language and is switchable (`localStorage` persistence); all UI copy lives in language packs; Chinese server errors are translated by a frontend mapping table.

---

## 3. Architecture

```
┌──────────────────────────────────────────────────────┐
│ Browser                                              │
│  ┌────────────────────────────────────────────────┐  │
│  │ Vue host app (incl. plugin host)                │  │
│  │  conversation/message UI  │  declarative renderer│  │
│  └───────────────┬────────────────────────────────┘  │
│  ┌───────────────┴────────────────────────────────┐  │
│  │ Plugin sandbox iframes × N (sandbox, no origin) │  │
│  │  pure logic, postMessage carries data/UI JSON   │  │
│  └────────────────────────────────────────────────┘  │
└───────────┬───────────────────────────┬──────────────┘
            │ REST /api (JSON)          │ WebSocket /ws
┌───────────┴───────────────────────────┴──────────────┐
│ Nginx: static hosting + /api reverse proxy + /ws      │
│        upgrade                                        │
└───────────────────────────┬───────────────────────────┘
┌───────────────────────────┴───────────────────────────┐
│ Go service (single instance): Gin + Hub(sharded) +    │
│ plugin service                                        │
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
├── docs/                   design / backend spec / frontend tech plan / ops manual / UI design / plugin dev / protocol body
├── deploy/                 nginx.conf.example (bare metal)  nginx.compose.conf (containers)
├── website/                Marketing site (landing page / docs site / plugin marketplace)
│   ├── public/docs/        generated by syncing repo docs (scripts/sync-docs.sh|ps1)
│   └── scripts/            sync-docs.sh / sync-docs.ps1
├── plugins/examples/       weather / dice / word-count / conv-stats example plugins
├── scripts/                install.sh / package-release.sh / update.sh / update-release.sh / uninstall.sh / coverage.sh
├── docker-compose.yml      MySQL + backend one-command environment
├── server/
│   ├── go.mod  .env.example  Dockerfile
│   ├── migrations/                 versioned SQL (0001…0022, used in production)
│   ├── test/                       integration tests (//go:build integration)
│   ├── data/uploads/               uploaded images / audio / video (when using local storage; gitignored)
│   ├── cmd/server/main.go
│   └── internal/
│       ├── config/                 env parsing
│       ├── version/                build version (ldflags injection)
│       ├── model/                  GORM models + response views + system settings
│       ├── store/                  data access + migrations runner + secretbox (AES-GCM) + Redis
│       ├── handler/                auth/user/friend/conversation/moderation/plugin/admin/
│       │                           admin_settings/upload/file/sticker/push/ops/site/health/version
│       ├── middleware/             logger/jwt/ban/admin/limiter/maintenance/guard/bodylimit/securityheaders
│       ├── service/                mailer (hot reload) / storage (local + S3) / thumbnail / push / backup / updater
│       ├── ws/                     hub(64 shards)/serve(read-write pumps)/dispatch/presence
│       ├── plugin/                 manifest validation / esbuild TS transpilation / URL fetch (SSRF protection)
│       ├── logx/ metrics/          log rotation / Prometheus metrics
│       └── router/                 all routes and middleware assembly
└── web/
    ├── vite.config.ts              # /api, /uploads and /ws proxies (dev and preview)
    ├── uno.config.ts               # atomic class config: colors/radii mapped to CSS variables
    ├── scripts/precompress.mjs     # generate .gz / .br after build
    └── src/
        ├── api/ stores/ router/    # stores: auth/ws/conversations/friends/favorites/plugins/prefs/layout/site
        ├── composables/            useWebSocket.ts usePluginHost.ts notifications.ts announcement.ts localCache.ts useOverlay.ts
        ├── components/
        │   ├── ui/                 # in-house control layer (13): UiButton/UiInput/UiTag/UiBadge/
        │   │                       #   UiSelect/UiSlider/UiSwitch/UiCheckbox/UiInputNumber/
        │   │                       #   UiTooltip/UiPopup/UiDialog/UiDrawer
        │   ├── chat/               icon rail/group filter pane/conversation list/message stream/composer/
        │   │                       detail panel/plugin drawer/favorites drawer/notification drawer/
        │   │                       announcement dialog/voice bubble/friend search dialog/profile dialog
        │   ├── common/             AuthScene / QuickPrefs / AppBackgroundPicker / PaneResizer / ContextMenu
        │   └── declarative/        whitelist renderers (11 components) + plugin panels
        ├── types/proto.ts
        └── views/                  AuthView/ForgotView/SettingsView/LegalView/JoinView/MaintenanceView/ChatLayout/admin
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
| type | 1=direct 2=group 3=system announcement conversation (stores announcements only, not shown in the conversation list) |
| name | group name (empty for direct) |
| avatar | group avatar URL (the URL returned by the upload API; empty = default avatar) |
| mute_all | mute all members (owner / admins unaffected) |
| owner_id | group owner (empty for direct chats / the initial public group) |
| direct_key | direct-chat dedup key `smallID:largeID`, unique index `idx_conversations_direct_key` (migration 0024); `NULL` for groups and pre-existing direct chats |
| announcement / announcement_at / announcement_by | group announcement body (≤500 chars), publish time, publisher |
| created_at | |

### conversation_members
| Field | Notes |
|---|---|
| id | PK |
| conversation_id | |
| user_id | |
| role | `owner` / `admin` / `member` (both sides are `member` in direct chats) |
| mute_until | mute expiry (NULL = not muted; lapses automatically) |
| last_read_message_id | read up to (read receipts) |
| joined_at | |
| unique key | `(conversation_id, user_id)` |

> Direct: one `conversation(type=1)` + 2 member rows; group: `type=2` + N rows.
> Direct-chat idempotency: `EnsureSingle` looks up by `direct_key`, creates the conversation, adds members and initializes the inbox inside a transaction; on concurrent inserts the unique index lets only one through, and the loser rolls back and reuses the existing conversation. Pre-existing direct chats have `direct_key` as `NULL`, so historical data is unaffected.

### friends
`id, user_id, friend_id, status(0=pending 1=accepted 2=rejected), created_at`
> Unique index `uk_friends_pair (user_id, friend_id)` (migration 0025) means concurrent/duplicate requests no longer insert duplicate rows in the same direction; accepting upserts the reverse row, so mutual requests don't collide on the index.
> After acceptance both sides get a `status=1` row; an existing status=0/1 relation blocks duplicate requests (409); after rejection re-applying updates the original record.
> Deleting a friend: deletes both status=1 rows; existing direct conversations and history are preserved, only the friend relation is removed.

### messages
| Field | Notes |
|---|---|
| id | auto-increment (in-conversation cursor, guarantees order) |
| conversation_id | |
| sender_id | |
| client_msg_id | client-generated UUID, **server idempotency key**, unique index `(conversation_id, client_msg_id)`, guards duplicate sends on reconnect |
| msg_type | five built-in kinds: `text` / `image` / `audio` / `sticker` / `video`; anything else is a plugin-registered custom type (character whitelist validated) |
| content | column is `TEXT` (migration 0023); `text`: cap controlled by admin `chat.maxMessageLength` (max 5000 chars); `image` / `audio` / `sticker` / `video`: **the URL of a file uploaded to this site** (local is `/uploads/2026/11/xxx.png`, S3 is the `publicBase` absolute URL); before WS writes to the DB it validates that the URL falls under addresses recognized by `Storage.Owns()` — external links are forbidden |
| is_recalled | soft recall flag |
| recalled_at | recall time (server-written); the frontend shows "XXX recalled a message" accordingly and **removes that tip from the chat history after 3 minutes**. Recalled messages with NULL are treated as already expired |
| edited_at | edit time (server-written); NULL means never edited, the frontend shows "edited" accordingly, and offers the "Edit" entry only for the user's own **text messages within 5 minutes** |
| reply_to_id | quoted message id (**must belong to the same conversation**, validated before writing, to prevent quoting content from another conversation); NULL means a normal message. The brief is not persisted and is assembled in batch at query time |
| mentions | @-mentioned user ids (JSON array, groups only). Before writing it is filtered to "real members + deduplicated + excluding self + capped at 20" |
| forwarded | whether this is a forwarded message. Forwarding "copies content + msgType and sends a new message", does not reference the original, and later recall/deletion of the original does not affect it |
| admin_deleted_at / admin_deleted_by | when and by whom an admin / group owner deleted a violating message; when non-NULL it is invisible to all members (the body is kept for audit) |
| created_at | DATETIME(3) |
| index | `(conversation_id, id)`, `sender_id` (migration 0021), FULLTEXT ngram `content` (migration 0022); `content` widened to `TEXT` in migration 0023 |

### user_conversations (inbox / conversation-list redundancy, performance-critical)
| Field | Notes |
|---|---|
| user_id | |
| conversation_id | |
| last_message_id / last_message_at | redundant, for ordering |
| unread_count | unread count |
| pinned | pin (**a personal setting**: my pin does not mean others pinned it) |
| muted | mute: only suppresses the red badge number (shown as a small dot on the frontend), unread still accumulates |
| archived | archive: collapsed out of the default list, viewable via the "Archived" filter |
| cleared_before_id | clear-history watermark: messages earlier than this id are no longer delivered to this user (the message bodies are kept) |
| mention_unread | there are unread @me messages: even when muted, "someone @mentioned me" is shown; cleared when a read is reported |
| unique key | `(user_id, conversation_id)` |

> The list query is `WHERE user_id=? ORDER BY pinned DESC, last_message_at DESC` — never scans the message table.
> On join/conversation creation this table is initialized: last_message_* synced to the conversation's latest message, unread_count=0, last_read_message_id = latest message id (no retroactive unread).
> Deleting a conversation = deleting this user's row; subsequent new messages recreate the row via `UpsertInbox`, so the conversation reappears.

### stickers (personal sticker library)
`id, user_id(index), url(255), created_at`; `(user_id, url)` unique, repeated favoriting is idempotent.
> Only URLs are stored; the files live in Storage. Deleting a sticker does not affect already-sent messages (messages store a URL snapshot).

### conversation_files (group file directory)
`id, conversation_id(index), uploader_id, name(120), url(255), size, created_at`
> The file bodies live in Storage; this table is just bookkeeping. The `name` snapshot is kept — storage uses random filenames that mean nothing to humans.
> Delete permission: the uploader or the group owner.

### invite_codes (registration invite codes)
`id, code(32, unique), created_by(index), max_uses, uses, expires_at(NULL=permanent), disabled, created_at`
> Codes are generated from **crypto/rand 12 bytes hex** (24 characters), never `math/rand` — an invite code is a credential and must be unpredictable.
> Redemption is done **atomically by a conditional UPDATE**: `UPDATE ... SET uses=uses+1 WHERE code=? AND disabled=0 AND uses<max_uses AND (expires_at IS NULL OR expires_at>NOW())`,
> all validity conditions live in the WHERE clause and are judged by the database, so concurrent registrations cannot oversubscribe the last slot.
> If registration fails midway (duplicate username etc.), `Refund` returns the slot; disabling only sets `disabled` without deleting, preserving the audit trail.

### login_attempts (login failure lockout)
`id, username(64, unique), failed_count, locked_until(NULL=not locked), updated_at`
> Counted by **username rather than uid**: failures are also counted when the user does not exist, closing the "username does not exist" bypass path (and the error copy stays identical, so it doesn't reveal whether an account exists).
> Failures increment atomically with `ON DUPLICATE KEY UPDATE failed_count = failed_count + 1`; on reaching the threshold `locked_until` is written and the count is reset;
> correct credentials delete the record; past the lock period it is cleaned up automatically. The threshold is configurable in the admin console, **0 = off**.

### plugins
| Field | Notes |
|---|---|
| id | PK |
| name / version / description | from the manifest |
| code_text | plugin code (stored server-side, ≤1MB) |
| permissions | JSON permission list |
| allowed_hosts | JSON, domains/paths allowed by the net permission |
| enabled | bool (user-level enable/disable) |
| **global_disabled** | bool, site admin global takedown (applies to all users, overrides enabled) |
| installed_by | |
| created_at | |

### admin_audits (site admin action audit)
`id, admin_id, action, target_type, target_id, target_name, detail, ip, created_at`
> Records every write action in the admin console (ban / unban / force logout / reset password / role change / plugin takedown and uninstall / announcement).
> `action` is a short enum string; `detail` holds readable information such as reason, days and affected rows; a write failure only logs a warning and does not block the main flow.

### email_codes (registration email verification codes)
`id, email(index), code_hash, purpose(register / change_email), attempts, expires_at(10 minutes), used_at, created_at`
> Only the **HMAC-SHA256 hash** is stored (key derived from `JWT_SECRET`), so even a database leak yields no usable code.
> Unique index `uk_email_codes_email_purpose (email, purpose)` (migration 0025): one row per email per purpose; sending a code row-locks and resets in place inside a transaction, preventing bypass of the 60-second cooldown.
> Five consecutive wrong attempts void the code automatically; successful verification consumes it (concurrent submissions of the same code succeed only once).

### password_resets
`id, user_id, token(unique, crypto/rand 64hex), expires_at(15 min), used(bool), created_at`

### message_deletions (delete for me)
`(user_id, message_id)` composite PK, created_at
> Affects only that user's queries and pagination; the message body is kept and other members still see it. The primary key makes repeated deletion idempotent.

### message_favorites (message favorites)
`(user_id, message_id)` composite PK, created_at; displayed newest favorite first.
> The favorites view JOINs messages and senders; recalled messages can still be favorited (the frontend renders by recall state).

### message_reactions (emoji reactions)
`(message_id, user_id)` composite PK plus emoji, created_at; each user has at most one emoji per message.
> History pagination aggregates them into `{emoji,count,mine}` and ships them with the messages; WS broadcasts increments only (set / remove).

### user_blocks (blocking)
`(blocker_id, blocked_id)` composite PK, created_at; a one-directional record, while the business layer treats any direction as "don't disturb each other".
> After blocking: no more messages can be sent in the direct chat and no new direct chat can be started; friend requests are intercepted in both directions.

### message_reports (message reports)
`id, reporter_id + message_id(unique), reason(≤200), status(0=pending/1=resolved/2=ignored), created_at`
> Each person can report a given message only once; admins deleting a violating message go through `messages.admin_deleted_*`, and the report record is kept.

### conversation_invites (group invite links)
`token(32 hex PK), conversation_id(index), created_by, expires_at(NULL=permanent), revoked, created_at`
> Any group member can generate one; group info can be viewed without logging in, but joining still requires login; validity 0–30 days (0=permanent).

### push_subscriptions (Web Push subscriptions)
`id, user_id(index), endpoint(500, unique), p256dh, auth, created_at`
> One row per browser endpoint; when a push returns 410/404 the row is deleted by endpoint. Skipped entirely when VAPID is not configured.

### system_settings (runtime configuration)
`key(PK,size:64), value(text), encrypted(bool), updated_by, updated_at`
> Key-value configuration written by admin "System Settings", **taking precedence over environment variables** (on startup the DB values override `Mailer` etc.).
> Only keys in the `store.IsKnownKey` whitelist are allowed: `smtp.*` / `redis.*` / `storage.*` / `site.*` / `chat.*` / `upload.*` / `security.*`.
> Secret fields (`smtp.pass`, `redis.pass`, `storage.secretKey`) are stored **AES-256-GCM encrypted**,
> with the key derived from `JWT_SECRET` (`store/secretbox.go`) — **changing `JWT_SECRET` makes these values undecryptable**.
> Public APIs never echo secret plaintext and only return `hasPass` / `hasSecretKey`; an empty value on submit means "do not modify".

---

## 6. REST API (prefix `/api`)

Everything except register/login/forgot/reset/logout requires `Authorization: Bearer <token>`. Logging in also issues a same-origin `HttpOnly` cookie `mc_auth`, used only for browsers loading `/uploads` sub-resources and as a token-free fallback for the WS handshake; REST endpoints still accept only `Authorization`, avoiding any CSRF surface.

| Method | Path | Notes |
|---|---|---|
| POST | `/api/auth/register` `/api/auth/login` | returns `{token, user}` and issues the `mc_auth` cookie; when email verification is enabled, registration must carry `email` + `emailCode` |
| POST | `/api/auth/logout` | **no auth required**, clears the `mc_auth` cookie; does not revoke the JWT or kick WS (usable in maintenance mode) |
| POST | `/api/auth/email-code` `{email}` | sends a registration verification code (6 digits, 10 minutes, one-time); 60-second cooldown per email, and during the cooldown it still returns 200 with the remaining seconds |
| GET | `/api/me` | current user |
| POST | `/api/auth/forgot` `{email}` | sends a reset link (printed to log in dev) |
| POST | `/api/auth/reset` `{token, newPassword}` | sets new password (token_version+1) |
| PATCH | `/api/me` `{nickname?, email?, avatar?, emailCode?}` | edit profile; **changing the email requires a code sent to the new address**; avatar is a png/jpeg/webp data URL ≤256KB |
| POST | `/api/me/email-code` `{email}` | sends an email-change code (to the new address) |
| POST | `/api/me/logout-others` | log out all devices: `token_version+1` invalidates every old JWT; simultaneously `KickUser` closes every established WS connection for that account (including the current device, which must log in again) |
| GET | `/api/users/search?kw=` | search people |
| GET | `/api/friends` `/api/friends/requests` | friends / incoming requests |
| POST | `/api/friends/apply` `{toUsername}` | send request |
| POST | `/api/friends/handle` `{requestId, accept}` | accept/reject |
| DELETE | `/api/friends/:friendId` | delete friend (both directions; direct conversations kept) |
| GET | `/api/conversations` | conversation list (last message, unread) |
| POST | `/api/conversations/single` `{toUserId}` | start/reuse direct chat |
| POST | `/api/conversations/group` `{name}` | create group (creator joins automatically) |
| GET | `/api/conversations/search?kw=` | search groups (public groups by fuzzy name match, returns id/name/member count) |
| GET | `/api/conversations/:id/messages?beforeId=&afterId=&limit=30` | history pagination (descending by `beforeId`); with `afterId` it switches to ascending incremental backfill (offline cap 200) |
| DELETE | `/api/conversations/:id/messages/:messageId` | delete for me: removed only from the current account's records, others still see it |
| GET | `/api/conversations/:id/members` | members |
| POST | `/api/conversations/:id/join` | join group |
| POST | `/api/conversations/:id/invite` `{userIds}` | pull your own friends into the group (up to 50; the whole batch is rejected if over the limit) |
| POST | `/api/conversations/:id/invite-link` `{expiresDays:0-30}` | generate a group invite link, returns `{token,path}` |
| GET | `/api/invite/:token` | **no login required** view of invited group info |
| POST | `/api/invite/:token/join` | join the group via the invite link |
| POST | `/api/conversations/:id/leave` | leave (owners cannot leave; transfer first) |
| POST | `/api/conversations/:id/read` `{upToId}` | report read (also clears "someone @mentioned me") |
| DELETE | `/api/conversations/:id` | remove from your own list (does not leave the group or delete messages) |
| POST | `/api/conversations/:id/clear` `{upToId?}` | clear my chat history (sets a personal watermark; message bodies are kept) |
| PATCH | `/api/conversations/:id/prefs` `{pinned?,muted?,archived?}` | personal conversation settings; omitted fields mean no change, only an explicit `false` cancels |
| PUT | `/api/conversations/:id/announcement` `{content}` | group announcement (owner only, ≤500 chars); an empty string revokes it; `conv_change` is pushed on change |
| GET | `/api/conversations/:id/files` | group file list (newest→oldest, ≤100); `[]` when empty |
| POST | `/api/conversations/:id/files` `{name,url,size}` | record a group file (the URL must belong to this site's storage) |
| DELETE | `/api/files/:id` | delete a group file (uploader or group owner) |
| GET/POST/DELETE | `/api/stickers` `/api/stickers/:id` | personal sticker library: list / favorite (idempotent) / delete; cap 200 |
| GET | `/api/site` | **no login required** public site info: `{name, allowRegister, maintenanceMode, maintenanceMessage, requireInviteCode, requireEmail, requireEmailVerify, maxMessageLength, recallMinutes, maxGroupMembers, maxImageMB, maxAudioMB, maxFileMB, maxVideoMB}`. Only publicly safe fields are exposed (`requireEmail/Verify` are the effective values); secret configuration is never in this API |
| PATCH | `/api/conversations/:id` `{name}` | owner renames |
| PUT | `/api/conversations/:id/avatar` `{avatar}` | group avatar (owner / admins; empty string restores the default) |
| POST | `/api/conversations/:id/kick` `{userId}` | owner / admins kick (cannot kick the owner or another admin) |
| POST | `/api/conversations/:id/admin` `{userId, admin}` | owner grants / revokes admin |
| POST | `/api/conversations/:id/mute` `{userId, minutes}` | mute / unmute (0–43200 minutes, 0=unmute) |
| POST | `/api/conversations/:id/mute-all` `{muted}` | mute all members (owner / admins can still speak) |
| POST | `/api/conversations/:id/transfer` `{userId}` | transfer ownership |
| GET | `/api/plugins` `/api/plugins/active` | my plugins / enabled list for boot |
| GET | `/api/plugins/:id/code` | serve plugin JS (own install & enabled; response `Content-Type: text/javascript`) |
| POST | `/api/upload?kind=image\|audio\|sticker\|file\|video` | multipart upload of images / audio / stickers / group files / video, rate-limited per user at 10/min. **Real type is decided by the file header** (HTML masquerading as .png is rejected; video falls back to container sniffing via `ftyp` brand / Matroska magic); image ≤8 MB, audio ≤5 MB, sticker ≤2 MB, group file ≤20 MB, video ≤30 MB (all configurable in the admin console); returns `{kind,size,url}` |
| GET | `/uploads/<year>/<month>/<random>.<ext>` | static serving of uploaded files (always mounted, compatible with historical URLs). **Login is required** (`Authorization` or the same-origin `mc_auth` cookie, `UploadAuth`); directory listing always 404; `Cache-Control: private, max-age=3600` + `nosniff`; non-admins cannot access in maintenance mode. With object storage, new files are served by S3/CDN and access control moves down to the bucket policy |
| GET | `/api/favorites?limit=100` | my favorites (newest favorite first); `[]` rather than `null` when empty |
| POST | `/api/messages/:messageId/favorite` | favorite a message (must be a conversation member), idempotent |
| DELETE | `/api/messages/:messageId/favorite` | remove from favorites |
| GET | `/api/search/messages?kw=&convId=&limit=` | message search: `convId` optional (present = within the conversation, omitted = global, searching only conversations the user has joined); ≥2 characters use FULLTEXT ngram phrase matching, single characters / index anomalies fall back to LIKE; excludes recalled / admin-deleted / self-deleted |
| GET/POST/DELETE | `/api/users/:id/block` | query / set / remove a block (one-directional record, intercepted in both directions at the business layer) |
| POST | `/api/messages/:messageId/report` `{reason}` | report a message (1–200 chars, once per person per message, repeats return `duplicate:true`) |
| POST | `/api/messages/:messageId/admin-delete` | admin / group owner deletes a violating message; broadcasts `message_admin_deleted`, each client removes it |
| GET | `/api/push/key` | Web Push public key and enabled flag `{enabled,key}` |
| POST | `/api/push/subscribe` | save subscription `{endpoint,p256dh,auth}` |
| DELETE | `/api/push/subscribe?endpoint=` | delete subscription |
| POST | `/api/plugins/upload` `/api/plugins/from-url` | upload / URL install |
| PATCH | `/api/plugins/:id` `{enabled}` | enable/disable |
| DELETE | `/api/plugins/:id` | uninstall |
| GET | `/api/admin/stats` | site admin: overview stats (users / conversations / messages / plugins / online count / uptime) |
| GET | `/api/admin/users?page=&kw=&role=&status=` | site admin: user list (filter by keyword / role / ban status) |
| GET | `/api/admin/users/:id` | site admin: user detail (including conversation, message and plugin counts and online status) |
| POST | `/api/admin/users/:id/kick` | site admin: force logout (without banning) |
| POST | `/api/admin/users/:id/ban` `{days, reason}` | ban (days=0 permanent), force logout |
| POST | `/api/admin/users/:id/unban` | unban |
| POST | `/api/admin/users/:id/reset-password` | reset password |
| GET | `/api/admin/plugins?page=` | site admin: plugins aggregated by name (installs/enabled/global state) |
| POST | `/api/admin/plugins/global-disable` `{name, disabled}` | global takedown / restore by plugin name (affects all users) |
| POST | `/api/admin/plugins/purge` `{name, userId?}` | force uninstall by plugin name (physical delete; omitting userId means all users) |
| POST | `/api/admin/users/:id/role` `{role}` | set / revoke site admin (at least one admin is kept) |
| POST | `/api/admin/announce` `{content}` | site-wide announcement: written into the system conversation (`type=3`) and pushed to all users |
| GET | `/api/admin/audits?page=` | site admin action audit (ban / kick / role / plugin / announcement) |
| GET | `/api/admin/invites` | site admin: invite code list (newest→oldest, with creator nickname) |
| POST | `/api/admin/invites` `{maxUses:1-1000, expiresDays:0-365, count:1-20}` | batch-generate invite codes (`expiresDays=0` permanent) |
| POST | `/api/admin/invites/:id/disable` `{disabled}` | disable / enable (no deletion, audit trail kept) |
| GET/PUT | `/api/admin/settings` | system settings (SMTP / Redis / object storage / site / chat rules / upload caps / login security / sensitive words / storage quota). Secrets only return `hasXxx`, empty means no change; out-of-range numbers return 400 rather than silently clamping |
| POST | `/api/admin/settings/smtp/test` `{to}` | send a test email; failure returns the mail server's raw error |
| GET | `/api/admin/storage/stats` | local attachment usage (file count / bytes / orphan count) |
| POST | `/api/admin/storage/gc` `{dryRun}` | clean up orphan files not referenced by messages / stickers / group files / avatars |
| GET/POST | `/api/admin/backups` | backup file list / back up now (requires `mysqldump`) |
| GET | `/api/admin/update/status` | online-update status snapshot (polling) |
| GET | `/api/admin/update/check` | check the latest version (release build Release / source build `origin/main`) |
| POST | `/api/admin/update/start` | trigger an update (systemd deployments only; disabled by `WEB_UPDATE=false`) |
| GET | `/api/health` | health probe (no auth) |
| GET | `/api/version` | current build `{version, commit, buildTime}` (no auth) |
| GET | `/api/metrics` | Prometheus metrics (requires Bearer when `METRICS_TOKEN` is non-empty) |

Conventions: errors `{error:"msg"}`; group admin checks the `owner` / `admin` roles; `/api/admin/*` checks `role=admin`.
`PATCH /api/me` changing the email must consume a code sent to the new address; only after success is it written and `email_verified=true` set; unbinding (empty string) needs no verification.
Plugin upload / URL install hitting "same user + same name" performs an **upgrade overwrite** (reuses the record, keeps enable state, response header `X-Plugin-Updated: true`).

---

## 7. WebSocket Protocol

Connection: `GET /ws?token=<JWT>`; when the token is missing it falls back to the same-origin `mc_auth` cookie (a newer approach that keeps tokens out of access logs; the current frontend still uses query). The handshake validates login state, `token_version`, ban status and maintenance mode; 25s heartbeat (plus a protocol-level ping fallback on the server).
The handshake also validates an **Origin whitelist** (`FRONTEND_URL` config, anti cross-site WebSocket hijacking; non-browser clients without Origin pass but still need a token).
**Every frame** re-checks ban status and `token_version` before routing (`Dispatch` → `checkConnState`); on failure it replies with an `error` frame and disconnects — no longer "checked only on the chat frame". All uplink frames are also checked for "is this user a member of the conversation"; non-members are dropped with an `error` frame.
`?token=` still ends up in reverse-proxy / application access logs, so `/ws` access logging should be disabled or redacted in deployment; the frontend can later migrate to a token-free cookie handshake (requiring same-origin deployment). See [安全与迁移指南.md](安全与迁移指南.md) §3.

### Uplink
```ts
type ClientFrame =
  | { type:"chat"; convId:number; clientMsgId:string; content:string; msgType?:string /* defaults to text; for image/audio/sticker/video the content must be a URL uploaded to this site */;
      replyToId?:number; mentions?:number[]; forwarded?:boolean }
  | { type:"typing"; convId:number }
  | { type:"read"; convId:number; upToId:number }
  | { type:"delivered"; messageId:number } /* direct-chat delivery receipt: the receiver confirms and it is relayed to the sender */
  | { type:"recall"; messageId:number }
  | { type:"edit"; messageId:number; content:string } /* own messages only, within 5 minutes, unrecalled text messages */
  | { type:"reaction"; messageId:number; emoji:string; action:"set"|"remove" }
  | { type:"ping" };
```

### Downlink
```ts
type ServerFrame =
  | { type:"message"; id:number; convId:number;
      from:{id:number;nickname:string}; content:string;
      msgType:string; /* text | image | audio | sticker | video | plugin-defined */ clientMsgId?:string;
      replyToId?:number; replyTo?:ReplyBrief; mentions?:number[]; forwarded?:boolean; createdAt:string }
  | { type:"message_update"; id:number; convId:number; content:string; editedAt:string /* RFC3339, server time */ }
  | { type:"recall"; messageId:number; convId:number; recalledAt:string /* RFC3339, server time */ }
  | { type:"reaction"; messageId:number; convId:number; emoji:string; before?:string; userId:number; action:"set"|"remove" }
  | { type:"message_delivered"; messageId:number; convId:number; userId:number }
  | { type:"message_admin_deleted"; messageId:number; convId:number }
  | { type:"read"; convId:number; userId:number; upToId:number }
  | { type:"typing"; convId:number; userId:number }
  | { type:"presence"; userId:number; online:boolean }
  | { type:"user_change"; userId:number; nickname:string; avatar:string; avatarColor:string }
  | { type:"conv_change"; convId:number; meta?:boolean /* true=members/group name/announcement changed */ }
  | { type:"kicked"; reason:string }
  | { type:"pong" } | { type:"error"; reason:string };
```

**Idempotency**: uplink `chat` carries `clientMsgId`; the server dedupes by unique index; the frontend dedupes by message `id` (by `clientMsgId` before echo), so reconnects never duplicate sends or receives.
**Uplink frame cap**: 64 KB (`maxMsgSize` in `ws/serve.go`). This must cover the byte length in UTF-8 of the `chat.maxMessageLength` cap of 5000 chars (about 20 KB) plus JSON overhead; the former 4 KB would disconnect legitimate long messages once an admin raised the length.

---

## 8. Plugin System (Option A: Declarative UI)

### 8.1 Runtime model
- Each plugin = one file (≤1MB), code served by the backend, injected by the frontend via **`srcdoc` into a sandbox iframe** (`sandbox="allow-scripts"`, without `allow-same-origin`), no cross-origin loading needed. Without `allow-same-origin` the parent page has no right to touch the iframe document, and `document.open/write` throws `SecurityError`, so a blob URL + `document.write` cannot be used.
- Plugins **never touch the DOM or the host window**; they run logic in the sandbox and talk to the host bridge via `postMessage`; the iframe is an opaque origin (`Origin: null`) with no access to the host's localStorage / cookie / JWT.
- **Language**: both JavaScript and TypeScript. When uploading a `.ts` (`?lang=ts`) or installing a URL ending in `.ts`, the server transpiles file by file to ES2020 JS with an embedded **esbuild** before storing; the frontend sandbox does no compilation, so neither plugin size nor the initial bundle is affected. The manifest is always parsed from the **source**; if transpilation drops comments, they are restored.
- See [`插件开发.md`](插件开发.md) for the development guide.

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

**Whitelist components**: `text / image / button / input / card / list / divider / link / table / progress / badge` (the last three for statistics and status display).
- Components outside the whitelist, `javascript:` links and inline scripts are always rejected.
- Custom message types work the same way: `api.registerMessageRenderer("weather", payload => UI description)`; the host renders inside the message bubble.

### 8.3 Plugin SDK
| API | Purpose | Permission |
|---|---|---|
| `api.on(event, fn)` | listen to `message:new / message:recall / conv:open / presence / ui:action / message:action` | `message:read` |
| `api.send(convId, payload, msgType?)` | send messages (custom types allowed) | `message:send` |
| `api.registerMessageRenderer(type, fn)` | custom message type → UI description | `message:render` |
| `api.addSidePanel(desc)` | send side-panel UI description | `ui:panel` |
| `api.registerMessageAction({id,label})` | message context-menu item: clicking it delivers `message:action` (only to the registrant) | `ui:actions` |
| `api.me()` / `api.conv.current()` / `api.conv.members()` / `api.messages.recent(n)` | read-only context: own identity, conversation overview, members, briefs of the latest n messages | `conv:read` |
| `api.nav.openConversation(id)` / `api.nav.jumpToMessage(id)` / `api.toast(text, kind?)` | host-side actions: switch conversation, locate a message, light toast | `ui:host` |
| `api.storage.get/set` | per-plugin namespaced storage | `storage` |
| `api.request(url, opts)` | **client-side bridged execution**: the host validates against `allowed_hosts` then forwards via fetch; natural CORS constraints, the server never egresses | `net` |
| `api.theme.apply(cssVars)` | change theme | `ui:theme` |

### 8.4 Security
- The sandbox injects a bootstrap script via `iframe.srcdoc`; CSP is `default-src 'none'`, only `script-src/style-src 'unsafe-inline'`, `img-src data:`, `connect-src` narrowed to the manifest's `allowedHosts` (`'none'` when undeclared), and `base-uri/form-action/frame-src/object-src/worker-src` disabled. `</script` and `<script` are escaped before injection to prevent prematurely closing the bootstrap script.
- The bridge enforces the manifest permissions + `allowed_hosts` double whitelist; plugins **cannot get the JWT**, cannot read host storage, cannot touch other plugins.
- **Events are filtered by permission**: `message:new` / `message:recall` / `presence` are only delivered to plugins declaring `message:read`; when a plugin lacks `message:read`, the `ui:actions` callback receives message metadata only, without the body.
- **Raw `fetch` no longer works**: the opaque origin plus CSP `connect-src` prevent plugins from issuing requests of their own, so they must use `mc.request` instead. The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method. It is still constrained by both `allowed_hosts` and the target site's CORS.
- Install shows the permission list; installed only after confirmation; the site admin can globally take down a plugin by name in the admin console (`plugins.global_disabled=1`, all users stop it immediately).
- **Upgrade**: installing the same name again as the same user upgrades the version in place — reuses the record (same id), keeps enable/takedown state, updates code & permissions; enabled plugins reload the new code immediately after frontend confirmation.
- Plugin network requests all execute browser-side through the host bridge (§8.3); the server never egresses and there is no plugin-side SSRF surface; SSRF protection only guards URL install.
- **URL install SSRF protection**: before fetching, the server resolves the host IP and rejects private/reserved/loopback addresses and non-http(s) schemes, with response-size and timeout limits.

---

## 9. Backend Core: Send Pipeline, Performance & Scaling

### 9.1 Send pipeline
1. Validate: conversation membership, ban status, rate limit; dedupe by `client_msg_id`.
2. Write `messages` (get id).
3. Batch upsert members' `user_conversations`: update last_message; `unread_count+1` for non-senders.
4. Fan-out `message` frames to online members; additionally push `conv_change` to receivers with unread.
5. When a read is reported on an open conversation: clear `unread_count`, write `last_read_message_id`, push `read` frames to other online members.

> Recall: only by the sender, within the `chat.recallMinutes` limit (default 2 minutes, 0=off); if a receiver hasn't read it yet (their `last_read_message_id < recalled id`), their `unread_count` decrements by one accordingly.

### 9.2 Performance
- The Hub shards by **userID** into 64 locks (`shards[uid%64]`); broadcast only locks the target shard; multi-device per user (`userID->[]*Client`) with reference-counted offline. Sharding by user keeps one user's devices in the same shard so each delivery locks one shard; sharding by conversation would require locking the multiple shards its members live on per fan-out.
- **Queries do not materialize irrelevant rows**: the conversation list only takes "member count (one `GROUP BY`) + the direct-chat peer" and no longer JOINs all members of all the user's conversations back to `users` (20 full groups drop from ~4000 rows to a few dozen); group search likewise replaces "N hits → N COUNT queries" with one `GROUP BY`.
- **Image message thumbnails**: uploading with `kind=image` generates a JPEG thumbnail ≤480px (`_t.jpg`; GIFs lose animation, tiny images yield no gain, decode failures are skipped, transparent images get a white backing — JPEG has no alpha). Local storage generates asynchronously; for S3 it appends a PutObject after the main file succeeds; frontend bubbles derive the thumbnail URL by convention, fall back to the original on 404, and the lightbox always uses the original.
- **Message search uses FULLTEXT**: `messages.content` gets an ngram full-text index (migration `0022`; AutoMigrate does not create full-text indexes, so `EnsureMessageFulltext` idempotently adds it at startup based on information_schema). ≥2-character keywords use `MATCH...AGAINST` phrase mode to approximate LIKE substring semantics (a quoted phrase guarantees contiguity); single characters (the ngram minimum token is 2) and index anomalies fall back to LIKE.
- **Don't COUNT when a boolean will do**: `/api/site` is polled by every online client every 30 seconds, so "does any account exist" uses `SELECT 1 ... LIMIT 1`, sparing InnoDB a full traversal of the smallest index's entries.
- **Cache parsed config**: `Ops()` is called at least twice per request (maintenance middleware + handler), so the parsed result is cached in an `atomic.Pointer` and cleared on `Save/Invalidate`; otherwise every request would deep-copy the config map and parse ~20 keys.
- **Index completion**: `messages(sender_id)` (admin stats by sender, migration `0021`), `messages(conversation_id)` (cursor pagination; an InnoDB secondary index leaf contains the PK, equivalent to `(conversation_id, id)`), `(conversation_id, client_msg_id)` unique (idempotency).
- Per-connection send channel (256) + write timeout; slow consumers are kicked; read/write pumps separated.
- **Group cap 200 members** (bounds fan-out write amplification and single-shard hotspots); mega-groups move to a "pull model" in v2 (no write fan-out).
- History uses cursor pagination (`beforeId`), OFFSET deep paging forbidden.
- Compression: REST uses `gin-contrib/gzip` (excluding `/ws`, responses <1KB not compressed); WS enables `permessage-deflate` (`EnableCompression` in `upgraderFor`, can be disabled under connection pressure); frontend build outputs gzip/brotli.

### 9.3 Rate limiting & account protection
- Login/register/forgot: 10/min per IP.
- Sending messages: 20/min per user.
- Plugin upload/URL install: 5/min per user.
- Media upload (image / audio / sticker / video / group file): 10/min per user (prevents use as an image host).
- **Login failure lockout** (`login_attempts` table, threshold and duration configurable in the admin console, 0 = off): failures accumulate per account,
  and on reaching the threshold the account is temporarily locked (429 + remaining minutes), auto-cleaned once the lock period passes. Counted by username rather than uid —
  failures are also counted when the user does not exist, closing the "username does not exist" bypass; correct credentials reset the count;
  **a failed count increment only logs and never returns 500**, otherwise it would hand attackers a way to cripple the login endpoint. Complementary to IP rate limiting.

### 9.4 File storage (local ↔ object storage)

Images / audio / video in messages store **URLs only**; where they actually live is decided by the `service.Storage` interface — swapping the storage implementation requires no data change. The on-disk extension for video is decided by container sniffing (mp4 / webm / mov / m4v), and the object storage `Content-Type` is given by kind + extension (`.webm` must distinguish `audio/webm` from `video/webm`, otherwise video is handled as audio). Uploads are **streamed** (`FileStreamSaver`): multipart parts are read and written to a temp file as they arrive, renamed atomically for local storage and uploaded to S3 with a known length — files never sit in memory whole; local quota checks use a 60s TTL cache plus incremental accumulation on success.

| Implementation | Trigger condition | URL shape | Static serving |
|---|---|---|---|
| `LocalStorage` | admin has not configured `storage.endpoint` + `storage.bucket` | `/uploads/2026/11/<128bit>.png` | backend `GET /uploads/**` (authenticated + `private, max-age=3600` + directory listing disabled) |
| `S3Storage` | the above two configured in the admin console (MinIO / AWS S3 / OSS / COS, minio-go) | `<publicBase>/2026/11/<128bit>.png` | served by object storage / CDN |

**Key constraints**:

1. **Type is decided by the file header, never the extension**. `http.DetectContentType` reads the magic bytes; containers such as webm/m4a that return `application/octet-stream` get a second recognition pass on their signature. Renaming HTML to `.png` is rejected (otherwise, combined with browser MIME sniffing, it could become stored XSS).
2. **Filenames are 128-bit random**, non-enumerable; directories are sharded by `year/month` to avoid too many files in one directory.
3. **URL ownership is validated before WS writes to the DB**: the content of `image` / `audio` must be an address recognized by `Storage.Owns()`, preventing users from injecting `https://evil.com/x.png` (the message would look normal but the receiver would load an external resource → IP leak, trackable pixel stats).
4. **Once S3 is configured it never silently falls back to local**: upload failure returns an error directly, otherwise you get a data split of "thinking it's in the cloud but losing everything when the machine is rebuilt".
5. **Switching storage does not migrate historical files**: `/uploads/...` in old messages is still served by the local static service (the local implementation stays); new messages use the new storage.
6. **In S3 mode `/uploads` auth does not apply**: new files are `publicBase` absolute URLs whose access control belongs to the bucket policy/CDN; the local static service only covers historical files. Attachment GC also only scans local `DATA_DIR/uploads`; S3 orphan objects need a separate lifecycle rule.

### 9.5 Scaling boundary (explicit, not vague)
- **This phase is single-instance**: presence and Hub routing live in memory; no horizontal scaling.
- Multi-instance in v2 requires: stateless Go + Redis (presence, connection routing, cross-instance pub/sub); the Hub would need a rewrite — not in this phase.

### 9.6 Operational switches (`store.Ops`, all take effect immediately)

The operational parameters in the admin "System Settings" and infrastructure configuration such as SMTP / Redis / storage ("needs a restart") are two different classes:
the former reads `store.SettingStore.Ops()` — typed reads with defaults and range clamping; after `Save` invalidates the cache, the next request sees the new value.

| Group | Setting | Wiring point |
|---|---|---|
| Site | `site.name` | browser title, login-page brand name (delivered by `GET /api/site`) |
| Site | `site.allowRegister` | register endpoint 403 + login page hides the register entry |
| Site | `site.maintenanceMode` / `maintenanceMessage` | see below |
| Site | `site.requireInviteCode` | registration must carry a valid invite code (atomic redemption, refunded on failure) |
| Chat | `chat.maxMessageLength` (100–5000) | WS length validation + frontend input `maxlength` / character counter |
| Chat | `chat.recallMinutes` (0–60, **0 = recall off**) | WS recall validation + frontend context menu |
| Chat | `chat.maxGroupMembers` (2–2000) | join transaction validation (holds even under concurrency) |
| Upload | `upload.maxImageMB / maxAudioMB / maxFileMB / maxVideoMB` (1–32 / 1–32 / 1–200 / 1–100) | the upload handler caps via `MaxBytesReader` using the current value; `storage.Save` gains a `maxBytes` parameter (size is a caller policy, not hard-wired in the storage layer) |
| Security | `security.loginMaxAttempts` (0 or 2–20) / `loginLockMinutes` (1–1440) | login failure lockout (see §9.3) |

**Maintenance mode** (`middleware.Maintenance`):

- Attached to the `authed` route group (after Auth, so the role is readable) and to `register / forgot / reset`; admins pass, everyone else gets **503 + `{maintenance:true, message}`**.
- Whitelisted (no middleware): `/api/site` (the frontend needs the maintenance copy), `/api/health`, `/api/version`, **`/api/auth/login` (must stay available so admins can log in)**.
- `ws.Serve` gains a settings parameter: during maintenance a non-admin handshake gets 503 directly — blocking REST while established connections can still send/receive would be no block at all.
- Frontend: `stores/site.ts` refreshes the config every 30 seconds, and the App layer renders `MaintenanceView` when "maintenance && not admin && not on /login"; the maintenance page has an "admin login" entry to close the loop.
- Admins can still reach `/api/admin/*` (that group does not go through the `authed` middleware chain), so they can get into the admin console and turn maintenance off.

**Defaults equal pre-change behavior**: `OpsFrom()` falls back to defaults for missing / dirty values, so a bad value in the DB never wedges a feature;
saving in the admin console validates ranges explicitly (out of range 400) without silent clamping — an admin entering 50000 and getting 5000 quietly would think the save failed.

---

## 10. Deployment

- Frontend `npm run build` → static files served by Nginx; Nginx reverse-proxies `/api` to `:8080`, `/ws` with `Upgrade`/`Connection` headers (examples: `deploy/nginx.conf.example` bare-metal / `deploy/nginx.compose.conf` containers).
- Local one-command environment: `docker-compose.yml` (MySQL 8 + backend, first start runs `MIGRATE=true` to create tables).
- Production creates tables with versioned SQL from `migrations/`, not AutoMigrate.
- Production must set `JWT_SECRET` (release mode refuses to start without it) and `FRONTEND_URL` (WS Origin whitelist + password-recovery links).
- **Install (release-build mode, recommended)**: `scripts/package-release.sh` tags on the build machine and produces a precompiled package (ldflags injects the version; contains the backend binary + frontend dist + migration SQL), uploaded as a Gitee / GitHub Release attachment; on the server `scripts/install.sh` downloads and deploys with one command (auto-detects architecture/distro, sha256 verification), **no Go / Node / git needed — the server compiles nothing** (details in Ops Manual §3.1).
- **Upgrades**: three equivalent paths — ① `scripts/update.sh` (source deployment, blue-green two-instance hot swap); ② the `scripts/update.sh` inside the release package (release deployment, download a new Release, replace and restart gracefully); ③ the site admin console's "Online Update" tab (`server/internal/service/updater.go`: in release mode it checks the latest Release tag against `/api/version` → downloads and verifies → atomic replace → systemd restart; in source mode it git-pulls and builds locally). Security boundary: only the site admin can trigger it (Admin middleware + audit), the URL and commands are all fixed (version numbers come from the Release server / `origin/main`, with no user input accepted), and `WEB_UPDATE=false` disables it entirely. This endpoint is essentially "an admin account can trigger a code update", so a leaked account equals server control — the early design omitted in-app upgrade to avoid that risk and later added it at the user's request, with the risk narrowed by the boundary above.
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
- Remote: Gitee `https://gitee.com/Sun-Si-Cheng/modern-chat-new.git`, branch `main`.
- After each finished module, automatically `git add -A` → commit → push, no reminders needed.
- **Docs and code update together**: users do not read commit logs, and docs are the only window into feature changes. Whenever a commit involves a content update (new feature, behavior change, entry-point move, config addition/removal, deployment change, etc.), the change must be written into the corresponding doc **in the same commit**, never deferred or backfilled:
  - Feature/architecture/protocol changes → `docs/设计文档.md` (and §13 implementation log when necessary)
  - API field/validation/error-code changes → `docs/后端需求规格.md` (change the doc before the code)
  - Frontend structure/interaction/information-architecture changes → `docs/前端技术方案.md`
  - Environment variable/deployment/ops changes → `docs/运维手册.md`
  - User-visible feature increments → `CHANGELOG.md` (the `[Unreleased]` section)
- `.gitignore` ignores `node_modules/`, `dist/`, `.env`, build artifacts, logs.

## 13. Review Conclusions & Implementation Log

### 13.1 v3 review additions (2026-10-24)
Group search/leave/delete-friend APIs (§6), `client_msg_id` unique-key scope (§5), join initialization (§5), recall unread rollback (§9), plugin global takedown field (§5/§8), `api.request` client-bridged execution (§8), direct-chat concurrency race accepted (§5), WS frame membership check (§7).

### 13.2 Implementation complete (2026-10-24)
- **Frontend `web/`**: auth (login/register/recovery), four-column chat (icon rail / group filter pane / conversation & contact list / chat area + detail panel), grouped consecutive messages with read/recall folding, add-friend dialog (username/nickname search, inline accept), group admin, plugin sandbox host + declarative renderers, plugin management drawer, admin console (users + plugins), responsive (tablet folds detail / phone single panel), design polish (contrast, tabular numerals, focus reachability).
- **Backend `server/`**: all §6 APIs, §7 WS protocol (64-shard Hub / read-write pumps / presence / kicked), plugins (manifest validation / SSRF / upgrade overwrite / global takedown), admin console, three rate-limit tiers, structured logs, graceful shutdown, `migrations/0001…0022`.
- **Tests**: `go build` / `go vet` / `gofmt` / unit tests / integration tests (real MySQL full chain: auth→friends→direct→WS send/receive→idempotency→read→recall→unread→groups→ban kick→plugin install/upgrade/SSRF/takedown) all green; frontend `vue-tsc` / `oxlint+eslint` / `vite build` all green.
- **Security hardening** (after code review): WS Origin whitelist, `JWT_SECRET` enforced in production, configurable recovery-link base + constant-time compare, certificate files gitignored.
- **Delivery assets**: `docker-compose.yml`, `server/Dockerfile`, dual nginx configs, example plugins, `scripts/install.sh` / `package-release.sh` / `update.sh` / `uninstall.sh`, `/api/version` version verification.

### 13.3 Deviations from the initial design (adjusted during implementation, written back into this doc)
- The send pipeline & presence live in `internal/ws` (coupled with the Hub to avoid a ws ↔ service dependency cycle); `internal/service` carries the mailer / storage / thumbnail / push / backup / updater.
- Added `internal/version` (build version injection), the admin plugin aggregation API, same-name plugin upgrade semantics, `/api/health` and `/api/version`.
- The spec document `后端需求规格.md` unified `before_id` / `new_password` to camelCase (`beforeId` / `newPassword`), matching the frontend.
- Hub shards by **userID** rather than the `conversation_id` written in §9.2 of this doc (`shards[uid%64]`): broadcasting is essentially "deliver by user", and sharding by user keeps one user's devices in the same shard so each delivery locks one shard; sharding by conversation would require locking the multiple shards its members live on per fan-out. Lock granularity and hotspot behavior are the same; only the shard key differs.
- The REST gzip minimum compression threshold is set to 1KB (`gzip.WithMinLength`): below that length, a response plus the gzip header and insufficient entropy would end up larger.

### 13.4 Doc completion & defect fixes (2026-11-06)
- **Doc completion**: wrote back per the current code — §1.3 scope, §4 directory structure, §5 tables (new delete / favorites / emoji reactions / block / report / invite link / Push subscription), all §6 REST routes, the complete §7 WS frames; `docs/后端需求规格.md`, `docs/前端技术方案.md`, `docs/运维手册.md`, `web/README.md`, `server/README.md` updated in sync.
- **Fix: WS uplink 4 KB limit**: `chat.maxMessageLength` can be configured up to 5000 chars (over 4 KB in UTF-8), and the former `maxMsgSize=4096` would disconnect legitimate long messages outright with no `error` reaching the user; changed to 64 KB.
- **Fix: `user_change` frame unhandled in the frontend**: the server kept broadcasting nickname/avatar changes, but the frontend `ServerFrame` lacked that type and had no branch, so the real-time sync promised by the docs actually required a page refresh; the type and handling are now added (conversation peer, member cache, friends/requests, own multi-device).
- **Fix: own sent messages did not update the conversation list**: the sending client never received the "purely new message" `conv_change`, and the echo branch did not write `lastMessage*`, so the list preview / time / ordering stayed on the old message; also removed the incorrect "record read watermark on send" optimization, which would otherwise leave the server-side unread count permanently uncleared after forwarding into a conversation with unread messages.
- **Fix: new members saw "no messages yet" in the list after joining a group**: `initInboxes` only inserted an empty inbox row without syncing the current latest message as designed (§5); active join / being invited / auto-joining the public group on registration are now unified to write the latest preview and time, and push `last_read` to the latest without retroactively counting unread from before joining.
- **Fix: `FRONTEND_URL` parsing to empty caused a panic in password recovery**: with `FRONTEND_URL=","` or all-blank entries `FrontendURLs` was empty and `Forgot` indexed `[0]` out of range; `config.Load` now returns an error directly and prompts that at least one source must be configured.
