# ModernChat Web

ModernChat frontend (Vue 3 + Vite + TypeScript + Pinia + Vue Router + UnoCSS + in-house `Ui*` controls).

Positioning, plugin development and upgrade deployment are in the root [README](../README.md); the full design is in [docs/设计文档.md](../docs/设计文档.md); API fields are in [docs/后端需求规格.md](../docs/后端需求规格.md); frontend structure/interaction conventions are in [docs/前端技术方案.md](../docs/前端技术方案.md).

> The frontend needs the backend on `:8080` to log in and exchange messages (Vite proxies `/api` / `/uploads` / `/ws`).

## Commands

```sh
npm install        # install dependencies
npm run dev        # dev server :5173 (/api and /ws proxied to :8080)
npm run build      # type-check + production build (dist/) + .gz/.br
npm run type-check # vue-tsc only
npm run lint       # oxlint + eslint
npm run format     # prettier for src/
```

> ⚠️ Both `lint` sub-scripts (`lint:oxlint` / `lint:eslint`) pass `--fix` and **rewrite source files**.
> To check without modifying, use `npx oxlint .` / `npx eslint .` (without `--fix`).

## Structure

```text
src/
├── api/
│   ├── client.ts            # fetch wrapper: /api prefix, Bearer, timeout, 401 clears token and redirects to login
│   └── index.ts             # domain APIs (auth/user/friend/conv/sticker/media/favorite/search/
│                            #   moderation/push/invite/plugin/admin/system)
├── i18n/                    # index.ts / zh-CN.ts / en-US.ts / errors.ts (Chinese errors → English)
├── stores/
│   ├── auth.ts              # token, current user, isAdmin
│   ├── ws.ts                # WS connection, heartbeat/reconnect, online set, frame dispatch (incl. user_change)
│   ├── conversations.ts     # conversation list, message cache, unread, optimistic messages, reply/edit/reaction
│   ├── friends.ts           # friends & requests
│   ├── favorites.ts         # favorite id set + drawer
│   ├── plugins.ts           # plugin list
│   ├── prefs.ts             # language / theme / auth-scene background / app-wide page background
│   ├── layout.ts            # widths of the three panes / docked / drawer
│   └── site.ts              # GET /api/site public config (30s poll)
├── composables/
│   ├── useWebSocket.ts      # connect on mount, disconnect on unmount, bind uplink
│   ├── usePluginHost.ts     # plugin sandbox host: iframe + postMessage bridge + permission whitelist + UI description validation
│   ├── notifications.ts     # desktop notifications / Web Push
│   ├── announcement.ts      # local read position for site-wide announcements
│   ├── localCache.ts        # Cache Storage stats & clearing
│   ├── useOverlay.ts        # overlay focus trap / body scroll lock
│   └── useAppI18n.ts        # t (copy) + te (error translation)
├── components/
│   ├── ui/                  # 13 in-house controls: UiButton/UiInput/UiTag/UiBadge/UiSelect/UiSlider/
│   │                        #   UiSwitch/UiCheckbox/UiInputNumber/UiTooltip/UiPopup/UiDialog/UiDrawer
│   ├── chat/
│   │   ├── AppRail.vue            # leftmost icon rail: avatar / chat / contacts / settings
│   │   ├── GroupPane.vue          # group filter: all / unread / direct / group / archived
│   │   ├── ConvSidebar.vue        # conversation & contact list, search, context menu (pin/mute/archive/clear/remove)
│   │   ├── MessagePane.vue        # virtualized message stream, reply/forward/@/edit/recall/reactions/report/lightbox/back-to-bottom
│   │   ├── MessageComposer.vue    # auto-grow, Enter to send, IME guard, typing, @ picker, image/voice/video/sticker
│   │   ├── DetailPanel.vue        # group management / group files / group announcement / in-conversation search / plugin side panel
│   │   ├── PluginDrawer.vue       # upload/URL install, permission confirmation, enable/disable, uninstall, upgrade reload
│   │   ├── FavoriteDrawer.vue     # favorites
│   │   ├── NotificationDrawer.vue # notifications (friend requests / announcements)
│   │   ├── AnnouncementDialog.vue # site-wide announcement dialog
│   │   ├── VoiceMessage.vue       # voice bubble (width by duration, progress bar, only one plays at a time)
│   │   ├── ProfileCard.vue        # profile card: avatar upload, settings, sign out
│   │   ├── AddFriendDialog.vue    # add friend (username/nickname search, accept in place)
│   │   └── AvatarBadge.vue        # avatar (custom image / default image / initial fallback) + online dot
│   └── common/                    # AuthScene / QuickPrefs / AppBackgroundPicker / PaneResizer / ContextMenu
│   └── declarative/               # declarative UI whitelist renderer (11 components) + plugin panel
├── types/proto.ts           # WS frames / business entities / UI description contracts (shared shape with the backend)
├── views/                   # AuthView/ForgotView/SettingsView/LegalView/JoinView/MaintenanceView/
│                            #   ChatLayout/admin/AdminView
├── router/index.ts          # routes + login / site-admin guards
└── assets/main.css          # helper tokens --mc-* + utilities + motion keyframes
```

## Implemented (frontend)

- **Four-column layout**: icon rail → filter pane → conversation/contact list → chat + detail panel; the three panes are resizable, double-click a divider to toggle docked / drawer.
- **Messages**: text / image / audio / video / sticker; reply, @mention, forward, merged forward (≤20 messages as one text), edit (5 min), recall (limit configured server-side), emoji reactions (off by default), favorites, delete-for-me, report.
- **Conversations**: pin / mute / archive / remove / clear; drafts; `lastMessageType` drives placeholder previews.
- **Groups**: announcement, files, avatar, admins, per-member mute, mute-all, invite links, invite friends, kick, transfer ownership.
- **Realtime**: heartbeat + exponential-backoff reconnect; `resync()` after reconnect; direct-chat delivered/read; multi-device read sync; `user_change` live nickname/avatar sync.
- **Experience**: desktop notifications, PWA, site-wide announcement dialog, unread tab title, image compression + thumbnails, 16:9 inline video, login-page effects, app-wide page background, zh/en, light/dark themes.
- **Plugins**: sandbox iframe + declarative UI + permission list + TypeScript (server-side transpile) + custom message renderers / side panels / context-menu actions. The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method.

## i18n & theme

**Language**: `src/i18n/`, following the browser by default (`zh` prefix → Chinese, otherwise English); the choice is written to `localStorage.mc_lang`.
When adding copy, add the key to both `zh-CN.ts` and `en-US.ts`; Chinese errors returned by the server are mapped to English by message in `errors.ts` (unknown messages pass through).

**Theme**: `src/stores/prefs.ts` drives TDesign tokens through `:root[theme-mode="dark"|"light"]`, with three states (follow system / light / dark) persisted in `localStorage.mc_theme`.
An inline script in `index.html` applies the theme before styles take effect, avoiding a white flash on dark first paint.

Custom colors always go through tokens: `--td-*` or the `--mc-*` tokens in `assets/main.css` (font size / radius / duration / easing / shadow / material); never hard-code color values.

## Settings page

`/settings` allows only appearance changes when signed out; account and other sections require login.

- **Appearance**: language, theme, app-wide page background (preset / custom color / local image + grid / dots / stripes)
- **Account**: nickname, email (changing it requires a verification code), sign out / sign out all devices
- **Layout**: widths of the three panes, docked / drawer, reset to defaults
- **Plugins**: upload / URL install / enable / disable / uninstall (returns to the chat page with `?panel=plugins` to open the panel)
- **Notifications**: desktop notifications, Web Push (requires VAPID on the server)
- **Local cache**: Cache Storage usage and one-click clear (keeps login & preferences)
- **Site admin** (only `isAdmin`): go to `/admin`
- **About**: current build version

Preferences live in `localStorage` (`mc_lang` / `mc_theme` / `mc_layout` / `mc_scene_*` / `mc_app_*`) and only affect this browser.

## Login-page background

Login / forgot-password share `components/common/AuthScene.vue` (mounted by `AuthView` / `ForgotView`; the legal page only has `QuickPrefs`, with no background layer):

- **Base color**: defaults to `#123a5c`, driven by `--mc-scene-color`
- **Effects**: `aurora` glow (default) / `waves` / `grid` / `none` (solid color)
- **Visitor-selectable**: 6 presets + a custom color picker + 4 effects + reset to defaults; preferences stored in `localStorage`
- **Accessibility / performance**: animation stops under `prefers-reduced-motion`; only `transform` / `opacity` are animated (the grid uses `background-position`)

## Design conventions

- **UnoCSS + in-house `Ui*` first**; `tdesign-vue-next` remains only for admin tables/tabs, auth forms and a few global configs. Plugin UI must use declarative whitelisted components.
- Message content uses `{{ }}` text interpolation (auto-escaped by Vue); no `v-html` anywhere in the project.
- Numbers (time, unread, counts) use `tabular-nums`.
- Motion is feedback only (hover / focus / state change), uses `--mc-dur-*` / `--mc-ease-*`, animates `transform` / `opacity`, and respects `prefers-reduced-motion`.
- **UnoCSS `/alpha` does not work on CSS variables**: use the predefined `*-soft` / `*-line` steps.
- **In-house buttons need `border-none`**: the browser default border becomes a white ring in dark mode.
- **Do not pass layout classes through multi-root components**: drawer positioning/z-index belongs to the parent container (a multi-root component does not receive the parent's class).
- **Never add `loading="lazy"` to message images**: `DynamicScroller` measures heights and the image would never load.
- `MessagePane`'s `min-item-size` (currently 88) is close to the real row height; changing bubble padding / meta requires adjusting it in sync.

## Testing status

No frontend automated tests yet; the quality gate is `vue-tsc` + `oxlint` + `eslint` + build. Suggested first tests:

1. `stores/conversations.ts` (optimistic messages / `clientMsgId` idempotency / frame dedupe / markRead)
2. `stores/ws.ts` (reconnect + `resync` trigger conditions / `user_change` dispatch)
3. `AvatarBadge` / `ProfileCard` (avatar fallback and upload branches)
4. i18n key parity between zh/en (can be compared with a script)
