#!/usr/bin/env bash
# ============================================================================
# ModernChat 一键安装脚本（宝塔面板风格）
#
# 流程：检测/安装运行环境（MySQL、nginx）→ 从 GitHub / Gitee Release 下载
#       预编译发行包（含前端/后端/迁移/部署脚本）→ 自动建库并写入配置
#       → systemd 常驻 + nginx 反代 → 输出可直接访问的站点链接。
#       无需 Go / Node / git —— 服务器上不编译任何东西。
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
#   RELEASE_SOURCE=gitee \        # gitee(默认，国内快) | github
#   RELEASE_TAG=latest \          # latest=最新 Release | 指定 tag 如 v0.10.3
#   bash install.sh
#
# 发行包资产（由 scripts/package-release.sh 产出并上传 Release 附件）：
#   modernchat-<版本>-linux-<架构>.tar.gz（bin/ + web/dist/ + server/migrations/ + deploy/ + scripts/update.sh）+ .sha256
#   GitHub 仓库与 Gitee 不同时，用 RELEASE_REPO_GITHUB=<owner>/<repo> 指定。
#
# 断点续跑：安装失败后重新运行同一命令，已完成的阶段自动跳过，参数不再重复
# 询问（进度与参数存 /var/lib/modernchat/install-state）。--force 全部重跑。
#
# 面板路径：自动探测 nginx 二进制与 vhost 目录 —— 标准安装（/etc/nginx）、
# 宝塔（/www/server/panel/vhost/nginx）、1Panel / OpenResty；都没有时允许
# 手动输入 nginx.conf 里 include 的目录。
#
# 日志：/var/log/modernchat/install-<时间戳>.log（latest 软链指向最新）。
#
# 卸载：bash scripts/uninstall.sh（停服务、删配置、可选删库删文件）。
#
# 设计取舍见 docs/运维手册.md §1：MySQL 8.0 是唯一必需依赖，Redis/SMTP 可选
# （装完后在「后台 → 系统设置」里补配即可）。
# ============================================================================
set -euo pipefail

# ---------------- 可预设参数 ----------------
RELEASE_SOURCE="${RELEASE_SOURCE:-gitee}"    # gitee | github
RELEASE_REPO_GITEE="${RELEASE_REPO_GITEE:-Sun-Si-Cheng/modern-chat-new}"
RELEASE_REPO_GITHUB="${RELEASE_REPO_GITHUB:-Sun-Si-Cheng/modern-chat-new}"
RELEASE_TAG="${RELEASE_TAG:-latest}"         # latest | 指定 tag（v0.10.3）
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
INSTALL_MYSQL="${INSTALL_MYSQL:-}"   # yes/no：没有 MySQL 时是否自动安装
INSTALL_NGINX="${INSTALL_NGINX:-}"   # yes/no：没有 nginx 时是否自动安装
FORCE="${FORCE:-0}"                  # 1=忽略断点记录，全部阶段重跑

# Release API / 下载前缀（也可直接用 RELEASE_API_URL / RELEASE_DL_URL 显式覆盖）
if [ -z "${RELEASE_API_URL:-}" ]; then
  if [ "$RELEASE_SOURCE" = "github" ]; then
    RELEASE_API_URL="https://api.github.com/repos/${RELEASE_REPO_GITHUB}/releases/latest"
    RELEASE_DL_URL="https://github.com/${RELEASE_REPO_GITHUB}/releases/download"
  else
    RELEASE_API_URL="https://gitee.com/api/v5/repos/${RELEASE_REPO_GITEE}/releases/latest"
    RELEASE_DL_URL="https://gitee.com/${RELEASE_REPO_GITEE}/releases/download"
  fi
fi
RELEASE_DL_URL="${RELEASE_DL_URL:-https://gitee.com/${RELEASE_REPO_GITEE}/releases/download}"

# ---------------- 目录约定 ----------------
STATE_DIR="/var/lib/modernchat"          # 断点续跑状态与参数（600 权限）
STATE_FILE="$STATE_DIR/install-state"
LOG_DIR="/var/log/modernchat"            # 安装日志
NGINX_CONF_NAME="modernchat.conf"
MANAGED_MARK="由 scripts/install.sh 生成"  # 我们生成的 nginx 配置标记（卸载时校验）

# ---------------- 输出工具 ----------------
C_G="\033[32m"; C_Y="\033[33m"; C_R="\033[31m"; C_B="\033[36m"; C_0="\033[0m"
info()  { printf "${C_G}==>${C_0} %s\n" "$*"; }
warn()  { printf "${C_Y}警告:${C_0} %s\n" "$*"; }
err()   { printf "${C_R}错误:${C_0} %s\n" "$*" >&2; }
die()   { err "$*"; err "已完成阶段已记录；重新运行同一命令将从当前阶段继续（已完成阶段自动跳过，--force 全部重跑）"; exit 1; }
ask()   { # ask "提示" "默认值" -> 回显到 REPLY
  local prompt="$1" def="${2:-}"
  if [ -n "$def" ]; then
    printf "${C_B}?%s${C_0} [%s]: " "$prompt" "$def" >&2
  else
    printf "${C_B}?%s${C_0}: " "$prompt" >&2
  fi
  # 支持 curl ... | bash 管道运行：stdin 是脚本流，交互输入必须从终端（/dev/tty）读
  if [ -t 0 ]; then
    read -r REPLY
  elif read -r REPLY </dev/tty 2>/dev/null; then
    :
  else
    REPLY=""   # 无终端（纯自动化）：全部取默认值
  fi
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
# 断点续跑：状态与参数恢复
#   state 文件分两段：参数段（PARAM_*，重跑时不重复询问）+ 阶段段（STAGE_x=done）
#   已有安装时自动发现：INSTALL_DIR 预设 > /opt/modernchat 等常见路径探测
# ============================================================================
mkdir -p "$STATE_DIR" "$LOG_DIR"
if [ -z "$INSTALL_DIR" ]; then
  for d in /opt/modernchat /www/wwwroot/modernchat /root/modernchat; do
    if [ -f "$STATE_DIR/install-state" ] && grep -q "^PARAM_INSTALL_DIR=$d\$" "$STATE_DIR" 2>/dev/null; then
      INSTALL_DIR="$d"; info "发现未完成的安装（$d），将从上次进度继续"; break
    fi
  done
fi

if [ -f "$STATE_FILE" ]; then
  # 恢复参数：只填"当前为空/未预设"的项（命令行预设的值优先于 state）
  while IFS='=' read -r k v; do
    case "$k" in
      PARAM_DOMAIN)       [ -z "$DOMAIN" ] && DOMAIN="$v" ;;
      PARAM_SITE_PORT)    [ -z "$SITE_PORT" ] && SITE_PORT="$v" ;;
      PARAM_BACKEND_PORT) [ -z "$BACKEND_PORT" ] && BACKEND_PORT="$v" ;;
      PARAM_DB_MODE)      [ -z "$DB_MODE" ] && DB_MODE="$v" ;;
      PARAM_DB_ROOT_PASS) [ -z "$DB_ROOT_PASS" ] && DB_ROOT_PASS="$v" ;;
      PARAM_DB_USER)      [ -z "$DB_USER" ] && DB_USER="$v" ;;
      PARAM_DB_PASS)      [ -z "$DB_PASS" ] && DB_PASS="$v" ;;
      PARAM_DB_NAME)      [ "$DB_NAME" = "modernchat" ] && DB_NAME="$v" ;;
      PARAM_RELEASE_TAG)  [ "$RELEASE_TAG" = "latest" ] && RELEASE_TAG="$v" ;;
      PARAM_PUBLIC_URL)   PUBLIC_URL="${PUBLIC_URL:-$v}" ;;
      PARAM_JWT_SECRET)   JWT_SECRET="${JWT_SECRET:-$v}" ;;
      PARAM_RUN_MODE)     RUN_MODE="${RUN_MODE:-$v}" ;;
      PARAM_NGINX_CONF)   NGINX_CONF_PATH="${NGINX_CONF_PATH:-$v}" ;;
      PARAM_NGINX_BIN)    NGINX_BIN="${NGINX_BIN:-$v}" ;;
    esac
  done < "$STATE_FILE"
  chmod 600 "$STATE_FILE" 2>/dev/null || true
fi

# 交互问答：env 预设 > state 恢复 > 询问
echo
printf "${C_B}=============================================================${C_0}\n"
printf "${C_B}        ModernChat 一键安装（发行版下载 → 部署）${C_0}\n"
printf "${C_B}=============================================================${C_0}\n\n"

if [ -f "$STATE_FILE" ] && grep -q "^STAGE_release=done$" "$STATE_FILE" 2>/dev/null; then
  info "检测到上次安装进度，已完成阶段将自动跳过（--force 可全部重跑）"
fi

[ -n "$INSTALL_DIR" ] || { ask "安装目录" "/opt/modernchat"; INSTALL_DIR="$REPLY"; }
[ -n "$DOMAIN" ]      || { ask "访问域名（没有域名直接回车，将用服务器公网 IP）" ""; DOMAIN="$REPLY"; }
[ -n "${SITE_PORT+x}" ]    || { ask "站点端口（nginx 对外端口）" "80"; SITE_PORT="$REPLY"; }
[ -n "${BACKEND_PORT+x}" ] || { ask "后端服务端口（仅本机访问）" "8080"; BACKEND_PORT="$REPLY"; }

if [ -z "${DB_MODE:-}" ]; then
  echo >&2
  echo "  数据库选择（MySQL 8.0 是唯一必需依赖）：" >&2
  echo "    1) 本机还没有 MySQL —— 脚本自动安装并建库（推荐，Ubuntu/CentOS 系）" >&2
  echo "    2) 已有 MySQL 实例（宝塔/云数据库等）—— 填连接信息" >&2
  ask "请选择 [1/2]" "1"; DB_MODE="$REPLY"
  [ "$DB_MODE" = "2" ] && DB_MODE="external" || DB_MODE="auto"
fi

# ---------------- 日志初始化（参数确定后，全部输出镜像到文件） ----------------
mkdir -p "$INSTALL_DIR"
LOG_FILE="$LOG_DIR/install-$(date +%Y%m%d-%H%M%S).log"
touch "$LOG_FILE"
ln -sfn "$LOG_FILE" "$LOG_DIR/install-latest.log"
exec > >(tee -a "$LOG_FILE") 2>&1
info "日志：$LOG_FILE"

# ---------------- 状态记录 ----------------
save_state() { # save_state <key> <value>
  local k="$1" v="$2"
  grep -v "^$k=" "$STATE_FILE" 2>/dev/null > "$STATE_FILE.tmp" || true
  mv "$STATE_FILE.tmp" "$STATE_FILE"
  echo "$k=$v" >> "$STATE_FILE"
  chmod 600 "$STATE_FILE"
}
stage_done() { grep -q "^STAGE_$1=done$" "$STATE_FILE" 2>/dev/null; }
mark_stage() { save_state "STAGE_$1" "done"; }

# 把交互答案持久化（断点续跑时不再重复询问；含密码，权限 600）
save_state "PARAM_INSTALL_DIR"  "$INSTALL_DIR"
save_state "PARAM_DOMAIN"       "$DOMAIN"
save_state "PARAM_SITE_PORT"    "$SITE_PORT"
save_state "PARAM_BACKEND_PORT" "$BACKEND_PORT"
save_state "PARAM_DB_MODE"      "$DB_MODE"
save_state "PARAM_DB_HOST"      "$DB_HOST"
save_state "PARAM_DB_PORT"      "$DB_PORT"
save_state "PARAM_DB_NAME"      "$DB_NAME"

# 失败时提示续跑方式
CURRENT_STAGE="(初始化)"
on_fail() {
  err "安装中断于阶段：$CURRENT_STAGE"
  err "已完成阶段已记录；重新运行同一命令将从该阶段继续（如：bash install.sh）"
  err "全部重跑：FORCE=1 bash install.sh   |   日志：$LOG_FILE"
}
trap on_fail ERR

# ============================================================================
# 阶段 1：基础工具 + MySQL —— 幂等（发行版模式无需 Go / Node / git）
# ============================================================================
CURRENT_STAGE="环境安装(deps)"
stage_deps() {
  NEED_BASE=()
  command -v curl >/dev/null 2>&1 || NEED_BASE+=(curl)
  command -v tar  >/dev/null 2>&1 || NEED_BASE+=(tar)
  if [ "${#NEED_BASE[@]}" -gt 0 ]; then
    info "安装基础工具：${NEED_BASE[*]}"
    pkg_update; pkg_install "${NEED_BASE[@]}"
  fi

  info "检测 MySQL …"

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
      if command -v systemctl >/dev/null 2>&1; then
        systemctl enable --now mysql 2>/dev/null || systemctl enable --now mysqld 2>/dev/null || systemctl enable --now mariadb 2>/dev/null || true
      fi
    fi
  fi
  [ -n "$MYSQL_CLI" ] || die "本机没有 mysql 客户端。请在宝塔面板安装 MySQL 8.0，或把 DB_MODE 设为 external 填已有实例"
}
stage_done deps && ! [ "$FORCE" = "1" ] && { info "跳过已完成阶段：环境安装"; } || stage_deps
mark_stage deps

# ============================================================================
# 阶段 2：数据库参数与建库
# ============================================================================
CURRENT_STAGE="数据库(db)"
if [ "$DB_MODE" = "external" ]; then
  [ -n "${DB_ROOT_PASS:-}" ] || { ask "数据库 root（或有建库权限的账号）密码" ""; DB_ROOT_PASS="$REPLY"; }
  [ -n "${DB_USER:-}" ]      || { ask "应用使用的数据库用户名（回车自动创建 modernchat）" "modernchat"; DB_USER="$REPLY"; }
else
  DB_ROOT_USER="${DB_ROOT_USER:-root}"
  DB_ROOT_AUTH_SOCKET=0
  if [ "$DB_MODE" = "auto" ]; then
    if mysql -uroot -e "SELECT 1" >/dev/null 2>&1; then
      DB_ROOT_PASS=""
    elif mysql -uroot --skip-password -e "SELECT 1" >/dev/null 2>&1; then
      DB_ROOT_PASS=""
    fi
  fi
  [ -n "${DB_ROOT_PASS:-}" ] || { ask "本机 MySQL root 密码（auth_socket/无密码直接回车）" ""; DB_ROOT_PASS="$REPLY"; }
  DB_USER="${DB_USER:-modernchat}"
fi
DB_HOST="${DB_HOST:-127.0.0.1}"; DB_PORT="${DB_PORT:-3306}"
[ -n "${DB_PASS:-}" ] || DB_PASS="$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
save_state "PARAM_DB_ROOT_USER" "$DB_ROOT_USER"
save_state "PARAM_DB_ROOT_PASS" "$DB_ROOT_PASS"
save_state "PARAM_DB_USER"      "$DB_USER"
save_state "PARAM_DB_PASS"      "$DB_PASS"

stage_done db && ! [ "$FORCE" = "1" ] && { info "跳过已完成阶段：数据库"; } || {
  mysql_exec() {
    if [ -n "$DB_ROOT_PASS" ]; then
      mysql -h"$DB_HOST" -P"$DB_PORT" -u"$DB_ROOT_USER" -p"$DB_ROOT_PASS" --protocol=TCP "$@"
    else
      mysql -h"$DB_HOST" -P"$DB_PORT" -u"$DB_ROOT_USER" --protocol=TCP "$@"
    fi
  }
  info "创建数据库 ${DB_NAME} 与用户 ${DB_USER} …"
  # 幂等：IF NOT EXISTS + 密码刷新；同时建 localhost 与 % 用户（宝塔/云库常限远程 %）
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
}
mark_stage db

# ============================================================================
# 阶段 3：下载发行包（GitHub / Gitee Release，含前端/后端/迁移/部署脚本）
# ============================================================================
CURRENT_STAGE="下载发行版(release)"
stage_done release && ! [ "$FORCE" = "1" ] && { info "跳过已完成阶段：下载发行版"; } || {
  # 解析版本：latest → 查 Release API；指定 tag 直接用
  if [ "$RELEASE_TAG" = "latest" ]; then
    info "查询最新 Release：$RELEASE_API_URL"
    RESP="$(curl -fsS --max-time 30 "$RELEASE_API_URL")" || die "查询 Release 失败（检查网络 / RELEASE_SOURCE）"
    TAG_NAME="$(printf '%s' "$RESP" | grep -o '"tag_name"[^,}]*' | head -n1 | sed 's/.*: *"//;s/"$//')"
    [ -n "$TAG_NAME" ] || die "未解析到最新 tag（仓库是否已发布 Release？见 scripts/package-release.sh）"
  else
    TAG_NAME="$RELEASE_TAG"
  fi
  info "安装版本：$TAG_NAME"
  save_state "PARAM_RELEASE_TAG" "$TAG_NAME"

  ASSET="modernchat-${TAG_NAME#v}-linux-$GO_ARCH.tar.gz"
  info "下载 $ASSET …"
  mkdir -p "$INSTALL_DIR.tmp"
  curl -fL --retry 3 -o "$INSTALL_DIR.tmp/$ASSET" "$RELEASE_DL_URL/${TAG_NAME}/$ASSET" \
    || die "下载失败：$RELEASE_DL_URL/${TAG_NAME}/$ASSET（Release 是否有该架构附件？）"

  info "校验 sha256 …"
  if curl -fsS --max-time 30 -o "$INSTALL_DIR.tmp/$ASSET.sha256" "$RELEASE_DL_URL/${TAG_NAME}/$ASSET.sha256" 2>/dev/null; then
    EXPECTED="$(awk '{print $1}' "$INSTALL_DIR.tmp/$ASSET.sha256")"
    ACTUAL="$(sha256sum "$INSTALL_DIR.tmp/$ASSET" | awk '{print $1}')"
    [ "$EXPECTED" = "$ACTUAL" ] || die "sha256 不匹配：期望 $EXPECTED，实际 $ACTUAL（下载损坏或被篡改）"
    info "校验通过"
  else
    warn "无 .sha256 附件，跳过校验"
  fi

  # 首次安装清空残留；重装/续跑只覆盖包内文件（保留 server/.env、data/、logs/）
  if [ ! -f "$INSTALL_DIR/server/.env" ]; then
    rm -rf "$INSTALL_DIR"
    mkdir -p "$INSTALL_DIR"
  fi
  tar -xzf "$INSTALL_DIR.tmp/$ASSET" -C "$INSTALL_DIR"
  rm -rf "$INSTALL_DIR.tmp"
  info "发行包已就位：bin/ + web/dist/ + server/migrations/ + scripts/update.sh"
}
mark_stage release
cd "$INSTALL_DIR"

# ============================================================================
# 阶段 5：写配置（server/.env）——JWT_SECRET 固化到 state，续跑不再换
# ============================================================================
CURRENT_STAGE="配置(config)"
if [ -z "${JWT_SECRET:-}" ]; then
  JWT_SECRET="$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  save_state "PARAM_JWT_SECRET" "$JWT_SECRET"
fi

if [ -z "${PUBLIC_URL:-}" ]; then
  if [ -n "$DOMAIN" ]; then
    if [ "$SITE_PORT" = "80" ]; then PUBLIC_URL="http://$DOMAIN"; else PUBLIC_URL="http://$DOMAIN:$SITE_PORT"; fi
  else
    PUBLIC_IP="$(curl -fsS --max-time 5 https://ifconfig.me 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}' || echo '127.0.0.1')"
    PUBLIC_URL="http://$PUBLIC_IP:$SITE_PORT"
  fi
  save_state "PARAM_PUBLIC_URL" "$PUBLIC_URL"
fi

stage_done config && ! [ "$FORCE" = "1" ] && [ -f server/.env ] && { info "跳过已完成阶段：配置"; } || {
  cat > server/.env <<ENV
# 由 scripts/install.sh 生成（$(date '+%F %T')）。修改后需 systemctl restart modernchat@<端口>
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
# 在线更新（Release 发行版）：后台图形化更新与 scripts/update.sh 共用
RELEASE_API_URL=$RELEASE_API_URL
RELEASE_DL_URL=$RELEASE_DL_URL
ENV
  chmod 600 server/.env
  info "已写入 server/.env（MIGRATE=true，首次启动自动建表）"
}
mark_stage config

# ============================================================================
# 阶段 6：常驻运行（systemd 模板单元；无 systemd 降级 nohup）
# ============================================================================
CURRENT_STAGE="服务(service)"
RUN_MODE="${RUN_MODE:-}"
stage_done service && ! [ "$FORCE" = "1" ] && { info "跳过已完成阶段：常驻服务"; } || {
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
    (cd "$INSTALL_DIR/server" && nohup "$INSTALL_DIR/bin/modernchat-server" >> logs/server.out 2>&1 & echo $! > server.pid)
    RUN_MODE="nohup"
    warn "重启机器后需手动启动：cd $INSTALL_DIR/server && nohup ../bin/modernchat-server >> logs/server.out 2>&1 &"
  fi
  save_state "PARAM_RUN_MODE" "$RUN_MODE"

  info "等待后端启动（含首次建表）…"
  ok=0
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:$BACKEND_PORT/api/health" >/dev/null 2>&1; then ok=1; info "后端健康检查通过"; break; fi
    sleep 1
  done
  if [ "$ok" -ne 1 ]; then
    if [ "$RUN_MODE" = "systemd" ]; then journalctl -u "modernchat@$BACKEND_PORT" -n 30 --no-pager || true
    else tail -n 30 "$INSTALL_DIR/server/logs/server.out" || true; fi
    die "后端 60s 内未就绪，上方是最近日志"
  fi
}
mark_stage service

# ============================================================================
# 阶段 7：nginx（自动适配面板路径；reload 用探测到的二进制）
# ============================================================================
CURRENT_STAGE="nginx"
stage_done nginx && ! [ "$FORCE" = "1" ] && { info "跳过已完成阶段：nginx"; } || {
  # --- 探测 nginx 二进制与 vhost 目录（宝塔 / 1Panel / OpenResty / 标准） ---
  NGINX_BIN="${NGINX_BIN:-}"
  PANEL_HINT=""
  if [ -z "$NGINX_BIN" ]; then
    if command -v nginx >/dev/null 2>&1; then
      NGINX_BIN="$(command -v nginx)"
    elif [ -x /www/server/nginx/sbin/nginx ]; then
      NGINX_BIN="/www/server/nginx/sbin/nginx"; PANEL_HINT="宝塔"
    elif [ -x /www/server/openresty/nginx/sbin/nginx ]; then
      NGINX_BIN="/www/server/openresty/nginx/sbin/nginx"; PANEL_HINT="宝塔(OpenResty)"
    elif [ -x /usr/local/openresty/nginx/sbin/nginx ]; then
      NGINX_BIN="/usr/local/openresty/nginx/sbin/nginx"; PANEL_HINT="OpenResty"
    elif [ -x /opt/1panel/apps/openresty/openresty/sbin/nginx ]; then
      NGINX_BIN="/opt/1panel/apps/openresty/openresty/sbin/nginx"; PANEL_HINT="1Panel"
    fi
  fi

  NGINX_CONF_DIR="${NGINX_CONF_DIR:-}"
  if [ -z "$NGINX_CONF_DIR" ]; then
    if [ -d /www/server/panel ]; then PANEL_HINT="${PANEL_HINT:-宝塔}"; fi
    # 候选按优先级：面板 vhost > 标准目录
    for d in /www/server/panel/vhost/nginx /www/server/nginx/conf/vhost \
             /opt/1panel/apps/openresty/openresty/conf/conf.d \
             /usr/local/openresty/nginx/conf/conf.d \
             /etc/nginx/conf.d /etc/nginx/sites-available; do
      if [ -d "$d" ]; then NGINX_CONF_DIR="$d"; break; fi
    done
  fi

  if [ -z "$NGINX_BIN" ] && [ -d /www/server/panel ]; then
    PANEL_HINT="${PANEL_HINT:-宝塔}"
    NGINX_BIN="/www/server/nginx/sbin/nginx"
  fi

  if [ -z "$NGINX_CONF_DIR" ] && [ -n "$NGINX_BIN" ]; then
    # 从主配置里找 include 目录（取第一个含 *.conf 的目录）
    inc="$("$NGINX_BIN" -T 2>/dev/null | grep -oE 'include [^;]+\.conf;' | head -n1 | awk '{print $2}')"
    if [ -n "$inc" ]; then NGINX_CONF_DIR="$(dirname "$inc")"; fi
  fi

  if [ -z "$NGINX_CONF_DIR" ]; then
    ask "未识别 nginx 的 vhost 目录（宝塔/1Panel 路径各不同）。请输入 nginx.conf 中 include 的 conf 目录" ""; NGINX_CONF_DIR="$REPLY"
    [ -d "$NGINX_CONF_DIR" ] || die "目录不存在：$NGINX_CONF_DIR"
  fi
  if [ -z "$NGINX_BIN" ]; then
    ask "未找到 nginx 可执行文件，请输入完整路径（用于 -t 校验与 reload）" "nginx"; NGINX_BIN="$REPLY"
  fi

  info "nginx：二进制=$NGINX_BIN | vhost 目录=$NGINX_CONF_DIR${PANEL_HINT:+ | 面板=$PANEL_HINT}"
  save_state "PARAM_NGINX_BIN"   "$NGINX_BIN"
  save_state "PARAM_NGINX_CONF"  "$NGINX_CONF_DIR/$NGINX_CONF_NAME"

  # 无 nginx 时可自动安装（包管理安装后重新探测）
  if [ ! -x "$NGINX_BIN" ] && ! command -v nginx >/dev/null 2>&1; then
    if [ -z "$INSTALL_NGINX" ]; then
      ask "没有检测到 nginx，是否自动安装（前端静态文件需要它托管）？[yes/no]" "yes"; INSTALL_NGINX="$REPLY"
    fi
    if [ "$INSTALL_NGINX" = "yes" ]; then
      info "安装 nginx …"; pkg_update; pkg_install nginx
      NGINX_BIN="$(command -v nginx || echo /usr/sbin/nginx)"
      NGINX_CONF_DIR="$([ -d /etc/nginx/conf.d ] && echo /etc/nginx/conf.d || echo /etc/nginx/sites-available)"
      save_state "PARAM_NGINX_BIN"   "$NGINX_BIN"
      save_state "PARAM_NGINX_CONF"  "$NGINX_CONF_DIR/$NGINX_CONF_NAME"
    else
      warn "跳过 nginx 安装。前端静态文件在 $INSTALL_DIR/web/dist，请自行托管（宝塔用户：建站点后把配置粘进站点的 nginx 配置）"
      save_state "PARAM_NGINX_CONF" ""
    fi
  fi

  if [ -x "$NGINX_BIN" ] || command -v nginx >/dev/null 2>&1; then
    NGINX_SERVER_NAME="${DOMAIN:-_}"
    NGINX_ROOT="$INSTALL_DIR/web/dist"
    NGINX_TARGET="$NGINX_CONF_DIR/$NGINX_CONF_NAME"
    save_state "PARAM_NGINX_CONF" "$NGINX_TARGET"

    if [ "$NGINX_CONF_DIR" = "/etc/nginx/sites-available" ]; then
      ln -sf "$NGINX_TARGET" /etc/nginx/sites-enabled/modernchat.conf
      rm -f /etc/nginx/sites-enabled/default   # 移除发行版默认站点，避免抢占 80
    fi

    cat > "$NGINX_TARGET" <<NGX
# ModernChat —— $MANAGED_MARK（卸载请用 scripts/uninstall.sh）
upstream modernchat_backend { server 127.0.0.1:$BACKEND_PORT; }

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
        proxy_pass http://modernchat_backend;
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

    "$NGINX_BIN" -t || die "nginx 配置校验失败：$NGINX_TARGET"
    # reload：优先二进制 -s reload（面板编译版通常不走 systemd）；失败再试 systemd；最后直接启动
    if "$NGINX_BIN" -s reload 2>/dev/null; then
      info "nginx 已平滑加载配置"
    elif command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^nginx'; then
      systemctl enable --now nginx 2>/dev/null || true
      systemctl reload nginx || systemctl restart nginx
    else
      "$NGINX_BIN" || die "nginx 启动失败"
    fi
    info "nginx 已加载配置：$NGINX_TARGET"
    if [ -n "$PANEL_HINT" ]; then
      warn "检测到 $PANEL_HINT：站点已在 vhost 目录生效，但面板『网站』列表不会显示它（不影响使用）；卸载用 bash $INSTALL_DIR/scripts/uninstall.sh"
    fi
  fi
}
mark_stage nginx

# ============================================================================
# 阶段 8：防火墙
# ============================================================================
CURRENT_STAGE="防火墙(firewall)"
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow "$SITE_PORT"/tcp >/dev/null 2>&1 && info "ufw 已放行 $SITE_PORT/tcp"
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port="${SITE_PORT}/tcp" >/dev/null 2>&1 && firewall-cmd --reload >/dev/null 2>&1 && info "firewalld 已放行 $SITE_PORT/tcp"
fi
warn "云服务器还需在控制台安全组放行 ${SITE_PORT}/tcp（宝塔用户：安全 → 端口规则）"
mark_stage firewall

# ============================================================================
# 完成
# ============================================================================
save_state "STAGE_ALL" "done"
trap - ERR
info "全部阶段完成，日志：$LOG_FILE"
echo
printf "${C_G}=============================================================${C_0}\n"
printf "${C_G}  ModernChat 部署完成！${C_0}\n"
printf "${C_G}=============================================================${C_0}\n"
echo "  访问地址 ： $PUBLIC_URL"
echo "  首个账号 ： 打开上面地址直接注册，第一个注册的账号自动成为站长"
echo "  数据库   ： $DB_NAME@$DB_HOST:$DB_PORT（用户 $DB_USER，密码在 server/.env 的 DB_DSN 里）"
if [ "$RUN_MODE" = "systemd" ]; then
  echo "  服务管理 ： systemctl status|restart|stop modernchat@$BACKEND_PORT"
  echo "  图形化更新： 站长后台 → 在线更新（或命令行 bash $INSTALL_DIR/scripts/update.sh）"
else
  echo "  服务管理 ： nohup 模式（无 systemd）——停止：kill \$(cat $INSTALL_DIR/server/server.pid)；启动：cd $INSTALL_DIR/server && nohup ../bin/modernchat-server >> logs/server.out 2>&1 &"
fi
echo "  升级     ： 命令行 bash $INSTALL_DIR/scripts/update.sh（下载新发行包并重启）"
echo "  卸载     ： bash $INSTALL_DIR/scripts/uninstall.sh（停服务/删配置，可选删库删文件）"
echo "  后台地址 ： $PUBLIC_URL/admin（用站长账号登录后可见）"
echo
echo "  可选能力（后台 → 系统设置 里配置，即时生效）："
echo "    - SMTP 找回密码邮件 / Web Push / 对象存储 / Redis 限流共享"
echo
echo "  nginx 配置：${NGINX_CONF_PATH:-（未生成，见上方说明）}"
echo "  安装日志 ： $LOG_DIR/install-latest.log"
printf "${C_G}=============================================================${C_0}\n"
