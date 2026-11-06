#!/usr/bin/env bash
# ModernChat 一键升级：从 Gitee 拉取最新代码 → 原子替换产物 → 热更新（不停机）
#
# 用法（在仓库根目录）：
#   ./scripts/update.sh                 # 拉 main，默认热更新（不停机）
#   ./scripts/update.sh dev             # 拉指定分支
#   ./scripts/update.sh --cold          # 强制旧式重启（systemctl restart）
#   ./scripts/update.sh --no-restart    # 只构建并原子替换产物，稍后手动切换
#   RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
#
# 热更新原理（蓝绿双实例）：
#   新二进制构建为临时文件后 mv 原子替换（运行中的老进程继续用老 inode）；
#   新实例起在备用端口（8080↔8081 对偶），健康检查通过后把 nginx upstream
#   切到新端口并 reload（reload 对已建立连接零中断）；最后 stop 老实例——
#   SIGTERM 触发后端优雅停机（在途 HTTP 收尾、WS 断开），前端断线自动重连
#   到新实例并按 clientMsgId 增量补齐，消息不丢。
#   前提：install.sh 部署（模板单元 modernchat@<端口>.service + nginx upstream）。
#   检测到旧式单端口 unit 时会自动迁移到模板单元再热更新。
#
# 设计取舍：升级由运维脚本执行，应用内不提供"触发 git pull"的接口——
# 那等于给管理员账号挂上远程代码执行，账号一旦泄露就等于丢服务器。
set -euo pipefail

BRANCH="${BRANCH:-main}"
NO_RESTART=0
COLD=0
for arg in "$@"; do
  case "$arg" in
    --no-restart) NO_RESTART=1 ;;
    --cold) COLD=1 ;;
    *) BRANCH="$arg" ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# ---------------- 更新日志 ----------------
LOG_DIR="$ROOT/logs"; mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/update-$(date +%Y%m%d-%H%M%S).log"
touch "$LOG_FILE"; ln -sfn "$LOG_FILE" "$LOG_DIR/update-latest.log"
exec > >(tee -a "$LOG_FILE") 2>&1
info "日志：$LOG_FILE"

MODULE="gitee.com/Sun-Si-Cheng/modern-chat-new/server"
HOTSTATE="$ROOT/.hotupdate"          # BASE_PORT / ACTIVE_PORT（install.sh 生成，update.sh 维护）
NGINX_CANDIDATES=(
  /etc/nginx/conf.d/modernchat.conf
  /www/server/panel/vhost/nginx/modernchat.conf
  /www/server/nginx/conf/vhost/modernchat.conf
  /opt/1panel/apps/openresty/openresty/conf/conf.d/modernchat.conf
  /usr/local/openresty/nginx/conf/conf.d/modernchat.conf
  /etc/nginx/sites-available/modernchat.conf
)
BEFORE="$(git rev-parse --short HEAD)"

echo "==> 当前: $BEFORE ($(git rev-parse --abbrev-ref HEAD))"

# 1) 拉取（--ff-only：本地有分叉就停下报错，绝不自动合并）
git fetch --all --prune
git checkout "$BRANCH"
git pull --ff-only origin "$BRANCH"

AFTER="$(git rev-parse --short HEAD)"
if [ "$BEFORE" = "$AFTER" ]; then
  echo "==> 已是最新（$AFTER），无需重建"
  exit 0
fi

VERSION="$(git describe --tags --always 2>/dev/null || echo "$AFTER")"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# 2) 后端：构建到临时名再 mv —— 直接覆盖运行中的二进制会 ETXTBSH
echo "==> 构建后端 $VERSION ($AFTER)"
mkdir -p bin
(cd server && go build -trimpath -ldflags \
  "-s -w -X ${MODULE}/internal/version.Version=${VERSION} -X ${MODULE}/internal/version.Commit=${AFTER} -X ${MODULE}/internal/version.BuildTime=${BUILD_TIME}" \
  -o ../bin/.modernchat-server.new ./cmd/server)
mv -f bin/.modernchat-server.new bin/modernchat-server

# 3) 前端：构建到 dist.new 再原子交换，避免构建期间用户请求到半成品
echo "==> 构建前端"
rm -rf web/dist.new
(cd web && npm ci --no-audit --no-fund && npx vite build --outDir dist.new --emptyOutDir && node scripts/precompress.mjs dist.new)
rm -rf web/dist.old
if [ -d web/dist ]; then mv web/dist web/dist.old; fi
mv web/dist.new web/dist
rm -rf web/dist.old

if [ "$NO_RESTART" -eq 1 ]; then
  echo "==> 跳过切换（产物已原子替换：bin/modernchat-server、web/dist；下次切换时生效）"
  exit 0
fi

# ----------------------------------------------------------------------------
# 4) 切换流量。优先级：热更新（模板单元 + nginx upstream）→ RESTART_CMD
#    → docker compose → systemd restart → nohup 提示
# ----------------------------------------------------------------------------
have_systemd() { [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; }

nginx_conf_for_hot() {  # 输出可热切换的 nginx 配置路径，找不到则输出空
  local f
  # 1) install.sh 记录的实际路径（状态文件）
  if [ -f /var/lib/modernchat/install-state ]; then
    f="$(sed -n 's/^PARAM_NGINX_CONF=//p' /var/lib/modernchat/install-state | head -n1)"
    if [ -n "$f" ] && [ -f "$f" ] && grep -q "upstream modernchat_backend" "$f"; then
      echo "$f"; return
    fi
  fi
  # 2) 常见路径兜底（标准 / 宝塔 / 1Panel / OpenResty）
  for f in "${NGINX_CANDIDATES[@]}"; do
    if [ -f "$f" ] && grep -q "upstream modernchat_backend" "$f"; then
      echo "$f"; return
    fi
  done
  echo ""
}

# 旧式单端口 unit（install.sh v1 / 手动部署）自动迁移为模板单元
migrate_legacy_unit() {
  have_systemd || return 1
  [ -f /etc/systemd/system/modernchat.service ] || return 1
  systemctl list-unit-files 'modernchat@.service' 2>/dev/null | grep -q 'modernchat@' && return 1
  local port; port="$(sed -n 's/^PORT=//p' "$ROOT/server/.env" 2>/dev/null | head -n1)"; port="${port:-8080}"
  write_template_unit
  cat > "$HOTSTATE" <<EOF
BASE_PORT=$port
ACTIVE_PORT=$port
EOF
  systemctl daemon-reload
  echo "==> 已把旧式 modernchat.service 迁移为模板单元 modernchat@$port（本次起支持热更新）"
}

write_template_unit() {
  cat > /etc/systemd/system/modernchat@.service <<UNIT
[Unit]
Description=ModernChat IM server (port %i)
After=network.target mysql.service mysqld.service

[Service]
Type=simple
WorkingDirectory=$ROOT/server
ExecStart=$ROOT/bin/modernchat-server
Restart=on-failure
RestartSec=3
Environment=PORT=%i
Environment=DATA_DIR=$ROOT/server/data
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT
}

hot_reload() {
  local base active next conf
  read -r base active < <(awk -F= '/BASE_PORT/{b=$2} /ACTIVE_PORT/{a=$2} END{print b, a}' "$HOTSTATE")
  [ -n "${active:-}" ] || return 1
  next=$([ "$active" = "$base" ] && echo "$((base + 1))" || echo "$base")

  echo "==> 热更新：新实例起在 :$next（当前 :$active）"
  systemctl enable "modernchat@$next.service" >/dev/null 2>&1 || true
  systemctl start "modernchat@$next.service"

  echo "==> 等待新实例健康…"
  local ok=0
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:$next/api/health" >/dev/null 2>&1; then ok=1; break; fi
    sleep 1
  done
  if [ "$ok" -ne 1 ]; then
    journalctl -u "modernchat@$next" -n 20 --no-pager || true
    systemctl stop "modernchat@$next.service" || true
    echo "!! 新实例未就绪，老实例继续服务（本次升级未生效，请查日志后重试）" >&2
    return 1
  fi

  conf="$(nginx_conf_for_hot)"
  if [ -z "$conf" ]; then
    echo "!! 找不到含 upstream modernchat_backend 的 nginx 配置，无法平滑切流" >&2
    systemctl stop "modernchat@$next.service" || true
    return 1
  fi

  echo "==> nginx 切流 :$active → :$next（reload，已建立连接不受影响）"
  sed -i "s/server 127\.0\.0\.1:[0-9]*;/server 127.0.0.1:$next;/" "$conf"
  nginx -t || { echo "!! nginx 配置校验失败，回滚 upstream"; sed -i "s/server 127\.0\.0\.1:[0-9]*;/server 127.0.0.1:$active;/" "$conf"; nginx -t; systemctl stop "modernchat@$next.service"; return 1; }
  nginx -s reload || {
    echo "!! nginx reload 失败，回滚 upstream（流量仍在老实例）"
    sed -i "s/server 127\.0\.0\.1:[0-9]*;/server 127.0.0.1:$active;/" "$conf"
    nginx -t && nginx -s reload || true
    systemctl stop "modernchat@$next.service" || true
    return 1
  }

  # 老实例优雅退出：SIGTERM → 在途 HTTP 收尾（≤10s）→ WS 断开 → 客户端重连新实例
  echo "==> 停止老实例 :$active（优雅停机，客户端自动重连）"
  systemctl stop "modernchat@$active.service" || true
  systemctl disable "modernchat@$active.service" >/dev/null 2>&1 || true
  systemctl enable "modernchat@$next.service" >/dev/null 2>&1 || true

  cat > "$HOTSTATE" <<EOF
BASE_PORT=$base
ACTIVE_PORT=$next
EOF
  echo "==> 热更新完成，当前实例 :$next"
}

# 自动迁移旧式部署（有旧 unit、无模板单元、有 .env）
if [ "$COLD" -eq 0 ] && have_systemd && [ -f /etc/systemd/system/modernchat.service ] \
   && [ -f "$ROOT/server/.env" ] && ! systemctl list-unit-files 'modernchat@.service' 2>/dev/null | grep -q 'modernchat@'; then
  migrate_legacy_unit || true
fi

# 4a) 热更新（默认）：模板单元 + nginx upstream + 状态文件齐备
if [ "$COLD" -eq 0 ] && have_systemd && [ -f "$HOTSTATE" ] \
   && systemctl list-unit-files 'modernchat@.service' 2>/dev/null | grep -q 'modernchat@' \
   && [ -n "$(nginx_conf_for_hot)" ]; then
  if hot_reload; then
    echo "==> 完成：$VERSION ($AFTER)（热更新，未停机）"
    echo "    验证：curl http://127.0.0.1:$(awk -F= '/ACTIVE_PORT/{print $2}' "$HOTSTATE")/api/version"
    exit 0
  fi
  echo "==> 热更新未执行成功，回落普通重启 …"
fi

# 4b) 回落：显式重启命令
if [ -n "${RESTART_CMD:-}" ]; then
  echo "==> 重启: $RESTART_CMD"
  bash -c "$RESTART_CMD"
elif command -v docker >/dev/null 2>&1 && [ -f docker-compose.yml ]; then
  echo "==> 重启（docker compose，容器重建有秒级中断）"
  docker compose up -d --build
elif have_systemd && systemctl list-unit-files 'modernchat@.service' 2>/dev/null | grep -q 'modernchat@'; then
  port="$(awk -F= '/ACTIVE_PORT/{print $2}' "$HOTSTATE" 2>/dev/null)"; [ -n "$port" ] || port="$(sed -n 's/^PORT=//p' "$ROOT/server/.env" 2>/dev/null | head -n1)"; port="${port:-8080}"
  echo "==> 重启（systemctl restart modernchat@$port，有数秒中断）"
  systemctl restart "modernchat@$port.service"
elif have_systemd && systemctl list-unit-files 2>/dev/null | grep -q '^modernchat.service'; then
  echo "==> 重启（systemctl restart modernchat，有数秒中断；建议迁移热更新：重跑 scripts/install.sh 或看运维手册 §4）"
  sudo systemctl restart modernchat
else
  echo "!! 未找到重启方式：设置 RESTART_CMD 环境变量，或手动启动 bin/modernchat-server"
fi

echo "==> 完成：$VERSION ($AFTER)"
echo "    验证：curl http://127.0.0.1:8080/api/version"
