# ModernChat Ops Manual

> For **deployment & maintenance**: environment variables, deployment shapes, backup & upgrade, logs, troubleshooting.
> API contracts: [后端需求规格.md](后端需求规格.md); frontend: [前端技术方案.md](前端技术方案.md).

---

## 1. Deployment Shape

**Technical premises**: single-instance self-hosting. MySQL 8.0 is the only required dependency; Redis (shared rate-limit quota) and SMTP (password-recovery emails) are **optional** and degrade gracefully when absent.

```
Browser ──HTTPS──▶ nginx (static assets + /api proxy + /ws upgrade)
                        │
                        └──▶ modernchat-server (:8080) ──▶ MySQL 8.0
```

Config samples:

- `deploy/nginx.conf.example` — bare metal (systemd + nginx)
- `deploy/nginx.compose.conf` — Docker Compose

**nginx must configure Upgrade headers for `/ws`**, otherwise the WebSocket handshake fails (see §7).

---

## 2. Environment Variables (full table)

The backend reads all config from environment variables (`server/internal/config/config.go`); the `.env` file lives in `server/` (gitignored, **never commit it**).

| Variable | Default | Required | Notes |
|---|---|---|---|
| `PORT` | `8080` | no | backend listen port |
| `GIN_MODE` | `release` | no | keep `release` in production; `debug` allows an auto-generated random `JWT_SECRET` |
| `DB_DSN` | `root:root@tcp(127.0.0.1:3306)/modernchat?...` | **yes** | MySQL DSN, keep `utf8mb4&parseTime=True&loc=Local` |
| `JWT_SECRET` | none | **yes** (prod) | signing key, **≥32 bytes**; production refuses to start unset. Generate: `openssl rand -hex 32`. **Changing it makes stored SMTP / Redis / object storage secrets undecryptable** (see §2.1) |
| `MIGRATE` | `false` | no | `true` runs `migrations/` at startup (prod recommended to run versioned SQL manually) |
| `FRONTEND_URL` | `http://localhost:5173` | **yes** | allowed frontend origins, comma-separated. Used for the **WS Origin whitelist** and the site base of recovery links |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | no | trusted reverse-proxy ranges; **setting it explicitly to an empty string = trust no proxy** (XFF is then not used to derive client IPs) |
| `PLUGIN_MAX_SIZE` | `1048576` (1 MB) | no | plugin upload size cap (bytes) |
| `LOG_LEVEL` | `info` | no | `debug` / `info` / `warn` / `error` |
| `LOG_DIR` | `logs` | no | log directory; empty = stdout only |
| `DATA_DIR` | `data` | no | data directory; uploaded images / voice / video live in `DATA_DIR/uploads/YYYY/MM/`. **Ignored when using object storage** (files are not written locally) |
| `LOG_RETAIN_DAYS` | `15` | no | log retention days |
| `REDIS_ADDR` | empty | no | when set, rate limiting uses a Redis shared quota (multi-instance); connection failures fall back to memory automatically. **Editable in the admin console** (restart required) |
| `REDIS_PASSWORD` | empty | no | Redis password. **Editable in the admin console** (restart required, stored encrypted) |
| `REDIS_DB` | `0` | no | Redis DB number. **Editable in the admin console** (restart required) |
| `SMTP_HOST` | empty | no | when empty, recovery links only go to the server log (**users get no email**). **Editable in the admin console** |
| `SMTP_PORT` | `587` | no | `465` = implicit TLS (QQ / 163 recommended); `587` = STARTTLS. **Editable in the admin console** |
| `SMTP_USER` | empty | no | login user; QQ / 163 use an **auth code**, not the account password. **Editable in the admin console** |
| `SMTP_PASS` | empty | no | auth code / password. **Editable in the admin console** (stored encrypted) |
| `SMTP_FROM` | `modernchat@localhost` | no | sender, **QQ / 163 require it to equal `SMTP_USER`**, otherwise mail is rejected. **Editable in the admin console** |
| `LOG_RESET_LINK` | `false` under release, `true` otherwise | no | whether to write the **full reset link** into the log when SMTP is unset. Off by default in production: a usable token in the log is an account-takeover entry point (log platforms and backups can see it). Turn it on explicitly only when you really need to fetch the link from the log |
| `WEB_UPDATE` | `true` | no | site admin console "online update" switch. When `false`, all `/api/admin/update/*` endpoints are unavailable; Docker / bare metal deployments are unavailable automatically (see §4.2) |
| `RELEASE_API_URL` / `RELEASE_DL_URL` | empty | no | release mode: latest-release version API and download prefix, written into `.env` by `install.sh`; when both are empty, source-code git mode is used |
| `METRICS_TOKEN` | empty | no | when non-empty, `GET /api/metrics` requires `Authorization: Bearer <token>`; when empty, same-site anonymous scraping is allowed — set it for internet-facing deployments |
| `BACKUP_DIR` | `<DATA_DIR>/backups` | no | database backup output directory |
| `BACKUP_INTERVAL_HOURS` | `0` | no | automatic backup interval in hours; `0` = only the manual "Backup now" in the admin console |
| `BACKUP_RETAIN` | `7` | no | number of automatic backups to retain (oldest deleted beyond this) |
| `MYSQLDUMP_PATH` | `mysqldump` | no | `mysqldump` executable path (absolute path when not on PATH) |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` | empty | no | Web Push key pair; enabled only when both are set, otherwise pushes are skipped silently |
| `VAPID_SUBJECT` | `mailto:admin@localhost` | no | Web Push contact email (`mailto:` prefix) |

> `FRONTEND_URL` notes: comma-separated, each item **without trailing spaces**; a trailing slash is fine (the backend strips it).
> Example: `FRONTEND_URL=https://chat.example.com,https://intranet.example.com`

### 2.1 Admin-configurable Settings (Database Overrides Environment Variables)

Values the site admin changes under **Admin console → System settings** are written to the `system_settings` table and **take precedence over environment variables**:

| Priority | Source | When it takes effect |
|---|---|---|
| High | Admin console "System settings" | SMTP: **hot-applied on save**; Redis: restart required; **site / chat rules / upload limits / login security: immediate (no restart)** |
| Low | Environment variables | read at startup |

So environment variables still act as a fallback (containerized deployments can run without touching the database), but **once configured in the admin console, the database wins**. The startup log prints `smtp config loaded from database` when the database config is used.

**Operational parameters at a glance** (take effect immediately after saving in the admin console, no restart):

| Group | Setting | Range / default | Where it applies |
|---|---|---|---|
| Site | Site name | ≤40 chars, default `ModernChat` | browser title, login page |
| Site | Open registration | on / off (default on) | register endpoint 403, login page hides the entry |
| Site | Maintenance mode + notice text | default off | non-admins get 503 + maintenance page (see below) |
| Site | Registration requires invite code | default off | registration must carry a valid invite code |
| Site | Registration requires email verification | default on (needs SMTP) | register page shows email (required) + verification code; same email has a 60-second cooldown, is valid for 10 minutes, and is invalidated after 5 consecutive wrong attempts. **Without SMTP configured, verification is automatically not required** (otherwise nobody could register); saving this switch as on in the admin console fails with a message to configure SMTP first. The first account is exempt from the code but **must still provide an email** (it is saved so the password can be recovered later). A user's "change email" also goes through a verification code (sent to the new address); without SMTP, that action is rejected |
| Chat | Single message length | 100–5000, default 1000 | WS validation + input counter |
| Chat | Recall window | 0–60 minutes, default 2; **0 = recall disabled** | WS validation + context menu |
| Chat | Group member limit | 2–2000, default 200 | join transaction validation |
| Upload | Image / voice / group file / video size | 1–32 / 1–32 / 1–200 / **1–100** MB, default 8 / 5 / 20 / **30** | upload endpoints (the request-body middleware is loosened with a 200 MB ceiling) |
| Security | Login failure threshold | 0 (off) or 2–20, default 5 | login endpoint 429 |
| Security | Lockout duration | 1–1440 minutes, default 15 | same as above |

**Security design**:

- **Secret fields are encrypted at rest** (`smtp.pass` / `redis.pass` / `storage.secretKey`) with AES-256-GCM; the key is derived from `JWT_SECRET`.
- The API **never echoes secrets in plaintext**; it only returns `hasPass: true/false`; leaving the field empty on submit means "do not modify".
- Every save and test send is written to the audit log (`admin_audits`).
- Out-of-range numbers **return 400 directly** (no silent clamping); missing / dirty values fall back to defaults on read, so one bad value never wedges a feature.

> ⚠️ **Side effects of changing `JWT_SECRET`**: the encryption key is derived from it, so after a change **already-saved secrets can no longer be decrypted** — those settings become unreadable, SMTP stops working, and the Redis password is lost.
> Fix: after changing `JWT_SECRET`, re-enter the SMTP / Redis / object storage passwords in the admin console.
> If you only want everyone to log in again, use "reset password" or bump `token_version` — there is **no need** to change `JWT_SECRET`.

### 2.2 Object Storage (Where Images / Voice Live)

Chat images and voice **store only a URL**; where the file lands is decided by config:

| Case | Storage location | URL shape |
|---|---|---|
| Endpoint + Bucket not set in the admin console | local disk `DATA_DIR/uploads/YYYY/MM/` | `/uploads/2026/11/xxx.png` (served statically by the backend) |
| Endpoint + Bucket set in the admin console | S3-compatible storage (AWS S3 / MinIO / Alibaba Cloud OSS / Tencent Cloud COS) | `<publicBase>/2026/11/xxx.png` or the auto-assembled endpoint/bucket |

Key points:

- **`storage.publicBase`**: the public access prefix (CDN domain); when empty it is auto-assembled from `endpoint/bucket`.
- **Once object storage is configured, upload failures are hard errors and never fall back to local** — avoiding data split where you "think it's in the cloud but it's actually on this machine".
- **Switching storage does not migrate historical files**: `/uploads/...` in old messages is still served locally (as long as the `uploads/` directory still exists), and new messages use the new storage. Full migration requires moving files yourself with `mc`/`rclone` and batch-updating `messages.content`.
- **`/uploads` requires login**: supports `Authorization: Bearer` or a same-origin `mc_auth` cookie (`UploadAuth`); directory listing always 404; responses use `Cache-Control: private, max-age=3600` + `nosniff` — no longer `public/immutable`, so private attachments never enter shared proxy caches.
- **Cross-site deployment warning**: `mc_auth` is `SameSite=Lax`, so when the frontend and API are on different sites, `<img src=".../uploads/...">` sends no cookie and gets 401. Prefer a same-domain reverse proxy; for true cross-site, expose S3/CDN public domains instead, or fetch blobs with `Authorization` from the frontend.
- **S3 mode boundaries**: new files are absolute `publicBase` URLs and bypass `/uploads` auth — access control is the bucket policy / CDN's job; historical `/uploads/` is still served locally. See [Security & Migration Guide.md](安全与迁移指南.md) §1.
- **Type is decided by file header**: renaming HTML to `.png` is rejected (blocks stored XSS); audio accepts browser-recording containers `video/webm` / `application/ogg` / `video/mp4`. Per-type size limits are adjustable under "System settings → Upload limits"; emoji stickers are fixed at 2 MB.
- **Thumbnails**: uploading an image asynchronously generates `_t.jpg` (≤480px); bubbles load the thumbnail and the lightbox loads the original. GIF / too-small images / decode failures are skipped, and transparent images get a white background. The image header is read before decoding; anything above 40 million total pixels is skipped to prevent decompression bombs; the generation path has a panic fallback.
- **Total quota and orphan cleanup**: `upload.maxStorageMB` (0 = unlimited) caps the local upload directory total; the admin console "Storage" tab shows usage and cleans up orphan files. GC collects references by `/uploads/` prefix from message bodies, emoji reactions, group files, group avatars and user avatars — **it does not enumerate by message type**; derived thumbnails follow the original file; files newer than 24 hours are kept; it scans only local `DATA_DIR/uploads`, so S3 orphan objects are out of scope.

### 2.3 Maintenance Mode (Standard Procedure for Upgrades / Incidents)

Entry: Admin console → System settings → Site info → Maintenance mode.

Once enabled:

| Target | Behavior |
|---|---|
| Regular user (logged in) | page switches to the maintenance page within 30 seconds; business endpoints return **503** + custom text |
| Anonymous visitor | sees the maintenance page directly; registration / password recovery are also blocked |
| Admin | **unaffected**: can log in, access the admin console and all endpoints normally |
| WebSocket | non-admin handshakes are rejected outright; existing connections can no longer send or receive |

Paths kept available (deliberate by design — do not change):

- `/api/auth/login` — **the only entry for admin login**, must remain
- `/api/site` — the frontend maintenance page relies on it for the site name and text
- `/api/health`, `/api/version` — health probes

**Standard flow**:

1. Enable maintenance mode in the admin console and fill in the estimated recovery time (visible to users);
2. Perform the upgrade / fix;
3. Disable maintenance mode; users recover automatically within 30 seconds.

**Troubleshooting**:

- **Users report 503**: first check `maintenanceMode` from `GET /api/site` — most likely maintenance mode was left on.
- **The admin is locked out too**: the login endpoint always works under maintenance, so sign in from the login page with an admin account; if even login fails, that is a different problem (check backend logs and DB connectivity), unrelated to maintenance mode.
- **Raised the upload limit but uploads still fail**: check whether nginx sits in front — when `client_max_body_size` is below the application limit, nginx returns 413 first (when the app message and nginx behavior disagree, suspect this layer first).

---

## 3. First Deployment

### 3.1 One-command Install Script (recommended, BT Panel style)

> **Logs**: the whole run is mirrored to `/var/log/modernchat/install-<timestamp>.log` (`install-latest.log` is a symlink to the latest); `scripts/update.sh` logs go to `logs/update-*.log` in the repo.
>
> **Resume after failure**: re-run the same command after a failed install; completed stages are skipped automatically and parameters are not asked again (progress and parameters live in `/var/lib/modernchat/install-state`, permissions 600). Every "skipping completed stage" notice includes how to re-run: **only that stage** — `sed -i '/^STAGE_<name>=done$/d' /var/lib/modernchat/install-state` then re-run; **everything** — `FORCE=1 bash install.sh` or `bash install.sh --force`. Creating the database and user are idempotent, so an interrupted run continues where it stopped.
>
> **Panel path adaptation**: nginx binary and vhost directory are auto-detected — standard install (`/etc/nginx/conf.d`), BT Panel (`/www/server/panel/vhost/nginx`, binary `/www/server/nginx/sbin/nginx`), 1Panel / OpenResty; when none is detected you may enter the directory included by `nginx.conf` manually. Reload uses the detected binary (panel-built nginx does not go through systemd). Note: the panel's "Websites" list does not show sites written by the script (this affects neither running nor access).

On a **fresh Linux server** (Ubuntu / Debian / CentOS family, root):

```bash
curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh -o install.sh
bash install.sh
```

The script does the whole flow "BT Panel style" and only asks a few questions:

1. **Environment self-check and gap filling**: installs whatever is missing — curl/tar basics, MySQL 8, nginx. **No Go / Node / git needed** (no local compilation anymore).
2. **Download the release**: fetch the prebuilt package `modernchat-<version>-linux-<arch>.tar.gz` from GitHub / Gitee Releases (sha256 verified; contains the backend binary + frontend dist + migration SQL + deploy scripts). `RELEASE_SOURCE=gitee|github` picks the source, `RELEASE_TAG=v0.10.3` pins the version (default latest).
3. **Automatic database creation**: MySQL is installed automatically when absent locally; for an existing instance (BT Panel / cloud database) choose "external" and fill in the connection info. Creates the `modernchat` database + a dedicated account, creating both `'localhost'` and `'%'` hosts to work around BT Panel restrictions. The **application DB password** can be preset with the `DB_PASS` environment variable or entered interactively; pressing Enter generates a random password — the final summary **explicitly prints** the database account and password (also stored in `server/.env` and `/var/lib/modernchat/install-state`, both 600; install logs are 600 too, root-readable only). In automatic-install mode MySQL root is **passwordless (auth_socket)**; the summary gives the command to set a root password (setting one does not affect the app, which uses the dedicated account).
4. **Configuration and persistence**: generates `JWT_SECRET`, writes `server/.env` (including `RELEASE_API_URL` / `RELEASE_DL_URL` for online updates), registers the systemd service `modernchat@<port>`, generates and loads the nginx config (static hosting + `/api` proxy + `/ws` upgrade + `/ws` access log off).
5. **Print the site link**: with a domain → `http://domain` (with the port when not 80); without one → `http://public-IP:port`. The first registered account automatically becomes the site admin.

Environment variables support non-interactive runs (unset items are still asked):

```bash
INSTALL_DIR=/opt/modernchat DOMAIN=chat.example.com SITE_PORT=80 BACKEND_PORT=8080 \
DB_MODE=external DB_HOST=127.0.0.1 DB_PORT=3306 DB_ROOT_USER=root DB_ROOT_PASS=xxx \
RELEASE_SOURCE=gitee RELEASE_TAG=latest bash install.sh
```

**Release flow** (publishing a new version for install / update):

1. Update `CHANGELOG.md` to finalize the version, align the `package.json` / `package-lock.json` of `web/` and `website/` plus the official site hero badge to the same version; run `sh scripts/check-version.sh` and make it pass (the baseline is the latest stable version in CHANGELOG);
2. Tag (e.g. `v0.10.3`) and push;
3. Run `scripts/package-release.sh` on a build machine (needs Go + Node) — it produces `dist-release/modernchat-<version>-linux-{amd64,arm64}.tar.gz` and the matching `.sha256`;
4. Upload these files as Gitee / GitHub Release assets (upload tags and assets on both platforms; the install script picks a source by `RELEASE_SOURCE`).

Notes:

- **Cloud security groups**: the script opens ufw / firewalld, but cloud console security groups must be opened manually for the site port (BT Panel users: Security → port rules).
- **BT Panel users**: you can also skip nginx install in the script — install MySQL 8.0 and nginx from the app store first, then run the script and choose the "external" database; the generated nginx config (`/etc/nginx/conf.d/modernchat.conf`) can be copied into the BT Panel site config.
- **Debian 12 caveat**: the apt repo only has MariaDB (no MySQL 8 ngram full-text index, so message-search migration 0022 is unusable); prefer "external" with a MySQL 8 installed by BT Panel.
- SMTP password recovery / Web Push / object storage / Redis are all optional and can be configured later under "Admin console → System settings".

### 3.2 Full Uninstall

```bash
bash /opt/modernchat/scripts/uninstall.sh            # interactive confirmation
bash /opt/modernchat/scripts/uninstall.sh /opt/modernchat   # explicit install directory
AUTO_CONFIRM=yes PURGE_DB=yes DELETE_FILES=yes bash scripts/uninstall.sh   # non-interactive full removal
```

What is removed and default behavior:

| Item | Behavior | Default |
|---|---|---|
| Backend service | stops and removes the `modernchat@<port>` template unit (including hot-updated old and new instances) and legacy units | always removed |
| nginx config | removes only config files **carrying the generated marker** (never hand-written config), then reloads | always removed |
| Database | `DROP DATABASE` + drop the dedicated user (both `'localhost'` and `'%'`) | **kept by default**, requires confirmation |
| Install directory | includes code, `server/.env`, uploaded files, logs | **kept by default**, requires confirmation |
| Install/update logs | removed with the install directory; `/var/log/modernchat/` is kept | kept |

System-level components such as Go / Node / MySQL / nginx are not uninstalled (other apps may use them).

### 3.3 Manual Deployment

```bash
# 1. Pull the code
git clone https://gitee.com/Sun-Si-Cheng/modern-chat-new.git && cd modern-chat-new

# 2. Prepare the database
mysql -uroot -p -e "CREATE DATABASE modernchat CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# 3. Configure
cp server/.env.example server/.env
# Edit server/.env: at least DB_DSN / JWT_SECRET / FRONTEND_URL

# 4. Build & start
cd server && go build -o ../bin/modernchat-server ./cmd/server   # add .exe on Windows
../bin/modernchat-server

# 5. Build the frontend and hand it to nginx
cd ../web && npm ci && npm run build     # output in web/dist, plus .gz / .br precompressed files

# 6. Verify
curl http://127.0.0.1:8080/api/health     # {"status":"ok"}
curl http://127.0.0.1:8080/api/version
```

**The first registered account automatically becomes the site admin** (`role=admin`); everyone after is a regular user. Registration entry: "Register now" on the login page.

### Static Asset Compression (must-read for deployment)

`npm run build` produces `dist/` and also generates a `.gz` and `.br` alongside every text asset (handled by `scripts/precompress.mjs`, Node built-in zlib, no extra dependencies).

Two matching nginx settings (`deploy/nginx.*.conf` already includes them):

```nginx
gzip_static on;   # serve .gz directly when present, zero runtime CPU
gzip on;          # real-time compression fallback when no precompressed file
```

Measured results (all assets):

| Form | Size | Ratio |
|---|---|---|
| Raw | 1288 KB | 100% |
| gzip | 358 KB | 28% |
| brotli | 311 KB | 24% |

Actual first-screen transfer drops from **161 KB to about 65 KB**.

> **brotli needs an extra module**: stock nginx does not ship `ngx_brotli`; run `nginx -V 2>&1 | grep -i brotli` before enabling, otherwise the config makes nginx fail to start. The relevant lines in the config file are commented out by default. Even if not enabled, keeping the `.br` files is harmless (no bandwidth used; they are only read when the module is installed).

> Recompress only (without rebuilding): `cd web && npm run compress`.

---

## 4. Upgrading Versions

```bash
# Linux / macOS
./scripts/update.sh            # default hot update (zero downtime)
./scripts/update.sh --cold     # force the legacy restart
./scripts/update.sh --no-restart  # only build and atomically replace artifacts, switch manually later
```

### 4.1 Hot Update (zero downtime, the default for install.sh deployments)

The principle is **blue-green with two instances**:

1. After pulling the code, build the new binary to a temp file and `mv` it into place atomically (the running old process keeps the old inode, so service is unaffected during the build; a direct `go build -o` over a running binary fails with `ETXTBSY`). The frontend builds to `dist.new` and then atomically swaps `dist` (nginx opens files by path, so there is no cache issue).
2. The new instance starts on the **standby port** (`8080`↔`8081` pair, carried by the `modernchat@<port>.service` template unit, both instances coexisting); proceed only after the health check passes.
3. Change the `upstream modernchat_backend` port in the nginx config → `nginx -t` → `nginx -s reload` (reload is zero-downtime for established connections; new requests go to the new instance).
4. `systemctl stop` the old instance: SIGTERM triggers the backend's **graceful shutdown** (in-flight HTTP requests finish in ≤10s, WS disconnects); online clients reconnect to the new instance automatically and backfill messages from the disconnection period incrementally by `clientMsgId`, so no message is lost.
5. Any failing step rolls back automatically: the new instance is stopped, the upstream is rolled back, and the old instance keeps serving.

State is recorded in `.hotupdate` at the repo root (`BASE_PORT` / `ACTIVE_PORT`), created by install.sh and maintained by update.sh.

### 4.2 Graphical Update (Site admin console → Online update)

The site admin console adds an "Online update" tab: check for updates → one-click update, with real-time logs shown in the tab throughout and status polled every 2 seconds while running. Two modes are supported (chosen automatically by deployment type):

| Mode | Trigger | Behavior |
|---|---|---|
| **Release** (install.sh default) | `server/.env` has `RELEASE_API_URL` / `RELEASE_DL_URL` | query the latest Release tag → download the prebuilt package → sha256 verify → atomically replace artifacts → restart. **No Go / Node / git on the server** |
| **Source** (git clone deployment) | the install directory has `.git` and no RELEASE_* configured | git pull → local build (Go / Node required on the server) → atomic replace → restart |

Equivalent to `scripts/update.sh` (release mode shares the same path as the graphical update: download a new Release, verify, replace, restart; source mode supports blue-green hot updates). Boundaries:

| Deployment | Available |
|---|---|
| install.sh (systemd template unit) | yes |
| Docker Compose | no (no toolchain or systemd inside the container; the UI says so) |
| Bare metal nohup | no (restart depends on systemd) |

Security boundaries: only the site admin (`middleware.Admin`) can trigger it; commands and URLs are all fixed — the version comes from the Release server / `origin/main` and never accepts user input; every trigger is written to the operation audit (`admin_audits`, action=`web_update`); the `WEB_UPDATE=false` environment variable disables the endpoints entirely. The endpoints are still essentially "the site admin account can trigger code updates" — a leaked account equals a controllable server: protect admin credentials, or set `WEB_UPDATE=false` when you do not trust multiple admins.

- **Legacy single-port deployments migrate automatically**: when an old `modernchat.service` (install.sh v1) is detected it is rewritten to the template unit and `.hotupdate` is written, so this upgrade goes through hot update.
- `--cold` forces the legacy `systemctl restart` (a few seconds of downtime; all WS connections reconnect).
- **Docker Compose deployments do not support hot updates yet**: `docker compose up -d --build` rebuilds the container with seconds of downtime (the frontend's reconnection mechanism covers it; no messages are lost).
- **Versions in Docker builds**: `docker compose` passes the `VERSION` / `COMMIT` / `BUILD_TIME` environment variables as build args to `server/Dockerfile` (falling back to `dev` / `unknown` when unset); verify with `curl http://127.0.0.1:8080/api/version` inside the container.
- **Docker build image sources are configurable**: the four build args `GO_IMAGE` / `RUNTIME_IMAGE` / `GOPROXY` / `APK_MIRROR` all default to official sources, so behavior is unchanged without arguments; see the Docker chapter of the README for China-network examples (verify third-party sources yourself).
- **Attachment persistence in containers**: `docker-compose.yml` does **not** mount a volume for `DATA_DIR` (default `data`) by default, so locally uploaded attachments are lost after `docker compose up -d --build` rebuilds the container. Production must mount a persistent volume or switch to S3; the `/uploads` static service itself works fine inside the container.
- `--no-restart`: only builds and atomically replaces artifacts. The running old process keeps the old version; the switch completes on the next `update.sh` run (without arguments).

The script: pulls Gitee → builds the backend (version injected) → builds the frontend → switches/restarts the service. Manual equivalent:

```bash
cd server
go build -ldflags "-X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=v1.0.0 \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=$(date -u +%FT%TZ)" \
  -o ../bin/modernchat-server ./cmd/server
```

The version is visible in the settings page "About" and `/api/version` (`scripts/update.sh` takes the version with `git describe --tags`, so releasing means tagging).

> **0.10.0 upgrade must-read (historical)**: ① run migration `0021_message_sender_index` (off-peak); ② nginx `client_max_body_size` ≥ **32 MB** (video default 30 MB); ③ "registration requires email verification" is on by default, with the first account exempt. Note: the "automatic fallback" when SMTP is unset was removed in later versions — it now returns 503 for everyone except the first account; see [Security & Migration Guide.md](安全与迁移指南.md) and the CHANGELOG "Unreleased".

---

## 5. Backup & Recovery

Two things must be backed up: the database + the upload directory.

```bash
# 1) Database (users, messages, plugin code, system settings, etc.)
mysqldump -uroot -p --single-transaction --default-character-set=utf8mb4 modernchat > backup_$(date +%F).sql

# 2) Uploaded images / voice / video (only needed with local storage)
tar czf uploads_$(date +%F).tar.gz -C server/data uploads

# Restore
mysql -uroot -p modernchat < backup_2026-10-25.sql
tar xzf uploads_2026-10-25.tar.gz -C server/data
```

- Recommended daily, retain 7–30 days; you can also click "Backup now" in the admin console "Backup" tab, or set `BACKUP_INTERVAL_HOURS` / `BACKUP_RETAIN` to let the service back up automatically (depends on `mysqldump`; use `MYSQLDUMP_PATH` when it is not on PATH)
- **Messages store only URLs** (`/uploads/2026/11/xxx.png`). Backing up only the database without `uploads/` means every image and voice 404s after a restore — the records are there, but the content cannot be opened
- **With object storage you do not need to back up `uploads/`** — the cloud provider handles it; the database backup is still mandatory
- `users.avatar` stores avatar data URLs (~20–40 KB each), which grows the DB; clean up as needed with more users
- Back up before upgrading (the script never touches data, but migration SQL alters schemas)
- **Migrations that add indexes must run off-peak**: e.g. `0021_message_sender_index` (indexes `messages.sender_id`). MySQL 8 Online DDL does not lock the table by default, but building an index on a large table still uses IO; with a very large message table, run it at low traffic and watch `SHOW PROCESSLIST`

### 5.1 One-time Notes on Migrations 0023 / 0024 / 0025

| Migration | Content | Impact | Rollback risk |
|---|---|---|---|
| `0023_message_content_text` | `messages.content` widened from `VARCHAR(1000)` to `TEXT` | supports up to 5000-character messages from the admin console; `ALTER` on a big table may take time | rolling back to `VARCHAR(1000)` truncates / fails; check `CHAR_LENGTH(content) > 1000` before rolling back |
| `0024_conversation_direct_key` | `conversations.direct_key` + unique index; `EnsureSingle` made transactional | concurrent direct-chat creation no longer produces duplicate conversations; existing direct chats stay `NULL` | drop the index then the column; the concurrent-duplicate problem returns |
| `0025_friend_email_unique` | unique indexes on `friends` / `email_codes`, including duplicate-friend cleanup | cleanup order is "accepted > pending > rejected, earliest id kept per status"; deleted rows are unrecoverable | you can only drop the index; dirty data does not come back — **backup is mandatory** |

Execution order: stop writes / off-peak → backup (full database recommended for 0025) → 0023 → 0024 → 0025 → start the new version (`MIGRATE=true` runs them in order automatically).
Run manually with `golang-migrate`; versions are recorded in `schema_migrations`; rollback order is reversed (0025 → 0024 → 0023).
Full rationale and verification items: [Security & Migration Guide.md](安全与迁移指南.md) §6.

> Never run `go test -tags integration ./test/` against a production database: the integration tests DELETE all business tables first.

---

## 6. Logs & Monitoring

- Structured logs (JSON lines), default to `server/logs/` (`LOG_DIR`) plus stdout
- Rotation & retention: `LOG_RETAIN_DAYS` (default 15 days)
- Key events:

| Log keyword | Meaning |
|---|---|
| `mail sent` / `mail send failed` | email delivery result |
| `password reset link` | without SMTP, the reset link only appears here (fetch from logs) |
| `ws slow consumer, closing conn` | a client consumed too slowly and was disconnected |
| `ws disconnected` | connection closed |
| `message recalled` | recall |
| `gorm` | SQL warnings (parameters redacted, values never printed) |

- Health check: `GET /api/health` (no auth)
- Version info: `GET /api/version`
- Prometheus: `GET /api/metrics` (requires `Authorization: Bearer <token>` when `METRICS_TOKEN` is non-empty), exporting route-level request counts and online connection counts

```bash
# See what a user did today
grep '"uid":12' server/logs/*.log | tail -50
# See rate limiting & security events
grep -E 'rate|ban|kick' server/logs/*.log | tail -30
```

---

## 7. FAQ

**Q1: A user says the reset email never arrived**
To send real email, configure `SMTP_HOST/PORT/USER/PASS/FROM`; `SMTP_FROM` must equal `SMTP_USER`. QQ / 163 use **auth codes**.

Without SMTP, the link is **no longer written to the log by default** (`LOG_RESET_LINK` is false in release mode, so a usable token in the log does not become an account-takeover entry point). For diagnosis, temporarily set `LOG_RESET_LINK=true` and restart, fetch the link, then turn it off immediately: `grep "password reset link" server/logs/*.log`.

**Q2: WebSocket won't connect, the console shows 403**
`FRONTEND_URL` doesn't include the actual domain. The browser Origin must match the whitelist **exactly** (no trailing slash). Also, nginx's `/ws` must have `Upgrade` / `Connection` headers.

**Q3: Logged in but instantly logged out / "token invalid"**
`JWT_SECRET` changes on every restart (production refuses to start unset; dev generates randomly). Production must configure it and keep it stable. Admin password resets bump `token_version`, forcing all devices offline.

**Q4: "Username taken", still taken with other names**
Usernames are unique and unchangeable (used for login). Nicknames may repeat.

**Q5: 429 / "too many requests"**
Rate limiting fired. Login, register, messaging and forgot-password each have a tier; wait for the window. **Multi-instance deployments need Redis**, otherwise each instance counts separately. Redis can be configured under "Admin console → System settings", but **a restart is required** for it to take effect (the limiter is created at startup).

**Q5.1: Where is SMTP configured? Does it need a restart?**
Two places; prefer **Admin console → System settings** (hot-applied on save, no restart):
- Configured in the admin console → the database wins and survives restarts;
- Never configured → use the `SMTP_*` environment variables (read at startup).

After configuring you can click "Send test email" to verify; on failure the raw error from the mail server is shown (e.g. `auth: 535 Login fail`), which is faster than digging through logs.

**Q5.2: SMTP stopped working / admin settings all empty after changing `JWT_SECRET`**
Secret fields (SMTP password, Redis password, object storage SecretKey) are stored encrypted with a key derived from `JWT_SECRET`, so **changing `JWT_SECRET` makes them undecryptable**. Re-enter the passwords in the admin console (other fields are unaffected). See §2.1.

**Q6: Plugin upload fails**
Check `PLUGIN_MAX_SIZE` (default 1 MB) and nginx `client_max_body_size` (often 1 MB by default, raise it too). Uploading the same name again counts as an **upgrade**.

**Q7: Forgot the username**
The system offers no username recovery. The site admin resets that user's password in the admin console and tells them offline.

**Q8: Can't access the admin console**
`/admin` requires `role=admin`. Only the **first registered account** is site admin. If the site admin account is lost, the only fix is flipping `users.role` to `admin` in the DB.

**Q9: Integration tests all SKIP yet show ok**
When integration tests can't reach MySQL they `t.Skip`, and `go test` still reports `ok` for skips — a **false green**. Correct approach: set `DB_DSN` to a test database (create it first: `CREATE DATABASE modernchat_test`), and use `scripts/coverage.sh` (Linux / macOS / Git Bash), which detects skips and warns.

**Q10: Pages still old after upgrade**
If nginx caches static assets, check whether `dist/assets` filename hashes changed; hard refresh the browser (Ctrl+F5). The frontend is pure static — restarting the backend doesn't refresh pages.

**Q11: Where are avatars stored, will they bloat the DB**
In `users.avatar` (data URL, 256×256 webp, ~20–40 KB each). Fine at current scale; move to file storage + URLs beyond a thousand users.

**Q12: The recovery email link doesn't open**
The link domain comes from `FRONTEND_URL`. In production it must be the real domain, otherwise users land on `localhost`.

**Q13: Plugin panel not showing in the chat area**
The plugin must be **enabled** in "Plugin manager"; plugins taken down globally by the site admin never load even when enabled.

---

## 8. Pre-launch Checklist

- [ ] `JWT_SECRET` set and ≥32 bytes (production refuses to start unset)
- [ ] `GIN_MODE=release`
- [ ] `FRONTEND_URL` includes the real domain, no trailing spaces
- [ ] `TRUSTED_PROXIES` matches the actual proxy ranges (keep loopback defaults if unsure)
- [ ] `SMTP_*` configured and **verified with a real test email** (or configured under "System settings" and verified with "Send test email")
- [ ] nginx has `gzip on` + `gzip_static on` (build artifacts are precompressed; first screen is about 65 KB)
- [ ] nginx `/ws` has Upgrade headers and `access_log off` (the token is in the query string, so default logs would leave a usable token)
- [ ] nginx sets `X-Frame-Options` / `X-Content-Type-Options` / `Referrer-Policy` (clickjacking and type-sniffing protection)
- [ ] `MIGRATE=false` (prod uses versioned SQL), or confirm migrations ran
- [ ] Backup strategy covers **the database + `DATA_DIR/uploads`** (the latter can be skipped with object storage)
- [ ] nginx `client_max_body_size` ≥ **32 MB** (video default cap is 30 MB; nginx's default 1 MB returns 413 outright. Raising the video cap requires raising this too; leave ~2 MB headroom)
- [ ] Firewall exposes only 80/443; backend 8080 reachable from localhost only
- [ ] `server/.env` not committed (`git check-ignore server/.env` prints a path)
