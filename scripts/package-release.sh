#!/usr/bin/env bash
# ============================================================================
# ModernChat 发行版打包脚本（在构建机上运行，Linux 或 WSL；需 Go + Node）
#
# 产出（dist-release/ 下，直接作为 Gitee / GitHub Release 附件上传）：
#   modernchat-<tag>-linux-amd64.tar.gz + .sha256
#   modernchat-<tag>-linux-arm64.tar.gz + .sha256
#
# 包内容（解压后直接覆盖安装目录，不含 server/.env / data / logs 等运行时文件）：
#   bin/modernchat-server        预编译后端（版本信息注入 tag）
#   web/dist/                    前端构建产物（含 .gz/.br 预压缩）
#   server/migrations/           版本化迁移 SQL（启动时 MIGRATE=true 自动执行）
#   deploy/nginx.conf.example    nginx 配置模板
#   scripts/update.sh            发行版在线更新脚本（命令行）
#
# 用法：
#   ./scripts/package-release.sh v0.10.3      # 用指定 tag（推荐：先打 tag 再打包）
#   ./scripts/package-release.sh              # 用 git describe（开发快照）
#
# 上传附件后，install.sh / 后台图形化更新即可从 Release 下载安装：
#   Gitee:  https://gitee.com/<owner>/<repo>/releases/download/<tag>/<asset>
#   GitHub: https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>
# ============================================================================
set -euo pipefail

TAG="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
TAG="${TAG#v}"                     # 资产名不带 v 前缀（v0.10.3 → 0.10.3）
VERSION_FULL="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"   # 注入二进制的完整版本串
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/dist-release"
MODULE="gitee.com/Sun-Si-Cheng/modern-chat-new/server"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

info() { printf "\033[32m==>\033[0m %s\n" "$*"; }
die()  { printf "\033[31m错误:\033[0m %s\n" "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "需要 Go（打包机构建后端）"
command -v node >/dev/null 2>&1 || die "需要 Node（打包机构建前端）"

cd "$ROOT"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
mkdir -p "$OUT"
STAGE="$(mktemp -d)"

# ---------------- 1) 前端（两个架构共用同一份 dist） ----------------
info "构建前端"
(cd web && npm ci --no-audit --no-fund && npm run build)

# ---------------- 2) 每个架构：构建后端 + 组装 + 打包 ----------------
for arch in amd64 arm64; do
  gobin="modernchat-server-linux-$arch"
  case "$arch" in
    amd64) goarch="amd64" ;;
    arm64) goarch="arm64" ;;
  esac

  info "构建后端 linux/$goarch"
  (cd server && CGO_ENABLED=0 GOOS=linux GOARCH=$goarch go build -trimpath -ldflags \
    "-s -w -X ${MODULE}/internal/version.Version=${VERSION_FULL} -X ${MODULE}/internal/version.Commit=${COMMIT} -X ${MODULE}/internal/version.BuildTime=${BUILD_TIME}" \
    -o "$STAGE/$gobin" ./cmd/server)

  # 组装包结构
  rm -rf "$STAGE/pkg"
  mkdir -p "$STAGE/pkg/bin" "$STAGE/pkg/web" "$STAGE/pkg/server" "$STAGE/pkg/scripts"
  cp "$STAGE/$gobin" "$STAGE/pkg/bin/modernchat-server"
  echo "$VERSION_FULL" > "$STAGE/pkg/bin/.release-version"
  cp -r web/dist "$STAGE/pkg/web/dist"
  cp -r server/migrations "$STAGE/pkg/server/migrations"
  mkdir -p "$STAGE/pkg/deploy"
  if [ -f deploy/nginx.conf.example ]; then
    cp deploy/nginx.conf.example "$STAGE/pkg/deploy/"
  fi
  cp scripts/update-release.sh "$STAGE/pkg/scripts/update.sh"

  asset="modernchat-$TAG-linux-$arch"
  info "打包 $asset.tar.gz"
  tar -czf "$OUT/$asset.tar.gz" -C "$STAGE/pkg" .
  (cd "$OUT" && sha256sum "$asset.tar.gz" > "$asset.tar.gz.sha256")
  info "完成：dist-release/$asset.tar.gz ($(du -h "$OUT/$asset.tar.gz" | cut -f1))"
done

rm -rf "$STAGE"
info "全部产物在 dist-release/："
ls -lh "$OUT" | grep "modernchat-$TAG" || true
info "上传到 Release 附件后，安装脚本与后台图形化更新即可使用。"
