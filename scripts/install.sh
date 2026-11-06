#!/usr/bin/env bash
# ============================================================================
# ModernChat 一键安装脚本（宝塔面板风格）
#
# 流程：检测/安装运行环境（Go、Node、MySQL、nginx）→ 从代码仓库拉最新代码
#       → 本地编译前后端 → 自动建库并写入配置 → systemd 常驻 + nginx 反代
#       → 输出可直接访问的站点链接。
#
# 用法（root 或 sudo）：
#   curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh -o install.sh
#   bash install.sh                          # 全程交互问答
#
#   # 也可以预设参数免交互（未给的项仍会询问）：
#   INSTALL_DIR=/opt/modernchat \
#   DOMAIN=chat.example.com \
#   SITE_PORT=80 \
#   BACKEND_PORT=8080 \
#   DB_MODE=auto \                # auto=本机自动装MySQL | external=已有实例
#   DB_HOST=127.0.0.1 DB_PORT=3306 DB_ROOT_USER=root DB_ROOT_PASS=xxx \
#   MIRROR=cn \                   # cn=国内镜像(默认) | official=官方源
#   bash install.sh
#
# 设计取舍见 docs/运维手册.md §1：MySQL 8.0 是唯一必需依赖，Redis/SMTP 可选
# （装完后在「后台 → 系统设置」里补配即可）。
# ============================================================================
set -euo pipefail

# ---------------- 可预设参数 ----------------
REPO_URL="${REPO_URL:-https://gitee.com/Sun-Si-Cheng/modern-chat-new.git}"
BRANCH="${BRANCH:-main}"
INSTALL_DIR="${INSTALL_DIR:-}"
DOMAIN="${DOMAIN:-}"
SITE_PORT="${SITE_PORT:-}"
BACKEND_PORT="${BACKEND_PORT:-}"
DB_MODE="${DB_MODE:-}"           # auto | external
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-3306}"
DB_ROOT_USER="${DB_ROOT_USER:-root}"
DB_ROOT_PASS="${DB_ROOT_PASS:-}"
DB_NAME="${DB_NAME:-modernchat}"
DB_USER="${DB_USER:-}"
DB_PASS="${DB_PASS:-}"
MIRROR="${MIRROR:-cn}"           # cn | official
GO_VERSION="${GO_VERSION:-1.26.0}"
NODE_VERSION="${NODE_VERSION:-22.20.0}"
INSTALL_MYSQL="${INSTALL_MYSQL:-}"   # yes/no：没有 MySQL 时是否自动安装
INSTALL_NGINX="${INSTALL_NGINX:-}"   # yes/no：没有 nginx 时是否自动安装

# ---------------- 输出工具 ----------------
C_G="\033[32m"; C_Y="\033[33m"; C_R="\033[31m"; C_B="\033[36m"; C_0="\033[0m"
info()  { printf "${C_G}==>${C_0} %s\n" "$*"; }
warn()  { printf "${C_Y}警告:${C_0} %s\n" "$*"; }
err()   { printf "${C_R}错误:${C_0} %s\n" "$*" >&2; }
die()   { err "$*"; exit 1; }
ask()   { # ask "提示" "默认值" -> 回显到 REPLY
  local prompt="$1" def="${2:-}"
  if [ -n "$def" ]; then
    printf "${C_B}?%s${C_0} [%s]: " "$prompt" "$def" >&2
  else
    printf "${C_B}?%s${C_0}: " "$prompt" >&2
  fi
  read -r REPLY
  REPLY="${REPLY:-$def}"
}

[ "$(id -u)" -eq 0 ] || die "请用 root 运行（或 sudo bash install.sh）。宝塔用户可在面板【终端】里执行。"

# ---------------- 系统识别 ----------------
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  GO_ARCH="amd64"; NODE_ARCH="x64" ;;
  aarch64) GO_ARCH="arm64"; NODE_ARCH="arm64" ;;
  *) die "不支持的架构：$ARCH（仅支持 x86_64 / arm64）" ;;
esac

PKG=""
if command -v apt-get >/dev/null 2>&1; then PKG="apt"
elif command -v dnf >/dev/null 2>&1; then PKG="dnf"
elif command -v yum >/dev/null 2>&1; then PKG="yum"
else die "未识别的包管理器（支持 apt / dnf / yum）"
fi
info "系统：$(. /etc/os-release && echo "$PRETTY_NAME") | 架构：$ARCH | 包管理：$PKG"

pkg_install() {
  case "$PKG" in
    apt) DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ;;
    dnf) dnf install -y "$@" ;;
    yum) yum install -y "$@" ;;
  esac
}
pkg_update() {
  case "$PKG" in
    apt) apt-get update -y ;;
    dnf) dnf makecache --refresh -y || true ;;
    yum) yum makecache -y || true ;;
  esac
}

# ============================================================================
# 1) 基础工具
# ============================================================================
NEED_BASE=()
command -v git   >/dev/null 2>&1 || NEED_BASE+=(git)
command -v curl  >/dev/null 2>&1 || NEED_BASE+=(curl)
command -v tar   >/dev/null 2>&1 || NEED_BASE+=(tar)
if [ "${#NEED_BASE[@]}" -gt 0 ]; then
  info "安装基础工具：${NEED_BASE[*]}"
  pkg_update; pkg_install "${NEED_BASE[@]}"
fi

# ============================================================================
# 2) 交互问答（环境变量已预设的项自动跳过）
# ============================================================================
echo
printf "${C_B}=============================================================${C_0}\n"
printf "${C_B}        ModernChat 一键安装（代码仓库 → 编译 → 部署）${C_0}\n"
printf "${C_B}=============================================================${C_0}\n\n"

[ -n "$INSTALL_DIR" ] || { ask "安装目录" "/opt/modernchat"; INSTALL_DIR="$REPLY"; }
[ -n "$DOMAIN" ]      || { ask "访问域名（没有域名直接回车，将用服务器公网 IP）" ""; DOMAIN="$REPLY"; }
[ -n "$SITE_PORT" ]   || { ask "站点端口（nginx 对外端口）" "80"; SITE_PORT="$REPLY"; }
[ -n "$BACKEND_PORT" ]|| { ask "后端服务端口（仅本机访问）" "8080"; BACKEND_PORT="$REPLY"; }

if [ -z "$DB_MODE" ]; then
  echo >&2
  echo "  数据库选择（MySQL 8.0 是唯一必需依赖）：" >&2
  echo "    1) 本机还没有 MySQL —— 脚本自动安装并建库（推荐，Ubuntu/CentOS 系）" >&2
  echo "    2) 已有 MySQL 实例（宝塔/云数据库等）—— 填连接信息" >&2
  ask "请选择 [1/2]" "1"; DB_MODE="$REPLY"
  [ "$DB_MODE" = "2" ] && DB_MODE="external" || DB_MODE="auto"
fi

# ============================================================================
# 3) 下载工具函数（国内镜像优先，官方兜底）
# ============================================================================
fetch() { # fetch <输出文件> <国内URL> <官方URL>
  local out="$1" cn="$2" of="$3"
  if [ "$MIRROR" = "official" ]; then
    curl -fL --retry 3 -o "$out" "$of" || curl -fL --retry 3 -o "$out" "$cn"
  else
    curl -fL --retry 3 -o "$out" "$cn" || curl -fL --retry 3 -o "$out" "$of"
  fi
}

install_go() {
  local v="$GO_VERSION" tmp; tmp="$(mktemp -d)"
  info "安装 Go ${v} → /usr/local/go"
  fetch "$tmp/go.tgz" \
    "https://mirrors.aliyun.com/golang/go${v}.linux-${GO_ARCH}.tar.gz" \
    "https://go.dev/dl/go${v}.linux-${GO_ARCH}.tar.gz"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp/go.tgz"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  rm -rf "$tmp"
  go version
}

install_node() {
  local v="$NODE_VERSION" tmp; tmp="$(mktemp -d)"
  info "安装 Node.js v${v} → /usr/local/node"
  fetch "$tmp/node.tgz" \
    "https://npmmirror.com/mirrors/node/v${v}/node-v${v}-linux-${NODE_ARCH}.tar.xz" \
    "https://nodejs.org/dist/v${v}/node-v${v}-linux-${NODE_ARCH}.tar.xz"
  rm -rf /usr/local/node
  mkdir -p /usr/local/node
  tar -xJf "$tmp/node.tgz" -C /usr/local/node --strip-components=1
  ln -sf /usr/local/node/bin/node /usr/local/bin/node
  ln -sf /usr/local/node/bin/npm  /usr/local/bin/npm
  rm -rf "$tmp"
  node -v
}

# npm 依赖走 npmmirror（国内服务器快一个数量级）
if [ "$MIRROR" = "cn" ]; then
  export GOPROXY="https://goproxy.cn,direct"
  NPM_REGISTRY="https://registry.npmmirror.com"
else
  export GOPROXY="https://proxy.golang.org,direct"
  NPM_REGISTRY="https://registry.npmjs.org"
fi

# ============================================================================
# 4) 运行环境检测与安装
# ============================================================================
info "检测 Go / Node / MySQL / nginx …"

command -v go >/dev/null 2>&1 || install_go
GO_MAJOR="$(go env GOVERSION 2>/dev/null | sed 's/^go//' | cut -d. -f1-2 || true)"
if [ -n "$GO_MAJOR" ] && [ "$GO_MAJOR" != "$(echo "$GO_VERSION" | cut -d. -f1-2)" ]; then
  warn "已有 Go $GO_MAJOR，与要求的 ${GO_VERSION%.*}.x 不同；继续使用现有版本（go.mod 要求 ${GO_VERSION%.*}+）"
fi

command -v node >/dev/null 2>&1 || install_node
NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
if [ "$NODE_MAJOR" -lt 20 ]; then
  die "Node.js 版本过低（$NODE_MAJOR.x），需要 ≥ 20；请升级后重跑"
fi

# --- MySQL ---
MYSQL_CLI=""
if command -v mysql >/dev/null 2>&1; then MYSQL_CLI="mysql"; fi
if [ "$DB_MODE" = "auto" ] && [ -z "$MYSQL_CLI" ]; then
  if [ -z "$INSTALL_MYSQL" ]; then
    ask "本机没有 MySQL 客户端，是否自动安装 MySQL 服务端？[yes/no]" "yes"; INSTALL_MYSQL="$REPLY"
  fi
  if [ "$INSTALL_MYSQL" = "yes" ]; then
    info "安装 MySQL 8（可能需要几分钟）…"
    pkg_update
    case "$PKG" in
      apt)
        pkg_install mysql-server mysql-client || pkg_install default-mysql-server default-mysql-client || pkg_install mariadb-server mariadb-client
        ;;
      dnf|yum)
        pkg_install mysql-server || pkg_install community-mysql-server || pkg_install mysql-community-server || die "无法用 $PKG 自动安装 MySQL，请在宝塔面板【软件商店】安装 MySQL 8.0 后重跑，或选已有实例"
        ;;
    esac
    command -v mysql >/dev/null 2>&1 || die "MySQL 安装后仍找不到 mysql 客户端，请手动安装"
    MYSQL_CLI="mysql"
    # 启动服务
    if command -v systemctl >/dev/null 2>&1; then
      systemctl enable --now mysql 2>/dev/null || systemctl enable --now mysqld 2>/dev/null || systemctl enable --now mariadb 2>/dev/null || true
    fi
  fi
fi
[ -n "$MYSQL_CLI" ] || die "本机没有 mysql 客户端。请在宝塔面板安装 MySQL 8.0，或把 DB_MODE 设为 external 填已有实例"

# ============================================================================
# 5) 数据库参数与建库
# ============================================================================
if [ "$DB_MODE" = "external" ]; then
  [ -n "$DB_ROOT_PASS" ] || { ask "数据库 root（或有建库权限的账号）密码" ""; DB_ROOT_PASS="$REPLY"; }
  [ -n "$DB_USER" ]      || { ask "应用使用的数据库用户名（回车自动创建 modernchat）" "modernchat"; DB_USER="$REPLY"; }
else
  DB_ROOT_USER="${DB_ROOT_USER:-root}"
  # Ubuntu 的 MySQL 8 root 默认 auth_socket；Debian 的 MariaDB root 同理
  DB_ROOT_AUTH_SOCKET=0
  if [ "$DB_MODE" = "auto" ]; then
    if $MYSQL_CLI -uroot -e "SELECT 1" >/dev/null 2>&1; then
      DB_ROOT_PASS=""
    elif $MYSQL_CLI -uroot --skip-password -e "SELECT 1" >/dev/null 2>&1; then
      DB_ROOT_PASS=""; DB_ROOT_AUTH_SOCKET=1
    fi
  fi
  [ -n "$DB_ROOT_PASS" ] || { ask "本机 MySQL root 密码（auth_socket/无密码直接回车）" ""; DB_ROOT_PASS="$REPLY"; }
  DB_USER="${DB_USER:-modernchat}"
fi

DB_HOST="${DB_HOST:-127.0.0.1}"; DB_PORT="${DB_PORT:-3306}"
[ -n "$DB_PASS" ] || DB_PASS="$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"

mysql_exec() { # mysql_exec <sql-file-or-inline> （走 stdin）
  if [ -n "$DB_ROOT_PASS" ]; then
    $MYSQL_CLI -h"$DB_HOST" -P"$DB_PORT" -u"$DB_ROOT_USER" -p"$DB_ROOT_PASS" --protocol=TCP "$@"
  else
    $MYSQL_CLI -h"$DB_HOST" -P"$DB_PORT" -u"$DB_ROOT_USER" --protocol=TCP "$@"
  fi
}

info "创建数据库 ${DB_NAME} 与用户 ${DB_USER} …"
# 数据库可能已存在（重装场景）：用 IF NOT EXISTS，用户密码 GRANT 幂等刷新。
# 同时建 'localhost' 与 '%' 两个 host 的用户：宝塔/云数据库常限制远程 % 账号，
# 而后端在DB_HOST=127.0.0.1 时走 localhost 匹配；远程数据库则用 %。
mysql_exec <<SQL
CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASS}';
ALTER USER '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASS}';
CREATE USER IF NOT EXISTS '${DB_USER}'@'%' IDENTIFIED BY '${DB_PASS}';
ALTER USER '${DB_USER}'@'%' IDENTIFIED BY '${DB_PASS}';
GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${DB_USER}'@'localhost';
GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${DB_USER}'@'%';
FLUSH PRIVILEGES;
SQL
info "数据库就绪"

# ============================================================================
# 6) 拉取代码
# ============================================================================
if [ -d "$INSTALL_DIR/.git" ]; then
  info "目录已存在，拉取最新代码：$INSTALL_DIR"
  git -C "$INSTALL_DIR" fetch --all --prune
  git -C "$INSTALL_DIR" checkout "$BRANCH"
  git -C "$INSTALL_DIR" pull --ff-only origin "$BRANCH"
else
  info "克隆仓库（分支 ${BRANCH}）→ $INSTALL_DIR"
  rm -rf "$INSTALL_DIR"
  git clone --depth=1 -b "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
fi
cd "$INSTALL_DIR"

VERSION="$(git describe --tags --always 2>/dev/null || echo "$(git rev-parse --short HEAD)")"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ============================================================================
# 7) 编译
# ============================================================================
info "编译后端 $VERSION"
mkdir -p bin
(cd server && go build -trimpath -ldflags \
  "-s -w -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=${VERSION} -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=${COMMIT} -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=${BUILD_TIME}" \
  -o ../bin/modernchat-server ./cmd/server)

info "编译前端（npm ci + build，首次约 1-3 分钟）"
(cd web && npm ci --no-audit --no-fund --registry="$NPM_REGISTRY" && npm run build)

# ============================================================================
# 8) 写配置（server/.env）
# ============================================================================
JWT_SECRET="$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"

# FRONTEND_URL：对外访问地址（WS Origin 白名单 + 找回密码链接）
if [ -n "$DOMAIN" ]; then
  if [ "$SITE_PORT" = "80" ]; then PUBLIC_URL="http://$DOMAIN"; else PUBLIC_URL="http://$DOMAIN:$SITE_PORT"; fi
else
  PUBLIC_IP="$(curl -fsS --max-time 5 https://ifconfig.me 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}' || echo '127.0.0.1')"
  PUBLIC_URL="http://$PUBLIC_IP:$SITE_PORT"
fi

cat > server/.env <<ENV
# 由 scripts/install.sh 生成（$(date '+%F %T')）。修改后需 systemctl restart modernchat
PORT=$BACKEND_PORT
GIN_MODE=release
DB_DSN=${DB_USER}:${DB_PASS}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}?charset=utf8mb4&parseTime=True&loc=Local
JWT_SECRET=$JWT_SECRET
FRONTEND_URL=$PUBLIC_URL
TRUSTED_PROXIES=127.0.0.1,::1
LOG_LEVEL=info
LOG_DIR=logs
LOG_RETAIN_DAYS=15
MIGRATE=true
PLUGIN_MAX_SIZE=1048576
REDIS_ADDR=
SMTP_HOST=
SMTP_PORT=465
SMTP_USER=
SMTP_PASS=
SMTP_FROM=
ENV
chmod 600 server/.env
info "已写入 server/.env（MIGRATE=true，首次启动自动建表）"

# ============================================================================
# 9) 常驻运行（优先 systemd；无 systemd 时降级 nohup）
#    systemd 用模板单元 modernchat@<端口>.service：热更新时新旧实例可并存
# ============================================================================
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  info "注册 systemd 模板单元 modernchat@.service（实例端口 $BACKEND_PORT）"
  cat > /etc/systemd/system/modernchat@.service <<UNIT
[Unit]
Description=ModernChat IM server (port %i)
After=network.target mysql.service mysqld.service

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR/server
ExecStart=$INSTALL_DIR/bin/modernchat-server
Restart=on-failure
RestartSec=3
# 端口由实例名注入（PORT=%i）；.env 里同名变量会被这个值覆盖（进程环境优先）
Environment=PORT=%i
Environment=DATA_DIR=$INSTALL_DIR/server/data
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now "modernchat@$BACKEND_PORT.service"
  RUN_MODE="systemd"
  # 热更新状态文件：BASE_PORT 固定基准，ACTIVE_PORT 当前服务端口（update.sh 维护）
  cat > "$INSTALL_DIR/.hotupdate" <<HOT
BASE_PORT=$BACKEND_PORT
ACTIVE_PORT=$BACKEND_PORT
HOT
else
  warn "未检测到 systemd（WSL / 容器 / 老系统），降级为 nohup 后台进程"
  mkdir -p "$INSTALL_DIR/server/logs"
  cd "$INSTALL_DIR/server"
  nohup "$INSTALL_DIR/bin/modernchat-server" >> logs/server.out 2>&1 &
  echo $! > "$INSTALL_DIR/server/server.pid"
  cd - >/dev/null
  RUN_MODE="nohup"
  warn "重启机器后需手动启动：cd $INSTALL_DIR/server && nohup ../bin/modernchat-server >> logs/server.out 2>&1 &"
fi

# 等待健康检查（迁移 45 张表需要一点时间）
info "等待后端启动（含首次建表）…"
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$BACKEND_PORT/api/health" >/dev/null 2>&1; then
    info "后端健康检查通过"; break
  fi
  [ "$i" = "60" ] && {
    if [ "$RUN_MODE" = "systemd" ]; then journalctl -u "modernchat@$BACKEND_PORT" -n 30 --no-pager || true
    else tail -n 30 "$INSTALL_DIR/server/logs/server.out" || true; fi
    die "后端 60s 内未就绪，上方是最近日志"
  }
  sleep 1
done

# ============================================================================
# 10) nginx
# ============================================================================
NGINX_CONF="/etc/nginx/conf.d/modernchat.conf"
[ -d /etc/nginx/conf.d ] || NGINX_CONF="/etc/nginx/sites-available/modernchat.conf"

if ! command -v nginx >/dev/null 2>&1; then
  if [ -z "$INSTALL_NGINX" ]; then
    ask "没有检测到 nginx，是否自动安装（前端静态文件需要它托管）？[yes/no]" "yes"; INSTALL_NGINX="$REPLY"
  fi
  if [ "$INSTALL_NGINX" = "yes" ]; then
    info "安装 nginx …"; pkg_update; pkg_install nginx
  else
    warn "跳过 nginx 安装。前端静态文件在 $INSTALL_DIR/web/dist，请自行托管（宝塔用户：建站点后把下方配置粘进站点的 nginx 配置）"
  fi
fi

if command -v nginx >/dev/null 2>&1; then
  NGINX_SERVER_NAME="${DOMAIN:-_}"
  NGINX_ROOT="$INSTALL_DIR/web/dist"
  NGINX_TARGET="$NGINX_CONF"
  if [ "$NGINX_CONF" = "/etc/nginx/sites-available/modernchat.conf" ]; then
    ln -sf "$NGINX_CONF" /etc/nginx/sites-enabled/modernchat.conf
    # 移除发行版默认站点，避免抢占 80 端口
    rm -f /etc/nginx/sites-enabled/default
  fi
  cat > "$NGINX_TARGET" <<NGX
# ModernChat —— 由 scripts/install.sh 生成
server {
    listen $SITE_PORT;
    server_name $NGINX_SERVER_NAME;

    root $NGINX_ROOT;
    index index.html;

    gzip on;
    gzip_vary on;
    gzip_comp_level 6;
    gzip_min_length 1024;
    gzip_proxied any;
    gzip_types text/plain text/css text/javascript application/javascript application/json application/xml image/svg+xml;
    gzip_static on;

    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "no-referrer" always;
    add_header Permissions-Policy "geolocation=(), camera=(), microphone=()" always;

    # 前端 SPA（history 路由回退）
    location / {
        try_files \$uri \$uri/ /index.html;
    }

    location /assets/ {
        expires 30d;
        add_header Cache-Control "public, immutable";
    }

    # REST API
    location /api/ {
        proxy_pass http://modernchat_backend;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        client_max_body_size 2m;
    }

    # 媒体上传（multipart）：应用侧 200 MB 天花板 + 余量，nginx 不放宽会先回 413
    location /api/upload {
        proxy_pass http://modernchat_backend;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        client_max_body_size 210m;
        proxy_request_buffering off;
    }

    # WebSocket（/ws：握手 + 升级；关闭访问日志防令牌泄露）
    location /ws {
        access_log off;
        proxy_pass http://127.0.0.1:$BACKEND_PORT;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
NGX
  # 在配置最前面插入 upstream 块（update.sh 热更新靠改这里的端口平滑切流）
  sed -i "1i upstream modernchat_backend { server 127.0.0.1:$BACKEND_PORT; }" "$NGINX_TARGET"
  nginx -t || die "nginx 配置校验失败：$NGINX_TARGET"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now nginx 2>/dev/null || true
    systemctl reload nginx || systemctl restart nginx
  else
    nginx -s reload || nginx
  fi
  info "nginx 已加载配置：$NGINX_TARGET"
fi

# ============================================================================
# 11) 防火墙放行
# ============================================================================
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow "$SITE_PORT"/tcp >/dev/null 2>&1 && info "ufw 已放行 $SITE_PORT/tcp"
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port="${SITE_PORT}/tcp" >/dev/null 2>&1 && firewall-cmd --reload >/dev/null 2>&1 && info "firewalld 已放行 $SITE_PORT/tcp"
fi
warn "云服务器还需在控制台安全组放行 ${SITE_PORT}/tcp（宝塔用户：安全 → 端口规则）"

# ============================================================================
# 12) 完成
# ============================================================================
echo
printf "${C_G}=============================================================${C_0}\n"
printf "${C_G}  ModernChat 部署完成！${C_0}\n"
printf "${C_G}=============================================================${C_0}\n"
echo "  访问地址 ： $PUBLIC_URL"
echo "  首个账号 ： 打开上面地址直接注册，第一个注册的账号自动成为站长"
echo "  数据库   ： $DB_NAME@$DB_HOST:$DB_PORT（用户 $DB_USER，密码在 server/.env 的 DB_DSN 里）"
if [ "$RUN_MODE" = "systemd" ]; then
  echo "  服务管理 ： systemctl status|restart|stop modernchat@$BACKEND_PORT"
  echo "  热更新   ： bash $INSTALL_DIR/scripts/update.sh（默认不停机：新旧实例蓝绿切换）"
else
  echo "  服务管理 ： nohup 模式（无 systemd）——停止：kill \$(cat $INSTALL_DIR/server/server.pid)；启动：cd $INSTALL_DIR/server && nohup ../bin/modernchat-server >> logs/server.out 2>&1 &"
fi
echo "  升级     ： bash $INSTALL_DIR/scripts/update.sh"
echo "  后台地址 ： $PUBLIC_URL/admin（用站长账号登录后可见）"
echo
echo "  可选能力（后台 → 系统设置 里配置，即时生效）："
echo "    - SMTP 找回密码邮件 / Web Push / 对象存储 / Redis 限流共享"
echo
echo "  nginx 配置：$NGINX_TARGET（宝塔用户可复制到站点配置里）"
printf "${C_G}=============================================================${C_0}\n"
