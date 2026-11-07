# ModernChat Frontend Guide (web/)

> For people **changing the frontend**: structure, state, realtime pipeline, performance conventions and the easy-to-hit pitfalls.
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
| Styles | **UnoCSS** | ^66 (atomic classes; colors map to the `--td-*` / `--mc-*` variables) |
| Components | **`components/ui/Ui*` (in-house)** | primary; `tdesign-vue-next` keeps only `TForm/TFormItem/TTable/TTabs/TList/TConfigProvider` (admin & auth pages) |
| Icons | tdesign-icons-vue-next | ^0.4.11 |
| Long lists | vue-virtual-scroller | ^3.0.5 |


Design principle: **keep the kernel thin**. Accounts / friends / conversations / text + image + audio + video messages / the plugin runtime belong to the kernel; emoji reactions, Markdown, theme skins, bots and similar enhancements come from plugins. The frontend only provides 11 whitelist declarative components for plugin rendering (`text / image / button / input / card / list / divider / link / table / progress / badge`).

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
│   ├── ws.ts               # WebSocket connection, online set, send/receive outlets, user_change dispatch
│   ├── conversations.ts    # conversation list, message cache, unread, filters, sending & optimistic messages
│   ├── friends.ts          # friends & requests
│   ├── favorites.ts        # favorite id set and drawer data
│   ├── plugins.ts          # plugin list
│   ├── layout.ts           # widths / docking / drawer mode of the three zones (localStorage)
│   ├── site.ts             # public site config (30s polling, paused while hidden)
│   └── prefs.ts            # language / theme / auth-page background / site-wide page background preferences (persisted)
├── composables/
│   ├── useWebSocket.ts     # connection lifecycle: connect on mount, disconnect on unmount, bind uplink outlets
│   ├── usePluginHost.ts    # plugin host: iframe sandbox, postMessage protocol, event bus
│   ├── notifications.ts    # desktop notifications / Web Push toggle & delivery
│   ├── announcement.ts     # local read position of the site-wide announcement
│   ├── localCache.ts       # local cache stats & cleanup (Cache Storage; never touches preferences)
│   ├── useOverlay.ts       # overlay focus trap / body scroll lock
│   └── useAppI18n.ts       # t / te (error message mapping)
├── components/
│   ├── chat/               # AppRail / GroupPane / ConvSidebar / MessagePane /
│   │                       # MessageComposer / DetailPanel / PluginDrawer / FavoriteDrawer /
│   │                       # NotificationDrawer / AnnouncementDialog / VoiceMessage /
│   │                       # ProfileCard / AddFriendDialog / AvatarBadge
│   ├── common/             # AuthScene (auth-page background scene) / QuickPrefs (language · theme · background)
│   │                       # AppBackgroundPicker (site-wide page background: color + texture)
│   │                       # PaneResizer (drag a zone wider) / ContextMenu (right-click menu)
│   ├── ui/                 # in-house control layer (see §8.1): UiButton / UiInput / UiTag / UiBadge
│   │                       # UiSelect / UiSlider / UiSwitch / UiCheckbox / UiInputNumber
│   │                       # UiTooltip / UiPopup / UiDialog / UiDrawer
│   └── declarative/        # plugin declarative UI renderers (11 whitelist components)
├── views/                  # Auth / Forgot / Settings / Legal / ChatLayout / admin
├── router/index.ts         # routes + auth & site-admin guards
├── i18n/                   # zh-CN.ts / en-US.ts / errors.ts (Chinese server errors → English)
├── content/legal.ts        # Terms / Privacy texts
├── types/proto.ts          # WS frame & entity type contracts (shared with backend)
└── assets/main.css         # auxiliary tokens --mc-* + utility classes + motion keyframes
```

---

## 3. State Management (Pinia)

| store | Responsibility | Key points |
|---|---|---|
| `auth` | token, current user, isAdmin | token stored at `localStorage.mc_token`, read/written by `api/client.ts`; `/uploads/**` now requires login and is served with `Cache-Control: private, max-age=3600`; logout also calls `POST /api/auth/logout` to clear the same-origin `mc_auth` cookie (clearing localStorage alone would leave `/uploads` directly openable in the same browser for 7 days after logout) |
| `ws` | connection state, online set, frames | `send()` returns a **boolean**: when the connection is unavailable the caller must mark the message failed, never silently drop it |
| `conversations` | conversation list, per-conversation message cache, unread, send queue | optimistic messages + `clientMsgId` idempotency; `resync()` fills data after reconnects |
| `friends` | friends, incoming requests, request count | decoupled from `conversations`; friend changes push separately |
| `favorites` | favorite id set (drives the right-click "favorite / unfavorite" entry) | fetch 200 ids at once, decide locally |
| `plugins` | plugin list (enable / disable / global takedown) | works with the plugin host `usePluginHost` |
| `layout` | widths, docked / drawer, expanded state of the group pane / conversation list / detail panel | `localStorage.mc_layout`, persisted across reloads |
| `site` | `GET /api/site` public config | 30s polling, paused while the page is hidden, shallow compare to avoid full-screen re-renders |
| `prefs` | language, theme, auth-page background color & effects, site-wide page background color & texture | writes `localStorage` only, never affects others |

**Data flow**

```
view ──calls──▶ store ──HTTP──▶ api/*  ──▶ backend
  ▲                              │
  └──── WS frames ──▶ ws store ──┘
```

- Reads go through REST, writes through store actions; views never `fetch` directly
- Downlink WS frames dispatch in `ws.ts`: `message` / `message_update` / `recall` / `reaction` / `message_delivered` / `message_admin_deleted` / `read` / `typing` / `presence` / `user_change` / `conv_change` / `kicked` / `error`
- **`user_change` is consumed directly by `ws.ts`**: it updates both `conversations` (direct-chat peers + group member caches) and `friends` (friends / requests), while the user's other devices also update `auth.user`; otherwise nickname/avatar changes would require a page refresh even though the server broadcasts the frame
- **`conv_change` is incremental only**: a plain frame (just a new message) no longer refetches `/api/conversations` — the `message` frame has already updated the last message / unread count locally; only a `meta` frame (member / group name / announcement changes) or a conversation **not yet present locally** triggers a refetch. The old behaviour fired a full list request plus a whole-array reference swap for every message from someone else (re-rendering the list, group pane and chat page)

---

## 4. Realtime Pipeline (WebSocket)

`composables/useWebSocket.ts` owns the connection lifecycle; `stores/ws.ts` owns state and frame handling.

- **Heartbeat**: client ping every 25s; the server judges dropout on pong timeout
- **Reconnect**: exponential backoff (from 1s, cap 30s); `closedByUser` distinguishes "intentional" (logout) from "unexpected"
- **Must resync after reconnect**: the server doesn't replay frames missed while offline, so `onopen` calls `conversations.resync()` on non-first connections → re-pull the conversation list + latest messages of the open conversation (merged & deduped by `clientMsgId`)
- **Clear the online set on disconnect**: otherwise stale "online" dots linger
- **Failed sends must be visible**: `ws.send()` returns `false` when the connection isn't ready; `conversations.sendText()` immediately marks the optimistic message `failed` so the user can retry (never stuck at "sending")
- **Every uplink frame is revalidated on the server**: a ban / invalidated `token_version` makes any uplink frame (even `ping`) return `error` and disconnect. The client must treat a `kicked` frame and "auth-type disconnects" as **requiring a fresh login**, not just as an ordinary dropout to retry with exponential backoff — otherwise it loops forever between "reconnected" and "kicked again"
- **Handshake token**: the frontend still uses `?token=`; the server already falls back to the same-origin `mc_auth` cookie when it is missing. Migrating to a cookie handshake keeps the token out of access logs, but requires the frontend and the API to be deployed on the same domain

Uplink frames: `chat` / `read` / `delivered` / `recall` / `edit` / `reaction` / `typing` (backend spec §3.3).
- **Direct chat delivery**: after receiving a direct chat message the client automatically sends `delivered`; the sender renders "Delivered" from it
- **Emoji reactions**: `toggleReaction()` sends `reaction`; locally the `{emoji,count,mine}` triple is maintained incrementally from broadcasts
- A single uplink frame is capped at 64 KB (server `maxMsgSize`), so the frontend needs no extra truncation; the input area still pre-validates against `site.maxMessageLength`

---

## 5. Routes & Guards

| Route | Access | Notes |
|---|---|---|
| `/login` `/register` | public | logged-in visitors bounce to the chat page |
| `/forgot` | public | two steps: email → token + new password. When the emailed link carries `?token=`, the page **jumps straight to step two and fills in the token**, then strips the token from the address bar (so screenshots/shares can't leak it); the email body also contains the raw token so it can still be pasted by hand if a gateway rewrites the link |
| `/terms` `/privacy` | public | legal texts, entry only from the login page (must be directly reachable) |
| `/settings` | public | appearance without login; the account area requires login |
| `/chat` | auth required | main UI |
| `/admin` | site admin required | admin console |

Guards live in `router/index.ts`: on refresh, `auth.fetchMe()` runs first to learn the role before allowing `/admin` (**never trusts the role stored in localStorage**, so a role change can't leave a stale frontend in the console).

---

## 6. i18n, Theme & Preferences

**i18n**: `i18n/index.ts` defaults to the browser language (`zh*` → Chinese, otherwise English), persisted at `localStorage.mc_lang`. New copy must add keys to **both** `zh-CN.ts` and `en-US.ts`. Chinese server errors are mapped to English by `i18n/errors.ts`; unmapped ones pass through unchanged.

**Theme**: `stores/prefs.ts` drives TDesign tokens via `:root[theme-mode="dark"|"light"]`, tri-state: system / light / dark. An inline script in `index.html` presets theme & background before styles apply, **avoiding first-paint white/blue flashes**.

**Color rules**: always tokens, never hardcoded colors.

- `--td-*`: native TDesign tokens
- `--mc-text-faint` / `--mc-link` / `--mc-selection` / `--mc-hairline` / `--mc-dur-*` / `--mc-ease-*` / `--mc-scene-color` / `--mc-app-bg` / `--mc-app-pattern`: project auxiliary tokens (see `assets/main.css`)

---

## 7. Performance Conventions

| Topic | Practice | Status |
|---|---|---|
| Component weight | after the migration to in-house `Ui*`, TDesign only remains for admin tables/tabs and auth-page forms; **no `app.use(TDesign)`**, still-used components are imported on demand | TDesign no longer on the first screen |
| Chunking | `manualChunks` pins only the framework core to `vendor`; admin pages are split per route and stay off the first screen | first screen ≈ **161 KB / 21 requests** (uncompressed) |
| Transport compression | the build emits `.gz` / `.br`; nginx `gzip_static on` serves the pre-compressed files first | first screen **161 KB → ≈ 65 KB** |
| Long lists | message stream uses `vue-virtual-scroller`; measured: 2000 history messages render only 21 DOM nodes | in `MessagePane` |
| Images | avatars get `loading="lazy" + decoding="async"` (100 conversations used to fire 100 requests at once); message images **must not** be lazy (conflicts with DynamicScroller measurement, see §8.2) | done |
| Global listeners | `UiPopup` / `ContextMenu` attach `scroll` / `resize` **only while open**, and `scroll` is `passive` | done |
| Animation | only `transform` / `opacity` (grid effect uses `background-position`), never layout | unified in `main.css` keyframes |
| Accessibility | `prefers-reduced-motion: reduce` stops all decorative animation | auth-page background, message entrances, slider spring, etc. covered |
| devtools | `vite-plugin-vue-devtools` only when `mode === 'development'` | not in production builds |

**Already measured — no need to optimize further** (numbers included so nobody invests twice):

- Backend endpoints take 6–18 ms; they are not the bottleneck;
- After virtual scrolling, the message list scrolls at 60 fps (p95 28.5 ms);
- Opening 2000 history messages has a 115 ms long task; it is pure DOM rendering (27 nodes per message) — optimizing means cutting structure with a high style-regression risk, so it is a **one-off cost and is not done**;
- Adding `content-visibility: auto` to the conversation list makes `scrollHeight` jump from the estimate to the measured value (the scrollbar "jumps once") — **already reverted**;
- The 112 MB heap / 55 MB transfer measured in dev is an artefact of DevTools + uncompressed modules; production is 3.4 MB / 161 KB — **conclusions must be based on `vite preview`**.

> **Pitfall**: not all TDesign components support bare `v-model`.
> `Drawer` / `Dialog` / `Popup` only have a `visible` prop — write `v-model:visible="xxx"`;
> with `v-model="xxx"` the value lands on the DOM as an **undeclared attribute** (like `modelvalue="true"`), the component never sees it, and overlays "won't open / stay off-screen".
> `Input` / `Checkbox` / `Tabs` / `Select` / `InputNumber` do have `modelValue` and accept plain `v-model`.

---

## 8. Component & Code Conventions

- Component filenames PascalCase, events kebab-case, props declared explicitly with defaults
- Icons imported **by name** from `tdesign-icons-vue-next` (`import { ChatIcon } from 'tdesign-icons-vue-next'`), never `import * as`
- **Layout classes belong on the parent container, never passed through a component**: layout properties such as a drawer's `position/z-index/width` must be carried by the **parent component's own element** (e.g. the `div.mc-pane--right` wrapper around `DetailPanel` in `ChatLayout`). Reason: the parent's scoped styles must reach the child's root node, but multi-root children (`DetailPanel` = `.mc-detail` + `ContextMenu` + several `UiDialog`s) **do not receive** the parent's `class` and **fail silently** — once `is-drawer` is lost the drawer has no `z-index` and the full-screen mask covers the whole panel (the symptom is "details are visible but buttons can't be clicked")
- If a multi-root component must receive attrs, use `defineOptions({ inheritAttrs: false })` + `v-bind="$attrs"` on the root container; even then, keep layout classes off it
- Styles use `<style scoped>`; the only global stylesheet is `assets/main.css`
- Theme-related colors always use tokens; avoid "heavy borders" in dark mode (use `--mc-hairline` for hairlines instead of `--td-component-border`)
- Auth-page background (`components/common/AuthScene.vue`): solid base color + three pure-CSS effects (aurora / waves / grid); visitors pick color & effects from the palette in the top-right; preferences persist in `localStorage`
- Site-wide page background (`components/common/AppBackgroundPicker.vue`): preset color / custom color / local image + four textures (none / grid / dots / diagonal stripes); images are compressed via canvas to webp (jpeg when unsupported), stored as a data URL ≤1.5M characters in `localStorage`; the color is layered over the body's theme base color at 30% opacity, and applies uniformly to chat, settings, admin console, legal pages and the login / forgot pages (the auth page layers it on top of its own scene color & effects); **the only entry point is the "Appearance" section of the settings page** (the floating button in the chat header was removed per the information-architecture rules; it only affects the current browser). The stack is built in `stores/prefs.ts` `buildBgStack`: texture → color → image softening layer → image; **the body box is only viewport height** (`html/body/#app` all `height: 100%`), so the solid color / softening layers must `repeat` (a tiled solid gradient is seamless, otherwise pages taller than the viewport lose the tint in their lower half), while the image layer uses `background-attachment: fixed` to lock to the viewport — the variable is handed down to body as `--mc-app-bg-attachment`

### 8.1 UI Component Layer (UnoCSS + Ui*)

**Background**: early versions depended entirely on TDesign — heavy and unstylable. Now it is **in-house controls + UnoCSS atomic classes**; `tdesign-vue-next` keeps only the few where migration wouldn't pay off (admin `TTable`/`TTabs`, auth-page `TForm`).

| Layer | Location | Notes |
|---|---|---|
| Design tokens | `assets/main.css` | 7 font-size steps / 4 radius steps / 3 duration steps / 3 easing curves (incl. spring) / 3 shadow steps / overlay material |
| Atomic classes | `uno.config.ts` | colors **all map to `--td-*` / `--mc-*`**, light/dark follows automatically; radius maps `--mc-radius-*` (avoiding a clash with uno's built-in 8px) |
| Controls | `components/ui/` | 13 in-house components, see the list below |

```
UiButton  UiInput  UiTag  UiBadge  UiSelect  UiSlider  UiSwitch
UiCheckbox  UiInputNumber  UiTooltip  UiPopup  UiDialog  UiDrawer
```

**Two hard constraints when writing UnoCSS** (both learned the hard way):

1. **Never apply the `/alpha` modifier to a CSS variable.** `bg-success/12` does not work on `var(--td-success-color)` — it degrades to a solid color, and paired with same-colored text it becomes "invisible text". For tints use predefined steps: `bg-success-soft`, `border-brand-line` (built on `color-mix`).
2. **In-house buttons must explicitly set `border-none`.** `<button>` carries the browser default border (`2px outset`) and gray background, which shows up as a "white ring / white block" in dark mode; bordered variants use `border-1 border-solid` (the project has no preflight, so a bare `border` misses the edges).

**Motion**: all transitions come from tokens (`var(--mc-dur-*)` / `var(--mc-ease-*)`); a bare `ease-out` is treated as non-conforming. Slider thumb growth on drag, switch-knob spring, and button press `scale(0.97)` are already implemented inside the controls — business code doesn't need to care.

**Overlays & keyboard (`composables/useOverlay.ts`)**: when `UiDialog` / `UiDrawer` opens it locks body scroll (reference-counted, nestable), moves focus into the container, cycles Tab/Shift+Tab inside the container, and returns focus on close; `UiSelect` is `role=combobox` + `role=listbox`, supporting ↑/↓/Home/End/Enter/Esc and `aria-activedescendant`, with options as `role=option` + `aria-selected`.

### 8.2 Media Messages (image / audio / video)

- **Send pipeline**: `mediaApi.upload()` (multipart, **cannot reuse `http.request`** — that hardcodes `Content-Type: application/json`, and FormData needs the browser to supply the boundary) → get the URL → `convStore.sendMedia(convId, 'image'|'audio'|'sticker'|'video', url)` → the WS `chat` frame carries only the URL.
- **Images**: after picking, compress via canvas (max edge 1600, webp q0.82); skip compression below 300 KB, keep the original if compression makes it larger, fall back to the original on failure. Click opens a lightbox, `Esc` closes it.
- **Audio**: recorded with `MediaRecorder`, **click to start / click again to stop** (press-and-hold conflicts with scrolling on mobile), 60s limit, recordings <1s discarded. The bubble width follows duration, the progress bar uses `scaleX` (no layout), and only one clip plays at a time; webm's `duration=Infinity` is resolved by seeking to force the real value.
- **Video**: the "video" button in the input area picks a local file (`accept="video/mp4,video/webm,video/quicktime,video/x-m4v"`); the frontend first checks size against `site.maxVideoMB`, then `upload(kind=video)`; **no frontend transcoding** (browser transcoding needs WASM and blocks the main thread for a long time — far less value than image compression). The bubble uses a native `<video controls playsinline preload="metadata">` with a fixed `16:9` container (`object-fit: contain`, portrait videos get black bars) — `DynamicScroller` relies on measured height, so expanding only after metadata arrives would make the list jump.
- **Rendering**: `MessagePane` branches on `msgType` (`image` → image + lightbox; `audio` → `VoiceMessage`; `video` → native player; `sticker` → sticker). Video right-click goes through the generic message menu (no "view full image / save image" specific entries).
- **Image thumbnails**: bubbles use `imageSrc()` — by convention the URL's extension is replaced to `_t.jpg` (generated at upload time, see design doc §9.2), with `@error` falling back to the original (old messages / GIF / failed generation have no thumbnail; a single 404 is the expected fallback, keyed by `content` so optimistic message ids don't interfere); **the lightbox always uses the original**. Emoji stickers skip thumbnails (120×120, and their transparency/animation must be preserved).
- **Placeholder copy**: quote blocks, merged forwards, conversation-list previews, notifications, favorites and search results all render `[Image]/[Audio]/[Sticker]/[Video]` by type — never expose the raw `/uploads/...` URL.
- **Pitfall**: images **must not** get `loading="lazy"` — `DynamicScroller` relies on measured item height; an unloaded image has height 0 → it never enters the viewport → it never loads (infinite loop). Virtual scrolling already guarantees only items near the viewport are rendered.
- **`min-item-size` must be close to the real row height (currently 88)**: an underestimated value makes the scroll position drift upward as real heights are measured (`scrollToBottom` then needs three corrective frames). Whenever the row height changes (padding/meta/bubble), adjust it too.

### 8.3 Stickers / Forwarding / Group Announcement / Group Files

| Feature | Key points |
|---|---|
| **Sticker packs** | a sticker is a "favorite relation + URL"; the file goes through `upload(kind=sticker)` (≤2 MB). The panel sits above the input box; one click sends it (`sendMedia(convId,'sticker',url)`), right-click deletes it. The message renders without a bubble background, as a 120×120 transparent image |
| **Message editing** | the right-click "Edit" entry is visible only for **own text messages within 5 minutes that are not recalled**; submission goes through the WS `edit` frame, and on `message_update` the local body is updated and "Edited" is shown. While the input area is in edit mode, sending attachments is disabled; if a `recall` frame hits the message being edited, edit mode exits immediately |
| **Forwarding** | right-click "Forward" → conversation picker (the current conversation disabled) → `forwardMessage()` copies `content + msgType` into a new message and flags it `forwarded`. It **does not quote the original**: recalling/deleting the original doesn't affect the forwarded copy. **Recalled messages cannot be forwarded** (it is checked again at send time in case one was recalled while the picker was open, otherwise recalled content would leak out); merged forwarding is capped at 20 messages (rejected on the spot beyond that), recalled ones are skipped with a count in the toast |
| **Group announcement** | stored in `conversations.announcement*` (shared by all members). A collapsible bar at the top of the chat area shows the full text and publisher when expanded; the owner edits/revokes it in the detail panel |
| **Group files** | "Group files" in the detail panel: upload (`upload(kind=file)` ≤20 MB) → record → list (name / size / uploader); the uploader or the group owner can delete |
| **In-conversation search** | the "Search chat history" section at the top of the detail panel (L2: scoped to the current conversation): `UiInput` input → 250ms debounce → `searchApi.messages(kw, convId)` → result list (sender/time/keyword `<mark>` highlight). Clicking a result calls `convStore.openConversationAtMessage()` → writes `pendingJumpId` → `MessagePane` scrolls to it and highlights it (up to 10 pages back for old history, otherwise a toast). **Ctrl/Cmd+F** is kept: it goes through `layout.requestDetailSearch()` to expand the detail drawer (only in drawer mode and when collapsed) and focus the input; switching conversations clears the search state. The list-pane global search (people/groups/messages mixed) remains L1 |

> **Forward vs merged forward**: a single forward copies `content + msgType`; a merged forward concatenates the selected messages into **one text message** (with a "【Merged forward】" header, up to 20 messages) — it is not WeChat's "chat record card". The latter needs per-message snapshots, while `messages.content` is capped at 1000 by default and 5000 at most (migration 0023 already widened it to `TEXT`), so 20 messages easily overflow; it would need a separate table or a `msg_meta` JSON column — deferred until there is a clear requirement.

**Failed sends & disconnects**: failed messages carry "Send failed · Retry" in their meta (for every conversation type; the click goes through `convStore.resend` and keeps quotes and @s); `ws.connected` together with `ws.hasConnected` decides whether the top "Reconnecting" bar shows — it only shows after a connection has succeeded once, so the first screen doesn't flash a yellow bar while connecting.

**Recall consistency (a `recall` frame must touch four places)**: ① the message itself gets `isRecalled` + `recalledAt`; ② the quote-block snapshot (`m.replyTo.id` hit) is marked recalled, otherwise others keep seeing the pre-recall body; ③ if that message is being edited, exit edit mode immediately (otherwise the server rejects it only at submit time); ④ when the conversation's last message is recalled, clear the preview and set `lastMessageType` to `recalled` (the list renders copy in the current language), and decrement the local unread count by 1 based on "whether that message was unread" (aligning with the server's `RecallUnreadAdjust`).

### 8.4 Favorites

`stores/favorites.ts` + `components/chat/FavoriteDrawer.vue` (`UiDrawer`). Right-clicking a message favorites it; the drawer lists favorites by favorite time descending, and supports jumping back to the original conversation and unfavoriting.

> **An empty list from the backend must be `[]`, not `null`**: Go's nil slice serializes to `null`, the frontend's `items.map()` then crashes and the whole drawer goes blank. Convention: every list endpoint returns an empty array, and the frontend store adds an `Array.isArray()` guard on top.

> **`isFavorited` is decided locally**: `ids` come from a single list fetch (explicitly requesting 200, the server defaults to 100). Once a user has more than 200 favorites, older messages still show "Favorite" — one click at that point is just a harmless idempotent write. A complete fix would need a server-side `GET /api/messages/:id/favorite` single-status endpoint, which is not implemented yet.

### 8.5 Consuming Operational Config (`stores/site.ts`)

`GET /api/site` (no login required) returns all publicly shareable config; `stores/site.ts` fetches it once, caches it and **refreshes every 30 seconds**:

- **Why poll**: maintenance mode may be switched on by the site admin while users are online. The backend blocks immediately (REST 503 / WS rejection), but the UI can only keep up through this 30-second probe — otherwise users would have to refresh manually.
- **Save bandwidth and renders**: polling is skipped while the page is hidden (`document.visibilityState !== 'visible'`) and resumed right away when it comes back; `refresh()` **shallow-compares** the returned fields and skips replacing the `config` object when nothing changed — `config` is subscribed to by message bubbles (`canRecall`), the input area (length cap), upload hints and many others, so replacing it wholesale would re-render the whole screen every 30 seconds.
- **Consumption points**: browser title and login-page brand name (`site.name`), registration entry visibility (`allowRegister`), input `maxlength` and character counter (`maxMessageLength`), right-click recall time window (`recallMinutes`, the whole entry is hidden when 0), oversized-image hint (`maxImageMB`), the email and verification-code fields on the registration page (`requireEmail` / `requireEmailVerify`, with the effective values supplied by the server).
- **Registration email verification**: `AuthView` renders the email row when `requireEmail` is true, and additionally the verification-code row when `requireEmailVerify` is true (`UiInput` + a send button whose label counts down to "{n}s to resend"); the request carries `email` / `emailCode` as needed. The cooldown seconds come from the server; the frontend only renders the countdown.
- **Maintenance interception**: `App.vue` renders `MaintenanceView` when "under maintenance && not admin && not on /login". Admins pass (otherwise the site admin locks themselves out) and `/login` passes (admins must be able to log in); the backend middleware is the hard guarantee, the frontend only keeps the UI in step.
- **Fetch failures use defaults**: the defaults are **conservative** (`allowRegister=false`, no email field rendered), and `site.loaded` controls entry visibility — the registration entry stays hidden until the config has arrived, so users don't fill in a form only to be rejected with 403/503. The maintenance state keeps the last successfully fetched value so a single failure cannot be misread as "maintenance over".

### 8.6 Local Cache Management (`composables/localCache.ts`)

The "Local cache" section of the settings page manages **Cache Storage only** (PWA static assets) and never touches credentials or preferences in localStorage:

- **Stats**: prefer `navigator.storage.estimate()` to get the origin's total usage in one call and subtract localStorage to approximate the static cache (no longer reading every cache entry with `blob()`); only when estimate is unavailable does it fall back to iterating `caches.keys()`. localStorage is estimated from its keys/values using `Blob` and is display-only, never cleared.
- **Clear**: removes all Cache Storage entries while keeping login, language, theme, layout, background and plugin data; confirmation goes through `DialogPlugin.confirm`. Static caches are regenerable, so offline resources are re-downloaded on the next visit.

### 8.7 Message Stream Reading Aids

- **Back to bottom / new message hint**: `MessagePane` maintains `stickToBottom` + `unseenCount`. When scrolling up through history a "Back to bottom" button appears at the bottom-right; messages arriving meanwhile turn it into "N new messages" (capped at 99+), and clicking it scrolls to the bottom and calls `markRead`.
- **New messages must not be detected by array length**: loading history upward **prepends** items, so the length grows while the last message id stays unchanged; instead the code watches the "id of the last persisted message" and ignores decrements caused by conversation switches / clears. Messages sent by the current user (`senderId === current user`) jump straight to the bottom, so a sent message never leaves the view stranded mid-history.
- **Tab title**: `App.vue` aggregates unread counts of non-muted conversations; under `/chat` with unread it sets `(N) site name`, other routes restore the site name; `stores/site.ts` no longer writes `document.title` directly (otherwise the 30-second poll would overwrite the unread prefix).

---

## 9. Plugin Runtime

`composables/usePluginHost.ts` is the most complex part of the frontend:

- One **sandbox iframe** per plugin (`sandbox="allow-scripts"`, **without `allow-same-origin`**); plugin code cannot touch the host DOM directly; the iframe is an opaque origin with no access to the host's localStorage / cookie / JWT
- **Injection must use `srcdoc`**: without same-origin the parent page has no right to manipulate the iframe document, so `document.open/write` throws `SecurityError` and the old blob URL + `document.write` approach no longer works; `bootHtml` escapes `</script` and `<script` in the plugin code before writing it in
- **CSP narrows the network egress**: `default-src 'none'`, `script-src/style-src 'unsafe-inline'`, `img-src data:`, and `connect-src` allows only the manifest's `allowedHosts` (`'none'` when undeclared), with `base-uri/form-action/frame-src/object-src/worker-src` disabled
- **Permission filtering**: `API_PERMISSION` governs each SDK method; `EVENT_PERMISSION` makes `message:new` / `message:recall` / `presence` deliver only to plugins holding `message:read`; without `message:read` the `ui:actions` callback receives only message metadata, never the body
- **Bare `fetch` is dead**: plugins must switch to `mc.request` (the repository's SDK object is named `api`, and its method is `api.request`), sent by the host page under both `allowedHosts` and the target site's CORS. The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method. The host-side `request` accepts http(s) only, and `hostAllowed` matches exactly or by `*.` suffix
- **Concurrent startup**: the code of all enabled plugins is fetched **concurrently** (`Promise.all`) and only then mounted in order — serial `await` would make the Nth plugin wait for all the earlier ones (5 plugins × 200ms ≈ 1 second); a single plugin failing only logs and toasts, without blocking the rest
- Communication goes through `postMessage`; the host validates origin & message structure before processing
- Plugin UI uses **declarative descriptions** (`components/declarative/index.ts`): plugins only say "put a button / text / image here"; the host renders with whitelist components, so plugins never get arbitrary HTML; there are 11 whitelist components (including the three data-display ones `table` / `progress` / `badge`)
- **Extension points**: menu items registered through `registerMessageAction` are appended to the message right-click menu (key prefix `plug:<pluginId>:<actionId>`, and `MessagePane` only dispatches events to the registrant); `conv:read` provides a read-only context (`me/conv/messages.recent`), `ui:host` provides `nav.*` and `toast`. **`messages.recent` only exposes persisted, visible messages** (optimistic/failed ones are filtered out and recalled messages get an empty `content`), so plugins can never obtain the recalled body
- **Language**: plugins can be written in TypeScript; the server transpiles them to plain JS, so no compilation happens on the sandbox side; type declarations live in `plugins/modernchat-sdk.d.ts`
- Uninstalling a plugin tears down the iframe, removes event listeners and cleans up state (panel and menu actions included), returning to the plain-text kernel

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
- **Errors**: the backend returns `{ error, code? }`; `api/client.ts` normalizes it into `ApiError`; Chinese errors map to English in `i18n/errors.ts`. `fetch` network-layer failures are wrapped as "network connection failed", and non-401 failures **never** clear the login state (only 401 clears the token and redirects to login, carrying `?redirect=` to return to the original page)
- **API change workflow**: update [后端需求规格.md](后端需求规格.md) first → then the backend & `proto.ts` → finally the frontend

## 12. Information Architecture (Which Layer Does a New Entry Belong To)

Don't pile buttons onto the UI. Every new entry must be classified by the three layers below — **don't decide by feel**:

| Layer | Location | What belongs here | Test |
|---|---|---|---|
| **L1 Main UI** | icon rail + list pane | actions used repeatedly while chatting: switching views, search, starting a new conversation | used many times a day |
| **L2 Context panel** | `DetailPanel` (right) | actions aimed at **the current conversation or the current member**: in-conversation search, renaming a group, transfer ownership, removing members, leaving a group | tightly bound to the current conversation |
| **L3 Settings page** | `SettingsView` | global, low-frequency, one-time configuration: account profile, appearance theme, language, plugin management, admin console, about, logout | global / low-frequency / only for the user or an admin |

Three hard constraints:

1. **High-frequency actions must not hide inside secondary menus.** "Add friend / New group chat" stay on the `+` at the top of the list pane (`ConvSidebar`) — don't bury them in some "More": users will think the feature is gone.
2. **Low-frequency configuration must not take icon-rail space.** Plugin management and the admin console have moved from the icon rail into the settings page (the admin section is visible only when `auth.isAdmin`).
3. **A moved entry must keep the feature reachable.** For example, the plugin panel belongs to the chat page, so the settings page navigates back with `router.push('/chat?panel=plugins')`, and `ChatLayout`'s `onMounted` reads `route.query.panel`, auto-expands the drawer and clears the query — avoiding the "changed it and now can't find the feature" situation.

The icon rail currently has only 4 items: avatar (profile card), chat, contacts, settings. **Before adding another icon, ask: isn't this an L2 or L3 thing?**

## 13. Adjustable Layout (Width + Docked/Drawer)

The three zones of the chat page (group pane / conversation list / detail panel) all support **two modes** and **arbitrary width**:

| Mode | Behaviour |
|---|---|
| **Docked** | participates in flex layout and occupies a fixed width; draggable splitters between zones |
| **Drawer** | leaves the layout and floats above the main area (`position: absolute`), without squeezing the message area; collapses on mask click or `Esc` |

**Interaction**:

- Drag a splitter → resize that zone (`PaneResizer`, with keyboard ←/→ and Shift to accelerate)
- Double-click a splitter → toggle between "docked / drawer"; **switching to drawer expands it immediately** (otherwise the zone would vanish and users would think the feature is gone)
- Settings page "Layout" section → sliders for width, a segmented control for mode, and one-click restore to defaults
- In drawer mode, the "chat / contacts" buttons in the icon rail expand/collapse that drawer; the detail drawer's entry is the button at the top-right of the main area

**State**: `stores/layout.ts` persists `{ panes: {width, drawer}, opened }` to `localStorage.mc_layout`, so width, mode and expanded state **survive reloads**. Drawers are mutually exclusive (several open at once would overlap each other).

> Implementation note: each zone component writes its width as `width: var(--mc-w-conv, 300px)` (the default matches the historical hardcoded value so the first screen doesn't jump), and the actual width is handed down as an inline CSS variable on `.mc-layout`.
