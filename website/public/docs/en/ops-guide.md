# ModernChat Ops Manual

> For **deployment & maintenance**: environment variables, deployment shapes, backup & upgrade, logs, troubleshooting.
> API contracts: [后端需求规格.md](后端需求规格.md); frontend: [前端技术方案.md](前端技术方案.md).

---

## 1. Deployment Shape

**Technical premises**: single-instance self-hosting. MySQL 8.0 is the only required dependency; Redis (shared rate-limit quota) and SMTP (recovery emails) are **optional** and degrade gracefully when absent.

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
| `JWT_SECRET` | none | **yes** (prod) | signing key, **≥32 bytes**; production refuses to start unset. Generate: `openssl rand -hex 32` |
| `MIGRATE` | `false` | no | `true` runs `migrations/` at startup (prod recommended to run versioned SQL manually) |
| `FRONTEND_URL` | `http://localhost:5173` | **yes** | allowed frontend origins, comma-separated. Used for the **WS Origin whitelist** and the site base of recovery links |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | no | trusted reverse-proxy ranges; **setting it to an empty string = trust no proxy** (XFF is then not used to derive client IPs) |
| `PLUGIN_MAX_SIZE` | `1048576` (1 MB) | no | plugin upload size cap (bytes) |
| `LOG_LEVEL` | `info` | no | `debug` / `info` / `warn` / `error` |
| `LOG_DIR` | `logs` | no | log directory; empty = stdout only |
| `LOG_RETAIN_DAYS` | `15` | no | log retention days |
| `REDIS_ADDR` | empty | no | when set, rate limiting uses a Redis shared quota (multi-instance); connection failures fall back to memory automatically |
| `REDIS_PASSWORD` | empty | no | Redis password |
| `REDIS_DB` | `0` | no | Redis DB number |
| `SMTP_HOST` | empty | no | when empty, recovery links only go to the server log (**users get no email**) |
| `SMTP_PORT` | `587` | no | `465` = implicit TLS (QQ/163 recommended); `587` = STARTTLS |
| `SMTP_USER` | empty | no | login user; QQ/163 use an **auth code**, not the account password |
| `SMTP_PASS` | empty | no | auth code / password |
| `SMTP_FROM` | `modernchat@localhost` | no | sender, **QQ/163 require it to equal `SMTP_USER`**, otherwise mail is rejected |

> `FRONTEND_URL` notes: comma-separated, each item **without trailing spaces**; a trailing slash is fine (the backend strips it).
> Example: `FRONTEND_URL=https://chat.example.com,https://intranet.example.com`

---

## 3. First Deployment

```bash
# 1. Pull the code
git clone https://gitee.com/Sun-Si-Cheng/modern-chat-new.git && cd modernchat-new

# 2. Prepare the database
mysql -uroot -p -e "CREATE DATABASE modernchat CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# 3. Configure
cp server/.env.example server/.env
# Edit server/.env: at least DB_DSN / JWT_SECRET / FRONTEND_URL

# 4. Build & start
cd server && go build -o ../bin/modernchat-server ./cmd/server   # add .exe on Windows
../bin/modernchat-server

# 5. Build the frontend and hand it to nginx
cd ../web && npm ci && npm run build     # output in web/dist

# 6. Verify
curl http://127.0.0.1:8080/api/health     # {"status":"ok"}
curl http://127.0.0.1:8080/api/version
```

**The first registered account automatically becomes the site admin** (`role=admin`); everyone after is a regular user. Registration entry: "Register now" on the login page.

---

## 4. Upgrading Versions

```bash
# Linux / macOS
./scripts/update.sh
# Windows
.\scripts\update.ps1
```

The script: pulls Gitee → builds the backend (version injected) → builds the frontend → restarts. Manual equivalent:

```bash
cd server
go build -ldflags "-X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=v1.0.0 \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=$(date -u +%FT%TZ)" \
  -o ../bin/modernchat-server ./cmd/server
```

The version is visible in the settings page "About" and `/api/version`.

---

## 5. Backup & Recovery

All data lives in MySQL (plugin code included), **backup = back up the database**:

```bash
# Backup
mysqldump -uroot -p --single-transaction --default-character-set=utf8mb4 modernchat > backup_$(date +%F).sql

# Restore
mysql -uroot -p modernchat < backup_2026-10-25.sql
```

- Recommended daily, retain 7–30 days
- `users.avatar` stores avatar data URLs (~20–40 KB each), which grows the DB; clean up as needed or move to file storage with more users
- Back up before upgrading (the script never touches data, but migration SQL alters schemas)

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

```bash
# See what a user did today
grep '"uid":12' server/logs/*.log | tail -50
# See rate limiting & security events
grep -E 'rate|ban|kick' server/logs/*.log | tail -30
```

---

## 7. FAQ

**Q1: A user says the reset email never arrived**
Without SMTP configured, the link is only written to the server log (`grep "password reset link" server/logs/*.log`). To send real email, configure `SMTP_HOST/PORT/USER/PASS/FROM`; `SMTP_FROM` must equal `SMTP_USER`. QQ/163 use **auth codes**.

**Q2: WebSocket won't connect, the console shows 403**
`FRONTEND_URL` doesn't include the actual domain. The browser Origin must match the whitelist **exactly** (no trailing slash). Also, nginx's `/ws` must have `Upgrade` / `Connection` headers.

**Q3: Logged in but instantly logged out / "token invalid"**
`JWT_SECRET` changes on every restart (production refuses to start unset; dev generates randomly). Production must configure it and keep it stable. Admin password resets bump `token_version`, forcing all devices offline.

**Q4: "Username taken", still taken with other names**
Usernames are unique and unchangeable (used for login). Nicknames may repeat.

**Q5: 429 / "too many requests"**
Rate limiting fired. Login, register, messaging and forgot-password each have a tier; wait for the window. **Multi-instance deployments need Redis**, otherwise each instance counts separately.

**Q6: Plugin upload fails**
Check `PLUGIN_MAX_SIZE` (default 1 MB) and nginx `client_max_body_size` (often 1 MB by default, raise it too). Uploading the same name again counts as an **upgrade**.

**Q7: Forgot the username**
The system offers no username recovery. The admin resets that user's password in the console and tells them offline.

**Q8: Can't access the console**
`/admin` requires `role=admin`. Only the **first registered account** is admin. If the admin account is lost, the only fix is flipping `users.role` to `admin` in the DB.

**Q9: Integration tests all SKIP yet show ok**
When integration tests can't reach MySQL they `t.Skip`, and `go test` still reports `ok` for skips — a **false green**. Correct approach: set `DB_DSN` to a test database (create it first: `CREATE DATABASE modernchat_test`), and use `scripts/coverage.ps1` / `coverage.sh`, which detect skips and warn.

**Q10: Pages still old after upgrade**
If nginx caches static assets, check whether `dist/assets` filename hashes changed; hard refresh the browser (Ctrl+F5). The frontend is pure static — restarting the backend doesn't refresh pages.

**Q11: Where are avatars stored, will they bloat the DB**
In `users.avatar` (data URL, 256×256 webp, ~20–40 KB each). Fine at current scale; move to file storage + URLs beyond a thousand users.

**Q12: The recovery email link doesn't open**
The link domain comes from `FRONTEND_URL`. In production it must be the real domain, otherwise users land on `localhost`.

**Q13: Plugin panel not showing in the chat area**
The plugin must be **enabled** in "Plugin manager"; plugins taken down globally never load even when enabled.

---

## 8. Pre-launch Checklist

- [ ] `JWT_SECRET` set and ≥32 bytes (production refuses to start unset)
- [ ] `GIN_MODE=release`
- [ ] `FRONTEND_URL` includes the real domain, no trailing spaces
- [ ] `TRUSTED_PROXIES` matches the actual proxy ranges (keep loopback defaults if unsure)
- [ ] `SMTP_*` configured and **verified with a real test email**
- [ ] nginx `/ws` has Upgrade headers
- [ ] `MIGRATE=false` (prod uses versioned SQL), or confirm migrations ran
- [ ] Firewall exposes only 80/443; backend 8080 reachable from localhost only
- [ ] Database has scheduled backups
- [ ] `server/.env` not committed (`git check-ignore server/.env` prints a path)
