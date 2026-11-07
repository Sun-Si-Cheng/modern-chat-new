# ModernChat 运维手册

> 面向**部署与维护**的人：环境变量、部署形态、备份升级、日志与排障。
> 接口契约见 [后端需求规格.md](后端需求规格.md)，前端见 [前端技术方案.md](前端技术方案.md)。

---

## 1. 部署形态

**技术前提**：单实例自托管。MySQL 8.0 是唯一必需依赖；Redis（限流共享配额）与 SMTP（找回密码邮件）都是**可选**，缺省时自动降级。

```
浏览器 ──HTTPS──▶ nginx（静态资源 + /api 反代 + /ws 升级）
                        │
                        └──▶ modernchat-server（:8080）──▶ MySQL 8.0
```

配置样例：

- `deploy/nginx.conf.example` —— 裸机部署（systemd + nginx）
- `deploy/nginx.compose.conf` —— Docker Compose 部署

**nginx 必须为 `/ws` 配置 Upgrade 头**，否则 WebSocket 握手失败（见 §7）。

---

## 2. 环境变量全表

后端从环境变量读取全部配置（`server/internal/config/config.go`），`.env` 文件在 `server/` 目录（已被 gitignore，**不要提交**）。

| 变量 | 默认值 | 必填 | 说明 |
|---|---|---|---|
| `PORT` | `8080` | 否 | 后端监听端口 |
| `GIN_MODE` | `release` | 否 | 生产固定 `release`；`debug` 时允许自动生成随机 `JWT_SECRET` |
| `DB_DSN` | `root:root@tcp(127.0.0.1:3306)/modernchat?...` | **是** | MySQL 连接串，建议带 `utf8mb4&parseTime=True&loc=Local` |
| `JWT_SECRET` | 无 | **是**（生产） | 签名密钥，**≥32 字节**；生产未设置会拒绝启动。生成：`openssl rand -hex 32`。**更换它会导致后台已保存的 SMTP / Redis / 对象存储密钥无法解密**（详见 §2.1） |
| `MIGRATE` | `false` | 否 | `true` 时启动自动执行 `migrations/`（生产建议用版本化 SQL 手工执行） |
| `FRONTEND_URL` | `http://localhost:5173` | **是** | 允许的前端来源，逗号分隔。用于 **WS Origin 白名单**与找回密码链接里的站点地址 |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | 否 | 可信反向代理网段；**显式设为空串 = 不信任任何代理**（此时不使用 XFF 判定来源 IP） |
| `PLUGIN_MAX_SIZE` | `1048576`（1 MB） | 否 | 插件上传大小上限（字节） |
| `LOG_LEVEL` | `info` | 否 | `debug` / `info` / `warn` / `error` |
| `LOG_DIR` | `logs` | 否 | 日志目录；留空则只输出 stdout |
| `DATA_DIR` | `data` | 否 | 数据目录，上传的图片 / 语音 / 视频存于 `DATA_DIR/uploads/年/月/`。**用对象存储时可忽略**（文件不落本地） |
| `LOG_RETAIN_DAYS` | `15` | 否 | 日志保留天数 |
| `REDIS_ADDR` | 空 | 否 | 配置后限流使用 Redis 共享配额（多实例）；连接失败自动降级为内存。**可在后台改**（需重启） |
| `REDIS_PASSWORD` | 空 | 否 | Redis 密码。**可在后台改**（需重启，加密存储） |
| `REDIS_DB` | `0` | 否 | Redis 库号。**可在后台改**（需重启） |
| `SMTP_HOST` | 空 | 否 | 留空时找回密码链接只打印到服务端日志（**用户收不到邮件**）。**可在后台改** |
| `SMTP_PORT` | `587` | 否 | `465` = 隐式 TLS（QQ / 163 推荐）；`587` = STARTTLS 升级。**可在后台改** |
| `SMTP_USER` | 空 | 否 | 登录用户名；QQ / 163 填**授权码**不是登录密码。**可在后台改** |
| `SMTP_PASS` | 空 | 否 | 授权码 / 密码。**可在后台改**（加密存储） |
| `SMTP_FROM` | `modernchat@localhost` | 否 | 发件人，**QQ / 163 必须与 `SMTP_USER` 一致**，否则被拒信。**可在后台改** |
| `LOG_RESET_LINK` | release 下 `false`，其他模式 `true` | 否 | 未配置 SMTP 时是否把**完整重置链接**写进日志。生产默认关闭：日志里的可用令牌等于账户接管入口（日志平台/备份都能看到）。确实要靠日志取链接时再显式开 |
| `WEB_UPDATE` | `true` | 否 | 站长后台「在线更新」开关。`false` 时 `/api/admin/update/*` 全部不可用；Docker / 裸机部署自动不可用（见 §4.2） |
| `RELEASE_API_URL` / `RELEASE_DL_URL` | 空 | 否 | 发行版模式：Release 最新版本 API 与下载前缀，由 `install.sh` 写入 `.env`；两值为空时按源码 git 模式处理 |
| `METRICS_TOKEN` | 空 | 否 | 非空时 `GET /api/metrics` 需 `Authorization: Bearer <token>`；留空则同站可匿名抓取，公网部署建议设置 |
| `BACKUP_DIR` | `<DATA_DIR>/backups` | 否 | 数据库备份输出目录 |
| `BACKUP_INTERVAL_HOURS` | `0` | 否 | 自动备份间隔小时数，`0` = 只保留后台手动「立即备份」 |
| `BACKUP_RETAIN` | `7` | 否 | 自动备份保留份数（超出后删最旧） |
| `MYSQLDUMP_PATH` | `mysqldump` | 否 | `mysqldump` 可执行文件路径（不在 PATH 时填绝对路径） |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` | 空 | 否 | Web Push 密钥对；两项都配置才启用，未配置时推送静默跳过 |
| `VAPID_SUBJECT` | `mailto:admin@localhost` | 否 | Web Push 联系邮箱（`mailto:` 前缀） |

> `FRONTEND_URL` 写法注意：逗号分隔、每项**不要带尾随空格**，可以带结尾斜杠（后端会去掉）。
> 例：`FRONTEND_URL=https://chat.example.com,https://intranet.example.com`

### 2.1 后台可配置项（数据库覆盖环境变量）

站长在「后台 → 系统设置」页签里改的值会写入 `system_settings` 表，**优先级高于环境变量**：

| 优先级 | 来源 | 何时生效 |
|---|---|---|
| 高 | 后台「系统设置」 | SMTP：**保存即热生效**；Redis：需重启；**站点 / 聊天规则 / 上传上限 / 登录安全：即时生效（无需重启）** |
| 低 | 环境变量 | 启动时读取 |

所以环境变量仍可作为兜底（容器化部署不改数据库也能跑），但**只要后台配过，就以数据库为准**。启动日志里会打印 `smtp config loaded from database` 表示走了数据库配置。

**可运营参数一览**（后台改完立即生效，无需重启）：

| 分组 | 配置项 | 范围 / 默认 | 生效位置 |
|---|---|---|---|
| 站点 | 站点名称 | ≤40 字，默认 `ModernChat` | 浏览器标题、登录页 |
| 站点 | 开放注册 | 开 / 关（默认开） | 注册接口 403、登录页隐藏入口 |
| 站点 | 维护模式 + 提示文案 | 默认关 | 非管理员 503 + 维护页（见下） |
| 站点 | 注册需邀请码 | 默认关 | 注册必须携带有效邀请码 |
| 站点 | 注册需邮箱验证 | 默认开（需 SMTP） | 注册页出现邮箱（必填）+ 验证码；同邮箱 60 秒冷却、10 分钟有效、连错 5 次作废。**未配 SMTP 时自动不要求验证码**（否则所有人都注册不了），后台保存该开关为开时会直接报错提示先配 SMTP。首个账号免验证码但**仍必须填邮箱**（邮箱会保存，便于日后找回密码）。用户「改邮箱」同样走验证码（发往新邮箱），未配置 SMTP 时该操作会被拒绝 |
| 聊天 | 单条消息长度 | 100–5000，默认 1000 | WS 校验 + 输入框计数 |
| 聊天 | 撤回时限 | 0–60 分钟，默认 2；**0 = 关闭撤回** | WS 校验 + 右键菜单 |
| 聊天 | 群成员上限 | 2–2000，默认 200 | 入群事务校验 |
| 上传 | 图片 / 语音 / 群文件 / 视频体积 | 1–32 / 1–32 / 1–200 / **1–100** MB，默认 8 / 5 / 20 / **30** | 上传接口（请求体中间件用 200 MB 天花板放宽） |
| 安全 | 登录失败次数阈值 | 0（关闭）或 2–20，默认 5 | 登录接口 429 |
| 安全 | 锁定时长 | 1–1440 分钟，默认 15 | 同上 |

**安全设计**：

- **密钥类字段加密落库**（`smtp.pass` / `redis.pass` / `storage.secretKey`），算法 AES-256-GCM，密钥由 `JWT_SECRET` 派生。
- 接口**永不回显**密钥明文，只返回 `hasPass: true/false`；提交时留空表示「不修改」。
- 所有保存与测试发信都会写入操作审计（`admin_audits`）。
- 数值越界**直接返回 400**（不静默收敛）；读取时缺失 / 脏值回落默认，不会因一个错值卡死功能。

> ⚠️ **更换 `JWT_SECRET` 的副作用**：加密密钥由它派生，换掉之后**已保存的密钥无法解密**，表现为这些配置项读不出来、SMTP 失效、Redis 密码丢失。
> 处理方式：换 `JWT_SECRET` 后，去后台把 SMTP / Redis / 对象存储的密码**重新填一遍**。
> 如果只是想让所有人重新登录，用「重置密码」或改 `token_version` 即可，**不需要**换 `JWT_SECRET`。

### 2.2 对象存储（图片 / 语音存哪）

聊天里的图片与语音**只存 URL**，文件落哪由配置决定：

| 情况 | 存储位置 | URL 形态 |
|---|---|---|
| 后台未填 Endpoint + Bucket | 本地磁盘 `DATA_DIR/uploads/年/月/` | `/uploads/2026/11/xxx.png`（后端静态服务） |
| 后台填了 Endpoint + Bucket | S3 兼容存储（AWS S3 / MinIO / 阿里云 OSS / 腾讯云 COS） | `<publicBase>/2026/11/xxx.png` 或自动拼接的 endpoint/bucket |

要点：

- **`storage.publicBase`**：对外访问前缀（CDN 域名），留空则按 `endpoint/bucket` 自动拼接。
- **配了对象存储后，上传失败会直接报错，不会回退本地**——避免出现"以为在云端、实际在本机"的数据分裂。
- **切换存储不迁移历史文件**：老消息里的 `/uploads/...` 仍由本地静态服务提供（前提是 `uploads/` 目录还在），新消息走新存储。要全量迁移需自行用 `mc`/`rclone` 搬文件并批量改 `messages.content`。
- **静态资源长缓存**：文件名是 128 位随机、内容不可变，所以 `/uploads` 下发 `Cache-Control: immutable`，放心缓存一年。
- **类型按文件头判定**：把 HTML 改名成 `.png` 会被拒（防存储型 XSS）。各类型体积上限在「系统设置 → 上传上限」里可调。
- **缩略图**：上传图片时异步生成 `_t.jpg`（≤480px），气泡加载缩略图、灯箱加载原图；GIF / 过小图 / 解码失败跳过，透明图垫白底。
- **总配额与孤儿清理**：`upload.maxStorageMB`（0=不限）限制本地上传目录总量；后台「存储」页签可查看用量并清理未被消息 / 表情 / 群文件引用的孤儿文件。

### 2.3 维护模式（升级 / 故障时的标准操作）

入口：后台 → 系统设置 → 站点信息 → 维护模式。

开启后：

| 对象 | 表现 |
|---|---|
| 普通用户（已登录） | 页面 30 秒内自动切到维护页；业务接口返回 **503** + 自定义文案 |
| 匿名访问者 | 直接看到维护页，注册 / 找回密码也被拦 |
| 管理员 | **不受影响**：可正常登录、访问后台与全部接口 |
| WebSocket | 非管理员握手直接拒绝；已有连接无法继续收发 |

保持可用的路径（设计上刻意的，不要改动）：

- `/api/auth/login` —— **管理员登录的唯一入口**，必须保留
- `/api/site` —— 前端维护页要靠它拿站点名与文案
- `/api/health`、`/api/version` —— 探活

**标准流程**：

1. 后台开启维护模式，填上预计恢复时间（用户看得到）；
2. 执行升级 / 修复；
3. 后台关闭维护模式，用户端 30 秒内自动恢复。

**排障**：

- **用户报 503**：先查 `GET /api/site` 的 `maintenanceMode`——大概率是维护模式忘了关。
- **管理员也被挡在门外**：登录接口在维护下始终可用，用管理员账号从登录页进入即可；若连登录都失败，那是别的问题（查后端日志与数据库连通性），与维护模式无关。
- **调大了上传上限还是传不上去**：检查前面是否有 nginx——`client_max_body_size` 小于应用上限时会先被 nginx 413（应用提示与 nginx 行为不一致时优先怀疑这一层）。

---

## 3. 首次部署

### 3.1 一键安装脚本（推荐，宝塔面板风格）

> **日志**：全程输出镜像到 `/var/log/modernchat/install-<时间戳>.log`（`install-latest.log` 软链指向最新）；`scripts/update.sh` 的日志在仓库 `logs/update-*.log`。
>
> **断点续跑**：安装失败后重新运行同一命令，已完成的阶段自动跳过、参数不再重复询问（进度与参数存 `/var/lib/modernchat/install-state`，权限 600）。每个「跳过已完成阶段」提示都会附带重跑方法：**只重跑该阶段**——`sed -i '/^STAGE_阶段名=done$/d' /var/lib/modernchat/install-state` 后重新运行；**全部重跑**——`FORCE=1 bash install.sh` 或 `bash install.sh --force`。数据库建库与用户创建都是幂等操作，中断在哪一步就在哪一步继续。
>
> **面板路径适配**：自动探测 nginx 二进制与 vhost 目录 —— 标准安装（`/etc/nginx/conf.d`）、宝塔（`/www/server/panel/vhost/nginx`，二进制 `/www/server/nginx/sbin/nginx`）、1Panel / OpenResty；都没识别到时允许手动输入 `nginx.conf` include 的目录。reload 用探测到的二进制（面板编译版 nginx 不走 systemd）。注意：面板『网站』列表不会显示脚本写入的站点（不影响运行与访问）。

在**全新的 Linux 服务器**（Ubuntu / Debian / CentOS 系，root）上执行：

```bash
curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh -o install.sh
bash install.sh
```

脚本按「宝塔面板」的方式完成全部流程，只需回答几个问题：

1. **环境自检与补装**：缺什么装什么 —— curl/tar 基础工具、MySQL 8、nginx。**无需 Go / Node / git**（不再本地编译）。
2. **下载发行版**：从 GitHub / Gitee Release 下载预编译包 `modernchat-<版本>-linux-<架构>.tar.gz`（sha256 校验；含后端二进制 + 前端 dist + 迁移 SQL + 部署脚本）。`RELEASE_SOURCE=gitee|github` 选源，`RELEASE_TAG=v0.10.3` 指定版本（默认 latest）。
3. **数据库自动创建**：本机没有 MySQL 时可自动安装；已有实例（宝塔 / 云数据库）选「external」填连接信息。自动建 `modernchat` 库 + 专用账号，同时创建 `'localhost'` 与 `'%'` 两个 host 兼容宝塔的限制。**应用数据库密码**可用环境变量 `DB_PASS` 预设或在问答里输入，直接回车则自动生成随机密码——安装完成的摘要会**明示**数据库账号与密码（同时存于 `server/.env` 与 `/var/lib/modernchat/install-state`，均 600 权限；安装日志同为 600，仅 root 可读）。自动安装模式下 MySQL root 为**免密（auth_socket）**，摘要里给出设置 root 密码的命令（设密码不影响应用运行，应用用的是专用账号）。
4. **配置与常驻**：自动生成 `JWT_SECRET`、写 `server/.env`（含 `RELEASE_API_URL` / `RELEASE_DL_URL`，供在线更新使用）、注册 systemd 服务 `modernchat@<端口>`、生成并加载 nginx 配置（静态托管 + `/api` 反代 + `/ws` 升级 + `/ws` 关闭访问日志）。
5. **输出站点链接**：有域名 → `http://域名`（非 80 端口带端口号）；没域名 → `http://公网IP:端口`。首个注册账号自动成为站长。

支持环境变量免交互（未给的项仍会询问）：

```bash
INSTALL_DIR=/opt/modernchat DOMAIN=chat.example.com SITE_PORT=80 BACKEND_PORT=8080 \
DB_MODE=external DB_HOST=127.0.0.1 DB_PORT=3306 DB_ROOT_USER=root DB_ROOT_PASS=xxx \
RELEASE_SOURCE=gitee RELEASE_TAG=latest bash install.sh
```

**发版流程**（发布新版本供安装 / 更新）：

1. 更新 `CHANGELOG.md` 定版，并把 `web/`、`website/` 的 `package.json` / `package-lock.json` 与官网首页 Hero 徽标对齐到同一版本；运行 `sh scripts/check-version.sh` 校验通过（基准为 CHANGELOG 最新正式版本）；
2. 打 tag（如 `v0.10.3`）并推送；
3. 在构建机（需 Go + Node）运行 `scripts/package-release.sh`——产出 `dist-release/modernchat-<版本>-linux-{amd64,arm64}.tar.gz` 与对应 `.sha256`；
4. 把这些文件上传为 Gitee / GitHub 的 Release 附件（两个平台的 tag 与附件都传，安装脚本按 `RELEASE_SOURCE` 选源）。

注意事项：

- **云服务器安全组**：脚本会放行 ufw / firewalld，但云控制台的安全组需要手动放行站点端口（宝塔用户：安全 → 端口规则）。
- **宝塔面板用户**：也可以不用脚本装 nginx —— 先在软件商店装好 MySQL 8.0 与 nginx，再跑脚本选「external」数据库；脚本生成的 nginx 配置（`/etc/nginx/conf.d/modernchat.conf`）可复制进宝塔的站点配置。
- **Debian 12 注意**：apt 源只有 MariaDB（不支持 MySQL 8 的 ngram 全文索引，消息搜索迁移 0022 不可用），建议选「external」填宝塔装的 MySQL 8。
- SMTP 找回密码 / Web Push / 对象存储 / Redis 都是可选能力，装完在「后台 → 系统设置」里补配即可。

### 3.2 完整卸载

```bash
bash /opt/modernchat/scripts/uninstall.sh            # 交互确认
bash /opt/modernchat/scripts/uninstall.sh /opt/modernchat   # 显式指定安装目录
AUTO_CONFIRM=yes PURGE_DB=yes DELETE_FILES=yes bash scripts/uninstall.sh   # 免交互彻底卸载
```

卸载内容与默认行为：

| 项目 | 行为 | 默认 |
|---|---|---|
| 后端服务 | 停止并删除 `modernchat@<端口>` 模板单元（含热更新的新旧实例）与旧式单元 | 必删 |
| nginx 配置 | 只删除**带生成标记**的配置文件（绝不误删手写配置），并 reload | 必删 |
| 数据库 | `DROP DATABASE` + 删除专用用户（`'localhost'` 与 `'%'` 一起删） | **默认保留**，需确认 |
| 安装目录 | 含代码、`server/.env`、上传文件、日志 | **默认保留**，需确认 |
| 安装/更新日志 | 随安装目录删除；`/var/log/modernchat/` 保留 | 保留 |

不卸载 Go / Node / MySQL / nginx 等系统级组件（可能被其它应用使用）。

### 3.3 手工部署

```bash
# 1. 拉代码
git clone https://gitee.com/Sun-Si-Cheng/modern-chat-new.git && cd modern-chat-new

# 2. 准备数据库
mysql -uroot -p -e "CREATE DATABASE modernchat CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# 3. 配置
cp server/.env.example server/.env
# 编辑 server/.env：至少填 DB_DSN / JWT_SECRET / FRONTEND_URL

# 4. 构建并启动
cd server && go build -o ../bin/modernchat-server ./cmd/server   # Windows 加 .exe
../bin/modernchat-server

# 5. 构建前端并交给 nginx
cd ../web && npm ci && npm run build     # 产物在 web/dist，同时生成 .gz / .br 预压缩文件

# 6. 校验
curl http://127.0.0.1:8080/api/health     # {"status":"ok"}
curl http://127.0.0.1:8080/api/version
```

**首个注册账号自动成为站长**（`role=admin`），之后注册的均为普通用户。注册入口在登录页「立即注册」。

### 静态资源压缩（部署必看）

`npm run build` 除了产出 `dist/`，还会为每个文本资源生成同名的 `.gz` 与 `.br`（由 `scripts/precompress.mjs` 负责，Node 内置 zlib，无额外依赖）。

nginx 侧对应两项配置（`deploy/nginx.*.conf` 已包含）：

```nginx
gzip_static on;   # 有 .gz 就直接发，零运行时 CPU
gzip on;          # 没有预压缩文件时实时压缩兜底
```

实测效果（全量 assets）：

| 形式 | 体积 | 占比 |
|---|---|---|
| 原始 | 1288 KB | 100% |
| gzip | 358 KB | 28% |
| brotli | 311 KB | 24% |

首屏实际传输从 **161 KB 降到约 65 KB**。

> **brotli 需要额外模块**：官方 nginx 未内置 `ngx_brotli`，启用前先 `nginx -V 2>&1 | grep -i brotli` 确认，否则配置会导致 nginx 启动失败。配置文件里相关行默认注释。即便不启用，`.br` 文件留着也无害（不占带宽，只在装了模块时被读取）。

> 单独重压（不重新构建）：`cd web && npm run compress`。

---

## 4. 版本升级

```bash
# Linux / macOS
./scripts/update.sh            # 默认热更新（不停机）
./scripts/update.sh --cold     # 强制旧式重启
./scripts/update.sh --no-restart  # 只构建并原子替换产物，稍后手动切换
```

### 4.1 热更新（不停机，install.sh 部署的默认行为）

原理是**蓝绿双实例**：

1. 拉取代码后构建新二进制到临时文件，`mv` 原子替换（运行中的老进程继续用老 inode，所以构建期间服务不受影响；直接 `go build -o` 覆盖运行中的二进制会 `ETXTBSH`）。前端构建到 `dist.new` 后原子交换 `dist`（nginx 按路径打开文件，无缓存问题）。
2. 新实例起在**备用端口**（`8080`↔`8081` 对偶，由 `modernchat@<端口>.service` 模板单元承载，双实例并存），健康检查通过才继续。
3. 改 nginx 配置里的 `upstream modernchat_backend` 端口 → `nginx -t` → `nginx -s reload`（reload 对已建立连接零中断，新请求进新实例）。
4. `systemctl stop` 老实例：SIGTERM 触发后端**优雅停机**（在途 HTTP 请求 ≤10s 收尾、WS 断开），在线客户端自动重连到新实例，并按 `clientMsgId` 增量补齐断线期间的消息，消息不丢。
5. 任何一步失败都自动回滚：新实例停掉、upstream 回滚、老实例继续服务。

状态记录在仓库根 `.hotupdate`（`BASE_PORT` / `ACTIVE_PORT`），由 install.sh 生成、update.sh 维护。

### 4.2 图形化更新（站长后台 → 在线更新）

站长后台新增「在线更新」页签：检查更新 → 一键更新，全程在页签内显示实时日志，进行中每 2 秒轮询状态。支持两种模式（按部署方式自动选择）：

| 模式 | 触发条件 | 行为 |
|---|---|---|
| **发行版**（install.sh 默认） | `server/.env` 里有 `RELEASE_API_URL` / `RELEASE_DL_URL` | 查询 Release 最新 tag → 下载预编译包 → sha256 校验 → 原子替换产物 → 重启。**服务器无需 Go / Node / git** |
| **源码**（git clone 部署） | 安装目录有 `.git` 且未配置 RELEASE_* | git pull → 本地编译（需服务器装 Go / Node）→ 原子替换 → 重启 |

与 `scripts/update.sh` 等价（发行版模式与图形化同源：下载新 Release 校验替换后重启；源码模式支持蓝绿热更新），边界：

| 部署方式 | 是否可用 |
|---|---|
| install.sh（systemd 模板单元） | 可用 |
| Docker Compose | 不可用（容器内无工具链与 systemd，界面会提示） |
| 裸机 nohup | 不可用（重启依赖 systemd） |

安全边界：仅站长（`middleware.Admin`）可触发；命令与 URL 全部固定——版本号来自 Release 服务端 / `origin/main`，不接收用户输入；每次触发写操作审计（`admin_audits`，action=`web_update`）；`WEB_UPDATE=false` 环境变量可整体关闭接口。接口本质仍是"站长账号可触发代码更新"，账号泄露等同服务器可控——保管好管理员凭据，或在不信任多管理员时用 `WEB_UPDATE=false` 关闭。

- **旧式单端口部署自动迁移**：检测到旧的 `modernchat.service`（install.sh v1）会自动改写为模板单元并写入 `.hotupdate`，本次升级即走热更新。
- `--cold` 强制走旧式 `systemctl restart`（有数秒中断，WS 全部重连）。
- **Docker Compose 部署暂不支持热更新**：`docker compose up -d --build` 会重建容器，有秒级中断（前端断线重连机制兜底，消息不丢）。
- **Docker 构建的版本号**：`docker compose` 把环境变量 `VERSION` / `COMMIT` / `BUILD_TIME` 作为 build args 传给 `server/Dockerfile`（未设置时回退 `dev` / `unknown`）；容器内 `curl http://127.0.0.1:8080/api/version` 可核对。
- **Docker 构建镜像源可配置**：`GO_IMAGE` / `RUNTIME_IMAGE` / `GOPROXY` / `APK_MIRROR` 四个 build arg 默认均为官方源，不传参时行为不变；国内网络示例见 README 的 Docker 章节（第三方源需自行确认可信度）。
- `--no-restart`：只构建并原子替换产物。运行中的老进程继续跑老版本，下次执行 `update.sh`（不带参数）时再完成切换。

脚本会：拉取 Gitee → 构建后端（注入版本号）→ 构建前端 → 切换/重启服务。也可手工执行：

```bash
cd server
go build -ldflags "-X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=v1.0.0 \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=$(date -u +%FT%TZ)" \
  -o ../bin/modernchat-server ./cmd/server
```

版本号在设置页「关于」与 `/api/version` 可见（`scripts/update.sh` 用 `git describe --tags` 取版本，所以发版即打 tag）。

> **0.10.0 升级必读**：① 执行迁移 `0021_message_sender_index`（低峰）；② nginx `client_max_body_size` ≥ **32 MB**（视频默认 30 MB）；③ 「注册需邮箱验证」默认开启——未配 SMTP 会自动降级、首个账号豁免，如需旧行为在后台关掉。详见 `CHANGELOG.md` 的「升级注意」。

---

## 5. 数据备份与恢复

需要备份**两处**：数据库 + 上传文件目录。

```bash
# 1) 数据库（用户、消息、插件代码、系统设置等）
mysqldump -uroot -p --single-transaction --default-character-set=utf8mb4 modernchat > backup_$(date +%F).sql

# 2) 上传的图片 / 语音 / 视频（只在用本地存储时需要）
tar czf uploads_$(date +%F).tar.gz -C server/data uploads

# 恢复
mysql -uroot -p modernchat < backup_2026-10-25.sql
tar xzf uploads_2026-10-25.tar.gz -C server/data
```

- 建议每日一次，保留 7–30 天；也可在后台「备份」页签点「立即备份」，或设置 `BACKUP_INTERVAL_HOURS` / `BACKUP_RETAIN` 让服务自动备份（依赖 `mysqldump`，不在 PATH 时用 `MYSQLDUMP_PATH` 指定）
- **消息里只存 URL**（`/uploads/2026/11/xxx.png`）。只备份数据库不备份 `uploads/` 的话，恢复后所有图片和语音都会 404——消息记录还在，但内容打不开
- **用对象存储时不需要备份 `uploads/`**，由云厂商负责；此时数据库备份仍必须
- `users.avatar` 存的是头像的 data URL（每张约 20–40 KB），会占库空间；用户多时按需清理
- 升级前建议先备份一次（脚本不会动数据，但迁移 SQL 会改表结构）
- **新增索引的迁移要在低峰期执行**：如 `0021_message_sender_index`（为 `messages.sender_id` 建索引）。MySQL 8 默认 Online DDL 不锁表，但大表建索引仍会占用 IO；消息表很大时建议放在业务低峰，并观察 `SHOW PROCESSLIST`

---

## 6. 日志与监控

- 结构化日志（JSON 行），默认写到 `server/logs/`（`LOG_DIR`），同时输出 stdout
- 轮转与保留：`LOG_RETAIN_DAYS`（默认 15 天）
- 关键事件：

| 日志关键字 | 含义 |
|---|---|
| `mail sent` / `mail send failed` | 邮件发送结果 |
| `password reset link` | 未配 SMTP 时重置链接只出现在这里（去日志里取） |
| `ws slow consumer, closing conn` | 客户端消费过慢被断开 |
| `ws disconnected` | 连接断开 |
| `message recalled` | 撤回 |
| `gorm` | SQL 警告（参数已脱敏，不打印参数值） |

- 健康检查：`GET /api/health`（无需鉴权）
- 版本信息：`GET /api/version`
- Prometheus：`GET /api/metrics`（`METRICS_TOKEN` 非空时需 `Authorization: Bearer <token>`），导出路由级请求计数与在线连接数

```bash
# 看某个用户当天做了什么
grep '"uid":12' server/logs/*.log | tail -50
# 看限流与安全事件
grep -E 'rate|ban|kick' server/logs/*.log | tail -30
```

---

## 7. 常见问题（FAQ）

**Q1：用户说没收到重置密码邮件**
要真正发信，需配置 `SMTP_HOST/PORT/USER/PASS/FROM`，其中 `SMTP_FROM` 必须与 `SMTP_USER` 一致。QQ / 163 用**授权码**。

未配置 SMTP 时，链接**默认不再写日志**（`LOG_RESET_LINK` 在 release 模式下为 false，避免日志里的可用令牌变成账户接管入口）。确需排查时临时设 `LOG_RESET_LINK=true` 并重启，取完链接立刻关掉：`grep "password reset link" server/logs/*.log`。

**Q2：WebSocket 连不上，控制台报 403**
`FRONTEND_URL` 没包含实际访问的域名。浏览器的 Origin 必须与白名单**完全一致**（不带尾斜杠）。另外 nginx 的 `/ws` 必须配 `Upgrade` / `Connection` 头。

**Q3：登录后立刻掉线 / 提示令牌失效**
`JWT_SECRET` 每次重启都变（生产未设置会直接拒绝启动，开发模式会随机生成）。生产必须显式配置并保持不变。管理员重置密码会让 `token_version` 递增，所有端被强制下线。

**Q4：注册提示"用户已存在"，换名字也不行**
用户名唯一且不可修改（用于登录）。昵称可以重复。

**Q5：提示 429 / 请求太频繁**
触发了限流。登录、注册、发消息、忘记密码各有限流档位，等窗口结束即可。**多实例部署时需配置 Redis**，否则各实例独立计数。Redis 可在「后台 → 系统设置」里配，但**改完需重启服务**才生效（限流器在启动时创建）。

**Q5.1：SMTP 在哪里配？改完要重启吗？**
两个地方，**优先「后台 → 系统设置」**（保存即热生效，不用重启）：
- 后台配过 → 以数据库为准，重启后依然生效；
- 没配过 → 用环境变量 `SMTP_*`（启动时读取）。

配完可以点「发送测试邮件」验证，失败会把邮件服务器返回的原始错误显示出来（例如 `auth: 535 Login fail`），比翻日志快。

**Q5.2：换过 `JWT_SECRET` 后 SMTP 失效 / 后台配置项全空**
密钥类字段（SMTP 密码、Redis 密码、对象存储 SecretKey）用 `JWT_SECRET` 派生的密钥加密存储，**换掉 `JWT_SECRET` 就解不开了**。去后台把密码重新填一遍即可（其余字段不受影响）。详见 §2.1。

**Q6：插件上传失败**
检查 `PLUGIN_MAX_SIZE`（默认 1 MB）与 nginx 的 `client_max_body_size`（默认可能只有 1 MB，需同步调大）。同名文件再次上传视为**升级**。

**Q7：想找回忘记的用户名**
系统不提供用户名找回。站长可在后台为该用户重置密码，再线下告知。

**Q8：后台进不去**
`/admin` 需要 `role=admin`。只有**首个注册账号**是站长。若站长账号丢失，只能进库把 `users.role` 改为 `admin`。

**Q9：跑集成测试全是 SKIP，却显示 ok**
集成测试连不上 MySQL 时会 `t.Skip`，而 `go test` 对 SKIP 仍返回 `ok`——是**假通过**。正确做法：设置 `DB_DSN` 指向测试库（库需存在：`CREATE DATABASE modernchat_test`），并用 `scripts/coverage.sh`（Linux / macOS / Git Bash），脚本会检测 SKIP 并告警。

**Q10：升级后页面还是旧的**
nginx 若缓存了静态资源，检查 `dist/assets` 的文件名哈希是否更新；浏览器强制刷新（Ctrl+F5）。前端是纯静态资源，重启后端不会刷新页面。

**Q11：头像存哪了，会不会把库撑爆**
存 `users.avatar`（data URL，256×256 webp，约 20–40 KB/张）。当前规模没问题；用户上千后建议改为文件存储 + URL。

**Q12：找回密码邮件里的链接打不开**
链接域名取自 `FRONTEND_URL`。生产环境务必改成真实域名，否则用户点开是 `localhost`。

**Q13：插件面板在聊天区不显示**
插件需在「插件管理」里处于**启用**状态；被站长全局下架的插件即使启用也不下发。

---

## 8. 上线前检查清单

- [ ] `JWT_SECRET` 已设置且 ≥32 字节（生产未设置会拒绝启动）
- [ ] `GIN_MODE=release`
- [ ] `FRONTEND_URL` 包含真实域名且无尾随空格
- [ ] `TRUSTED_PROXIES` 按实际反代网段配置（不确定就留默认回环）
- [ ] `SMTP_*` 已配置并**实测过一封邮件**（也可在后台「系统设置」里配并点「发送测试邮件」）
- [ ] nginx 已开 `gzip on` + `gzip_static on`（构建产物已预压缩，首屏约 65 KB）
- [ ] nginx `/ws` 已配 Upgrade 头，且 `access_log off`（token 在 query 里，默认日志会留下可用令牌）
- [ ] nginx 已配 `X-Frame-Options` / `X-Content-Type-Options` / `Referrer-Policy`（防点击劫持与类型嗅探）
- [ ] `MIGRATE=false`（生产用版本化 SQL），或确认迁移已执行
- [ ] 备份策略覆盖**数据库 + `DATA_DIR/uploads`**（用对象存储时后者可省）
- [ ] nginx `client_max_body_size` ≥ **32 MB**（视频默认上限 30 MB；nginx 默认 1 MB 会直接 413。调大视频上限时要同步调大这一项，建议留 2 MB 余量）
- [ ] 防火墙只暴露 80/443，后端 8080 仅本机可达
- [ ] `server/.env` 未被提交进版本库（`git check-ignore server/.env` 应有输出）

