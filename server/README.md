# ModernChat Server

Go 后端（Gin + GORM + MySQL + gorilla/websocket）。接口契约见 [docs/后端需求规格.md](../docs/后端需求规格.md)。

## 依赖

- Go：以 `go.mod` 的 `go` 指令为准（当前依赖链要求 1.26+）
- MySQL 8.0（utf8mb4）

> 生产环境必须设置 `JWT_SECRET`（`GIN_MODE=release` 时缺省会拒绝启动）与 `FRONTEND_URL`（WS Origin 白名单 + 找回密码链接）。

## 十分钟跑起来

```bash
# 1. 启动 MySQL（Docker 示例）
docker run -d --name modernchat-mysql \
  -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=modernchat \
  -p 3306:3306 mysql:8.0

# 2. 配置
cd server
cp .env.example .env       # 默认 DSN 即对应上面的 Docker MySQL

# 3. 运行（开发：AutoMigrate 自动建表）
go run ./cmd/server        # 监听 :8080

# 4. 验证
curl http://localhost:8080/api/health    # {"status":"ok"}
curl http://localhost:8080/api/version   # {"version":"dev","commit":"unknown",...}
```

前端联调：`cd web && npm run dev`（Vite 已代理 `/api` 与 `/ws` 到 `:8080`）。
第一个注册的账号自动成为站长，可访问前端 `/admin`（用户管理 / 插件全局下架）。

## 配置

全部环境变量见 [`.env.example`](.env.example)，关键项：

| 键 | 说明 |
|---|---|
| `JWT_SECRET` | ≥32 字节；release 模式缺省即拒绝启动 |
| `FRONTEND_URL` | 前端来源，逗号分隔；用于 WS Origin 白名单与找回密码链接 |
| `MIGRATE` | `true` 走 `migrations/` 版本化 SQL（生产）；`false` 用 AutoMigrate（开发） |
| `PLUGIN_MAX_SIZE` | 插件文件上限，默认 1MB |
| `LOG_DIR` | 日志目录，默认 `logs`（相对运行目录）；留空则只输出 stdout |
| `LOG_RETAIN_DAYS` | 日志保留天数，默认 **15**；超期文件自动删除 |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | 可选。配置后限流走 Redis 共享配额（支持多实例）；**连接失败自动降级为内存限流并打 WARN 日志**，不影响启动 |
| `SMTP_*` | 可选；留空时找回密码链接打印到日志。端口 `465` 走隐式 TLS（QQ/163 推荐）、`587` 走 STARTTLS；QQ/163 需用 **SMTP 授权码**，且 `SMTP_FROM` 必须与 `SMTP_USER` 一致 |

## 日志

- **双通道**：同时写 stdout（容器/服务管理器）与 `logs/app-YYYYMMDD.log`（按天自动切换，追加写入）。
- **自动清理**：启动时清理一次，之后每小时检查；只删除本目录内 `app-*.log` 且日期超过 `LOG_RETAIN_DAYS`（默认 15 天）的文件，不会误删其他内容。
- **级别约定**：
  - `info`：业务动作（注册、登录成功、好友关系变更、会话创建/加入/退出/转让、插件安装/升级/启停/卸载、消息撤回）
  - `warn`：安全与异常信号（登录失败、封禁、限流命中、WS Origin 拒绝、WS 鉴权失败、Redis 降级、慢请求 >500ms、4xx 请求）
  - `error`：服务端故障（5xx 请求、数据库写失败）
  - `debug`：消息落库元数据（conv/msg/sender/字数）
- **隐私边界**：不记录密码、令牌、消息正文；登录失败只记用户名与 IP。
- 排查示例：

```bash
# 看某个用户当天做了什么
grep '"uid":42' logs/app-$(date +%Y%m%d).log | jq -r '.msg + " " + (.path // "")'

# 看限流/安全事件
grep -E '"level":"WARN"' logs/app-*.log | jq -r 'select(.msg=="rate limited") | .key'
```

启动时会读取同目录的 `.env`（已被 gitignore）；同名环境变量优先于 `.env`。
启动日志会明确输出当前限流后端：`rate limiting backed by redis` 或 `redis unavailable, falling back to in-memory limiter`。

## 生产部署

1. 环境变量：`JWT_SECRET`、`DB_DSN`、`GIN_MODE=release`、`MIGRATE=true`、`FRONTEND_URL`
2. 建表：`MIGRATE=true` 启动时自动执行 `migrations/`（也可用 golang-migrate CLI 手动执行）
3. 构建：`go build -o modernchat-server ./cmd/server`（版本注入见下）
4. Nginx：参考 `../deploy/nginx.conf.example`（`/api` 反代 + `/ws` 升级）
5. 停机：`SIGTERM` 后 10s 内排空连接

### 版本注入与升级

构建时注入版本，便于核对升级是否生效：

```bash
go build -trimpath \
  -ldflags "-X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=$(git describe --tags --always) \
            -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=$(git rev-parse --short HEAD) \
            -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o modernchat-server ./cmd/server
```

日常升级不用手敲：仓库根目录执行 `../scripts/update.sh`（拉 Gitee → 构建前后端 → 重启，详见根 README）。

## 测试

```bash
go test ./...                          # 单元测试（manifest/SSRF 规则/限流/校验/帧工具）
go test -tags integration ./test/      # 集成测试：需要本机 MySQL（读 DB_DSN，默认本地 3306）
go vet ./... && gofmt -l .             # 静态检查
```

集成测试覆盖：注册（首注册者=站长）→ 好友申请/同意 → 单聊幂等 → WS 收发 → `clientMsgId` 重发幂等 → 已读回执 → 撤回 → 未读数 → 群聊/加入/成员 → 封禁踢线 → 插件上传/同名升级/代码下发/SSRF 拒绝/全局下架。

> **坑**：集成测试连不上 MySQL 时会 `t.Skip`，而 `go test` 对 SKIP 仍返回 `ok` —— 看上去"通过"，实际一条都没跑（覆盖率接近 0）。用下面的脚本会自动检测并告警。

### 覆盖率（单测 + 集成合并）

```bash
# Windows（在 server/ 目录）
$env:DB_DSN = 'root:密码@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local'
..\scripts\coverage.ps1

# Linux / macOS
DB_DSN='root:密码@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local' ../scripts/coverage.sh

# HTML 报告
go tool cover -html=cover.out -o coverage.html
```

要点：
- 集成测试库需提前创建：`CREATE DATABASE modernchat_test CHARACTER SET utf8mb4;`
- 覆盖率必须用 `-coverpkg=./internal/...` 指定被统计的包，否则 `go test ./...` 只报"各包自己单测"的数字（store/handler 会假性显示 0%）
- 当前基线：**总计 53.8%**（config 96%、version 100%、router 97%、service 82%、model 100%、ws 70%、plugin 67%、middleware 67%、store 62%、handler 44%、logx 47%）
- 覆盖较薄弱的是 `handler`（主要靠集成测试覆盖）与 `logx`（日志轮转，依赖文件系统）

## 结构

```
cmd/server/main.go        入口：config → db → hub → router → 优雅停机
internal/config           环境变量解析
internal/version          构建版本（ldflags 注入）
internal/model            GORM 模型 + 响应视图（ConversationView / PluginView 等）
internal/store            数据访问（user/friend/conversation/message/plugin/password_reset）
                          + migrations 执行器（RunMigrations）
internal/handler          HTTP（auth/user/friend/conversation/plugin/admin/health/version）
internal/middleware       logger / jwt / ban / admin / limiter
internal/plugin           manifest 解析校验 + URL 拉取（SSRF：逐跳公网 IP 校验）
internal/ws               hub(64 分片) / serve(握手+读写 pump) / dispatch(上行帧) / presence
internal/service          mailer（SMTP 可选，缺省打日志）
migrations/               版本化 SQL（0001_init.up/down）
test/                     集成测试（//go:build integration）
Dockerfile                多阶段构建（含 migrations）
```

## 与前端的契约

- JSON 字段一律 camelCase；时间 ISO8601 UTC
- REST 请求/响应形状：`docs/后端需求规格.md` §3.2（与 `web/src/api/index.ts` 逐项对齐）
- WS 帧：`web/src/types/proto.ts` 的 `ClientFrame` / `ServerFrame`
- 错误统一 `{"error": "<中文消息>"}`，401 会触发前端跳转登录
- 插件上传/URL 安装命中同名时返回 `X-Plugin-Updated: true`（升级覆盖）
