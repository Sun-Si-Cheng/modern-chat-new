#!/usr/bin/env sh
# ============================================================
# 校验产品版本号一致性
# 基准：CHANGELOG.md 中最新正式版本（跳过「未发布」）
# 校验：web/ 与 website/ 的 package.json、package-lock.json，
#       website 的版本常量（src/links.ts）；并提示最新 git tag 是否一致
# 用法：sh scripts/check-version.sh
# 退出码：0 = 一致；1 = 存在不一致或无法解析基准
# ============================================================
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

EXPECTED="$(sed -n 's/^## \[\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)\].*/\1/p' CHANGELOG.md | head -n 1)"
if [ -z "$EXPECTED" ]; then
  echo "错误：无法从 CHANGELOG.md 解析最新正式版本号" >&2
  exit 1
fi

status=0

field_version() { # $1=文件：取首个 "version" 字段（package/lock 顶层版本）
  sed -n 's/.*"version": "\([0-9][0-9.]*\)".*/\1/p' "$1" | head -n 1
}

check() { # $1=显示名 $2=实际值
  if [ "$2" = "$EXPECTED" ]; then
    printf 'OK   %s = %s\n' "$1" "$2"
  else
    printf 'FAIL %s = %s（期望 %s）\n' "$1" "$2" "$EXPECTED" >&2
    status=1
  fi
}

echo "基准版本（CHANGELOG.md 最新正式条目）：$EXPECTED"
check "web/package.json"          "$(field_version web/package.json)"
check "web/package-lock.json"     "$(field_version web/package-lock.json)"
check "website/package.json"      "$(field_version website/package.json)"
check "website/package-lock.json" "$(field_version website/package-lock.json)"
check "website/src/links.ts"      "$(sed -n "s/.*export const VERSION = '\([0-9][0-9.]*\)'.*/\1/p" website/src/links.ts | head -n 1)"

# git tag 仅提示不判失败：HEAD 可领先于最新 tag（后端版本由 git describe 注入，属正常）
if command -v git >/dev/null 2>&1; then
  LATEST_TAG="$(git tag --list 'v[0-9]*' --sort=-v:refname 2>/dev/null | head -n 1)"
  if [ -n "$LATEST_TAG" ] && [ "$LATEST_TAG" != "v$EXPECTED" ]; then
    printf 'WARN 最新 git tag 为 %s，与 CHANGELOG %s 不一致（发版前请核对）\n' "$LATEST_TAG" "$EXPECTED" >&2
  fi
fi

if [ "$status" -ne 0 ]; then
  echo "校验失败：请把上述版本号对齐到 $EXPECTED" >&2
  exit 1
fi
echo "校验通过：所有产品版本号一致"
