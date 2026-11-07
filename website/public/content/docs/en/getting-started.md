# ModernChat

A **frontend-backend separated, plugin-first** web chat platform. It works out of the box: accounts, friends, direct chats / group chats, and **text + image + voice messages**; users can also write and install their own JS plugins to add emoji, themes, bots, mini-games, or any other capability to the site.

> Status: both the frontend (`web/`) and backend (`server/`) are implemented and tested, ready to run.

---

## Documentation Index

| Doc | Contents | When to read |
|---|---|---|
| [README.md](README.md) | Positioning, quick start, plugin development, upgrades & deployment | First contact with the project |
| [docs/设计文档.md](docs/设计文档.md) | Product positioning, architecture, database, protocol, plugin system, performance boundaries | Understanding design trade-offs |
| [docs/后端需求规格.md](docs/后端需求规格.md) | API contracts (request/response fields), validation rules, error codes, acceptance criteria | Changing APIs / integration / acceptance |
| [docs/前端技术方案.md](docs/前端技术方案.md) | Frontend structure, state management, WS realtime pipeline, performance conventions, common pitfalls | Changing the frontend |
| [docs/运维手册.md](docs/运维手册.md) | Full env-var table, admin-console configurable items, object storage, deployment, backup & upgrade, logs, troubleshooting FAQ | Deployment & operations |
| [docs/安全与迁移指南.md](docs/安全与迁移指南.md) | Attachment authorization, logout & token_version, WS re-validation, plugin sandbox, upload pipeline, migrations 0023–0025, Docker parameters | Taking over the project / debugging security & data consistency |
| [docs/UI设计方案.md](docs/UI设计方案.md) | Design tokens, motion conventions, component feel, acceptance criteria & implementation log | Changing styles / adding motion / adding controls |
| [server/README.md](server/README.md) | Backend runbook (config, migrations, tests, structure) | Backend deployment & operations |
| [web/README.md](web/README.md) | Frontend structure overview & commands | Changing the frontend |
| [website/README.md](website/README.md) | Website structure, commands, doc syncing & plugin marketplace | Changing the website / docs site |
| [docs/legal/README.md](docs/legal/README.md) | How to maintain the Terms of Service / Privacy Policy, compliance tips | Replace operator info before going live |
| [CHANGELOG.md](CHANGELOG.md) | Version change log | Finding when a feature landed or a bug was fixed |

## Core Philosophy: Kernel vs Plugins

**Kernel (cannot be disabled)**
- Accounts: register / login / password recovery, JWT (with `token_version` active revocation), bans
- Friends, conversations (direct & group are isomorphic), plain-text messaging, message roaming, WebSocket pipeline
- Plugin runtime: sandbox loader, permission model, event bus, declarative UI renderer
- Admin console, plugin enable/disable / global takedown

**Plugins (installable & removable — remove them and you're back to "text + image + voice")**
- Emoji, Markdown rendering, notification sounds, auto-reply bots, weather panels, mini-games… all implemented by plugins

> Images and voice are core IM fundamentals, so they live in the kernel; plugins focus on enhancements and gameplay rather than filling in basic capabilities.

## Implemented Features

| Module | Capabilities |
|---|---|
| Accounts | Registration (first registrant = site admin), login, password recovery/reset (`token_version` invalidates all sessions), nickname/email change, **custom avatars** (frontend crops & compresses to 256px, stored as data URL, shown site-wide), ban & forced logout |
| Friends | Search by username/nickname, apply / accept / reject / delete (can re-apply after rejection) |
| Conversations | Idempotent direct chats, group chats (create/search/join/leave, 200-member cap), **initial public group "Public Chat Room"** (all users joined on first start, new registrants added automatically, can search and rejoin after leaving), conversation list (last message + unread badge + filter groups: all/unread/direct/group/**archive**), **archived conversations**, **delete conversation (removes it from your own list only)**, **clear chat history** |
| Messages | Text / **image** / **voice** / **video** send & receive, **quoted replies** (clicking the quote jumps back to the original message and highlights it), **@mentions** (pick members in a group, mention highlighted, "someone @mentioned you" still shown while muted), **message search** (per-conversation + global, FULLTEXT ngram), **edit** (within 5 minutes, marked "edited"), **emoji reactions** (disabled by default, enabled via `FEATURES.reactions`), direct-chat send status (sending / sent / delivered / read), cursor pagination, **incremental offline catch-up by read cursor**, recall (time limit configurable in the admin console; the notice is removed from chat history automatically after 3 minutes), "peer typing", read receipts, online presence, `client_msg_id` idempotency, automatic resync after reconnect, failed-send flag & resend, **delete for yourself only** |
| Content moderation | Block a user (blocks messages and friend requests in both directions), message report (once per user per message), admin / group owner deletion of offending messages (removed on all clients), backend sensitive words filtering |
| Conversation experience | Pin (synced across devices), mute (unread badge becomes a small dot), **multi-device read sync** (reading on any device of the same account clears unread on the others), unsent drafts preserved and marked `[Draft]` in the list; image right-click to view full size / save / favorite; **desktop notifications** (when the page is in the background, muted conversations excluded), **PWA** (installable to desktop, offline caching of static assets) |
| Media | Canvas compression before image upload (max edge 1600 / webp), click-to-zoom lightbox, server-side thumbnail generation; voice recording (click to start/stop, 60s cap) with a custom bubble (width follows duration, progress bar, only one plays at a time); video sent by picking a local file (mp4/webm/mov/m4v, native player, 16:9 inline playback); file type detected by **file header**, defaults: image ≤8 MB, voice ≤5 MB, video ≤30 MB, group files ≤20 MB (adjustable in the admin console) |
| Favorites | Right-click a message to favorite / unfavorite; the favorites drawer is sorted newest first and can jump back to the original conversation; empty state has guidance |
| Emoji / forwarding | Personal sticker library (upload ≤2 MB, one tap to send, right-click to delete); right-click forwarding of a message to any conversation (single message, bubble marked "Forwarded") |
| Group collaboration | Group announcement (published by the owner, collapsible bar at the top of the chat area + edited/revoked in the detail panel), group files (≤20 MB, members can upload, uploader or owner can delete), invite friends into a group, **group invite link** (any member can generate, valid for 0–30 days, landing page shows group info without login) |
| Group management | Rename group, **group avatar** (uploaded by owner / admin, can be reset to default), **admins** (set / removed by the owner), **mute a member** (owner and admins, lifted automatically when it expires), **mute all** (owner/admins can still speak), invite friends into the group, kick members, transfer ownership (`owner_id` and member roles synced in one transaction; owners must transfer before leaving) |
| Plugins | Upload / URL install (SSRF protection), permission-list confirmation, enable/disable, uninstall, same-name upload = upgrade, custom message-type rendering, admin global takedown |
| UI | Feishu-style four columns (icon rail / group pane / conversation list / chat area), unified settings page (appearance / account / layout / plugins / site admin / about), **site-wide announcement popup** (shown immediately while online, re-shown on the next visit to the chat page if it arrived while offline, dismiss marks it read; the system notification conversation is storage only and is not shown in the conversation list), login-page background color & effects (aurora/waves/grid, visitor-selectable, persisted locally), site-wide page background (preset colors / custom color / local image + grid / dots / diagonal stripes; images are compressed and stored locally, applied site-wide; **the entry point exists only in Settings → Appearance**), grouped consecutive messages, live avatar/nickname sync, Chinese/English switch, light/dark/system themes |
| Layout | Group pane / conversation list / detail panel can all be **drag-resized**, double-click a divider to toggle between "docked / drawer", width and mode persist across reloads, one-click restore defaults in Settings |
| Admin console | Overview stats, user management (search / filter / ban / force logout / reset password), plugin listing & takedown, **site-wide announcements**, audit, **invite codes** (bulk generation / disable / atomic redemption), system settings (SMTP / Redis / object storage, secrets encrypted at rest and never echoed back; **site name / registration toggle / maintenance mode / message length / recall time limit / group cap / upload size / login lockout** take effect immediately) |
| Security & operations | **Maintenance mode** (non-admins only see the maintenance page; admins can still enter the console and turn it off), **invite codes** (bulk generation / disable in the console, atomic redemption), **login lockout** (per-account counter + temporary lock, complementing IP rate limiting) |
| Mail | Password recovery email (SMTP, 465 implicit TLS / 587 STARTTLS, QQ / 163 auth codes); link printed to server log when SMTP is not configured |
| Ops | Structured logs (rotation + retention days), health check `/api/health`, version `/api/version`, Prometheus `/api/metrics` (`METRICS_TOKEN` optional protection), three-tier rate limiting, WS Origin whitelist, database backup (manual in the console / scheduled), orphan attachment cleanup, one-command install & online update for release builds, coverage script (unit + integration merged) |

**Not in this phase (v2)**: video calls, merged forwarding (chat-record cards; this phase sends "multiple messages concatenated into one text"), advanced permissions beyond group-admin muting, plugin marketplace (manual install only in this phase), horizontal scaling (Redis rate limiting is already supported), multi-select image preview & send, voice waveform and seek, the default entry for emoji reactions (implemented in code, disabled by default).

## Tech Stack

| Layer | Choice |
|---|---|
| Frontend | Vue 3 + Vite + TypeScript + Pinia + Vue Router |
| UI | **UnoCSS atomic classes + in-house control layer `components/ui/Ui*`** (13 components); `tdesign-vue-next` remains only for admin tables, tabs and auth-page forms. Colors/radii/font sizes/motion all use the `--td-*` and `--mc-*` tokens, so plugins can reskin directly |
| Long lists | vue-virtual-scroller (virtual scrolling for message streams) |
| Storage | Local disk (`DATA_DIR/uploads`) or **S3-compatible object storage** (MinIO / AWS S3 / OSS / COS), switchable in the admin console |
| Realtime | Native WebSocket wrapper (25s heartbeat + exponential-backoff reconnect) |
| Backend | Go (see `server/go.mod` for the exact version; the dependency chain requires 1.26+) / Gin / gorilla/websocket |
| Database | MySQL 8.0 / GORM, utf8mb4; production uses versioned SQL from `migrations/` |

## Directory Structure

```
modernchat-new/
├── docs/                       # Design doc / backend spec / frontend guide / ops manual / UI guide / plugin development / legal texts
├── deploy/                     # nginx config examples (bare-metal / compose)
├── website/                    # Website (Vue 3 + Vite + TDesign): landing page / docs site / plugin marketplace
│   ├── public/docs/            # Docs-site content generated by syncing repo docs (website/scripts/sync-docs.*)
│   └── scripts/sync-docs.*     # Syncs README / docs / server / web docs to the website
├── plugins/examples/           # Example plugins (weather / dice / word-count / conv-stats)
├── scripts/                    # install.sh (one-command release install) / package-release.sh (build-machine packaging)
│                               # update.sh (hot update / release upgrade) / update-release.sh / uninstall.sh / coverage.sh
├── server/                     # Go backend
│   ├── cmd/server/main.go      # Entry: config → db → hub → router → graceful shutdown
│   ├── internal/
│   │   ├── config/             # Env-var parsing & defaults
│   │   ├── version/            # Build version (ldflags injection)
│   │   ├── model/              # GORM models + response views
│   │   ├── store/              # Data access + migrations runner + AES-GCM secretbox + system settings
│   │   ├── handler/            # auth / user / friend / conversation / moderation / plugin /
│   │   │                       # admin / admin_settings / upload / file / sticker / push / ops / site
│   │   ├── middleware/         # logger / jwt / ban / admin / limiter / maintenance / guard
│   │   ├── ws/                 # hub(64 shards) / serve(read-write pumps) / dispatch / presence
│   │   ├── plugin/             # manifest validation / esbuild TS transpile / URL fetch (SSRF protection)
│   │   ├── service/            # mailer / storage(local+S3) / thumbnail / push / backup / updater
│   │   ├── logx/ metrics/      # Log rotation / Prometheus metrics
│   │   └── router/             # All routes & middleware wiring
│   ├── migrations/             # 0001_init … 0022_message_fulltext (versioned SQL)
│   ├── test/                   # Integration tests (-tags integration; needs MySQL, see server/README)
│   └── Dockerfile
├── web/                        # Vue frontend
│   └── src/
│       ├── api/                # REST client (unified 401 handling)
│       ├── stores/             # auth / ws / conversations / friends / favorites / plugins / prefs / layout / site
│       ├── composables/        # useWebSocket, usePluginHost, notifications, announcement, localCache, useOverlay
│       ├── components/
│       │   ├── ui/             # 13 in-house controls (UiButton/UiInput/UiDialog/UiDrawer/…)
│       │   ├── chat/           # icon rail / group pane / conversation list / message stream / composer / detail panel /
│       │   │                   # plugin drawer / favorites drawer / notification drawer / voice bubble / profile card / avatar
│       │   ├── common/         # AuthScene / QuickPrefs / AppBackgroundPicker / PaneResizer / ContextMenu
│       │   └── declarative/    # Declarative-UI whitelist renderer (11 components) + plugin panels
│       ├── types/proto.ts      # WS frames & business contracts (shared with backend)
│       └── views/              # login/register / password recovery / settings / legal / chat main / group invite / maintenance page / admin console
├── docker-compose.yml          # MySQL + backend one-command environment
└── bin/ dist-release/          # Local build artifacts / release package output (gitignored)
```

## Quick Start

### Option 0: One-command install on a production server (BT Panel style)

On a fresh Linux server (root), **one command** completes "install environment → download the release build → create the database → deploy → print the access URL":

```bash
curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh | bash
```

The script automatically detects the server architecture (amd64/arm64) and distro (apt/dnf/yum), then downloads the matching prebuilt release package — the server **needs no Go / Node / git**, and nothing is compiled at any point. Every interactive question has a default, so pressing Enter all the way gives a fully automatic run; values can also be preset via environment variables (`RELEASE_SOURCE=gitee|github` selects the source, `RELEASE_TAG` pins the version, `DOMAIN`/`DB_MODE` etc.). See [Ops Manual §3.1](docs/运维手册.md).

Release flow: update `CHANGELOG.md` and sync the version numbers in `web/` and `website/`, then run `sh scripts/check-version.sh` to confirm they match (the latest formal version in CHANGELOG is the baseline), then tag and run `scripts/package-release.sh` on the build machine, and upload the tar.gz and .sha256 from `dist-release/` as Release assets (Gitee / GitHub both work; the console's graphical update and the install script download the new version automatically according to `RELEASE_SOURCE`).

### Option 1: Docker one-command backend (local development)

```bash
docker compose up -d --build           # MySQL 8 + backend (:8080); first start creates tables automatically
cd web && npm install && npm run dev   # frontend :5173
```

To use a custom secret, change `JWT_SECRET` in `docker-compose.yml` (e.g. `openssl rand -hex 32`).

The image build injects `VERSION` / `COMMIT` / `BUILD_TIME` via build args (`/api/version` reports them); if not passed they fall back to `dev` / `unknown`, and a build context without `.git` will not fail. Build with a version and verify:

```bash
VERSION=$(git describe --tags --always) COMMIT=$(git rev-parse --short HEAD) \
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) docker compose up -d --build
curl http://127.0.0.1:8080/api/version
# Build the image directly: docker build --build-arg VERSION=v0.10.3 -t modernchat-server ./server
```

Image sources default to the official ones; on mainland China networks they can be overridden with build args (the following third-party mirrors are examples — verify availability and trustworthiness yourself):

```bash
GO_IMAGE=docker.m.daocloud.io/library/golang:1.26-alpine \
RUNTIME_IMAGE=docker.m.daocloud.io/library/alpine:3.20 \
GOPROXY=https://goproxy.cn,direct \
APK_MIRROR=mirrors.aliyun.com \
VERSION=$(git describe --tags --always) COMMIT=$(git rev-parse --short HEAD) \
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
docker compose up -d --build

# Building the image directly works the same
docker build --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg APK_MIRROR=mirrors.aliyun.com -t modernchat-server ./server
```

- `GO_IMAGE` / `RUNTIME_IMAGE`: base images for the build stage and the runtime stage
- `GOPROXY`: Go module proxy, default `https://proxy.golang.org,direct`
- `APK_MIRROR`: Alpine package source domain (domain only), default `dl-cdn.alpinelinux.org`

### Option 2: Run manually

```bash
# Backend (bring your own MySQL 8.0; AutoMigrate creates tables when MIGRATE=false)
cd server
cp .env.example .env       # DB_DSN + JWT_SECRET (required when GIN_MODE=release)
go run ./cmd/server        # :8080

# Frontend
cd web
npm install
npm run dev                # http://localhost:5173, /api and /ws proxied to :8080
```

The first registered user automatically becomes the site admin (can access `/admin`: ban users / reset passwords / global plugin takedown).

### Check commands

```bash
# Frontend
cd web && npm run type-check && npm run lint && npm run build

# Backend
cd server && go build ./... && go vet ./... && go test ./...
cd server && go test -tags integration ./test/   # needs a local MySQL
```

---

## Product & Operations Notes (documentation only, never shown in the UI)

The rules below are written for deployers and developers; UI copy stays user-facing and never reveals internal rules or development notes:

- **The first registered account automatically becomes the site admin** (`role=admin`); everyone after that is a regular user. The admin console provides user ban/unban/random password reset and global plugin takedown.
- **Forgot username**: the system does not offer username recovery. The site admin can reset that user's password in the console and tell them offline.
- **Password recovery**: goes to the registered email. Without SMTP configured, the reset link is only printed to the server log (see the "Logs" section in `server/README.md`); ops must fetch it and hand it over. The UI consistently says "if this email is registered, the reset link will be sent to it" and never reveals whether an email exists.
- **Email usage**: registration email verification (enabled by default, can be turned off in the console), password recovery, and verification when changing the email. The first registered account is exempt from the verification code but must still provide an email. **When verification is enabled but SMTP is not configured, registration returns 503 directly** (only the first account is exempt; no more silent degradation, preventing bulk registration that bypasses site policy).
- **Upgrades**: deployers run `scripts/update.sh` (see "Update & Upgrade"). The UI only shows the current build version, never ops commands.

## Plugin Development

Each plugin is a single JS file (≤1MB), stored on the server and loaded by the frontend via a `srcdoc`-injected sandbox iframe (`allow-scripts`, no same-origin). Plugins never touch the DOM and cannot get the JWT; they communicate with the host bridge only via `postMessage`, and all UI is delivered as **declarative JSON** rendered by whitelist components.

> **Plugins that rely on bare `fetch` are broken and must switch to `mc.request`.** The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method. The sandbox is an opaque origin with CSP `default-src 'none'`, and network egress is limited to `allowedHosts`. See [插件开发.md](docs/插件开发.md).

### Entry convention

Define `__mcActivate(api)` in the plugin file; the host injects an `api` object on load:

```js
// my-plugin.js
function __mcActivate(api) {
  // 1. Listen to events (permission: message:read)
  api.on('message:new', (frame) => {
    // frame: { type:'message', convId, from, content, msgType, ... }
  });

  // 2. Side panel (permission: ui:panel); button actions come back via 'ui:action'
  api.addSidePanel({
    title: 'Weather',
    body: [
      { component: 'text', props: { content: 'Hangzhou Sunny 22°C' } },
      { component: 'button', props: { text: 'Refresh' }, action: 'refresh' },
    ],
  });
  api.on('ui:action', ({ action }) => {
    if (action === 'refresh') {
      // call api.request(...) then re-submit addSidePanel
    }
  });

  // 3. Register a custom message-type renderer (permission: message:render)
  api.registerMessageRenderer('weather', (payload) => ({
    title: '',
    body: [{ component: 'text', props: { content: `${payload.city} ${payload.sky} ${payload.temp}°C` } }],
  }));
}
```

### SDK overview

| API | Purpose | Permission |
|---|---|---|
| `api.on(event, fn)` | Subscribe to `message:new` / `message:recall` / `presence` / `conv:open` / `ui:action` | `message:read` |
| `api.send(convId, payload, msgType?)` | Send a message; objects are JSON-serialized automatically | `message:send` |
| `api.registerMessageRenderer(type, fn)` | Custom message type → UI description, rendered by the host inside the bubble | `message:render` |
| `api.addSidePanel(desc)` | Submit a side-panel UI description | `ui:panel` |
| `api.storage.get(key)` / `set(key, value)` | Per-plugin namespaced storage (≤64KB per value) | `storage` |
| `api.request(url, opts?)` | Host-mediated fetch; only `allowedHosts` domains pass (subject to browser CORS) | `net` |
| `api.theme.apply(cssVars)` | Modify `--td-*` theme tokens, site-wide instant reskin | `ui:theme` |

### Manifest

Written in the **first block comment** of the code; the server parses and validates it (invalid name/version/permissions/whitelist → install rejected):

```js
/* {
  "name": "weather-demo",
  "version": "1.0.0",
  "description": "Example plugin: sidebar weather panel",
  "permissions": ["message:read", "message:send", "message:render", "ui:panel", "storage"],
  "allowedHosts": ["api.example.com", "*.openweathermap.org"]
} */
```

### Whitelist components

UI descriptions only accept 8 components: `text / image / button / input / card / list / divider / link`. Props accept primitive values only; `link`/`image` only allow `http(s)`; components outside the whitelist and `javascript:` links are always rejected.

### Install / upgrade

- **Upload a file**: chat page → Plugin manager → upload JS
- **URL install**: server-side SSRF protection before fetching (rejects private / loopback / non-http(s), limited size & timeout)
- After install a permission list pops up — **enabled only after you confirm**
- **Upgrade**: uploading a plugin with the same name again upgrades it — reuses the same record (same id, enable/disable state preserved); an enabled plugin reloads the new code after confirmation
- The site admin can **globally take down** a plugin by name in `/admin` → "Plugins" (effective for all users immediately)

### Try the example plugin

The repo ships with three example plugins (also downloadable from the "Marketplace" page on the website):

- [`plugins/examples/weather.plugin.js`](plugins/examples/weather.plugin.js): sidebar weather panel + custom `weather` message type
- [`plugins/examples/dice.plugin.js`](plugins/examples/dice.plugin.js): dice roller panel + custom `dice` message type
- [`plugins/examples/word-count.plugin.js`](plugins/examples/word-count.plugin.js): conversation stats panel (`message:new` event + storage persistence)

1. After login, click the "Plugins" icon in the sidebar → upload the file → confirm the permission list and enable it
2. The right detail panel shows a "Weather (example plugin)" card: click "Refresh data" to see the panel update, and "Send to current chat" to send it to the open conversation
3. The message bubble is rendered by the plugin's custom renderer (whitelist component card), not raw JSON
4. A site admin account can see installs aggregated by name in `/admin` → "Plugins" and perform a global takedown

The example deliberately makes no network requests; for wiring a real API (`net` permission + `allowedHosts` + `api.request`) see the comment at the top of the file.

---

## Protocol Overview

- **REST**: prefix `/api`; everything except register/login/logout/forgot/reset/health/version requires `Authorization: Bearer <token>`; errors are always `{ "error": "msg" }`. Login also sets a same-origin `mc_auth` cookie, used only for `/uploads` subresources and the WS handshake fallback; REST still only accepts the header. Full endpoints & fields: [Backend Spec §3](docs/后端需求规格.md).
- **Attachments**: `GET /uploads/**` requires login (`Authorization` or the `mc_auth` cookie), directory listing is forbidden, and caching is `private, max-age=3600`. In S3 mode new files are served by the bucket/CDN and access control is delegated there.
- **WebSocket**: `GET /ws?token=<JWT>` (compatible with older clients; falls back to the `mc_auth` cookie when the token is missing), Origin whitelist check; **every frame** re-validates the ban and `token_version`, replying with an `error` frame and disconnecting on failure. Uplink frames `chat / typing / read / delivered / recall / edit / reaction / ping`; downlink frames `message / message_update / recall / reaction / message_delivered / message_admin_deleted / read / typing / presence / user_change / conv_change / kicked / pong / error`. Messages are deduplicated by `clientMsgId`, and a single uplink frame is capped at 64 KB. See [Design Doc §7](docs/设计文档.md).
- **Version**: `GET /api/version` → `{version, commit, buildTime}`, used to verify an upgrade took effect.

## Update & Upgrade

Upgrade with **one command** on the deploy machine; the script detects the deployment form automatically:

```bash
./scripts/update.sh                          # release: download the latest Release; source: git pull + build; both hot-update
./scripts/update.sh dev                      # source mode with a specific branch
./scripts/update.sh --no-restart             # prepare artifacts only, switch at a time of your choosing
./scripts/update.sh --cold                   # force the old-style restart (seconds of downtime)
RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
```

- **Release deployment (install.sh)**: look up the latest tag from `RELEASE_API_URL` → download the matching tar.gz + `.sha256` verification → atomically replace the binary / frontend artifacts → graceful restart (blue-green pair of instances; online users reconnect automatically and catch up on messages).
- **Source deployment (git clone)**: `git pull --ff-only` (errors out on local divergence, never auto-merges) → `go build` (version injected) → `npm ci && npm run build` → blue-green switch.

Windows local development does not use the upgrade script: pull and rebuild manually.

**Verify it took effect**: `curl http://127.0.0.1:8080/api/version`, or check the build info in the top-right of the admin console (`version · commit · build time`).

> **Two upgrade paths**:
> - **Graphical (recommended for daily use)**: admin console → Online update tab, one-click upgrade + automatic restart. Release installs (install.sh) download the Release (sha256 verified, no local build needed); source deployments do git pull + local build. Safety boundary: site admin only, target version comes from the Release server / origin/main (never user input), operation written to audit, only available for install.sh systemd deployments, and `WEB_UPDATE=false` turns it off entirely. The risk is essentially the same as script upgrades (the site admin account can trigger code updates), so keep the administrator account safe.
> - **Script (recommended for production / unattended)**: `scripts/update.sh` in the install directory (release mode: download, verify and replace a new Release then restart; source mode: git pull + build). See Ops Manual §4.

## Deployment

- **One-command install**: `scripts/install.sh` (BT Panel style: environment self-check & installation → download the release build (no Go / Node / git needed) → create the database → systemd + nginx → print the site URL; see Ops Manual §3.1)
- **Docker**: `docker compose up -d --build` starts MySQL + backend; the built frontend can be served by the nginx container from `deploy/nginx.compose.conf` (usage in the comment at the end of `docker-compose.yml`)
- **Bare metal**: `go build -o modernchat-server ./cmd/server`; frontend `npm run build` output served by Nginx, `/api` reverse-proxied to `:8080`, `/ws` with `Upgrade` / `Connection` headers (`deploy/nginx.conf.example`)
- Production must set: `GIN_MODE=release`, `JWT_SECRET` (≥32 bytes, refuses to start by default), `FRONTEND_URL` (WS Origin whitelist + password-recovery links), `MIGRATE=true`
- Config (DB DSN, JWT_SECRET, SMTP) goes through environment variables, never committed
- This phase is a **single instance** architecture (presence & Hub routing are in memory); horizontal scaling is planned for v2

## Repository

- Remote: <https://gitee.com/Sun-Si-Cheng/modern-chat-new> (branch `main`)
- Commit convention: after finishing each module, `git add -A` → commit → push

## License

This project is licensed under the [Apache License 2.0](LICENSE), Copyright 2026 Sun-Si-Cheng.

You may freely use, modify and distribute (including commercial use) under the license; redistribution must retain the copyright & license notice, and modified files must carry a change note. Full terms in [LICENSE](LICENSE).

## Terms & Compliance

The site provides a [Terms of Service](/terms) and a [Privacy Policy](/privacy) page (bilingual, reachable from the login page and the profile dialog); the texts live in [`web/src/content/legal.ts`](web/src/content/legal.ts).

Before running a public instance, replace the operator, contact email and domain in `legalMeta`, and read [docs/legal/README.md](docs/legal/README.md) (a mapping between clauses and implementation code, plus compliance tips for different jurisdictions).
