# Security & Migration Guide

> For **whoever takes over this project**: this round of frontend/backend defect fixes changed a lot of "default assumptions", so reading only CHANGELOG makes it easy to know the what but not the why.
> This document explains the motivation, boundaries and pitfalls of every change, and marks the code locations so you can cross-check the implementation.
>
> Contract fields and endpoint lists still defer to [后端需求规格.md](后端需求规格.md); architecture trade-offs are in [设计文档.md](设计文档.md);
> day-to-day deployment steps are in [运维手册.md](运维手册.md); plugin authors should also read [插件开发.md](插件开发.md).

---

## 0. Quick Reference

| Topic | One-line conclusion | Code location |
|---|---|---|
| `/uploads` authorization | Login required; either `Authorization` or the same-origin `mc_auth` cookie; directory listing returns 404; cache is `private, max-age=3600` | `server/internal/router/router.go` `serveUpload`, `onlyFilesFS`; `server/internal/middleware/jwt.go` `UploadAuth` |
| Logout | `POST /api/auth/logout` only clears the cookie, it does not revoke the JWT; only "log out all devices" bumps `token_version` and kicks the WS | `handler/auth.go` `Logout`; `handler/user.go` `LogoutOthers`; `ws/hub.go` `KickUser` |
| Realtime channel | Handshake keeps `?token=`, falling back to the cookie when missing; **every frame** re-checks ban state and tv, and on failure sends an error frame and disconnects | `ws/serve.go` `Serve`; `ws/dispatch.go` `Dispatch` / `checkConnState` |
| Plugin isolation | `sandbox="allow-scripts"` (no same-origin), started via `srcdoc`, CSP closes the egress; **bare `fetch` no longer works, `mc.request` must be used** | `web/src/composables/usePluginHost.ts` `mountRuntime` / `buildSandboxCsp` / `hostAllowed` |
| Upload pipeline | Sniff the first 512 bytes first and **write them back**; limits follow the admin-console settings; thumbnails capped at 40 megapixels; GC decides by the `/uploads/` prefix | `service/storage.go` `SaveStream` / `extFor`; `service/thumbnail.go`; `handler/ops.go` `referencedUploads` |
| Migrations | 0023 widens to TEXT, 0024 unique key for direct chats, 0025 unique indexes for friends / email codes; back up before upgrading | `server/migrations/0023..0025` |
| Deployment | Docker version comes from `VERSION`/`COMMIT`/`BUILD_TIME`, image sources from `GO_IMAGE`/`RUNTIME_IMAGE`/`GOPROXY`/`APK_MIRROR` | `server/Dockerfile`; `docker-compose.yml` |

---

## 1. Attachment Storage & Access Model

### 1.1 Why this changed

Before the change, `/uploads` was an anonymous public `StaticFS`: anyone who got or guessed a path could download it, and the web server even listed directories; the response also carried `public, max-age=31536000, immutable`, so private images stayed in proxy/browser caches for a long time.
URLs in historical messages carry no signature, so switching straight to "signed URLs" would make every old image return 404; that is why the chosen approach is **login state + same-origin cookie**.

### 1.2 The three access paths

| Scenario | Credential | Notes |
|---|---|---|
| Frontend API requests | `Authorization: Bearer <JWT>` | `middleware.Auth` **only** accepts the header, never the cookie, to avoid opening a CSRF surface |
| Browser loading `<img>` / `<video>` / downloads | Same-origin `mc_auth` cookie | The browser cannot attach an Authorization header to subresources, so it can only rely on the cookie |
| Downloaders / legacy clients | `Authorization` header | `UploadAuth` checks the header first, then the cookie; both are supported |

The decision order of `UploadAuth` (`middleware/jwt.go`) is: `Authorization: Bearer` → `mc_auth` cookie → 401.
After parsing the JWT it also validates `token_version` and ban state, so **an old cookie returns 401 immediately after a password reset**.

### 1.3 Cookie issuance, attributes and invalidation

The cookie is named `mc_auth` and holds **the same string** as the JWT in localStorage (`middleware.AuthCookieName`).

Issued when:

1. Login / registration succeeds (`handler/auth.go`);
2. The WebSocket handshake succeeds (`ws/serve.go` `Serve`, `SetAuthCookie` before the upgrade);
3. Any authenticated REST request carrying `Authorization` (`setAuthCookie` in `middleware.Auth`, skipped when the cookie is identical);
4. `/uploads` is accessed with `Authorization` (`UploadAuth` re-issues it).

Attributes (`middleware.SetAuthCookie`):

| Attribute | Value | Reason |
|---|---|---|
| `HttpOnly` | true | Plugins/scripts cannot read it, shrinking the XSS theft surface |
| `SameSite` | `Lax` | Allows same-site subresources to carry it, while cross-site POST does not |
| `Path` | `/` | Works for both `/uploads` and `/ws` |
| `MaxAge` | 7 days (matches `middleware.TokenTTL`) | Aligned with the JWT lifetime |
| `Secure` | true over TLS or with `X-Forwarded-Proto: https` | An HTTP deployment is not locked out by a forced Secure flag |

Invalidation rules:

- **JWT `tv` mismatching `token_version` in the database → 401** (REST, uploads and WS all check it);
- The browser cookie **does not disappear by itself**, it merely fails validation; real removal relies on `POST /api/auth/logout` (`ClearAuthCookie`, `MaxAge=-1`);
- If you clear localStorage without clearing the cookie, the same browser can still open `/uploads` directly for 7 days after logout — that is the direct reason the logout endpoint was added.

### 1.4 Directory listing and caching

- `onlyFilesFS.Open` returns an error for directories, so `http.FileServer`'s directory listing page becomes a 404 (`router.go`).
- The response headers are fixed to `Cache-Control: private, max-age=3600` + `X-Content-Type-Options: nosniff`.
  File names are random and immutable, so a short private cache can be reused without entering shared proxy caches.
- Maintenance mode behaves the same as the API: non-admins get `503 {"error":"站点维护中","maintenance":true}` (site under maintenance), while admins can still access attachments.

### 1.5 Boundaries of S3 / object storage mode

Static serving is **always mounted** (`router.go` comment: after switching to S3, historical `/uploads/...` must still open).
Newly uploaded files return an absolute URL of `publicBase + key` and **do not go through `UploadAuth`**. Therefore:

- Access control moves down to the bucket policy / CDN: the bucket must be private and paired with signed URLs, or you explicitly accept "publicly readable";
- The server does not set an ACL on S3 objects and does not generate signatures;
- Attachment GC only scans the local `DATA_DIR/uploads`; **orphan objects on S3 are out of scope** and need a separate lifecycle policy;
- `S3Storage.Owns` accepts both the `publicBase` prefix and the historical `/uploads/` prefix, so historical files can still be forwarded/referenced after switching storage.

### 1.6 Cross-origin deployment limitations

`SameSite=Lax` means: **when the frontend and API are on different sites, the browser loading `<img src="https://api.example.com/uploads/...">` will not send the cookie**, resulting in 401.

- Recommended: frontend and API on the same domain (nginx reverse-proxies `/api`, `/ws`, `/uploads`), so the cookie is naturally same-origin;
- Different subdomains of the same main domain (`app.example.com` and `api.example.com`) still count as same-site, and Lax will carry it;
- Fully cross-site (`app.com` calling `api.other.com`) can only use a public S3/CDN domain, or have the frontend fetch the blob with `Authorization`;
- Cross-origin also affects WS: the cookie fallback does not work in cross-site scenarios either, so `?token=` is still required there.

### 1.7 Known boundaries

- **No per-conversation membership check**: anyone logged in who knows the URL can read it. The URL itself is a 128-bit random name, but that is not an authorization model. Doing this properly would need a "file → conversation/message" ownership table.
- Logged-out access is always 401, including historical images of public chats; this is an intentional trade-off.
- No role-level exemption: logged-in users (admins included) read under the same rules, and admins get no extra file permission from their identity.

---

## 2. Authentication and Logout

### 2.1 Relationship between cookie and JWT

- The JWT (HS256, claims `uid` / `tv` / `iat` / `exp`) is the only identity credential, `TokenTTL = 7 days`;
- The cookie is only a copy of the same JWT, used where the browser cannot set request headers;
- REST still only accepts `Authorization`; the cookie **cannot** call the API directly (CSRF protection);
- Frontend logout must drop the localStorage token and call `POST /api/auth/logout`.

### 2.2 `POST /api/auth/logout`

- Requires no auth and also works in maintenance mode (otherwise users could not sign out during maintenance);
- Only performs `ClearAuthCookie` and returns `{ "ok": true }`;
- **Does not bump `token_version` and does not kick the WS**: a leaked Bearer token stays usable for the remaining 7 days;
- The frontend `auth.logout` uses `fetch(..., { method: 'POST', keepalive: true })`, so a page navigation does not abort the request.

When you need "revoke immediately", use "log out all devices" in the next section.

### 2.3 Invalidation semantics of `token_version`

- On login/registration the user's current `token_version` is written into the JWT as `tv`;
- Every authentication compares `u.TokenVersion == cl.TV` against the database; a mismatch is 401;
- Scenarios that bump it:
  - "Log out all devices" (`UserStore.BumpTokenVersion`);
  - Password reset via email token (`handler/auth.go` `Reset` does `u.TokenVersion++` in the same transaction);
  - Admin ban / unban and random password reset (`handler/admin.go` `TokenVersion++`);
- An admin changing a role does **not** bump it (`handler/admin.go` comment: the auth middleware queries the database every time, so role changes take effect immediately);
- The semantics are "invalidate everywhere"; there is no per-session ID, so a single device cannot be kicked.

### 2.4 Why "log out all devices" must kick WS

Bumping `token_version` alone does not affect an **already established** WebSocket: the connection only caches the `tv` from the handshake.
Therefore `LogoutOthers` explicitly calls `notifyFor(c).KickUser(uid, ...)` after bumping:

- It sends `{"type":"kicked","reason":"已退出所有设备，请重新登录"}` (logged out on all devices, please sign in again) to all sharded connections of that user, then closes them;
- `KickUser` uses a non-blocking `trySend`, so slow consumers cannot stall the kick;
- Together with the per-frame re-validation in `Dispatch`, this is a double safety net: even if the `kicked` frame is ignored, the next uplink is rejected and the connection dropped.

Note: `LogoutOthers` also invalidates the **current device** (the current token's tv becomes stale), so the frontend should then clear the local token and return to the login page.

---

## 3. Realtime Channel Constraints

### 3.1 Handshake authentication

`ws/serve.go` `Serve`:

1. Prefer `?token=` (compatible with the existing frontend, handshake contract unchanged);
2. Fall back to the `mc_auth` cookie when it is empty;
3. `ParseToken` → look up the user → `u.TokenVersion == cl.TV` → `IsBanned` → maintenance mode (503 for non-admins);
4. On success `SetAuthCookie` re-issues the cookie, and only then is `Upgrade` performed.

### 3.2 Per-frame connection state re-validation

`ws/dispatch.go` `Dispatch` calls `checkConnState(c, f.ConvID)` uniformly after `json.Unmarshal` and before entering the `switch`:

- User deleted / banned / `token_version` changed → `error` frame + `denyConn` disconnect;
- Covers **all frames**: `ping` / `typing` / `read` / `recall` / `edit` / `reaction` / `delivered` / `chat`;
- When `deps.Users == nil` (unit tests or not injected) the check is skipped to stay compatible.

This was the focus of this round of fixes: previously only `chat` was re-validated, so a banned user could keep sending `typing/read` without posting messages, or even affect data through `edit/recall`.

### 3.3 Token transport and log risk

- `?token=` is still kept: the URL ends up in nginx / reverse proxy / application access logs, **which is equivalent to writing a 7-day-valid JWT into the logs**;
- The cookie fallback is the compatibility path left for "a later frontend refactor"; the current frontend still uses the query;
- Suggestions:
  - Disable or redact `access_log` for `/ws` in nginx (the ops manual checklist already lists this);
  - Migrate the frontend to "handshake without token + same-origin cookie" (requires same-domain deployment);
  - Add a `token=` redaction rule on the log-collection side;
  - Never dump full URLs into application logs.

### 3.4 Frame size and rate limiting

- Read limit is 64 KB (the former 4 KB wrongly rejected 5000-character messages, which exceed 4 KB in UTF-8);
- Sending messages is limited to 20 per minute per user (`MsgLimiter`); over the limit returns an `error` frame instead of disconnecting.

---

## 4. Plugin Security Model

### 4.1 Isolation boundary

Plugins run inside `iframe sandbox="allow-scripts"` (`usePluginHost.ts` `mountRuntime`):

- **No `allow-same-origin`**: the iframe's origin is opaque (`null`), so it cannot reach the parent DOM, localStorage, JWT or cookies;
- All capabilities go through the `postMessage` bridge, and the host filters by `API_PERMISSION`;
- Plugins are isolated from each other, each with its own iframe and storage namespace.

### 4.2 Why `srcdoc`

Without `allow-same-origin` the parent page has no right to access `iframe.contentWindow.document`, and `document.open/write` throws `SecurityError`, so the plugin would never start.
That is why `iframe.srcdoc = bootHtml(...)` must be used, writing the bootstrap script before mounting; this does not change the opaque-origin isolation property.

`bootHtml` also escapes `</script` and `<script` in the plugin code, to prevent the bootstrap script from being closed early during HTML parsing.

### 4.3 CSP policy

`buildSandboxCsp` generates:

```
default-src 'none';
script-src 'unsafe-inline';
style-src 'unsafe-inline';
img-src data:;
connect-src <allowedHosts or 'none'>;
base-uri 'none'; form-action 'none'; frame-src 'none'; object-src 'none'; worker-src 'none'
```

Key points:

- `default-src 'none'`: external images, fonts and media are all blocked by default; `image` in a UI description only accepts http(s) and is rendered by the host;
- `script-src 'unsafe-inline'` is required because the bootstrap script is inline, but the plugin code itself also lives in that inline script — **do not read this as "scripts are trusted"**;
- `connect-src` confines `fetch` / XHR / WebSocket / beacon to the manifest's `allowedHosts`; `'none'` when undeclared;
- Host names only accept `example.com` or `*.example.com`; ports/paths/quotes are filtered out so they cannot be spliced into a CSP directive.

### 4.4 Permission and event filtering

`API_PERMISSION` maps each SDK method to a permission:

| Permission | Capability |
|---|---|
| `message:read` | `api.on(...)` subscribe to events |
| `message:send` | `api.send(...)` |
| `message:render` | `api.registerMessageRenderer(...)` |
| `ui:panel` | `api.addSidePanel(...)` |
| `ui:actions` | `api.registerMessageAction(...)` |
| `conv:read` | `api.me()` / `api.conv.current()` / `api.conv.members()` / `api.messages.recent()` |
| `ui:host` | `api.nav.*` / `api.toast()` |
| `storage` | `api.storage.get/set` (per-plugin namespace, value ≤64KB) |
| `net` | `api.request()` (also limited by `allowedHosts`) |
| `ui:theme` | `api.theme.apply()` (`--td-*` tokens only) |

Event dispatch is additionally filtered by permission (`EVENT_PERMISSION`):

- The payloads of `message:new` / `message:recall` / `presence` contain message bodies or user lists, so they are only sent to plugins declaring `message:read`;
- `conv:open` / `ui:action` carry no private body, so no extra restriction;
- `message:action` is delivered only to **plugins that registered that action**, never broadcast;
- `messages.recent` only returns visible messages already persisted and not pending/failed; the `content` of recalled messages is blanked so the recalled original is never handed to plugins.

> Note: in the `ui:actions` context-menu callback, if a plugin does **not** have `message:read`, the host only provides `messageMeta` (id/convId/senderId/msgType/createdAt/isRecalled) and no body. This is deliberate design, not a bug.

### 4.5 How `net` and `allowed_hosts` are enforced

Two layers:

1. **Server-side manifest validation** (`server/internal/plugin/manifest.go`): `allowedHosts` ≤20 entries, the regex only accepts host names / `*.` subdomains;
2. **Frontend enforcement** (`usePluginHost.ts` `request`):
   - Only `http:` / `https:` are allowed;
   - `hostAllowed(hostname, allowedHosts)` matches exactly or by `*.` suffix (`*.example.com` also allows the bare `example.com`);
   - Otherwise it returns `host not allowed` and no request is made.

Note: `allowedHosts` also determines the CSP `connect-src`. **CSP only allows by domain, regardless of whether `net` is declared**; if a plugin declares `allowedHosts` but not `net`, it cannot request through `api.request`, but in theory it can still use a bare `fetch` inside the iframe for those domains (still subject to the target site's CORS). When auditing plugins, do not look only at `net`; look at `allowedHosts` too.

### 4.6 Must state clearly: bare `fetch` is broken

> **Plugins that relied on bare `fetch` are broken and must switch to `mc.request`.**
>
> The SDK object injected by the host is named `api`, so plugin code calls `api.request(url, opts)` (typed as `McApi`); `mc.request` refers to this same method.
> Why bare `fetch` broke: the iframe is an opaque origin (`Origin: null`), and target sites usually do not allow `null` in CORS; at the same time the CSP `connect-src` only allows `allowedHosts`, and is `'none'` when nothing is declared.
> `api.request` is sent by the host page on the plugin's behalf; it is constrained by the same CORS and `allowedHosts`, but at least it carries the host page's real Origin.

### 4.7 Other constraints

- Plugin size ≤1 MB; `.ts` uploads are stripped of types by server-side esbuild, and `import` is not resolved;
- For URL installs, `plugin.Fetch` performs DNS resolution and dialing inside a custom `DialContext` to prevent DNS rebinding, and disables environment proxies;
- The site admin can globally take down / force uninstall, effective immediately for all users.

---

## 5. Upload Pipeline

### 5.1 Type sniffing and "writing back the 512 bytes"

`service/storage.go` `LocalStorage.SaveStream` / `S3Storage.SaveStream`:

1. `io.ReadFull` reads the first 512 bytes into `head`;
2. `sniffMime` strips the charset; `extFor(kind, mime, head)` decides the extension from the kind whitelist;
3. **`tmp.Write(head)` writes the head back into the temporary file**;
4. `io.Copy(tmp, io.LimitReader(r, maxBytes+1-hn))`, and `total = hn + n` determines whether the limit was exceeded.

This is one of the most serious fixes of this round: previously only the remaining part was written, so **every streamed upload lost its first 512 bytes** — images would not open, audio was silent, video only had the second half; the S3 branch's `PutObject` also used the remaining length.

### 5.2 Limits

| Type | Cap | Source |
|---|---|---|
| Image / audio / group file / video | Admin console `MaxImageMB` / `MaxAudioMB` / `MaxFileMB` / `MaxVideoMB` | `handler/upload.go` `limits` |
| Sticker | 2 MB (`MaxStickerBytes`) | `service/storage.go` |
| Request body | `maxBytes + 1 MB` (multipart boundary overhead) | `http.MaxBytesReader` |

Over the limit returns 413 `"文件过大（kind 上限 N MB）"` (file too large — the kind's N MB cap); when a `MultipartReader` failure is caused by `MaxBytesReader`, it is also recognized as an over-limit first, rather than wrongly reporting "missing file field".

### 5.3 Allowed types

- Images: jpeg / png / webp / gif;
- Audio: webm / ogg / mpeg / mp4(m4a) / wav;
- Video: mp4 / webm / quicktime(mov) / x-m4v;
- Group files: no MIME whitelist, size only (still sniffed but any binary is allowed).

Audio has a compatibility fallback: audio recorded by the browser's `MediaRecorder` is often detected by `http.DetectContentType` as `video/webm`, `application/ogg` or `video/mp4`, so `extFor` uses `guessAudioExt` on the container signature when `kind=audio`, preventing legitimate voice messages from being rejected.

### 5.4 Thumbnail safety limits

`service/thumbnail.go`:

- First `image.DecodeConfig` reads **only the header**; when the pixel count exceeds 40 million (about 6325×6325) the thumbnail is skipped entirely and the frontend falls back to the original image;
- The whole generation path has a `recover`; a decoder panic on a malformed image is only logged and does not affect the upload;
- Thumbnails are named like `xxx_t.jpg` and live in the same directory as the original file.

### 5.5 Rules for attachment GC

`handler/ops.go`:

- `referencedUploads` collects five reference kinds: message `content LIKE '/uploads/%'`, sticker `url`, group file `url`, group avatar, user avatar;
- **It does not enumerate by `msg_type`**: any message whose content starts with an upload path counts as a reference, so new message types cannot be missed;
- `addUploadRefs` also marks the derived `xxx_t.jpg` thumbnails as referenced, so they follow the lifecycle of the original file;
- `orphanMinAge = 24h`: a file just uploaded but not yet sent in a message is not deleted by mistake;
- Only the local `DATA_DIR/uploads` is scanned; S3 objects are out of scope;
- `dryRun` and real deletion share `gcOrphans`; check the count and byte size with dryRun before executing.

---

## 6. Rollout and Migrations

### 6.1 Execution mechanism

- `server/internal/store/migrations.go` `RunMigrations` uses `golang-migrate`, sourced from `migrations/` in the image/working directory;
- With `MIGRATE=true` the application runs `Up()` automatically at startup; default is `false`, so it must be executed manually;
- Versions are recorded in the `schema_migrations` table;
- Migration files come in pairs: `NNNN_name.up.sql` / `.down.sql`.

### 6.2 0023: message body widened to `TEXT`

| Item | Content |
|---|---|
| Impact | `messages.content` changes from `VARCHAR(1000)` to `TEXT`; the admin console can raise the per-message limit to 5000 characters, about 15 KB for multi-byte characters, far below the TEXT cap |
| Risk | On a large table the `ALTER` may lock the table / take a long time; whether `VARCHAR → TEXT` supports `ALGORITHM=INSTANT` depends on the MySQL version and column definition, so **do not assume it never locks** — run it off-peak and watch `SHOW PROCESSLIST` |
| Rollback | `ALTER COLUMN content VARCHAR(1000)`; **messages longer than 1000 characters will be truncated or make the ALTER fail** |
| Backup | Back up `messages` before rolling back (at least back up the oversized rows) |

### 6.3 0024: unique key on direct chat `direct_key`

| Item | Content |
|---|---|
| Impact | `conversations` gains a unique index on `direct_key` (`smallID:largeID`); `EnsureSingle` wraps dedupe / conversation creation / member addition / inbox initialization in one transaction |
| Existing data | Existing direct chats keep `direct_key` as `NULL` (a MySQL unique index allows multiple NULLs), so existing data is unaffected; only new conversations write a value |
| Risk | Concurrent inserts: only one succeeds, the loser rolls back and reuses the existing conversation; no duplicate direct chats are produced |
| Rollback | First `DROP INDEX idx_conversations_direct_key`, then `DROP COLUMN direct_key`; after rollback the concurrent-duplicate problem returns |
| Backup | Involves `conversations` / `conversation_members` / `user_conversations`; a full database backup is recommended |

### 6.4 0025: friend / verification code unique indexes (incl. duplicate data cleanup)

| Item | Content |
|---|---|
| Impact | `friends (user_id, friend_id)` unique index `uk_friends_pair`; `email_codes (email, purpose)` unique index `uk_email_codes_email_purpose` |
| Data cleanup | The migration first cleans duplicate friend rows in the same direction, keeping priority **accepted > pending > rejected**, and among the same priority the row with the smallest `id`; on the verification-code side only the row with the largest `id` (newest) per `(email, purpose)` is kept, and code issuing is changed at runtime to row locking + in-place reset |
| Risk | The cleanup is **destructive**; deleted duplicate rows are not restored; the reverse record when accepting a friend request becomes an upsert, so mutual requests do not collide with the index |
| Rollback | Only the two indexes are dropped; already-cleaned dirty data is not restored |
| Backup | You **must back up** `friends` and `email_codes` — this is the only one of the three migrations that deletes data |

### 6.5 Recommended order

1. Stop writes or use a low-peak window, and `mysqldump` the affected tables (0025 preferably the whole database);
2. Run 0023 first (column widening, lowest risk);
3. Then 0024 (add column + unique index);
4. Finally 0025 (includes cleanup, highest risk);
5. Start the new version (`MIGRATE=true` runs them in order automatically; when doing it manually, follow 0023 → 0024 → 0025);
6. Verify:
   - Send a message close to 5000 characters;
   - Both sides create a direct chat at the same time and confirm only one conversation is produced;
   - Send friend requests concurrently and confirm 409 with no duplicate rows;
   - `/api/version` returns the correct version.

> Do not run `go test -tags integration ./test/` against a production database: the integration tests first DELETE all business tables.

### 6.6 Rollback order

The reverse of the upgrade: 0025 → 0024 → 0023. Before rolling back 0023, check whether rows with `CHAR_LENGTH(content) > 1000` exist, otherwise the `ALTER` will fail or truncate.

---

## 7. Deployment

### 7.1 Docker version injection

`server/Dockerfile` injects via ldflags in the build stage:

| build arg | Purpose | When omitted |
|---|---|---|
| `VERSION` | Version number | `dev` |
| `COMMIT` | Commit hash | `unknown` |
| `BUILD_TIME` | Build time | `unknown` |

`docker-compose.yml` already passes these three parameters through; `/api/version` reads exactly these values. The build works even when the build context has no `.git`, so there is no need to mount repository metadata just for the version.

### 7.2 Configurable image source parameters

| build arg | Overrides | Default |
|---|---|---|
| `GO_IMAGE` | Go build image | Official `golang` |
| `RUNTIME_IMAGE` | Runtime image | Official Alpine |
| `GOPROXY` | Go module proxy | Official proxy |
| `APK_MIRROR` | Alpine package mirror | Official mirror |

All defaults are official sources, so behavior is unchanged when no argument is passed. Verify the trustworthiness of any third-party image/proxy yourself; domestic-network examples are in [README.md](../README.md) and [运维手册.md](运维手册.md).

### 7.3 Behavior verified inside the container

- At startup, with `MIGRATE=true`, `migrations/` inside the image is applied to the latest version automatically;
- `/api/health` can be used for the compose healthcheck;
- The `/uploads` static service works normally inside the container, but **the default compose mounts no volume for `DATA_DIR`**: local attachments are lost when the container is recreated. A production deployment must mount a persistent volume for `DATA_DIR` (default `data`), or switch to S3;
- The reverse proxy must preserve `X-Forwarded-Proto`, otherwise `mc_auth` will not carry `Secure` under an HTTPS deployment (still usable, but weaker).

### 7.4 Common deployment pitfalls

- Frontend static assets and API on different domains: the `SameSite=Lax` on `mc_auth` makes `/uploads` return 401, see §1.6;
- Mounting only `nginx` and not `DATA_DIR`: attachments are lost;
- Recreating the container with `docker compose up -d` but not rebuilding the image: `VERSION` stays the old value and `/api/version` does not change;
- Maintenance mode: `/uploads` and WS are both affected, non-admins cannot connect, so do not mistake it for "the service is down".

---

## 8. Code Reference Table

| Behavior | File | Key symbols |
|---|---|---|
| `/uploads` routing, cache, directory rejection | `server/internal/router/router.go` | `serveUpload`, `onlyFilesFS` |
| Cookie issuance/clearing, REST/upload auth | `server/internal/middleware/jwt.go` | `AuthCookieName`, `SetAuthCookie`, `ClearAuthCookie`, `Auth`, `UploadAuth` |
| Logout endpoint | `server/internal/handler/auth.go` | `Logout` |
| Log out all devices | `server/internal/handler/user.go` | `LogoutOthers` |
| WS handshake and cookie fallback | `server/internal/ws/serve.go` | `Serve` |
| Per-frame re-validation | `server/internal/ws/dispatch.go` | `Dispatch`, `checkConnState` |
| Kick connection | `server/internal/ws/hub.go` | `KickUser` |
| Plugin manifest / permissions | `server/internal/plugin/manifest.go` | `Manifest`, `ValidPermissions` |
| Plugin URL install SSRF protection | `server/internal/plugin/fetch.go` | `Fetch` |
| Plugin sandbox / CSP / permission filtering | `web/src/composables/usePluginHost.ts` | `mountRuntime`, `buildSandboxCsp`, `bootHtml`, `API_PERMISSION`, `EVENT_PERMISSION`, `hostAllowed` |
| Upload limits and streaming save | `server/internal/handler/upload.go`, `server/internal/service/storage.go` | `Upload`, `SaveStream`, `sniffMime`, `extFor` |
| Thumbnails | `server/internal/service/thumbnail.go` | Decoded-size cap |
| Attachment GC | `server/internal/handler/ops.go` | `referencedUploads`, `addUploadRefs`, `gcOrphans` |
| Migration execution | `server/internal/store/migrations.go` | `RunMigrations` |
| Docker parameters | `server/Dockerfile`, `docker-compose.yml` | `GO_IMAGE` / `RUNTIME_IMAGE` / `GOPROXY` / `APK_MIRROR` / `VERSION` / `COMMIT` / `BUILD_TIME` |

---

## 9. Common Pitfalls

1. **Thinking logout revoked the token**: `/api/auth/logout` only clears the cookie; real revocation needs "log out all devices".
2. **Thinking the cookie expires by itself**: it is only a 7-day copy; after a password reset the old cookie returns 401, but the browser keeps it until logout or natural expiry.
3. **Forgetting to rebuild the backend after an API change**: locally you run `bin/modernchat-server.exe`, so after editing Go sources you must `go build -o ../bin/modernchat-server.exe ./cmd/server` and restart.
4. **Relying on `/uploads` authorization in S3 mode**: S3 URLs do not go through `UploadAuth`; access control is the bucket/CDN's job.
5. **Still using `fetch` in a plugin**: it no longer works, switch to `mc.request` / `api.request`; also confirm the manifest declares `net` and `allowedHosts`.
6. **Treating `allowedHosts` as a substitute for `net`**: CSP only allows by domain, the permission still needs `net`.
7. **Writing only the remaining stream after upload**: any "read the head then forward" implementation must write the head back, otherwise the first 512 bytes of the file are lost.
8. **GC enumerating by message type**: new message types get missed; collect by the `/uploads/` prefix instead.
9. **Running integration tests directly**: they wipe business tables, so only run them against a separate test database.
10. **Not backing up before 0025**: it deletes duplicate friend / verification-code rows, and a rollback cannot restore the data.
