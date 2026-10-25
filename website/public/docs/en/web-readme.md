# ModernChat Web

ModernChat frontend (Vue 3 + Vite + TypeScript + Pinia + tdesign-vue-next).

> This file is a structure & command overview. **The full frontend architecture lives in [docs/前端技术方案.md](../docs/前端技术方案.md)**.

For project positioning, plugin development and upgrades & deployment, see the root [README](../README.md); full design in [docs/设计文档.md](../docs/设计文档.md); API fields in [docs/后端需求规格.md](../docs/后端需求规格.md).

> The frontend needs the backend on `:8080` for login & messaging (Vite proxies `/api` and `/ws`).

## Commands

```sh
npm install        # install deps
npm run dev        # dev server :5173 (/api and /ws proxied to :8080)
npm run build      # type check + production build (output dist/)
npm run type-check # vue-tsc type check only
npm run lint       # oxlint + eslint
```

> ⚠️ Both `lint` sub-scripts (`lint:oxlint` / `lint:eslint`) carry `--fix` and **rewrite source files**.
> For a check-only pass, use `npx oxlint .` / `npx eslint .` (no `--fix`).
> To format after changes, use `npm run format` (prettier), which also writes files.

## Structure overview

```
src/
├── api/
│   ├── client.ts            # fetch wrapper: /api prefix, Bearer, unified 401 → clear token & jump to login
│   └── index.ts             # domain APIs (auth/user/friend/conv/plugin/admin/system)
├── i18n/
│   ├── index.ts             # createI18n (defaults to browser language, localStorage persistence)
│   ├── zh-CN.ts / en-US.ts  # full UI copy
│   └── errors.ts            # Chinese server errors → current language mapping
├── stores/                  # auth / ws / conversations / friends / plugins / prefs (language+theme)
├── composables/
│   ├── useWebSocket.ts      # connect on mount, disconnect on unmount, uplink outlet binding
│   ├── useAppI18n.ts        # t (copy) + te (error translation)
│   └── usePluginHost.ts     # plugin sandbox host: document.write bootstrap page, postMessage bridge, permission whitelist, UI description validation
├── components/
│   ├── common/
│   │   └── QuickPrefs.vue       # login-page quick toggles: language + light/dark + background (color/effect)
│   ├── chat/
│   │   ├── AppRail.vue          # leftmost icon rail: avatar (profile card) + new menu + chats/contacts/plugins/admin + settings
│   │   ├── GroupPane.vue        # filters: all / unread / direct / group (counts by conversations)
│   │   ├── ConvSidebar.vue      # conversation & contact list: search (people/groups), presence, friend requests
│   │   ├── AddFriendDialog.vue  # add friend: username/nickname search, apply, already-friend/pending states, inline accept
│   │   ├── MessagePane.vue      # virtual-scrolled message stream, 5-minute grouping, read/recall, back-to-bottom, plugin message rendering
│   │   ├── MessageComposer.vue  # 1–6 auto-growing rows, Enter to send / Shift+Enter newline, IME protection, typing throttle
│   │   ├── DetailPanel.vue      # group name/members & admin, direct-chat profile + plugin sidebar
│   │   ├── PluginDrawer.vue     # upload/URL install, permission confirmation, enable/disable, uninstall, upgrade reload
│   │   ├── ProfileCard.vue      # profile card: avatar upload (hover camera), settings/logout/admin
│   │   └── AvatarBadge.vue      # avatar (custom image / default cartoon public/default-avatar.svg / initial fallback) + online dot
│   └── declarative/
│       ├── index.ts             # render functions for the 8 whitelist components (primitive props only; text/image/card/list/link are native tags, button/input/divider use TDesign)
│       └── PluginPanel.vue      # plugin panel + action feedback
├── types/proto.ts           # WS frames / entities / UI description contracts (shared with backend)
├── views/
│   ├── AuthView.vue         # login/register (immersive background + centered card)
│   ├── ForgotView.vue       # password recovery (two steps + progress)
│   ├── SettingsView.vue     # settings: appearance (language/theme) / account / about (background only on the login page)
│   ├── LegalView.vue        # Terms / Privacy
│   ├── ChatLayout.vue       # Feishu-style four-column layout + responsive (tablet folds detail, phone single panel)
│   └── admin/AdminView.vue  # admin console: user management / plugin aggregated takedown / build version
├── router/index.ts          # routes + auth/admin guards (refresh runs fetchMe before checking roles)
└── assets/main.css          # auxiliary tokens (--mc-*) + global utilities + motion keyframes
```

Auxiliary tokens exposed by `assets/main.css` (all other colors use TDesign's `--td-*`):

| Token | Purpose | Light | Dark |
| --- | --- | --- | --- |
| `--mc-text-faint` | faintest text (placeholders/hints, ≥4.5:1 in both themes) | `#6b6b6b` | `#a8a8a8` |
| `--mc-link` | links | `#0052d9` | `#7cb8ff` |
| `--mc-selection` | text selection background | brand blue 16% | bright blue 28% |
| `--mc-hairline` | card/group hairlines (softer than `--td-component-border`) | black 8% | white 8% |
| `--mc-dur-fast` / `--mc-dur-base` / `--mc-dur-slow` | motion durations | 120ms / 200ms / 320ms | same |
| `--mc-ease-out` / `--mc-ease-spring` | easing curves | `cubic-bezier` | same |
| `--mc-scene-color` | login-page background base (written at runtime by `stores/prefs.ts`) | default `#123a5c` | same (user-selectable) |

Motion keyframes (`mc-fade-in` / `mc-fade-up` / `mc-pop-in` / `mc-bubble-in` / `mc-dot-bounce` / `mc-online-pulse` etc.)
and route/conversation transition classes (`mc-route-*` / `mc-swap-*` / `mc-fade-scale-*`) are all maintained in `main.css`.

## Chat UI

Four columns: icon rail (56px) → filter pane (148px) → conversation/contact list → chat area, with the current conversation's detail panel on the right. Tablet (≤1024px) hides the filter pane & detail panel; phone (≤768px) shows list and chat area mutually exclusively.

- **Message stream**: consecutive messages by one sender within 5 minutes merge into a group; avatars & nicknames appear only at the group head; time & read receipts only at the group tail; direct-chat read marks only the latest self message; group messages use real avatars & nicknames from the members API; recall & resend fold into a meta row below the message, revealed on hover or keyboard focus; a "back to latest" button appears when scrolled away from the bottom
- **Composer**: 1–6 auto-growing rows; Enter sends, Shift+Enter newlines; pressing Enter during IME composition never mis-sends; the whole panel is focusable; a hint appears near the length cap
- **Contacts**: the top of the contact list, the new menu and the empty state all open the "Add friend" dialog with username/nickname search; existing friends show status, incoming requests can be accepted inline, successful sends show "requested"; when chatting with a non-friend, the right profile area also offers "Add friend / accept request"; nickname/avatar changes broadcast via WS and sync instantly to conversation, friend & group member lists
- **Empty state**: with no conversations, show the product name, tagline and "Add friend / Plugin manager" entries; the duplicate "Add friend" entry hides while in the contacts view

## i18n & Theme

**Language**: `src/i18n/`, defaults to the browser language (`zh*` = Chinese, otherwise English), persisted at `localStorage.mc_lang` after switching.
New copy adds keys to both `zh-CN.ts` / `en-US.ts`; Chinese server errors map to English via `errors.ts` (unmapped ones pass through).

**Theme**: `src/stores/prefs.ts` drives TDesign tokens via `:root[theme-mode="dark"|"light"]`, tri-state "system / light / dark", persisted at `localStorage.mc_theme`.
An inline script in `index.html` presets the theme before styles apply, avoiding a dark-mode white flash.
Custom colors always go through tokens: `--td-*` (TDesign) or the auxiliary tokens in `assets/main.css` — `--mc-text-faint` / `--mc-link` / `--mc-selection` / `--mc-hairline` / `--mc-dur-*` / `--mc-ease-*` (full list above), never hardcoded values.

## Settings Page

`/settings` is the post-login settings entry (gear at the bottom of the chat icon rail, header of the admin console):

- **Appearance**: language (CN/EN), theme (system/light/dark)
- **Account** (login required): nickname, email, log out
- **About**: current build version

> Login / register / recovery / legal pages have three quick buttons in the top-right: language, light/dark, background (color + effect).
> Language & light/dark share the same preferences as the settings page (`localStorage.mc_lang` / `mc_theme`);
> **the background belongs only to the login page** (`/settings` offers no background item) and only affects pages mounting `AuthScene.vue` (login & recovery);
> the chat UI and legal pages (which mount only `QuickPrefs`, no background layer) are unaffected.

Preferences persist in `localStorage` (`mc_lang` / `mc_theme` / `mc_scene_color` / `mc_scene_effect`), scoped to the current browser.

## Login-page Background (color & effects)

Login / recovery pages share `components/common/AuthScene.vue` (`AuthView` / `ForgotView` mount it; the legal pages only mount `QuickPrefs`, no background layer):

- **Base color**: default `#123a5c` (deep blue), driven by the CSS variable `--mc-scene-color`
- **Effects** (three, pure CSS, no images, no external deps):
  - `aurora`: three soft glow blobs (blurred solid circles) drifting slowly, default
  - `waves`: concentric rings expanding outward
  - `grid`: a faint grid flowing slowly + a breathing central glow
  - `none`: solid color
- **Visitor-selectable**: top-right palette → 6 preset colors + custom picker + 4 effects + reset; preferences persist in `localStorage` (`mc_scene_color` / `mc_scene_effect`), scoped to that browser
- **Changing defaults**: `DEFAULT_SCENE_COLOR` and `detectSceneEffect()` in `stores/prefs.ts`
- **Accessibility**: `prefers-reduced-motion` stops all animations
- **Performance**: animations only use `transform` / `opacity` (`grid` uses `background-position`), never layout

### Replacing with an image

Drop an image at `web/public/login-bg.jpg` to automatically override the base color (the build copies it to the site root), with a theme-aware darkening layer on top for card readability. Prefer ≥1920×1080 landscape with the subject off-center (the card covers roughly the central 420×360 area). For `.png` / `.webp`, change one `url()` line on `.auth-scene__img` in `AuthScene.vue`.

## Design Conventions

- A single component library (TDesign), never a second design system; plugin UI always goes through declarative whitelist components
- Message content uses `{{ }}` text interpolation (Vue auto-escapes); the project never uses `v-html`
- Numbers (times, unread, counts) use `tabular-nums`
- Motion is feedback-only (hover / focus / transitions) and respects `prefers-reduced-motion`
- Motion always uses the `--mc-dur-*` / `--mc-ease-*` tokens and animates only `transform` / `opacity`; route & conversation switches, message entrances, sidebar view switches and layered empty-state entrances all have transitions and degrade under reduced-motion preferences
