# ModernChat Server

Go backend (Gin + GORM + MySQL + gorilla/websocket). API contracts: [docs/后端需求规格.md](../docs/后端需求规格.md).

## Dependencies

- Go: per the `go` directive in `go.mod` (the current dependency chain requires 1.26+)
- MySQL 8.0 (utf8mb4)

> Production must set `JWT_SECRET` (release mode refuses to start without it) and `FRONTEND_URL` (WS Origin whitelist + recovery links).

## Running in ten minutes

```bash
# 1. Start MySQL (Docker example)
docker run -d --name modernchat-mysql \
  -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=modernchat \
  -p 3306:3306 mysql:8.0

# 2. Configure
cd server
cp .env.example .env       # the default DSN matches the Docker MySQL above

# 3. Run (dev: AutoMigrate creates tables)
go run ./cmd/server        # listens on :8080

# 4. Verify
curl http://localhost:8080/api/health    # {"status":"ok"}
curl http://localhost:8080/api/version   # {"version":"dev","commit":"unknown",...}
```

Frontend integration: `cd web && npm run dev` (Vite proxies `/api` and `/ws` to `:8080`).
The first registered account automatically becomes the site admin, with access to the frontend's `/admin` (user management / global plugin takedown).

## Configuration

All environment variables are listed in [`.env.example`](.env.example); the key ones:

| Key | Notes |
|---|---|
| `JWT_SECRET` | ≥32 bytes; release mode refuses to start without it |
| `FRONTEND_URL` | frontend origins, comma-separated; WS Origin whitelist + recovery links |
| `MIGRATE` | `true` = versioned SQL from `migrations/` (prod); `false` = AutoMigrate (dev) |
| `PLUGIN_MAX_SIZE` | plugin file cap, default 1MB |
| `LOG_DIR` | log directory, default `logs` (relative to the working directory); empty = stdout only |
| `LOG_RETAIN_DAYS` | log retention days, default **15**; expired files auto-deleted |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | optional. When set, rate limiting uses a Redis shared quota (multi-instance); **on connection failure it falls back to the in-memory limiter with a WARN log**, startup unaffected |
| `SMTP_*` | optional; when empty, recovery links print to logs. Port `465` = implicit TLS (QQ/163 recommended), `587` = STARTTLS; QQ/163 need an **auth code**, and `SMTP_FROM` must equal `SMTP_USER` |
| `LOG_RESET_LINK` | whether the full reset link is written to logs when SMTP is not configured; defaults to `false` in release mode (a usable token in logs is an account-takeover entry point) |
| `DATA_DIR` | upload directory, default `data`; images / voice / video / group files land in `DATA_DIR/uploads/<year>/<month>/`. With S3-compatible object storage configured in the admin console, new files go to the bucket and this directory only serves legacy URLs |
| `TRUSTED_PROXIES` | trusted reverse-proxy CIDRs, default `127.0.0.1,::1`; an explicit empty string = trust no proxy |
| `METRICS_TOKEN` | when non-empty, `/api/metrics` requires a Bearer token |
| `BACKUP_DIR` / `BACKUP_INTERVAL_HOURS` / `BACKUP_RETAIN` / `MYSQLDUMP_PATH` | database backup directory / automatic interval (0 = off) / number of copies retained / mysqldump path |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` / `VAPID_SUBJECT` | Web Push (optional); enabled only when both keys are configured |
| `WEB_UPDATE` / `RELEASE_API_URL` / `RELEASE_DL_URL` | site-admin console online-update switch and release update source (written by `install.sh`) |

## Logs

- **Dual channel**: stdout (for containers/service managers) plus `logs/app-YYYYMMDD.log` (auto-switched daily, append-only).
- **Auto cleanup**: once at startup, then hourly; only deletes `app-*.log` files in that directory older than `LOG_RETAIN_DAYS` (default 15 days) — nothing else is touched.
- **Level conventions**:
  - `info`: business actions (register, login success, friend relation changes, conversation create/join/leave/transfer, plugin install/upgrade/enable/disable/uninstall, message recall)
  - `warn`: security & anomaly signals (login failures, bans, rate-limit hits, WS Origin refusals, WS auth failures, Redis fallback, slow requests >500ms, 4xx requests)
  - `error`: server faults (5xx requests, DB write failures)
  - `debug`: message persistence metadata (conv/msg/sender/length)
- **Privacy boundaries**: no passwords, tokens, or message contents; login failures log only the username & IP.
- Query examples:

```bash
# See what user 42 did today
grep '"uid":42' logs/app-$(date +%Y%m%d).log | jq -r '.msg + " " + (.path // "")'

# Rate limiting / security events
grep -E '"level":"WARN"' logs/app-*.log | jq -r 'select(.msg=="rate limited") | .key'
```

Startup reads a same-directory `.env` (gitignored); real environment variables win over `.env`.
The startup log states the active limiter: `rate limiting backed by redis` or `redis unavailable, falling back to in-memory limiter`.

## Production Deployment

1. Env vars: `JWT_SECRET`, `DB_DSN`, `GIN_MODE=release`, `MIGRATE=true`, `FRONTEND_URL`
2. Tables: `MIGRATE=true` runs `migrations/` at startup (or the golang-migrate CLI manually)
3. Build: `go build -o modernchat-server ./cmd/server` (version injection below)
4. Nginx: see `../deploy/nginx.conf.example` (`/api` proxy + `/ws` upgrade)
5. Shutdown: `SIGTERM` drains connections within 10s

### Version injection & upgrades

Versions are injected at build time to verify upgrades:

```bash
go build -trimpath \
  -ldflags "-X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=$(git describe --tags --always) \
            -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=$(git rev-parse --short HEAD) \
            -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o modernchat-server ./cmd/server
```

Day-to-day upgrades need no hand-typing: run `../scripts/update.sh` from the repo root (pull Gitee → build both ends → restart; see the root README).

## Testing

```bash
go test ./...                          # unit tests (manifest/SSRF rules/rate limit/validation/frame utils)
go test -tags integration ./test/      # integration tests: needs local MySQL (reads DB_DSN, default local 3306)
go vet ./... && gofmt -l .             # static checks
```

Integration coverage: register (first registrant = site admin) → friend apply/accept → direct-chat idempotency → WS send/receive → `clientMsgId` resend idempotency → read receipts → recall → unread → groups/join/members → ban & kick → plugin upload/same-name upgrade/code serving/SSRF refusal/global takedown.

> **Pitfall**: when integration tests can't reach MySQL they `t.Skip`, and `go test` still returns `ok` for skips — looks "passed" while nothing ran (coverage near 0). The script below detects and warns.

### Coverage (unit + integration merged)

```bash
# Linux / macOS / Git Bash (in server/)
DB_DSN='root:password@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local' ../scripts/coverage.sh

# HTML report
go tool cover -html=cover.out -o coverage.html
```

Key points:
- Create the integration DB first: `CREATE DATABASE modernchat_test CHARACTER SET utf8mb4;`
- Coverage must use `-coverpkg=./internal/...` to name measured packages, otherwise `go test ./...` only reports "each package's own unit tests" (store/handler falsely show 0%)
- Current baseline: **53.8% overall** (config 96%, version 100%, router 97%, service 82%, model 100%, ws 70%, plugin 67%, middleware 67%, store 62%, handler 44%, logx 47%)
- Thinnest are `handler` (mostly covered by integration tests) and `logx` (log rotation, filesystem-bound)

## Structure

```
cmd/server/main.go        entry: config → db → hub → router → graceful shutdown
internal/config           env parsing
internal/version          build version (ldflags injection)
internal/model            GORM models + response views (ConversationView / PluginView etc.)
internal/store            data access (user/friend/conversation/message/moderation/plugin/setting/push…)
                          + migrations runner (RunMigrations) + secretbox (AES-GCM) + Redis
internal/handler          HTTP (auth/user/friend/conversation/moderation/plugin/admin/
                          admin_settings/upload/file/sticker/push/ops/site/health/version)
internal/middleware       logger / jwt / ban / admin / limiter / maintenance / guard
internal/plugin           manifest parsing & validation + esbuild TS transpile + URL fetch (SSRF: per-hop public-IP checks)
internal/ws               hub(64 shards) / serve(handshake+read-write pumps, 64KB uplink) / dispatch(uplink frames) / presence
internal/service          mailer / storage (local + S3) / thumbnail / push / backup / updater
internal/logx metrics     log rotation / Prometheus metrics
internal/router           all routes & middleware wiring
migrations/               versioned SQL (0001_init … 0025_friend_email_unique)
test/                     integration tests (//go:build integration)
Dockerfile                multi-stage build (incl. migrations)
```

## Contract with the frontend

- JSON fields always camelCase; times ISO8601 UTC
- REST request/response shapes: `docs/后端需求规格.md` §3.2 (item-by-item aligned with `web/src/api/index.ts`)
- WS frames: `web/src/types/proto.ts`'s `ClientFrame` / `ServerFrame`
- Errors are always `{"error": "<Chinese message>"}`; 401 makes the frontend jump to login
- Plugin upload / URL install returns `X-Plugin-Updated: true` on a same-name hit (upgrade overwrite)
- Attachments: login is required for `GET /uploads/**` (`Authorization: Bearer` or the same-origin `mc_auth` cookie); directory listing is 404; `Cache-Control: private, max-age=3600` plus `nosniff`; in S3 mode new files are served by the bucket/CDN, with access control delegated there
- WS: the handshake accepts `?token=` and falls back to the `mc_auth` cookie; **every frame** re-checks ban status and `token_version` before routing, and a failure returns an `error` frame and closes the connection
