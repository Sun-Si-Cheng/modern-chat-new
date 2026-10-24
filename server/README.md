# ModernChat Server

Go 后端（Gin + GORM + MySQL + gorilla/websocket）。接口契约见 [docs/后端需求规格.md](../docs/后端需求规格.md)。

## 依赖

- Go 1.22+
- MySQL 8.0（utf8mb4）

## 十分钟跑起来

```bash
# 1. 启动 MySQL（Docker 示例）
docker run -d --name modernchat-mysql ^
  -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=modernchat ^
  -p 3306:3306 mysql:8.0

# 2. 配置
cd server
copy .env.example .env     # 默认 DSN 即对应上面的 Docker MySQL

# 3. 运行（开发：AutoMigrate 自动建表）
go run ./cmd/server        # 监听 :8080

# 4. 验证
curl http://localhost:8080/api/health
```

前端联调：`cd web && npm run dev`（Vite 已代理 `/api` 与 `/ws` 到 `:8080`）。
第一个注册的账号自动成为站长，可访问前端 `/admin` 页面。

## 生产部署

1. 环境变量：`JWT_SECRET`（`openssl rand -hex 32`）、`DB_DSN`、`GIN_MODE=release`、`MIGRATE=true`
2. 建表：`MIGRATE=true` 时启动自动执行 `migrations/` 版本化 SQL（也可用 golang-migrate CLI 手动执行）
3. 构建：`go build -o modernchat-server ./cmd/server`
4. Nginx：参考 `../deploy/nginx.conf.example`（`/api` 反代 + `/ws` 升级）
5. 停机：`SIGTERM` 后 10s 内排空连接

## 测试

```bash
go test ./...                        # 单元测试（纯逻辑：manifest/SSRF/限流key/校验）
go test -tags integration ./...      # 集成测试（需要本机 MySQL，读 DB_DSN）
go vet ./... && gofmt -l .           # 静态检查
```

## 结构

```
cmd/server/main.go        入口：config → db → hub → router → 优雅停机
internal/config           环境变量解析（.env.example 全键注释）
internal/model            GORM 模型 + 响应视图（ConversationView 等）
internal/store            数据访问（user/friend/conversation/message/plugin/password_reset）
internal/handler          HTTP 处理（auth/user/friend/conversation/plugin/admin/health）
internal/middleware       logger / jwt / ban / admin / ratelimit
internal/plugin           manifest 解析校验 + URL 拉取（SSRF 防护）
internal/ws               hub(64 分片) / serve(读写 pump) / dispatch(上行帧路由) / presence
internal/service          mailer（SMTP 可选，缺省打日志）
migrations/               版本化 SQL（0001_init）
```

## 与前端的契约

- JSON 字段一律 camelCase；时间 ISO8601 UTC
- REST 请求/响应形状：`docs/后端需求规格.md` §3.2（与 `web/src/api/index.ts` 逐项对齐）
- WS 帧：`web/src/types/proto.ts` 的 `ClientFrame` / `ServerFrame`
- 错误统一 `{"error": "<中文消息>"}`，401 会触发前端跳转登录
