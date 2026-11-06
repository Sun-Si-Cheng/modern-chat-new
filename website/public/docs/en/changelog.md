# Changelog

This file records the important changes of ModernChat. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versioning follows [Semantic Versioning](https://semver.org/).

> The project is currently in the `0.x` stage; in `0.MINOR.PATCH`, an increment of MINOR indicates potentially incompatible changes.

---

## [Unreleased]

### Fixed

- **Live nickname/avatar sync didn't work**: the server already broadcast `user_change`, but `ServerFrame` didn't declare it and no handler existed, so changes needed a page refresh. `stores/ws.ts` now updates direct-chat peers, group member caches, friends/requests and the user's other devices.
- **Own messages didn't update the conversation list**: the sender never receives the "new message" `conv_change`; the echo branch now writes `lastMessageId/At/Preview/Type`, so preview, time and ordering no longer stay on the previous message.
- **WS 4 KB uplink limit broke long messages**: `chat.maxMessageLength` allows up to 5000 characters (over 4 KB in UTF-8); the old `maxMsgSize=4096` closed the connection without an `error` frame. Raised to 64 KB.
- **New group members saw "no messages"**: `initInboxes` inserted an empty inbox row without syncing the latest message, while opening the conversation showed history. Join/invite now writes the latest preview/time and advances `last_read` to the latest message (historical unread is not retroactively counted).
- **`FRONTEND_URL` with only blank entries panicked on password reset**: a value like `FRONTEND_URL=","` parsed to zero origins, rejecting every browser Origin and leaving `Forgot` to index `FrontendURLs[0]` out of range. `config.Load` now returns an error at startup (with a unit test).

### Docs

- Rewrote the design doc, backend API spec, frontend guide, ops manual, `web/README.md` and `server/README.md` against the current code; removed references to deleted `update.ps1` / `coverage.ps1` and fixed a malformed changelog entry.

### Added (earlier unreleased notes)

- Frontend technical guide (`docs/前端技术方案.md`): structure, state management, realtime pipeline, performance conventions & common pitfalls
- Ops manual (`docs/运维手册.md`): full env-var table, deployment, backup & upgrade, logs, troubleshooting FAQ
- This changelog (`CHANGELOG.md`)

---

## [0.9.0] — 2026-10-25

### Added

- **User custom avatars**: backend `users.avatar` field + `PATCH /api/me` accepts data URLs (png/jpeg/webp, ≤256 KB); frontend avatar hover shows a camera upload (center-crop to 256×256 webp compression); displayed site-wide, incl. conversation list, message bubbles, group members, admin tables
- **Profile card**: click the sidebar avatar to open (avatar / nickname / @username / settings / log out / admin console)
- **Feishu-style four-column layout**: icon rail (avatar, new, chats, contacts, plugins, admin, settings) + filter pane (all / unread / direct / group) + conversation & contact list + chat area
- **Selectable login-page background**: solid base color + aurora / waves / grid effects, 6 preset colors + custom picker, preference persisted locally
- **Unified settings page** (`/settings`): language, theme, account, version
- **Coverage tooling**: `scripts/coverage.sh` merges unit & integration coverage and detects "silently skipped" false-green runs
- Unit tests for the backend config / version / mail packages

### Fixed

- **Owner transfer didn't sync `owner_id`**: the frontend read `ownerId` for admin UI while the backend read the member `role` for permissions — contradictory conclusions → the old owner still saw admin actions (403 on click), the new owner saw none; now synced within one transaction
- **Plugin drawer wouldn't open**: TDesign `Drawer` only has a `visible` prop; a bare `v-model` landed on the DOM as an undeclared attribute, leaving the drawer off-screen → switched to `v-model:visible`
- **Trailing space in `FRONTEND_URL` left the slash unstripped**: whitelist mismatched the browser Origin, WS handshake rejected
- **Messages stuck at "sending" while offline**: `ws.send()` now returns a boolean; when the connection is unavailable the optimistic message is marked failed and can be resent
- **No resync after reconnect**: reconnecting now re-fetches the conversation list and the latest messages of the open conversation (merged & deduplicated by `clientMsgId`)
- **Stale "online" dots after disconnect**: the online set is cleared on disconnect
- **Clicking an old conversation after being kicked showed a blank page**: failed conversation open now falls back to the empty state
- **Admin user list couldn't paginate**: the total was overwritten by an unassigned variable
- **Emails not sending**: empty SMTP auth parameters, no 465 implicit TLS support, garbled Chinese subjects, missing timeout protection
- Friend list deduplicated per peer (fixed seeing two rows after adding a friend)
- Scrolling up history no longer replays the entrance animation of the last message
- Avatar / nickname changes sync in real time via WebSocket

### Performance

- TDesign switched to on-demand imports (removed `app.use(TDesign)` global registration), components chunked with their routes: **first-load gzip 262 kB → 104 kB**
- `vite-plugin-vue-devtools` only enabled in development

---

## [0.8.0] — 2026-10-24

### Added

- Unified motion system: route / conversation transitions, message entrance & micro-interactions
- Chat UI polish and an "Add friend" dialog (four entry points: conversation list, new menu, empty state, profile area)
- "Add friend / accept request" actions in the direct-chat profile panel
- Conversation list filters (all / unread / direct / group)
- Avatar / nickname changes broadcast via WebSocket to conversation, friend & group member lists

---

## [0.7.0] — 2026-10-24

### Added

- Immersive login page: background scene component `AuthScene` (custom background images), centered card, register switched to a link toggle
- Redesigned password recovery page: same scene, two-step progress indicator, keeps the filled email
- Terms agreement checkbox: checked by default on login, actively required on register, state remembered locally
- Favicon and mobile `theme-color` (follows light/dark theme)

### Changed

- De-developmentalized UI copy: register subtitle, recovery hints, email notes, admin version note reworded for end users; internal rules centralized in the README
- Terms / Privacy entry points kept only on the login / register pages

---

## [0.6.0] — 2026-10-24

### Fixed

- Unread count could go negative on recall
- Inconsistent permission checks after owner transfer
- Duplicate group join returned 500
- Login / register rate-limit bypass (tightened trusted proxy ranges)
- GORM logs printing SQL parameters in plaintext

### Added

- Unified motion & theme token system (`--mc-*`)
- Declarative rendering whitelist for plugin panels

---

[Unreleased]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.9.0...HEAD
[0.9.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.8.0...v0.9.0
[0.8.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.7.0...v0.8.0
[0.7.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/compare/v0.6.0...v0.7.0
[0.6.0]: https://gitee.com/Sun-Si-Cheng/modern-chat-new/releases/tag/v0.6.0
