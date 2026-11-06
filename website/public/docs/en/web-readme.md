# ModernChat Web

ModernChat frontend (Vue 3 + Vite + TypeScript + Pinia + Vue Router + UnoCSS + in-house `Ui*` controls).

Positioning, plugin development and upgrades are in the root [README](../README.md); the full design is in [docs/设计文档.md](../docs/设计文档.md); API fields are in [docs/后端需求规格.md](../docs/后端需求规格.md); frontend structure/interaction conventions are in [docs/前端技术方案.md](../docs/前端技术方案.md).

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
│   ├── client.ts            # fetch wrapper: /api prefix, Bearer, timeout, 401 clears token
│   └── index.ts             # auth/user/friend/conv/sticker/media/favorite/search/
│                            #   moderation/push/invite/plugin/admin/system
├── i18n/                    # index.ts / zh-CN.ts / en-US.ts / errors.ts
├── stores/
│   ├── auth.ts              # token, current user, isAdmin
│   ├── ws.ts                # WS lifecycle, heartbeat/reconnect, online set, frame dispatch
│   ├── conversations.ts     # conversations, message cache, unread, optimistic messages
│   ├── friends.ts           # friends & requests
│   ├── favorites.ts         # favorite id set + drawer
│   ├── plugins.ts           # plugin list
│   ├── prefs.ts             # language / theme / auth-scene background / app background
│   ├── layout.ts            # panel widths, docked / drawer, opened state
│   └── site.ts              # GET /api/site public config (30s poll)
├── composables/
│   ├── useWebSocket.ts      # mount/connect, unmount/disconnect, bind uplink
│   ├── usePluginHost.ts     # sandbox iframe + postMessage bridge + permission/UI validation
│   ├── notifications.ts     # desktop notifications / Web Push
│   ├── announcement.ts      # local announcement read position
│   ├── localCache.ts        # Cache Storage stats & clearing
│   ├── useOverlay.ts        # overlay focus trap / body scroll lock
│   └── useAppI18n.ts        # t + te (error translation)
├── components/
│   ├── ui/                  # 13 in-house controls
│   ├── chat/                # AppRail/GroupPane/ConvSidebar/MessagePane/MessageComposer/
│   │                        # DetailPanel/PluginDrawer/FavoriteDrawer/NotificationDrawer/
│   │                        # AnnouncementDialog/VoiceMessage/ProfileCard/AddFriendDialog/AvatarBadge
│   ├── common/              # AuthScene/QuickPrefs/AppBackgroundPicker/PaneResizer/ContextMenu
│   └── declarative/         # plugin declarative UI renderer (11 whitelisted components)
├── types/proto.ts           # WS frames / entities / UI description contracts
├── views/                   # Auth/Forgot/Settings/Legal/Join/Maintenance/ChatLayout/admin
└── assets/main.css          # --mc-* tokens + utilities + motion keyframes
```

## Implemented (frontend)

- **Four-column layout**: icon rail → filter pane → conversation/contact list → chat + detail panel; panels are resizable, double-click a divider to toggle docked / drawer.
- **Messages**: text / image / audio / video / sticker; reply, @mention, forward, merged forward (≤20 messages as one text), edit (5 min), recall (limit configured server-side), reactions (off by default), favorites, delete-for-me, report.
- **Conversations**: pin / mute / archive / remove / clear; drafts; `lastMessageType` drives placeholder previews.
- **Groups**: announcement, files, avatar, admins, per-member mute, mute-all, invite links, invite friends, kick, transfer ownership.
- **Realtime**: heartbeat + exponential-backoff reconnect; `resync()` after reconnect; direct-chat delivered/read; multi-device read sync; `user_change` live nickname/avatar sync.
- **Experience**: desktop notifications, PWA, site-wide announcement dialog, unread tab title, image compression + thumbnails, 16:9 inline video, login-page effects, app-wide background, zh/en, light/dark themes.
- **Plugins**: sandbox iframe + declarative UI + permission list + TypeScript (server-side transpile) + custom message renderers / side panels / context-menu actions.

## i18n & theme

Language follows the browser by default (`zh` → Chinese, otherwise English) and is stored in `localStorage.mc_lang`. Add keys to both `zh-CN.ts` and `en-US.ts`; Chinese server errors are mapped in `errors.ts` (unknown messages pass through).

Theme is driven by `:root[theme-mode="dark"|"light"]` with three states (system / light / dark) stored in `localStorage.mc_theme`; an inline script in `index.html` applies it before styles to avoid a flash.

Always use tokens (`--td-*` or the `--mc-*` tokens in `assets/main.css`) instead of hard-coded colors.

## Settings page

`/settings` is public; account and other sections require login.

- **Appearance**: language, theme, app-wide background (preset / custom color / local image + grid / dots / stripes)
- **Account**: nickname, email (change requires a code), sign out / sign out all devices
- **Layout**: panel widths, docked/drawer, reset to defaults
- **Plugins**: upload / URL install / enable / uninstall (returns to `/chat?panel=plugins`)
- **Notifications**: desktop notifications, Web Push (requires VAPID on the server)
- **Local cache**: Cache Storage usage and one-click clear (keeps login & preferences)
- **Admin** (only `isAdmin`): go to `/admin`
- **About**: current build version

Preferences live in `localStorage` (`mc_lang` / `mc_theme` / `mc_layout` / `mc_scene_*` / `mc_app_*`) and only affect this browser.

## Login-page background

Login / forgot-password share `components/common/AuthScene.vue`:

- Base color defaults to `#123a5c`, driven by `--mc-scene-color`
- Effects: `aurora` (default) / `waves` / `grid` / `none`
- Visitors can pick 6 presets + a custom color + 4 effects; stored locally
- Honors `prefers-reduced-motion`; animates only `transform` / `opacity`

## Design conventions

- **UnoCSS + in-house `Ui*` first**; `tdesign-vue-next` remains only for admin tables/tabs, auth forms and a few global configs. Plugin UI must use declarative whitelisted components.
- Message content uses `{{ }}` interpolation (auto-escaped); no `v-html` anywhere.
- Numbers (time, unread, counts) use `tabular-nums`.
- Motion is feedback only (hover / focus / state change), uses `--mc-dur-*` / `--mc-ease-*`, animates `transform` / `opacity`, and respects `prefers-reduced-motion`.
- **UnoCSS `/alpha` does not work on CSS variables**: use the predefined `*-soft` / `*-line` steps.
- **In-house buttons need `border-none`**: the browser default border becomes a white ring in dark mode.
- **Do not pass layout classes through multi-root components**: drawer positioning/z-index belongs to the parent container.
- **Never add `loading="lazy"` to message images**: `DynamicScroller` measures heights and the image would never enter the viewport.
- Keep `min-item-size` (currently 88) close to the real row height when changing bubble padding/meta.

## Testing status

No frontend automated tests yet; the quality gate is `vue-tsc` + `oxlint` + `eslint` + build. Suggested first tests:

1. `stores/conversations.ts` (optimistic messages / `clientMsgId` idempotency / frame dedupe / markRead)
2. `stores/ws.ts` (reconnect + `resync` conditions / `user_change` dispatch)
3. `AvatarBadge` / `ProfileCard` (avatar fallback and upload branches)
4. i18n key parity between zh/en
