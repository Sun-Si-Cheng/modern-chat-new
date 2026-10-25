# ModernChat

A **frontend-backend separated, plugin-first** web chat platform. Out of the box you only get the plainest accounts + plain-text chat; users can write and install their own JS plugins to add images, emoji, themes, bots, mini-games, or anything else.

> Status: both the frontend (`web/`) and backend (`server/`) are implemented and tested, ready to run.

---

## Documentation Index

| Doc | Contents | When to read |
|---|---|---|
| [README.md](README.md) | Positioning, quick start, plugin development, upgrades & deployment | First contact with the project |
| [docs/设计文档.md](docs/设计文档.md) | Product positioning, architecture, database, protocol, plugin system, performance boundaries | Understanding design trade-offs |
| [docs/后端需求规格.md](docs/后端需求规格.md) | API contracts (request/response fields), validation rules, error codes, acceptance criteria | Changing APIs / integration / acceptance |
| [docs/前端技术方案.md](docs/前端技术方案.md) | Frontend structure, state management, WS realtime pipeline, performance conventions, common pitfalls | Changing the frontend |
| [docs/运维手册.md](docs/运维手册.md) | Full env-var table, deployment, backup & upgrade, logs, troubleshooting FAQ | Deployment & operations |
| [server/README.md](server/README.md) | Backend runbook (config, migrations, tests, structure) | Backend deployment & operations |
| [web/README.md](web/README.md) | Frontend structure overview & commands | Changing the frontend |
| [docs/legal/README.md](docs/legal/README.md) | How to maintain the Terms of Service / Privacy Policy, compliance tips | Replace operator info before going live |
| [CHANGELOG.md](CHANGELOG.md) | Version change log | Finding when a feature landed or a bug was fixed |

## Core Philosophy: Kernel vs Plugins

**Kernel (cannot be disabled)**
- Accounts: register / login / password recovery, JWT (with `token_version` active revocation), bans
- Friends, conversations (direct & group are isomorphic), plain-text messaging, message roaming, WebSocket pipeline
- Plugin runtime: sandbox loader, permission model, event bus, declarative UI renderer
- Admin console, plugin enable/disable / global takedown

**Plugins (installable & removable — remove them and you're back to plain text)**
- Image messages, emoji, Markdown rendering, dark theme, notification sounds, auto-reply bots, weather panels, mini-games… all implemented by plugins

## Implemented Features

| Module | Capabilities |
|---|---|
| Accounts | Registration (first registrant = site admin), login, password recovery/reset (`token_version` invalidates all sessions), nickname/email change, **custom avatars** (frontend crops & compresses to 256px, stored as data URL, shown site-wide), ban & forced logout |
| Friends | Search by username/nickname, apply / accept / reject / delete (can re-apply after rejection) |
| Conversations | Idempotent direct chats, groups (create/search/join/leave, 200-member cap), conversation list (last message + unread badge + filter groups: all/unread/direct/group) |
| Messages | Plain-text send/receive, cursor pagination, 2-minute recall, "peer typing", read receipts, online presence, `client_msg_id` idempotency, auto resync after reconnect, failed-send flag & resend |
| Group admin | Rename group, kick member, transfer ownership (`owner_id` and member roles synced in one transaction; owners must transfer before leaving) |
| Plugins | Upload / URL install (SSRF protection), permission-list confirmation, enable/disable, uninstall, same-name upload = upgrade, admin global takedown, custom message-type rendering |
| UI | Feishu-style four columns (icon rail / filter pane / conversation list / chat area), unified settings page (language / theme / background / account), login-page background & effects (aurora/waves/grid, visitor-selectable, persisted locally), grouped consecutive messages, live avatar/nickname sync, Chinese/English switch, light/dark/system themes |
| Mail | Password recovery email (SMTP, 465 implicit TLS / 587 STARTTLS, QQ/163 auth codes); link printed to server log when SMTP is not configured |
| Ops | Structured logs (rotation + retention days), health check `/api/health`, version `/api/version`, three-tier rate limiting, WS Origin whitelist, one-command upgrade from Gitee, coverage script (unit + integration merged) |

**Not in this phase (v2)**: voice/video, emoji reactions, @mentions, message forwarding/search/edit, group admins & muting, plugin marketplace, multi-device read sync, horizontal scaling (Redis rate limiting is already supported).

## Tech Stack

| Layer | Choice |
|---|---|
| Frontend | Vue 3 + Vite + TypeScript + Pinia + Vue Router |
| UI | tdesign-vue-next (TDesign design tokens are CSS variables; plugins can reskin directly) |
| Long lists | vue-virtual-scroller (virtual scrolling for message streams) |
| Realtime | Native WebSocket wrapper (25s heartbeat + exponential-backoff reconnect) |
| Backend | Go (see `server/go.mod` for the exact version; the dependency chain requires 1.26+) / Gin / gorilla/websocket |
| Database | MySQL 8.0 / GORM, utf8mb4; production uses versioned SQL from `migrations/` |

## Directory Structure

```
modernchat-new/
├── docs/                       # Design doc / backend spec / frontend guide / ops manual / legal texts
├── deploy/                     # nginx config examples (bare-metal / compose)
├── plugins/examples/           # Example plugins (ready to upload & try)
├── scripts/                    # update.sh / update.ps1 (Gitee upgrade) + coverage.sh / coverage.ps1 (coverage)
├── server/                     # Go backend
│   ├── cmd/server/main.go      # Entry: config → db → hub → router → graceful shutdown
│   ├── internal/
│   │   ├── config/             # Env-var parsing & defaults
│   │   ├── version/            # Build version (ldflags injection)
│   │   ├── model/              # GORM models + response views
│   │   ├── store/              # Data access (incl. migrations runner)
│   │   ├── handler/            # auth / user / friend / conversation / plugin / admin
│   │   ├── middleware/         # logger / jwt / ban / admin / limiter
│   │   ├── ws/                 # hub(64 shards) / serve(read-write pumps) / dispatch / presence
│   │   ├── plugin/             # manifest validation + URL fetch (SSRF protection)
│   │   └── service/            # mailer (SMTP: 465 implicit TLS / 587 STARTTLS)
│   ├── migrations/             # 0001_init + 0002_user_avatar (user custom avatars)
│   ├── test/                   # Integration tests (-tags integration; needs MySQL, see server/README)
│   └── Dockerfile
├── web/                        # Vue frontend
│   └── src/
│       ├── api/                # REST client (unified 401 handling)
│       ├── stores/             # auth / ws / conversations / friends / plugins / prefs
│       ├── composables/        # useWebSocket, usePluginHost (sandbox host)
│       ├── components/
│       │   ├── chat/           # Icon rail (AppRail) / filter pane (GroupPane) / conversation list / message pane / composer / detail panel / plugin drawer / profile card / avatar
│       │   ├── common/         # AuthScene (login-page background scene) / QuickPrefs (login-page quick prefs)
│       │   └── declarative/    # Declarative-UI whitelist renderer (8 components) + plugin panels
│       ├── types/proto.ts      # WS frames & business contracts (shared with backend)
│       └── views/              # login/register / password recovery / settings / legal / chat main / admin console
└── docker-compose.yml          # MySQL + backend one-command environment
```

## Quick Start

### Option 1: Docker one-command backend (recommended)

```bash
docker compose up -d --build           # MySQL 8 + backend (:8080); first start creates tables automatically
cd web && npm install && npm run dev   # frontend :5173
```

To use a custom secret, change `JWT_SECRET` in `docker-compose.yml` (e.g. `openssl rand -hex 32`).

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
- **Forgot username**: the system does not offer username recovery. The admin can reset that user's password in the console and tell them offline.
- **Password recovery**: goes to the registered email. Without SMTP configured, the reset link is only printed to the server log (see the "Logs" section in `server/README.md`); ops must fetch it and hand it over. The UI consistently says "if this email is registered, the reset link will be sent to it" and never reveals whether an email exists.
- **Email is only for password recovery**: this version has no email-verification flow, `email_verified` stays `false`; changing the email resets the flag at the data layer.
- **Upgrades**: deployers run `scripts/update.sh` (see "Update & Upgrade"). The UI only shows the current build version, never ops commands.

## Plugin Development

Every plugin is a single JS file (≤1MB), stored on the server, loaded by the frontend in a sandbox iframe (`allow-scripts`, no same-origin). Plugins never touch the DOM and cannot get the JWT; they communicate with the host bridge only via `postMessage`, and all UI is delivered as **declarative JSON** rendered by whitelist components.

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
- The admin can **globally take down** a plugin by name in `/admin` → "Plugins" (effective for all users immediately)

### Try the example plugin

The repo ships with [`plugins/examples/weather.plugin.js`](plugins/examples/weather.plugin.js): a sidebar weather panel + a custom `weather` message type.

1. After login, click the "Plugins" icon in the sidebar → upload the file → confirm the permission list and enable it
2. The right detail panel shows a "Weather (example plugin)" card: click "Refresh data" to see the panel update, and "Send to current chat" to send it to the open conversation
3. The message bubble is rendered by the plugin's custom renderer (whitelist component card), not raw JSON
4. An admin account can see installs aggregated by name in `/admin` → "Plugins" and perform a global takedown

The example deliberately makes no network requests; for wiring a real API (`net` permission + `allowedHosts` + `api.request`) see the comment at the top of the file.

---

## Protocol Overview

- **REST**: prefix `/api`; everything except register/login/forgot/reset/health/version requires `Authorization: Bearer <token>`; errors are always `{ "error": "msg" }`. Full endpoints & fields: [Backend Spec §3](docs/后端需求规格.md).
- **WebSocket**: `GET /ws?token=<JWT>` (Origin whitelist check). Uplink frames `chat / typing / read / recall / ping`; downlink frames `message / recall / read / typing / presence / conv_change / kicked / pong / error`. Messages are deduplicated by `clientMsgId`. See [Design Doc §7](docs/设计文档.md).
- **Version**: `GET /api/version` → `{version, commit, buildTime}`, used to verify an upgrade took effect.

## Update & Upgrade

After pushing code to Gitee, run **one command** on the deploy machine to pull and rebuild (no manual file uploads):

```bash
./scripts/update.sh                          # pull master → build frontend & backend → restart
./scripts/update.sh dev                      # specific branch
./scripts/update.sh --no-restart             # build artifacts only, restart later
RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
```

On Windows locally: `.\scripts\update.ps1` (supports `-Branch` / `-NoRestart`).

What the script does: `git pull --ff-only` (errors out on local divergence, never auto-merges) → `go build` (version injected) → `npm ci && npm run build` → restart via `RESTART_CMD` / docker compose / systemd in that order.

**Verify it took effect**: `curl http://127.0.0.1:8080/api/version`, or check the build info in the top-right of the admin console (`version · commit · build time`).

> Why no "click-to-auto-upgrade" in the console: an admin API triggering `git pull` + restart is equivalent to attaching remote code execution to that account; if it leaks, the server is lost. Upgrades are always performed by the ops script.

## Deployment

- **Docker**: `docker compose up -d --build` starts MySQL + backend; the built frontend can be served by the nginx container from `deploy/nginx.compose.conf` (usage in the comment at the end of `docker-compose.yml`)
- **Bare metal**: `go build -o modernchat-server ./cmd/server`; frontend `npm run build` output served by Nginx, `/api` reverse-proxied to `:8080`, `/ws` with `Upgrade` / `Connection` headers (`deploy/nginx.conf.example`)
- Production must set: `GIN_MODE=release`, `JWT_SECRET` (≥32 bytes, refuses to start by default), `FRONTEND_URL` (WS Origin whitelist + password-recovery links), `MIGRATE=true`
- Config (DB DSN, JWT_SECRET, SMTP) goes through environment variables, never committed
- This phase is a **single instance** (presence & Hub routing are in memory); horizontal scaling is planned for v2

## Repository

- Remote: <https://gitee.com/Sun-Si-Cheng/modern-chat-new> (branch `master`)
- Commit convention: after finishing each module, `git add -A` → commit → push

## License

This project is licensed under the [Apache License 2.0](LICENSE), Copyright 2026 Sun-Si-Cheng.

You may freely use, modify and distribute (including commercial use) under the license; redistribution must retain the copyright & license notice, and modified files must carry a change note. Full terms in [LICENSE](LICENSE).

## Terms & Compliance

The site provides a [Terms of Service](/terms) and a [Privacy Policy](/privacy) page (bilingual, reachable from the login page and the profile dialog); the texts live in [`web/src/content/legal.ts`](web/src/content/legal.ts).

Before running a public instance, replace the operator, contact email and domain in `legalMeta`, and read [docs/legal/README.md](docs/legal/README.md) (a mapping between clauses and implementation code, plus compliance tips for different jurisdictions).
