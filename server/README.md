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
| `SMTP_*` | 可选；留空时找回密码链接打印到日志 |

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
