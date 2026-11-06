#!/usr/bin/env bash
# ============================================================================
# ModernChat 完整卸载脚本
#
# 移除内容：
#   1. 停止并删除 systemd 模板单元 modernchat@<端口>.service（或 nohup 进程）
#   2. 删除 install.sh 生成的 nginx 配置（校验生成标记，绝不误删手写配置）
#      —— 自动适配标准路径 / 宝塔 / 1Panel / OpenResty
#   3. 数据库：可选 DROP DATABASE + 删除专用用户（默认保留，需确认）
#   4. 安装目录：可选整目录删除（含 server/.env、上传文件、日志；默认保留，需确认）
#
# 用法（root）：
#   bash scripts/uninstall.sh                    # 交互确认
#   bash scripts/uninstall.sh /opt/modernchat    # 指定安装目录
#   AUTO_CONFIRM=yes PURGE_DB=no DELETE_FILES=yes bash scripts/uninstall.sh
#
# 不卸载：Go / Node / MySQL / nginx 等系统级组件（可能被其它应用使用），如需
# 清理请手动处理。安装/更新日志在 /var/log/modernchat 与 <安装目录>/logs。
# ============================================================================
set -euo pipefail

C_G="\033[32m"; C_Y="\033[33m"; C_R="\033[31m"; C_B="\033[36m"; C_0="\033[0m"
info() { printf "${C_G}==>${C_0} %s\n" "$*"; }
warn() { printf "${C_Y}警告:${C_0} %s\n" "$*"; }
err()  { printf "${C_R}错误:${C_0} %s\n" "$*" >&2; }
die()  { err "$*"; exit 1; }
ask()  {
  local prompt="$1" def="${2:-}"
  if [ -n "$def" ]; then
    printf "${C_B}?%s${C_0} [%s]: " "$prompt" "$def" >&2
  else
    printf "${C_B}?%s${C_0}: " "$prompt" >&2
  fi
  read -r REPLY
  REPLY="${REPLY:-$def}"
}

[ "$(id -u)" -eq 0 ] || die "请用 root 运行（或 sudo bash uninstall.sh）"

AUTO_CONFIRM="${AUTO_CONFIRM:-no}"
PURGE_DB="${PURGE_DB:-}"          # yes/no：是否删除数据库与专用用户
DELETE_FILES="${DELETE_FILES:-}"  # yes/no：是否删除安装目录（含上传文件）

# ---------------- 定位安装目录 ----------------
INSTALL_DIR="${1:-${INSTALL_DIR:-}}"
STATE_FILE="/var/lib/modernchat/install-state"

# 从状态文件读取（install.sh 写入：参数 + nginx 路径 + 运行模式）
STATE_INSTALL_DIR=""; STATE_NGINX_CONF=""; STATE_NGINX_BIN=""
STATE_DB_NAME=""; STATE_DB_USER=""; STATE_DB_PASS=""; STATE_DB_HOST="127.0.0.1"; STATE_DB_PORT="3306"
STATE_RUN_MODE=""; STATE_BASE=""; STATE_ACTIVE=""

if [ -f "$STATE_FILE" ]; then
  while IFS='=' read -r k v; do
    case "$k" in
      PARAM_INSTALL_DIR) STATE_INSTALL_DIR="$v" ;;
      PARAM_NGINX_CONF)  STATE_NGINX_CONF="$v" ;;
      PARAM_NGINX_BIN)   STATE_NGINX_BIN="$v" ;;
      PARAM_DB_NAME)     STATE_DB_NAME="$v" ;;
      PARAM_DB_USER)     STATE_DB_USER="$v" ;;
      PARAM_DB_PASS)     STATE_DB_PASS="$v" ;;
      PARAM_DB_HOST)     STATE_DB_HOST="$v" ;;
      PARAM_DB_PORT)     STATE_DB_PORT="$v" ;;
      PARAM_RUN_MODE)    STATE_RUN_MODE="$v" ;;
    esac
  done < "$STATE_FILE"
fi
if [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/.hotupdate" ]; then
  STATE_BASE="$(awk -F= '/BASE_PORT/{print $2}' "$INSTALL_DIR/.hotupdate" 2>/dev/null || true)"
  STATE_ACTIVE="$(awk -F= '/ACTIVE_PORT/{print $2}' "$INSTALL_DIR/.hotupdate" 2>/dev/null || true)"
fi

if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR="$STATE_INSTALL_DIR"
fi
if [ -z "$INSTALL_DIR" ]; then
  for d in /opt/modernchat /www/wwwroot/modernchat /root/modernchat; do
    if [ -d "$d/.git" ]; then INSTALL_DIR="$d"; break; fi
  done
fi
[ -n "$INSTALL_DIR" ] || die "未找到安装目录：传入路径参数（bash uninstall.sh /opt/modernchat），或确认 install.sh 已运行过"
[ -d "$INSTALL_DIR" ] || warn "安装目录不存在（可能已删除）：$INSTALL_DIR"

# hotupdate 优先于 state（update.sh 会更新 ACTIVE_PORT）
if [ -f "$INSTALL_DIR/.hotupdate" ]; then
  STATE_BASE="$(awk -F= '/BASE_PORT/{print $2}' "$INSTALL_DIR/.hotupdate")"
  STATE_ACTIVE="$(awk -F= '/ACTIVE_PORT/{print $2}' "$INSTALL_DIR/.hotupdate")"
fi

info "安装目录：$INSTALL_DIR"
[ -n "$STATE_RUN_MODE" ] && info "运行模式：$STATE_RUN_MODE" || true

# ---------------- 1) 停止服务 ----------------
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  STOPPED=0
  for port in "$STATE_ACTIVE" "$STATE_BASE" "$(( ${STATE_BASE:-8080} + 1 ))"; do
    [ -n "$port" ] || continue
    if systemctl is-active --quiet "modernchat@$port.service" 2>/dev/null; then
      info "停止 modernchat@$port.service"
      systemctl stop "modernchat@$port.service" || true
      STOPPED=1
    fi
    systemctl disable "modernchat@$port.service" >/dev/null 2>&1 || true
  done
  if [ -f /etc/systemd/system/modernchat@.service ]; then
    rm -f /etc/systemd/system/modernchat@.service
    systemctl daemon-reload
    systemctl reset-failed 'modernchat@*' 2>/dev/null || true
    info "已删除 systemd 模板单元 modernchat@.service"
    STOPPED=1
  fi
  # 旧式单端口 unit（install.sh v1）
  if [ -f /etc/systemd/system/modernchat.service ]; then
    systemctl stop modernchat 2>/dev/null || true
    systemctl disable modernchat >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/modernchat.service
    systemctl daemon-reload
    info "已删除旧式单元 modernchat.service"
    STOPPED=1
  fi
  [ "$STOPPED" -eq 1 ] || warn "未发现 systemd 单元（可能以其它方式运行）"
else
  PID_FILE="$INSTALL_DIR/server/server.pid"
  if [ -f "$PID_FILE" ]; then
    pid="$(cat "$PID_FILE")"
    if kill -0 "$pid" 2>/dev/null; then
      info "停止 nohup 进程（PID $pid）"; kill "$pid" || true
      sleep 2; kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$PID_FILE"
  fi
fi

# ---------------- 2) 删除 nginx 配置 ----------------
NGINX_CONF="$STATE_NGINX_CONF"
if [ -z "$NGINX_CONF" ]; then
  # 状态缺失时按常见路径探测（只删带生成标记的文件）
  for f in /etc/nginx/conf.d/modernchat.conf /www/server/panel/vhost/nginx/modernchat.conf \
           /www/server/nginx/conf/vhost/modernchat.conf /etc/nginx/sites-available/modernchat.conf; do
    if [ -f "$f" ]; then NGINX_CONF="$f"; break; fi
  done
fi

if [ -n "$NGINX_CONF" ] && [ -f "$NGINX_CONF" ]; then
  if grep -q "由 scripts/install.sh 生成" "$NGINX_CONF"; then
    rm -f "$NGINX_CONF"
    rm -f /etc/nginx/sites-enabled/modernchat.conf   # Debian 系软链
    info "已删除 nginx 配置：$NGINX_CONF"
  else
    warn "跳过 nginx 配置 $NGINX_CONF：不含生成标记（可能是你手写的配置，请人工确认）"
  fi
  # reload：二进制探测顺序与 install.sh 一致
  NGINX_BIN="${STATE_NGINX_BIN:-}"
  if [ -z "$NGINX_BIN" ]; then
    NGINX_BIN="$(command -v nginx 2>/dev/null || true)"
  fi
  if [ -z "$NGINX_BIN" ] && [ -x /www/server/nginx/sbin/nginx ]; then
    NGINX_BIN="/www/server/nginx/sbin/nginx"
  fi
  if [ -z "$NGINX_BIN" ] && [ -x /usr/local/openresty/nginx/sbin/nginx ]; then
    NGINX_BIN="/usr/local/openresty/nginx/sbin/nginx"
  fi
  if [ -n "$NGINX_BIN" ] && [ -x "$NGINX_BIN" ]; then
    if "$NGINX_BIN" -t 2>/dev/null; then
      "$NGINX_BIN" -s reload 2>/dev/null && info "nginx 已重新加载" || true
    else
      warn "nginx -t 校验失败，请人工检查剩余配置"
    fi
  else
    warn "未找到 nginx 二进制，请手动 reload"
  fi
else
  info "未发现生成的 nginx 配置"
fi

# ---------------- 3) 数据库（默认保留，需确认） ----------------
if [ -z "$PURGE_DB" ]; then
  if [ "$AUTO_CONFIRM" = "yes" ]; then
    PURGE_DB="no"
  else
    echo
    echo "  数据库 $STATE_DB_NAME@$STATE_DB_HOST:$STATE_DB_PORT 包含用户/消息/插件等全部数据。" >&2
    ask "是否删除数据库与专用用户？[no=保留(可日后重新部署) / yes=彻底删除]" "no"; PURGE_DB="$REPLY"
  fi
fi

if [ "$PURGE_DB" = "yes" ]; then
  if [ -n "$STATE_DB_NAME" ] && command -v mysql >/dev/null 2>&1; then
    info "删除数据库 $STATE_DB_NAME 与用户 $STATE_DB_USER …"
    # 用应用账号删（权限只到自己库，比 root 密码更可用）；失败再用 root 尝试
    if mysql -h"$STATE_DB_HOST" -P"$STATE_DB_PORT" -u"$STATE_DB_USER" -p"$STATE_DB_PASS" --protocol=TCP \
         -e "DROP DATABASE IF EXISTS \`$STATE_DB_NAME\`;" 2>/dev/null \
       || mysql -h"$STATE_DB_HOST" -P"$STATE_DB_PORT" -u"$STATE_DB_USER" -p"$STATE_DB_PASS" --protocol=TCP \
         -e "DROP DATABASE IF EXISTS \`$STATE_DB_NAME\`; DROP USER IF EXISTS '$STATE_DB_USER'@'localhost'; DROP USER IF EXISTS '$STATE_DB_USER'@'%'; FLUSH PRIVILEGES;" 2>/dev/null; then
      info "数据库已删除"
    elif mysql -e "DROP DATABASE IF EXISTS \`$STATE_DB_NAME\`; DROP USER IF EXISTS '$STATE_DB_USER'@'localhost'; DROP USER IF EXISTS '$STATE_DB_USER'@'%'; FLUSH PRIVILEGES;" 2>/dev/null; then
      info "数据库已删除（本机 root 直连）"
    else
      warn "数据库删除失败：请手动执行 DROP DATABASE $STATE_DB_NAME;（宝塔用户可在面板【数据库】里删）"
    fi
  else
    warn "缺少 mysql 客户端或库名，跳过数据库删除（库：$STATE_DB_NAME）"
  fi
else
  info "保留数据库（可日后重新部署继续使用）"
fi

# ---------------- 4) 安装目录（默认保留，需确认） ----------------
if [ -z "$DELETE_FILES" ]; then
  if [ "$AUTO_CONFIRM" = "yes" ]; then
    DELETE_FILES="no"
  else
    echo
    echo "  安装目录包含：代码、server/.env（JWT_SECRET/数据库密码）、上传的图片/语音/视频、日志。" >&2
    ask "是否删除整个安装目录 $INSTALL_DIR ？[no=保留 / yes=删除]" "no"; DELETE_FILES="$REPLY"
  fi
fi

if [ "$DELETE_FILES" = "yes" ]; then
  info "删除安装目录 $INSTALL_DIR …"
  rm -rf "$INSTALL_DIR"
  rm -f "$STATE_FILE" "$STATE_FILE.tmp" 2>/dev/null || true
  info "状态文件已清理：$STATE_FILE"
else
  info "保留安装目录 $INSTALL_DIR（重新部署可直接运行 scripts/install.sh，进度与参数会被复用）"
fi

echo
printf "${C_G}=============================================================${C_0}\n"
printf "${C_G}  卸载完成${C_0}\n"
printf "${C_G}=============================================================${C_0}\n"
echo "  未卸载的系统级组件（可能被其它应用使用，需手动清理）："
echo "    - Go（/usr/local/go）  Node.js（/usr/local/node）"
echo "    - MySQL / nginx（含宝塔面板安装的版本）"
echo "  安装/更新日志：/var/log/modernchat/ 与 <安装目录>/logs/"
printf "${C_G}=============================================================${C_0}\n"
