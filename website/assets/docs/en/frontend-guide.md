# ModernChat Frontend Guide (web/)

> For people **changing the frontend**: structure, state, realtime pipeline, performance conventions and pitfalls.
> API contracts: [后端需求规格.md](后端需求规格.md); product & architecture trade-offs: [设计文档.md](设计文档.md).

---

## 1. Scope & Stack

The frontend is a Vue 3 SPA that **does not depend on any server rendering**: all data comes from REST (`/api/*`) and one long-lived connection (`/ws`).

| Layer | Choice | Version |
|---|---|---|
| Framework | Vue 3 (`<script setup>` + Composition API) | ^3.5.42 |
| Build | Vite | ^8.2.2 |
| Language | TypeScript | ~6.0.0 |
| State | Pinia | ^4.0.3 |
| Router | Vue Router | ^5.3.1 |
| i18n | vue-i18n | ^11.4.16 |
| UI | tdesign-vue-next | ^1.20.10 |
| Icons | tdesign-icons-vue-next | ^0.4.11 |
| Long lists | vue-virtual-scroller | ^3.0.5 |

Design principle: **keep the kernel thin**. Beyond accounts / messages / conversations / the plugin runtime, every enhancement (image messages, emoji, Markdown, theme skins, bots) comes from plugins; the frontend only provides 8 whitelist declarative components for plugin rendering.

---

## 2. Directory Structure

```
web/src/
├── main.ts                 # App assembly: pinia / i18n / router; no global TDesign registration (see §7)
├── App.vue                 # Root: RouterView + global styles
├── api/
│   ├── client.ts           # fetch wrapper: Bearer injection, error normalization, 401 clears token
│   └── index.ts            # Domain APIs: auth/user/conv/friend/plugin/admin
├── stores/                 # Pinia (see §3)
│   ├── auth.ts             # token + current user + isAdmin
│   ├── ws.ts               # WebSocket connection, online set, send/receive outlets
│   ├── conversations.ts    # conversation list, message cache, unread, filters, sending & optimistic messages
│   ├── friends.ts          # friends & requests
│   ├── plugins.ts          # plugin list
│   └── prefs.ts            # language / theme / login-page background preferences (persisted)
├── composables/
│   ├── useWebSocket.ts     # connection lifecycle: connect on mount, disconnect on unmount, bind uplink outlets
│   ├── usePluginHost.ts    # plugin host: iframe sandbox, postMessage protocol, event bus
│   └── useAppI18n.ts       # t / te (error message mapping)
├── components/
│   ├── chat/               # AppRail / GroupPane / ConvSidebar / MessagePane /
│   │                       # MessageComposer / DetailPanel / PluginDrawer /
│   │                       # ProfileCard / AddFriendDialog / AvatarBadge
│   ├── common/             # AuthScene (login background scene) / QuickPrefs (language · theme · background)
│   └── declarative/        # plugin declarative UI renderers (8 whitelist components)
├── views/                  # Auth / Forgot / Settings / Legal / ChatLayout / admin
├── router/index.ts         # routes + auth & admin guards
├── i18n/                   # zh-CN.ts / en-US.ts / errors.ts (Chinese server errors → English)
├── content/legal.ts        # Terms / Privacy texts
├── types/proto.ts          # WS frame & entity type contracts (shared with backend)
└── assets/main.css         # auxiliary tokens --mc-* + utility classes + motion keyframes
```

---

## 3. State Management (Pinia)

| store | Responsibility | Key points |
|---|---|---|
| `auth` | token, current user, isAdmin | token stored at `localStorage.mc_token`, read/written by `api/client.ts` |
| `ws` | connection state, online set, frames | `send()` returns a **boolean**: when the connection is unavailable the caller must mark the message failed, never silently drop it |
| `conversations` | conversation list, per-conversation message cache, unread, send queue | optimistic messages + `clientMsgId` idempotency; `resync()` fills data after reconnects |
| `friends` | friends, incoming requests, request count | decoupled from `conversations`; friend changes push separately |
| `plugins` | plugin list (enable / disable / global takedown) | works with the plugin host `usePluginHost` |
| `prefs` | language, theme, login-page color & effects | writes `localStorage` only, never affects others |

**Data flow**

```
view ──calls──▶ store ──HTTP──▶ api/*  ──▶ backend
  ▲                              │
  └──── WS frames ──▶ ws store ──┘
```

- Reads go through REST, writes through store actions; views never `fetch` directly
- Downlink WS frames dispatch in `ws.ts`: `message` / `read` / `recall` / `typing` / `presence` / `conv_change` / `user_change` / `kicked` / `error`

---

## 4. Realtime Pipeline (WebSocket)

`composables/useWebSocket.ts` owns the connection lifecycle; `stores/ws.ts` owns state and frame handling.

- **Heartbeat**: client ping every 25s; the server judges dropout on pong timeout
- **Reconnect**: exponential backoff (from 1s, cap 30s); `closedByUser` distinguishes "intentional" (logout) from "unexpected"
- **Must resync after reconnect**: the server doesn't replay frames missed while offline, so `onopen` calls `conversations.resync()` on non-first connections → re-pull the conversation list + latest messages of the open conversation (merged & deduped by `clientMsgId`)
- **Clear the online set on disconnect**: otherwise stale "online" dots linger
- **Failed sends must be visible**: `ws.send()` returns `false` when the connection isn't ready; `conversations.sendText()` immediately marks the optimistic message `failed` with a resend option (never stuck at "sending")

Uplink frames: `chat` / `read` / `recall` / `typing` (backend spec §3.3).

---

## 5. Routes & Guards

| Route | Access | Notes |
|---|---|---|
| `/login` `/register` | public | logged-in visitors bounce to the chat page |
| `/forgot` | public | two steps: email → token + new password |
| `/terms` `/privacy` | public | legal texts, entry only from the login page (must be directly reachable) |
| `/settings` | public | appearance without login; the account area requires login |
| `/chat` | auth required | main UI |
| `/admin` | admin required | admin console |

Guards live in `router/index.ts`: on refresh, `auth.fetchMe()` runs first to learn the role before allowing `/admin` (**never trusts the role stored in localStorage**, so a role change can't leave a stale frontend in the console).

---

## 6. i18n, Theme & Preferences

**i18n**: `i18n/index.ts` defaults to the browser language (`zh*` → Chinese, otherwise English), persisted at `localStorage.mc_lang`. New copy must add keys to **both** `zh-CN.ts` and `en-US.ts`. Chinese server errors are mapped to English by `i18n/errors.ts`; unmapped ones pass through unchanged.

**Theme**: `stores/prefs.ts` drives TDesign tokens via `:root[theme-mode="dark"|"light"]`, tri-state: system / light / dark. An inline script in `index.html` presets theme & background before styles apply, **avoiding first-paint white/blue flashes**.

**Color rules**: always tokens, never hardcoded colors.

- `--td-*`: native TDesign tokens
- `--mc-text-faint` / `--mc-link` / `--mc-selection` / `--mc-hairline` / `--mc-dur-*` / `--mc-ease-*` / `--mc-scene-color`: project auxiliary tokens (see `assets/main.css`)

---

## 7. Performance Conventions

| Topic | Practice | Status |
|---|---|---|
| Bundle size | **no `app.use(TDesign)`**; components imported per-SFC via `import { X as TX } from 'tdesign-vue-next'`, styles imported fully | TDesign chunk 1,371 kB → 726 kB |
| Chunking | `manualChunks` pins only the framework core to `vendor`; TDesign components chunk **with the routes that reference them** | first-load gzip 262 kB → **104 kB** |
| Long lists | message stream uses `vue-virtual-scroller` virtual scrolling | in `MessagePane` |
| Animation | only `transform` / `opacity` (grid effect uses `background-position`), never layout | unified in `main.css` keyframes |
| Accessibility | `prefers-reduced-motion: reduce` stops all decorative animation | login background, message entrances, etc. |
| devtools | `vite-plugin-vue-devtools` only when `mode === 'development'` | not in production builds |

> **Pitfall**: not all TDesign components support bare `v-model`.
> `Drawer` / `Dialog` / `Popup` only have a `visible` prop — write `v-model:visible="xxx"`;
> with `v-model="xxx"` the value lands on the DOM as an **undeclared attribute** (like `modelvalue="true"`), the component never sees it, and overlays "won't open / stay off-screen".
> `Input` / `Checkbox` / `Tabs` / `Select` / `InputNumber` do have `modelValue` and accept plain `v-model`.

---

## 8. Component & Code Conventions

- Component filenames PascalCase, events kebab-case, props declared explicitly with defaults
- Icons imported **by name** from `tdesign-icons-vue-next` (`import { ChatIcon } from 'tdesign-icons-vue-next'`), never `import * as`
- Styles use `<style scoped>`; the only global stylesheet is `assets/main.css`
- Theme-related colors always use tokens; avoid "heavy borders" in dark mode (use `--mc-hairline` for hairlines instead of `--td-component-border`)
- Login background (`components/common/AuthScene.vue`): solid base color + three pure-CSS effects (aurora / waves / grid); visitors pick color & effects from the palette in the top-right; preferences persist in `localStorage`

---

## 9. Plugin Runtime

`composables/usePluginHost.ts` is the most complex part of the frontend:

- One **sandbox iframe** per plugin (`sandbox="allow-scripts"`); plugin code cannot touch the host DOM
- Communication via `postMessage`; the host validates origin & message structure before processing
- Plugin UI uses **declarative descriptions** (`components/declarative/index.ts`): plugins only say "put a button / text / image here"; the host renders with whitelist components — plugins never get arbitrary HTML
- Uninstalling removes the iframe, event listeners and state, returning to the plain-text kernel

---

## 10. Testing Status & Suggestions

**Status**: the frontend **has no automated tests yet** (0 spec files); quality gates are `vue-tsc` + `oxlint` + `eslint` + build. Type checking catches most static errors but not runtime issues like "binding direction wrong" or "state out of sync" (historically, `PluginDrawer`'s `v-model` bug passed type checks and only surfaced on real clicks).

**Suggested priorities**:

1. `stores/conversations.ts` — optimistic messages, `clientMsgId` idempotency, frame dedupe, `markRead` reporting
2. `stores/ws.ts` — reconnect & `resync` triggering conditions
3. `components/chat/AvatarBadge.vue` / `ProfileCard.vue` — avatar fallbacks & upload branches
4. `i18n` — key-set consistency between Chinese & English (a script could diff the two files' keys)

---

## 11. Contract Boundaries with the Backend

- **Types**: `web/src/types/proto.ts` maps 1:1 to `server/internal/model/view.go` (field names, types, enums); changing one side requires changing the other
- **WS frames**: `proto.ts`'s `ClientFrame` / `ServerFrame` correspond to the backend's uplink/downlink handling in `ws/dispatch.go`
- **Errors**: the backend returns `{ error, code? }`; `api/client.ts` normalizes into `Error`; Chinese errors map to English in `i18n/errors.ts`
- **API change workflow**: update [后端需求规格.md](后端需求规格.md) first → then the backend & `proto.ts` → finally the frontend
