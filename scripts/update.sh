#!/usr/bin/env bash
# ModernChat 一键升级：从 Gitee 拉取最新代码 → 重建前后端 → 重启服务
#
# 用法（在仓库根目录）：
#   ./scripts/update.sh                 # 拉 master 并重启
#   ./scripts/update.sh dev             # 拉指定分支
#   ./scripts/update.sh --no-restart    # 只构建产物，稍后手动重启
#   RESTART_CMD="sudo systemctl restart modernchat" ./scripts/update.sh
#
# 设计取舍：升级由运维脚本执行，应用内不提供"触发 git pull"的接口——
# 那等于给管理员账号挂上远程代码执行，账号一旦泄露就等于丢服务器。
set -euo pipefail

BRANCH="${BRANCH:-master}"
NO_RESTART=0
for arg in "$@"; do
  case "$arg" in
    --no-restart) NO_RESTART=1 ;;
    *) BRANCH="$arg" ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODULE="gitee.com/Sun-Si-Cheng/modern-chat-new/server"
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

# 2) 后端（版本信息注入，便于 /api/version 核对升级是否生效）
echo "==> 构建后端 $VERSION ($AFTER)"
mkdir -p bin
(cd server && go build -trimpath -ldflags \
  "-s -w -X ${MODULE}/internal/version.Version=${VERSION} -X ${MODULE}/internal/version.Commit=${AFTER} -X ${MODULE}/internal/version.BuildTime=${BUILD_TIME}" \
  -o ../bin/modernchat-server ./cmd/server)

# 3) 前端
echo "==> 构建前端"
(cd web && npm ci --no-audit --no-fund && npm run build)

if [ "$NO_RESTART" -eq 1 ]; then
  echo "==> 跳过重启（产物已更新：bin/modernchat-server、web/dist）"
  exit 0
fi

# 4) 重启：优先 RESTART_CMD，其次 docker compose，再次 systemd
if [ -n "${RESTART_CMD:-}" ]; then
  echo "==> 重启: $RESTART_CMD"
  bash -c "$RESTART_CMD"
elif command -v docker >/dev/null 2>&1 && [ -f docker-compose.yml ]; then
  echo "==> 重启（docker compose）"
  docker compose up -d --build
elif command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^modernchat'; then
  echo "==> 重启（systemctl）"
  sudo systemctl restart modernchat
else
  echo "!! 未找到重启方式：设置 RESTART_CMD 环境变量，或手动启动 bin/modernchat-server"
fi

echo "==> 完成：$VERSION ($AFTER)"
echo "    验证：curl http://127.0.0.1:8080/api/version"
