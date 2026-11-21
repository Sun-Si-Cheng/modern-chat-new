#!/usr/bin/env bash
# ============================================================================
# ModernChat 发行版更新脚本（随发行包分发，命令行在线更新）
#
# 适用：scripts/install.sh 的发行版安装（从 GitHub Release 下载预编译产物）。
# 与后台图形化更新同源：下载新发行包 → sha256 校验 → 原子替换产物 → 优雅重启。
# 源码 git 部署请用仓库里的 scripts/update.sh（本地编译，支持蓝绿热更新）。
#
# 用法（在安装目录，root）：
#   bash scripts/update.sh            # 更新到最新 Release
#   TAG=v0.11.0 bash scripts/update.sh  # 更新到指定版本
#   bash scripts/update.sh --check    # 只检查不更新
#
# 可选环境变量：
#   GITHUB_TOKEN=ghp_xxx     # GitHub API 令牌：匿名 api.github.com 限流 60 次/小时/IP，
#                            # 配置后提升到 5000 次/小时（也可写进 server/.env）
# ============================================================================
set -euo pipefail

C_G="\033[32m"; C_Y="\033[33m"; C_R="\033[31m"; C_0="\033[0m"
info() { printf "${C_G}==>${C_0} %s\n" "$*"; }
warn() { printf "${C_Y}警告:${C_0} %s\n" "$*"; }
die()  { printf "${C_R}错误:${C_0} %s\n" "$*" >&2; exit 1; }

GITHUB_TOKEN="${GITHUB_TOKEN:-}"
# curl 附加认证：仅 GitHub 官方域携带令牌（防匿名 API 限流；私有仓库附件下载也需要），
# .env 里配置的第三方镜像地址不携带，避免令牌外泄。令牌来源：环境变量或 server/.env。
gh_curl() {
  local url="${!#}" # 最后一个参数是目标 URL
  case "$url" in
    https://github.com/* | https://api.github.com/*) ;;
    *) curl "$@"; return $? ;;
  esac
  if [ -n "$GITHUB_TOKEN" ]; then
    curl -H "Authorization: Bearer $GITHUB_TOKEN" "$@"
    return $?
  fi
  curl "$@"
}

# 安装目录：本脚本位于 <安装目录>/scripts/ 下
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT/server/.env"
[ -f "$ENV_FILE" ] || die "未找到 $ENV_FILE —— 请在安装目录内运行（或手动设置 RELEASE_API_URL/RELEASE_DL_URL）"

# 从 .env 读配置（install.sh 写入）；显式地址缺失时按 RELEASE_REPO_GITHUB（或默认
# 官方仓库）派生，与后台图形化更新同源——旧实例无需重跑 install.sh。
get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | head -n1; }
API_URL="$(get_env RELEASE_API_URL)"
DL_BASE="$(get_env RELEASE_DL_URL)"
PORT="$(get_env PORT)"
if [ -z "$API_URL" ] || [ -z "$DL_BASE" ]; then
  RELEASE_REPO="${RELEASE_REPO_GITHUB:-$(get_env RELEASE_REPO_GITHUB)}"
  RELEASE_REPO="${RELEASE_REPO:-modern-day-team/modern-chat-new}"
  info ".env 未配置 RELEASE_API_URL / RELEASE_DL_URL，按托管仓库派生更新源：$RELEASE_REPO"
  API_URL="https://api.github.com/repos/${RELEASE_REPO}/releases/latest"
  DL_BASE="https://github.com/${RELEASE_REPO}/releases/download"
fi
# 令牌也可写进 server/.env（600 权限），环境变量优先
GITHUB_TOKEN="${GITHUB_TOKEN:-$(get_env GITHUB_TOKEN)}"

CHECK_ONLY=0
for arg in "$@"; do
  case "$arg" in --check) CHECK_ONLY=1 ;; *) TAG="$arg" ;; esac
done

# 当前版本：打包时写入 bin/.release-version
CURRENT="$(cat "$ROOT/bin/.release-version" 2>/dev/null || echo unknown)"
info "当前版本：$CURRENT"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  GO_ARCH="amd64" ;;
  aarch64) GO_ARCH="arm64" ;;
  *) die "不支持的架构：$ARCH" ;;
esac

info "检查最新版本：$API_URL"
RESP="$(gh_curl -fsS --max-time 30 "$API_URL")" || die "查询 Release 失败（检查网络或 URL；403 多为 GitHub API 限流，可配置 GITHUB_TOKEN）"
LATEST_TAG="$(printf '%s' "$RESP" | grep -o '"tag_name"[^,}]*' | sed 's/.*: *"//;s/"$//')"
[ -n "$LATEST_TAG" ] || die "未解析到最新 tag"
info "最新版本：$LATEST_TAG"

if [ "$CHECK_ONLY" -eq 1 ]; then
  echo "  本地：$(cat "$ROOT/bin/.release-version" 2>/dev/null || echo unknown)"
  exit 0
fi

ASSET="modernchat-${LATEST_TAG#v}-linux-$GO_ARCH.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

info "下载 $ASSET …"
gh_curl -fL --retry 3 -o "$TMP/$ASSET" "$DL_BASE/${LATEST_TAG}/$ASSET" || die "下载失败：$DL_BASE/${LATEST_TAG}/$ASSET"

info "校验 sha256 …"
if gh_curl -fsSL --max-time 30 -o "$TMP/$ASSET.sha256" "$DL_BASE/${LATEST_TAG}/$ASSET.sha256" 2>/dev/null; then
  EXPECTED="$(awk '{print $1}' "$TMP/$ASSET.sha256")"
  ACTUAL="$(sha256sum "$TMP/$ASSET" | awk '{print $1}')"
  # 附件可能被平台重定向到 HTML 页（反盗链/登录页）；64 位十六进制才算有效清单
  if ! printf '%s' "$EXPECTED" | grep -Eqi '^[0-9a-f]{64}$'; then
    warn "sha256 附件内容异常（返回了 HTML？），跳过校验；本包哈希：$ACTUAL"
  elif [ "$EXPECTED" = "$ACTUAL" ]; then
    info "校验通过"
  else
    die "sha256 不匹配：期望 $EXPECTED，实际 $ACTUAL"
  fi
else
  warn "无 sha256 附件，跳过校验（建议发布时附带 .sha256）"
fi

info "解压 …"
EXTRACT="$TMP/extract"; mkdir -p "$EXTRACT"
tar -xzf "$TMP/$ASSET" -C "$EXTRACT"
# 发行包可能在 Windows 上打包（NTFS 无执行位），解压后强制补齐可执行权限
chmod 755 "$EXTRACT/bin/modernchat-server" "$EXTRACT/scripts/update.sh" 2>/dev/null || true

# 原子替换产物（保留 server/.env、data/、logs/ —— 它们不在包里，天然不被覆盖）
info "替换后端二进制（老进程继续用老 inode，直到重启）"
if [ -f "$ROOT/bin/modernchat-server" ]; then
  mv -f "$ROOT/bin/modernchat-server" "$ROOT/bin/modernchat-server.old"
fi
mv "$EXTRACT/bin/modernchat-server" "$ROOT/bin/modernchat-server"
rm -f "$ROOT/bin/modernchat-server.old"

info "切换前端产物"
if [ -d "$ROOT/web/dist.new" ]; then rm -rf "$ROOT/web/dist.new"; fi
if [ -d "$EXTRACT/web/dist" ]; then
  rm -rf "$ROOT/web/dist.old"
  if [ -d "$ROOT/web/dist" ]; then mv "$ROOT/web/dist" "$ROOT/web/dist.old"; fi
  mv "$EXTRACT/web/dist" "$ROOT/web/dist"
  rm -rf "$ROOT/web/dist.old"
fi

# 迁移 SQL / 部署模板 / 更新脚本：直接覆盖
for rel in server/migrations deploy scripts/update.sh; do
  if [ -e "$EXTRACT/$rel" ]; then
    rm -rf "$ROOT/$rel"
    cp -r "$EXTRACT/$rel" "$ROOT/$rel"
  fi
done
echo "$LATEST_TAG" > "$ROOT/bin/.release-version"

# 重启：systemd 模板单元（install.sh 部署）；SIGTERM 优雅停机，客户端自动重连
info "重启服务 modernchat@$PORT（优雅停机）"
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  systemctl restart "modernchat@$PORT.service"
else
  warn "无 systemd：请手动重启后端进程"
fi

for i in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1 && { info "健康检查通过"; break; }
  [ "$i" = "30" ] && die "重启后 30s 未就绪，请查 journalctl -u modernchat@$PORT"
  sleep 1
done
info "更新完成：$LATEST_TAG"
echo "    验证：curl http://127.0.0.1:$PORT/api/version"
